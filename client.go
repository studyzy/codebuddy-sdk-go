// client.go
// Client 结构体 - SDK 入口和工厂对象。
//
// Client 持有全局配置，通过 NewSession() 创建有状态的 Session，
// 通过 ResumeSession() 恢复已有会话。
// 兼容旧版 Connect/Send/ReceiveResponse 接口，新代码推荐使用 Session API。

package codebuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Client 是 SDK 入口和工厂对象，持有全局配置并提供 Session 工厂方法。
//
// 推荐用法（新代码）：
//
//	client := codebuddy.NewClient(opts)
//	session := client.NewSession(nil)
//	defer session.Close()
//	session.Send(ctx, "hello")
//	result, _ := session.ReceiveResponse(ctx)
//
// 兼容旧接口（仍可用）：
//
//	client := codebuddy.NewClient(nil)
//	if err := client.Connect(ctx, nil); err != nil { ... }
//	defer client.Close()
//	client.Send(ctx, "What is 1+1?")
//	result, _ := client.ReceiveResponse(ctx)
type Client struct {
	opts *Options

	// 传输层
	mu        sync.Mutex
	transport Transport
	connected bool

	// 消息通道：background reader → 消费者
	messageChannel chan Message
	streamingMode  bool

	// background reader 生命周期管理
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

	// 会话状态（从第一条 CLI 消息中捕获）
	sessionID    atomic.Value // string
	hasSentQuery atomic.Bool

	// 权限模式与模型（本地状态 + 同步标志）
	stateMu             sync.Mutex
	currentPermMode     PermissionMode
	initialPermMode     PermissionMode
	currentModel        string
	initialModel        string
	permModePendingSync bool
	modelPendingSync    bool

	// 请求 ID 计数器（原子递增）
	reqCounter atomic.Int64
}

type controlResponseResult struct {
	response map[string]any
	err      error
}

// NewClient 创建 Client 实例。opts 为 nil 时使用默认配置。
//
// Client 是配置容器和工厂对象，本身不建立任何网络连接。
// 使用 NewSession() 创建 Session 以进行多轮对话，
// 或使用 Query() 发起一次性查询。
func NewClient(opts *Options) *Client {
	if opts == nil {
		opts = &Options{}
	}

	initialPerm := PermissionModeDefault
	if opts.PermissionMode != nil {
		initialPerm = *opts.PermissionMode
	}

	initialModel := ""
	if opts.Model != nil {
		initialModel = *opts.Model
	}

	return &Client{
		opts:             opts,
		messageChannel:   make(chan Message, 100),
		streamingMode:    true,
		closeCh:          make(chan struct{}),
		pendingResponses: make(map[string]chan controlResponseResult),
		hookRegistry:     make(HookCallbackRegistry),
		currentPermMode:  initialPerm,
		initialPermMode:  initialPerm,
		currentModel:     initialModel,
		initialModel:     initialModel,
	}
}

// NewSession 创建一个新的 Session。
//
// Session 是多轮对话的核心对象，拥有稳定的 ID。
// sessionOpts 为 nil 时继承 Client 的全局配置。
//
// 示例：
//
//	client := codebuddy.NewClient(opts)
//	session := client.NewSession(nil)
//	defer session.Close()
//	if err := session.Send(ctx, "hello"); err != nil { ... }
//	result, _ := session.ReceiveResponse(ctx)
func (c *Client) NewSession(sessionOpts *SessionOptions) *Session {
	// 深拷贝 opts 避免 Session 修改影响 Client 共享状态
	optsCopy := *c.opts
	return newSession(&optsCopy, sessionOpts, "")
}

// ResumeSession 恢复已有会话。
//
// resumeSessionID 为要恢复的会话 ID。加锁保证同一会话不会被并发 resume。
// 使用完毕后必须调用 session.Close() 释放锁。
//
// 示例：
//
//	session := client.ResumeSession("my-session-id", nil)
//	defer session.Close()
//	// 先 Stream() 读取历史消息，再 Send() 继续对话
func (c *Client) ResumeSession(resumeSessionID string, sessionOpts *SessionOptions) *Session {
	optsCopy := *c.opts
	return newSession(&optsCopy, sessionOpts, resumeSessionID)
}

