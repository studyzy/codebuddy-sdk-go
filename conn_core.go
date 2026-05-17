// conn_core.go
// connCore 是 Client 和 Session 共享的连接核心逻辑。
//
// Client 和 Session 都通过嵌入 connCore 来复用控制请求路由、
// 待处理响应管理、Hook 回调、权限/模型查询等共享方法，
// 避免在两个结构体之间重复实现相同的逻辑。

package codebuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// controlResponseResult 封装控制请求的响应结果或错误。
type controlResponseResult struct {
	response map[string]any
	err      error
}

// connCore 封装 Client 和 Session 共享的连接管理逻辑。
// 不可导出，仅供内部嵌入使用。
type connCore struct {
	// opts 全局配置
	opts *Options

	// 传输层
	mu        sync.Mutex
	transport Transport

	// 消息通道：backgroundReader → 消费者
	messageChannel chan Message

	// backgroundReader 生命周期管理
	closeCh chan struct{}
	wg      sync.WaitGroup

	// 待处理的控制响应：request_id → chan controlResponseResult
	pendingMu        sync.Mutex
	pendingResponses map[string]chan controlResponseResult

	// Hook 回调注册表
	hookRegistry HookCallbackRegistry

	// 可动态替换的权限回调（覆盖 opts.CanUseTool）
	canUseToolMu         sync.Mutex
	overriddenCanUseTool CanUseToolFunc

	// 权限模式与模型（本地状态）
	stateMu         sync.Mutex
	currentPermMode PermissionMode
	currentModel    string

	// 请求 ID 计数器（原子递增）
	reqCounter atomic.Int64
}

// initConnCore 初始化 connCore 的各字段。
func initConnCore(c *connCore, opts *Options, permMode PermissionMode, model string) {
	c.opts = opts
	c.messageChannel = make(chan Message, 100)
	c.closeCh = make(chan struct{})
	c.pendingResponses = make(map[string]chan controlResponseResult)
	c.hookRegistry = make(HookCallbackRegistry)
	c.currentPermMode = permMode
	c.currentModel = model
}

// --- 控制响应路由 ---

// routeControlResponse 解析控制响应，通知对应的 sendControlRequest 等待者。
func (c *connCore) routeControlResponse(data map[string]any) {
	resp, _ := data["response"].(map[string]any)
	if resp == nil {
		return
	}
	requestID, _ := resp["request_id"].(string)
	if requestID == "" {
		return
	}

	c.pendingMu.Lock()
	ch, ok := c.pendingResponses[requestID]
	if ok {
		delete(c.pendingResponses, requestID)
	}
	c.pendingMu.Unlock()

	if !ok {
		return
	}

	// 判断是 success 还是 error
	subtype, _ := resp["subtype"].(string)
	result := controlResponseResult{}
	if subtype == "error" {
		errMsg, _ := resp["error"].(string)
		result.err = &ControlRequestError{Message: errMsg}
	} else {
		inner, _ := resp["response"].(map[string]any)
		if inner == nil {
			inner = make(map[string]any)
		}
		result.response = inner
	}
	select {
	case ch <- result:
	default:
	}
}

// drainPendingResponses 关闭所有待处理的控制请求通道，避免 goroutine 泄漏。
func (c *connCore) drainPendingResponses() {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pendingResponses {
		close(ch)
		delete(c.pendingResponses, id)
	}
}

// --- 控制请求发送 ---