// Connect 建立与 CodeBuddy CLI 的连接并启动后台读取 goroutine。
//
// prompt 的语义与 Query 函数一致：
//   - nil：流式模式，之后用 Send 发送消息
//   - string：立即发送该 prompt（单次模式）
//   - <-chan map[string]any：流式输入，后台异步发送
func (c *Client) Connect(ctx context.Context, prompt any) error {
	return c.connectWithTransport(ctx, prompt, nil)
}

// connectWithTransport 是 Connect 的内部实现，接受可注入的 transport（用于测试）。
// 若 injectTransport 为 nil，则创建默认的 SubprocessTransport。
func (c *Client) connectWithTransport(ctx context.Context, prompt any, injectTransport Transport) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return &CLIConnectionError{Message: "already connected"}
	}

	var transport Transport
	if injectTransport != nil {
		transport = injectTransport
		if err := transport.Connect(ctx); err != nil {
			return err
		}
	} else {
		t := NewSubprocessTransport(c.opts, prompt)
		if err := t.Connect(ctx); err != nil {
			return err
		}
		transport = t
	}
	c.transport = transport

	// 重置通道和关闭信号（支持重复使用）
	c.messageChannel = make(chan Message, 100)
	c.closeCh = make(chan struct{})
	c.streamingMode = true
	if _, ok := prompt.(string); ok {
		c.streamingMode = false
	}

	// 构建 hooks 配置并获取注册表，然后发送 initialize
	hooksConfig, registry := BuildHooksConfig(c.opts.Hooks)
	c.hookRegistry = registry

	sdkMCPServers := transport.SDKMCPServerNames()
	var sdkMCPServersVal any
	if len(sdkMCPServers) > 0 {
		sdkMCPServersVal = sdkMCPServers
	}

	initReqID := fmt.Sprintf("init_%d", time.Now().UnixNano())
	initRequest := map[string]any{
		"type":       "control_request",
		"request_id": initReqID,
		"request": map[string]any{
			"subtype":       "initialize",
			"hooks":         hooksConfig,
			"sdkMcpServers": sdkMCPServersVal,
			"hasPrompt":     prompt != nil,
			"capabilities": map[string]any{
				"askUserQuestion": true,
			},
		},
	}

	// 注册 initialize 的 pending channel（需在发送前注册，避免竞争）
	initRespCh := make(chan controlResponseResult, 1)
	c.pendingMu.Lock()
	c.pendingResponses[initReqID] = initRespCh
	c.pendingMu.Unlock()

	b, err := json.Marshal(initRequest)
		if err != nil {
			c.pendingMu.Lock()
			delete(c.pendingResponses, initReqID)
			c.pendingMu.Unlock()
			transport.Close() //nolint:errcheck
			return err
		}
		if err := transport.Write(ctx, string(b)); err != nil {
		c.pendingMu.Lock()
		delete(c.pendingResponses, initReqID)
		c.pendingMu.Unlock()
		transport.Close() //nolint:errcheck
		return err
	}

	c.connected = true

	// 启动后台读取 goroutine
	c.wg.Add(1)
	go c.backgroundReader(ctx)

	// 等待 initialize 响应（带超时）
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var initResp map[string]any
	select {
	case resp, ok := <-initRespCh:
		if ok {
			if resp.err != nil {
				return resp.err
			}
			initResp = resp.response
		}
	case <-initCtx.Done():
		// 超时忽略，继续
	}

	// 从 initialize 响应更新 model
	if initResp != nil {
		if modelID, ok := initResp["currentModelId"].(string); ok && modelID != "" {
			c.stateMu.Lock()
			if c.currentModel == "" {
				c.currentModel = modelID
				c.initialModel = modelID
			}
			c.stateMu.Unlock()
		}
	}

	// 发送 prompt（如有）
	if prompt != nil {
		c.hasSentQuery.Store(true)
		if err := sendPrompt(ctx, transport, prompt); err != nil {
			return err
		}
	}

	return nil
}

// backgroundReader 在独立 goroutine 中持续读取传输层消息，
// 将控制响应路由到 pendingResponses，控制请求派发到独立 goroutine，
// 普通消息解析后发送到 messageChannel。
func (c *Client) backgroundReader(ctx context.Context) {
	defer c.wg.Done()
	defer close(c.messageChannel)

	for {
		select {
		case <-c.closeCh:
			// 唤醒所有待处理的控制请求，避免泄漏
			c.drainPendingResponses()
			return
		case raw, ok := <-c.transport.ReadMessages():
			if !ok {
				// transport 已关闭
				c.drainPendingResponses()
				return
			}

			if raw.Err != nil {
				// 尝试把错误传给消费者
				select {
				case c.messageChannel <- &ErrorMessage{Error: raw.Err.Error()}:
				default:
				}
				c.drainPendingResponses()
				return
			}

			data := raw.Data
			msgType, _ := data["type"].(string)

			switch msgType {
			case "control_response":
				// 路由到等待中的 sendControlRequest 调用
				c.routeControlResponse(data)

			case "control_request":
				// 派发到独立 goroutine，不阻塞 backgroundReader
				go c.handleControlRequest(ctx, data)

			default:
				// 从第一条有 session_id 的消息捕获 sessionID
				if c.sessionID.Load() == nil || c.sessionID.Load() == "" {
					if sid, ok := data["session_id"].(string); ok && sid != "" {
						c.sessionID.Store(sid)
						// 同步待处理的权限模式/模型变更
						c.stateMu.Lock()
						needPerm := c.permModePendingSync
						needModel := c.modelPendingSync
						c.permModePendingSync = false
						c.modelPendingSync = false
						perm := c.currentPermMode
						model := c.currentModel
						c.stateMu.Unlock()

						if needPerm {
							go c.fireAndForgetPermMode(ctx, string(perm), sid)
						}
						if needModel {
							go c.fireAndForgetModel(ctx, model, sid)
						}
					}
				}

				msg := ParseMessage(data)
				if msg == nil {
					continue
				}
				AttachRawJSON(msg, raw.Raw)
				select {
				case c.messageChannel <- msg:
				case <-c.closeCh:
					c.drainPendingResponses()
					return
				}
			}
		}
	}
}