// sendControlRequest 发送控制请求并等待响应。
// 生成唯一 request_id，注册 pending channel，序列化发送，然后阻塞等待响应或 ctx 超时。
// closedMsg 参数用于自定义连接关闭时的错误消息。
func (c *connCore) sendControlRequest(ctx context.Context, payload map[string]any, closedMsg string) (map[string]any, error) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()

	if transport == nil {
		return nil, &CLIConnectionError{Message: closedMsg}
	}

	requestID := fmt.Sprintf("req_%d_%d", time.Now().UnixNano(), c.reqCounter.Add(1))

	// 注册 pending channel 先于发送，避免竞争
	respCh := make(chan controlResponseResult, 1)
	c.pendingMu.Lock()
	c.pendingResponses[requestID] = respCh
	c.pendingMu.Unlock()

	request := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    payload,
	}
	b, err := json.Marshal(request)
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pendingResponses, requestID)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("序列化控制请求失败: %w", err)
	}
	if err := transport.Write(ctx, string(b)); err != nil {
		c.pendingMu.Lock()
		delete(c.pendingResponses, requestID)
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case resp, ok := <-respCh:
		if !ok {
			return nil, &CLIConnectionError{Message: fmt.Sprintf("控制请求 '%v' 失败: 连接已关闭", payload["subtype"])}
		}
		if resp.err != nil {
			if controlErr, ok := resp.err.(*ControlRequestError); ok {
				controlErr.Subtype = fmt.Sprint(payload["subtype"])
			}
			return nil, resp.err
		}
		return resp.response, nil
	case <-ctx.Done():
		// 超时：从 pending 中清除，避免泄漏
		c.pendingMu.Lock()
		delete(c.pendingResponses, requestID)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// --- 控制请求处理 ---

// handleControlRequest 处理来自 CLI 的控制请求（在独立 goroutine 中执行）。
func (c *connCore) handleControlRequest(ctx context.Context, data map[string]any) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport == nil {
		return
	}

	requestID, _ := data["request_id"].(string)
	request, _ := data["request"].(map[string]any)
	if request == nil {
		return
	}
	subtype, _ := request["subtype"].(string)

	defer func() {
		// 确保不会因 panic 让 goroutine 静默崩溃
		if r := recover(); r != nil {
			_ = writeControlResponse(ctx, transport, BuildControlErrorResponse(requestID, fmt.Sprintf("panic: %v", r)))
		}
	}()

	switch subtype {
	case "hook_callback":
		callbackID, _ := request["callback_id"].(string)
		hookInput, _ := request["input"].(map[string]any)
		toolUseID := getStringPtrFromMap(request, "tool_use_id")

		output := executeHook(ctx, callbackID, hookInput, toolUseID, c.hookRegistry)
		_ = writeControlResponse(ctx, transport, BuildControlResponse(requestID, output))

	case "can_use_tool":
		// 优先使用动态设置的 overriddenCanUseTool，否则回退到 opts.CanUseTool
		c.canUseToolMu.Lock()
		override := c.overriddenCanUseTool
		c.canUseToolMu.Unlock()
		if override != nil {
			optsCopy := *c.opts
			optsCopy.CanUseTool = override
			handlePermissionRequest(ctx, transport, requestID, request, &optsCopy)
		} else {
			handlePermissionRequest(ctx, transport, requestID, request, c.opts)
		}

	case "mcp_message":
		transport.HandleMCPMessageRequest(ctx, requestID, request)
	}
}

// --- 消息接收 ---

// receiveResponse 从消息通道中读取，直到收到 ResultMessage 或 ErrorMessage 为止。
// closedMsg 参数用于自定义通道关闭时的错误消息。
func (c *connCore) receiveResponse(ctx context.Context, closedMsg string) (*ResultMessage, error) {
	for {
		select {
		case msg, ok := <-c.messageChannel:
			if !ok {
				return nil, &CLIConnectionError{Message: closedMsg}
			}
			switch m := msg.(type) {
			case *ResultMessage:
				if m.IsError && len(m.Errors) > 0 {
					return nil, NewExecutionError(m.Errors, m.Subtype)
				}
				return m, nil
			case *ErrorMessage:
				return nil, &CLIConnectionError{Message: m.Error}
			}
			// 其他消息类型（AssistantMessage、SystemMessage 等）继续循环

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// --- 权限/模型/状态查询 ---

// getPermissionMode 返回当前权限模式（本地状态）。
func (c *connCore) getPermissionMode() PermissionMode {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.currentPermMode
}

// getModel 返回当前模型名称（本地状态）。
func (c *connCore) getModel() string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.currentModel
}

// setHooks 替换 Hook 回调注册表，不向 CLI 发送任何控制请求。
func (c *connCore) setHooks(hooks map[HookEvent][]HookMatcher) {
	_, registry := BuildHooksConfig(hooks)
	c.hookRegistry = registry
}

// setCanUseTool 动态替换工具权限回调。
func (c *connCore) setCanUseTool(handler CanUseToolFunc) {
	c.canUseToolMu.Lock()
	c.overriddenCanUseTool = handler
	c.canUseToolMu.Unlock()
}

// getCanUseTool 返回当前生效的权限回调。
func (c *connCore) getCanUseTool() CanUseToolFunc {
	c.canUseToolMu.Lock()
	override := c.overriddenCanUseTool
	c.canUseToolMu.Unlock()
	if override != nil {
		return override
	}
	return c.opts.CanUseTool
}

// --- MCP 状态查询 ---

// mcpServerStatus 查询 MCP 服务器状态列表。
func (c *connCore) mcpServerStatus(ctx context.Context, closedMsg string) ([]MCPServerStatus, error) {
	resp, err := c.sendControlRequest(ctx, map[string]any{"subtype": "mcp_status"}, closedMsg)
	if err != nil {
		return nil, err
	}

	servers, _ := resp["mcp_servers"].([]any)
	result := make([]MCPServerStatus, 0, len(servers))
	for _, s := range servers {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		var serverInfo map[string]any
		if si, ok := m["serverInfo"].(map[string]any); ok {
			serverInfo = si
		}
		result = append(result, MCPServerStatus{
			Name:       getString(m, "name"),
			Status:     getString(m, "status"),
			ServerInfo: serverInfo,
		})
	}
	return result, nil
}

// --- 模型查询 ---

// getAvailableModes 从 CLI 获取可用权限模式列表。
// sessionID 参数由调用者提供（Client 用 GetSessionID()，Session 用 s.id）。
func (c *connCore) getAvailableModes(ctx context.Context, sessionID string, closedMsg string) ([]AvailableMode, error) {
	resp, err := c.sendControlRequest(ctx, map[string]any{
		"subtype":    "get_available_modes",
		"session_id": sessionID,
	}, closedMsg)
	if err != nil {
		return nil, err
	}
	raw, _ := resp["availableModes"].([]any)
	result := make([]AvailableMode, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, AvailableMode{
			ID:          getString(m, "id"),
			Description: getString(m, "description"),
		})
	}
	return result, nil
}

// getAvailableModels 从 CLI 获取可用模型列表（简化格式）。
func (c *connCore) getAvailableModels(ctx context.Context, closedMsg string) ([]AvailableModel, error) {
	resp, err := c.sendControlRequest(ctx, map[string]any{"subtype": "get_available_models"}, closedMsg)
	if err != nil {
		return nil, err
	}
	raw, _ := resp["availableModels"].([]any)
	result := make([]AvailableModel, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, AvailableModel{
			ModelID: getString(m, "modelId"),
			Name:    getString(m, "displayName"),
		})
	}
	return result, nil
}