// routeControlResponse 解析控制响应，通知对应的 sendControlRequest 等待者。
func (c *Client) routeControlResponse(data map[string]any) {
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
func (c *Client) drainPendingResponses() {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pendingResponses {
		close(ch)
		delete(c.pendingResponses, id)
	}
}

// sendControlRequest 发送控制请求并等待响应。
// 生成唯一 request_id，注册 pending channel，序列化发送，然后阻塞等待响应或 ctx 超时。
func (c *Client) sendControlRequest(ctx context.Context, payload map[string]any) (map[string]any, error) {
	c.mu.Lock()
	transport := c.transport
	connected := c.connected
	c.mu.Unlock()

	if !connected || transport == nil {
		return nil, &CLIConnectionError{Message: "not connected"}
	}

	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano()+c.reqCounter.Add(1))

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
		return nil, fmt.Errorf("marshal control request: %w", err)
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
			return nil, &CLIConnectionError{Message: fmt.Sprintf("control request '%v' failed: connection closed", payload["subtype"])}
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

// handleControlRequest 处理来自 CLI 的控制请求（在独立 goroutine 中执行）。
func (c *Client) handleControlRequest(ctx context.Context, data map[string]any) {
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

// Send 向 CLI 发送新的用户消息（流式模式）。
// 适用于 Connect(ctx, nil) 之后的多轮对话。
func (c *Client) Send(ctx context.Context, prompt any) error {
	c.mu.Lock()
	transport := c.transport
	connected := c.connected
	c.mu.Unlock()

	if !connected || transport == nil {
		return &CLIConnectionError{Message: "not connected, call Connect first"}
	}
	if !c.streamingMode {
		return &CLIConnectionError{Message: "cannot call Send in non-streaming mode; Connect was called with a string prompt"}
	}

	c.hasSentQuery.Store(true)
	return sendPrompt(ctx, transport, prompt)
}

// ReceiveMessages 返回消息通道，调用方可 range 迭代所有消息。
// 通道在连接关闭或读取完毕后自动关闭。
func (c *Client) ReceiveMessages() <-chan Message {
	return c.messageChannel
}

// ReceiveResponse 从消息通道中读取，直到收到 ResultMessage 或 ErrorMessage 为止。
// 若 ResultMessage.IsError 为 true，则返回 ExecutionError。
func (c *Client) ReceiveResponse(ctx context.Context) (*ResultMessage, error) {
	for {
		select {
		case msg, ok := <-c.messageChannel:
			if !ok {
				return nil, &CLIConnectionError{Message: "connection closed"}
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

// Interrupt 发送中断信号，触发 CLI 停止当前执行。
// 这是 fire-and-forget 操作（不等待响应）。
func (c *Client) Interrupt(ctx context.Context) error {
	c.mu.Lock()
	transport := c.transport
	connected := c.connected
	c.mu.Unlock()

	if !connected || transport == nil {
		return &CLIConnectionError{Message: "not connected"}
	}

	request := map[string]any{
		"type":       "control_request",
		"request_id": fmt.Sprintf("interrupt_%d", time.Now().UnixNano()),
		"request":    map[string]any{"subtype": "interrupt"},
	}
	b, _ := json.Marshal(request)
	return transport.Write(ctx, string(b))
}

// SetPermissionMode 更新权限模式。
//   - 若尚未发送任何查询，仅更新本地状态，待第一条 CLI 消息到来后同步
//   - 若已发送查询且有 session_id，立即 fire-and-forget 同步到 CLI
func (c *Client) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	c.stateMu.Lock()
	c.currentPermMode = mode
	c.stateMu.Unlock()

	sidRaw := c.sessionID.Load()
	if !c.hasSentQuery.Load() || sidRaw == nil {
		// 标记待同步
		c.stateMu.Lock()
		c.permModePendingSync = true
		c.stateMu.Unlock()
		return nil
	}

	sid, _ := sidRaw.(string)
	if sid == "" {
		c.stateMu.Lock()
		c.permModePendingSync = true
		c.stateMu.Unlock()
		return nil
	}

	go c.fireAndForgetPermMode(ctx, string(mode), sid)
	return nil
}

// GetPermissionMode 返回当前权限模式（本地状态）。
func (c *Client) GetPermissionMode() PermissionMode {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.currentPermMode
}

// SetModel 更新 AI 模型。
//   - 若尚未发送任何查询，仅更新本地状态，待第一条 CLI 消息到来后同步
//   - 若已发送查询且有 session_id，立即 fire-and-forget 同步到 CLI
func (c *Client) SetModel(ctx context.Context, model string) error {
	c.stateMu.Lock()
	c.currentModel = model
	c.stateMu.Unlock()

	sidRaw := c.sessionID.Load()
	if !c.hasSentQuery.Load() || sidRaw == nil {
		c.stateMu.Lock()
		c.modelPendingSync = true
		c.stateMu.Unlock()
		return nil
	}

	sid, _ := sidRaw.(string)
	if sid == "" {
		c.stateMu.Lock()
		c.modelPendingSync = true
		c.stateMu.Unlock()
		return nil
	}

	go c.fireAndForgetModel(ctx, model, sid)
	return nil
}

// GetModel 返回当前模型名称（本地状态）。
func (c *Client) GetModel() string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.currentModel
}

// GetSessionID 返回当前会话ID。
// 会话ID是在与CLI建立连接后从第一条包含session_id的消息中自动捕获的。
// 如果尚未捕获到会话ID，返回空字符串。
func (c *Client) GetSessionID() string {
	sid := c.sessionID.Load()
	if sid == nil {
		return ""
	}
	return sid.(string)
}

// MCPServerStatus 查询 MCP 服务器状态列表。
func (c *Client) MCPServerStatus(ctx context.Context) ([]MCPServerStatus, error) {
	resp, err := c.sendControlRequest(ctx, map[string]any{"subtype": "mcp_status"})
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

// SetHooks 替换 Hook 回调注册表，不向 CLI 发送任何控制请求。
func (c *Client) SetHooks(hooks map[HookEvent][]HookMatcher) {
	_, registry := BuildHooksConfig(hooks)
	c.hookRegistry = registry
}

// SetCanUseTool 动态替换工具权限回调。
func (c *Client) SetCanUseTool(handler CanUseToolFunc) {
	c.canUseToolMu.Lock()
	c.overriddenCanUseTool = handler
	c.canUseToolMu.Unlock()
}

// GetCanUseTool 返回当前生效的权限回调。
func (c *Client) GetCanUseTool() CanUseToolFunc {
	c.canUseToolMu.Lock()
	override := c.overriddenCanUseTool
	c.canUseToolMu.Unlock()
	if override != nil {
		return override
	}
	return c.opts.CanUseTool
}

// GetAvailableModes 从 CLI 获取可用权限模式列表。
func (c *Client) GetAvailableModes(ctx context.Context) ([]AvailableMode, error) {
	sid := c.GetSessionID()
	resp, err := c.sendControlRequest(ctx, map[string]any{
		"subtype":    "get_available_modes",
		"session_id": sid,
	})
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

// GetAvailableModels 从 CLI 获取可用模型列表（简化格式）。
func (c *Client) GetAvailableModels(ctx context.Context) ([]AvailableModel, error) {
	resp, err := c.sendControlRequest(ctx, map[string]any{"subtype": "get_available_models"})
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

// GetAvailableModelsRaw 从 CLI 获取完整模型配置。
func (c *Client) GetAvailableModelsRaw(ctx context.Context) ([]map[string]any, error) {
	resp, err := c.sendControlRequest(ctx, map[string]any{"subtype": "get_available_models"})
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

// GetAvailableCommands 订阅 commands 通道并返回当前可用命令列表（一次性获取）。
func (c *Client) GetAvailableCommands(ctx context.Context) ([]AvailableCommand, error) {
	c.mu.Lock()
	transport := c.transport
	connected := c.connected
	c.mu.Unlock()
	if !connected || transport == nil {
		return nil, &CLIConnectionError{Message: "not connected"}
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

	sid := c.GetSessionID()
	_, err := c.sendControlRequest(ctx, map[string]any{
		"subtype": "subscribe",
		"channel": string(SubscriptionChannelCommands),
		"session_id": sid,
	})
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
		return nil, fmt.Errorf("timeout waiting for commands notification")
	}
}

// SubscribeToCommands 持久订阅 commands 通道。
func (c *Client) SubscribeToCommands(ctx context.Context, handler NotificationHandler) error {
	c.mu.Lock()
	transport := c.transport
	connected := c.connected
	c.mu.Unlock()
	if !connected || transport == nil {
		return &CLIConnectionError{Message: "not connected"}
	}

	transport.OnNotification(SubscriptionChannelCommands, handler)
	sid := c.GetSessionID()
	_, err := c.sendControlRequest(ctx, map[string]any{
		"subtype":    "subscribe",
		"channel":    string(SubscriptionChannelCommands),
		"session_id": sid,
	})
	if err != nil {
		transport.OffNotification(SubscriptionChannelCommands, handler)
		return err
	}
	return nil
}

// UnsubscribeFromCommands 取消注册 commands 通道的指定 handler。
func (c *Client) UnsubscribeFromCommands(handler NotificationHandler) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport != nil {
		transport.OffNotification(SubscriptionChannelCommands, handler)
	}
}

// Disconnect 断开连接，停止后台 goroutine 并释放资源。
func (c *Client) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil
	}
	c.connected = false
	transport := c.transport
	c.transport = nil

	// 发送关闭信号
	select {
	case <-c.closeCh:
		// 已关闭
	default:
		close(c.closeCh)
	}
	c.mu.Unlock()

	// 等待 backgroundReader 退出
	c.wg.Wait()

	if transport != nil {
		return transport.Close()
	}
	return nil
}

// Close 是 Disconnect 的别名，实现 io.Closer 接口。
func (c *Client) Close() error {
	return c.Disconnect(context.Background())
}

// --- 内部辅助方法 ---

// fireAndForgetPermMode 以 fire-and-forget 方式向 CLI 同步权限模式。
func (c *Client) fireAndForgetPermMode(ctx context.Context, mode string, sessionID string) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport == nil {
		return
	}

	request := map[string]any{
		"type":       "control_request",
		"request_id": fmt.Sprintf("perm_%d", time.Now().UnixNano()),
		"request": map[string]any{
			"subtype":    "set_permission_mode",
			"session_id": sessionID,
			"mode":       mode,
		},
	}
	_ = writeControlResponse(ctx, transport, request)
}

// fireAndForgetModel 以 fire-and-forget 方式向 CLI 同步模型设置。
func (c *Client) fireAndForgetModel(ctx context.Context, model string, sessionID string) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport == nil {
		return
	}

	request := map[string]any{
		"type":       "control_request",
		"request_id": fmt.Sprintf("model_%d", time.Now().UnixNano()),
		"request": map[string]any{
			"subtype":    "set_model",
			"session_id": sessionID,
			"model":      model,
		},
	}
	_ = writeControlResponse(ctx, transport, request)
}