// getAvailableModelsRaw 从 CLI 获取完整模型配置。
func (c *connCore) getAvailableModelsRaw(ctx context.Context, closedMsg string) ([]map[string]any, error) {
	resp, err := c.sendControlRequest(ctx, map[string]any{"subtype": "get_available_models"}, closedMsg)
	if err != nil {
		return nil, err
	}
	raw, _ := resp["rawModels"].([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result, nil
}

// --- 命令订阅 ---

// getAvailableCommands 订阅 commands 通道并返回当前可用命令列表（一次性获取）。
// ensureReady 是可选的连接保证回调（Session 传入 ensureConnected，Client 传入 nil）。
// sessionID 参数由调用者提供。
func (c *connCore) getAvailableCommands(ctx context.Context, sessionID string, closedMsg string, ensureReady func(context.Context) error) ([]AvailableCommand, error) {
	if ensureReady != nil {
		if err := ensureReady(ctx); err != nil {
			return nil, err
		}
	}

	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport == nil {
		return nil, &CLIConnectionError{Message: closedMsg}
	}

	resultCh := make(chan []AvailableCommand, 1)

	var handler NotificationHandler
	handler = func(notification ControlNotificationMessage) {
		transport.OffNotification(SubscriptionChannelCommands, handler)
		data := notification.Data
		rawCmds, _ := data["commands"].([]any)
		cmds := parseAvailableCommands(rawCmds)
		select {
		case resultCh <- cmds:
		default:
		}
	}
	transport.OnNotification(SubscriptionChannelCommands, handler)

	payload := map[string]any{
		"subtype": "subscribe",
		"channel": string(SubscriptionChannelCommands),
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	_, err := c.sendControlRequest(ctx, payload, closedMsg)
	if err != nil {
		transport.OffNotification(SubscriptionChannelCommands, handler)
		return nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case cmds := <-resultCh:
		return cmds, nil
	case <-timeoutCtx.Done():
		transport.OffNotification(SubscriptionChannelCommands, handler)
		return nil, fmt.Errorf("等待命令通知超时")
	}
}

// subscribeToCommands 持久订阅 commands 通道。
// ensureReady 是可选的连接保证回调。
func (c *connCore) subscribeToCommands(ctx context.Context, handler NotificationHandler, sessionID string, closedMsg string, ensureReady func(context.Context) error) error {
	if ensureReady != nil {
		if err := ensureReady(ctx); err != nil {
			return err
		}
	}

	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport == nil {
		return &CLIConnectionError{Message: closedMsg}
	}

	transport.OnNotification(SubscriptionChannelCommands, handler)
	payload := map[string]any{
		"subtype": "subscribe",
		"channel": string(SubscriptionChannelCommands),
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	_, err := c.sendControlRequest(ctx, payload, closedMsg)
	if err != nil {
		transport.OffNotification(SubscriptionChannelCommands, handler)
		return err
	}
	return nil
}

// unsubscribeFromCommands 取消注册 commands 通道的指定 handler。
func (c *connCore) unsubscribeFromCommands(handler NotificationHandler) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport != nil {
		transport.OffNotification(SubscriptionChannelCommands, handler)
	}
}

// --- 动态配置更新 ---

// setConfig 通过 set_config 控制请求动态更新 CLI 配置。
// 仅在连接后可调用。返回已更新的配置项和失败的配置项。
func (c *connCore) setConfig(ctx context.Context, sessionID string, config map[string]any, closedMsg string) (*SetConfigResult, error) {
	payload := map[string]any{
		"subtype": "set_config",
		"config":  config,
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	resp, err := c.sendControlRequest(ctx, payload, closedMsg)
	if err != nil {
		return nil, err
	}
	result := &SetConfigResult{}
	if updated, ok := resp["updated"].(map[string]any); ok {
		result.Updated = updated
	}
	if errors, ok := resp["errors"].(map[string]any); ok {
		result.Errors = make(map[string]string, len(errors))
		for k, v := range errors {
			if s, ok := v.(string); ok {
				result.Errors[k] = s
			}
		}
	}
	return result, nil
}

// --- 中断信号 ---

// interrupt 发送中断信号，触发 CLI 停止当前执行。
// extraFields 允许调用者添加额外字段（如 Session 的 session_id）。
func (c *connCore) interrupt(ctx context.Context, closedMsg string, extraFields map[string]any) error {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()

	if transport == nil {
		return &CLIConnectionError{Message: closedMsg}
	}

	reqPayload := map[string]any{"subtype": "interrupt"}
	for k, v := range extraFields {
		reqPayload[k] = v
	}

	request := map[string]any{
		"type":       "control_request",
		"request_id": fmt.Sprintf("interrupt_%d", time.Now().UnixNano()),
		"request":    reqPayload,
	}
	b, _ := json.Marshal(request)
	return transport.Write(ctx, string(b))
}
