// session.go
// Session - 对话会话的核心对象。
//
// Session 是多轮对话的一等公民，拥有稳定的 ID，管理连接和状态机。
// Client 仅作为工厂和配置容器，通过 NewSession() 创建 Session。
//
// 设计原则（参考 NodeJS SDK）：
//   - SessionID 在构造时立即确定，不依赖 CLI 消息回流
//   - Session 管理自身连接生命周期
//   - SetModel/SetPermissionMode：连接前修改 CLI 参数，连接后发送控制请求
//   - Resume：通过 ResumeSessionID 恢复历史会话，加锁防止并发 resume

package codebuddy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// SessionOptions 创建 Session 时的附加选项，用于覆盖 Client 级别的全局配置。
type SessionOptions struct {
	// SessionID 指定会话 ID（可选）；不指定则自动生成 UUID。
	SessionID string
	// PermissionMode 覆盖 Client 默认权限模式。
	PermissionMode *PermissionMode
	// Model 覆盖 Client 默认模型。
	Model *string
	// MaxTurns 最大对话轮次。
	MaxTurns *int
}

// Session 代表一次有状态的对话会话。
//
// Session 拥有稳定的 ID，支持多轮 Send/Stream，以及 Interrupt、
// SetPermissionMode、SetModel 等操作。
//
// 典型用法：
//
//	client := codebuddy.NewClient(nil)
//	session := client.NewSession(nil)
//	defer session.Close()
//
//	if err := session.Connect(ctx); err != nil { ... }
//	if err := session.Send(ctx, "hello"); err != nil { ... }
//	for msg := range session.Stream() {
//	    // 处理消息
//	}
type Session struct {
	id string // 会话 ID，构造时立即确定

	opts         *Options
	sessionOpts  *SessionOptions
	resumeID     string // 非空时表示 resume 模式
	lockedResume bool   // 是否已获取 resume 锁

	// 连接状态
	mu          sync.Mutex
	transport   Transport
	initialized bool
	initOnce    sync.Once
	initErr     error

	// 消息通道：background reader → 消费者
	messageChannel chan Message
	closeCh        chan struct{}
	wg             sync.WaitGroup

	// 控制请求等待
	pendingMu        sync.Mutex
	pendingResponses map[string]chan controlResponseResult

	// Hook 回调注册表
	hookRegistry HookCallbackRegistry

	// 状态
	stateMu         sync.Mutex
	currentPermMode PermissionMode
	currentModel    string

	// 可动态替换的权限回调（覆盖 opts.CanUseTool）
	canUseToolMu        sync.Mutex
	overriddenCanUseTool CanUseToolFunc

	// 标志
	hasSentMessage  atomic.Bool
	historyConsumed atomic.Bool // resume 会话历史是否已通过 Stream() 消费
	closed          atomic.Bool
	reqCounter      atomic.Int64
}

// newSession 内部构造函数，由 Client 和 ResumeSession 调用。
func newSession(opts *Options, sessionOpts *SessionOptions, resumeID string) *Session {
	if opts == nil {
		opts = &Options{}
	}
	if sessionOpts == nil {
		sessionOpts = &SessionOptions{}
	}

	// 确定 SessionID：resumeID > sessionOpts.SessionID > opts.SessionID > UUID
	id := resumeID
	if id == "" {
		id = sessionOpts.SessionID
	}
	if id == "" && opts.SessionID != nil {
		id = *opts.SessionID
	}
	if id == "" {
		id = generateSessionID()
	}

	// 确定权限模式：sessionOpts > opts
	permMode := PermissionModeDefault
	if opts.PermissionMode != nil {
		permMode = *opts.PermissionMode
	}
	if sessionOpts.PermissionMode != nil {
		permMode = *sessionOpts.PermissionMode
	}

	// 确定模型：sessionOpts > opts
	model := ""
	if opts.Model != nil {
		model = *opts.Model
	}
	if sessionOpts.Model != nil {
		model = *sessionOpts.Model
	}

	s := &Session{
		id:               id,
		opts:             opts,
		sessionOpts:      sessionOpts,
		resumeID:         resumeID,
		messageChannel:   make(chan Message, 100),
		closeCh:          make(chan struct{}),
		pendingResponses:  make(map[string]chan controlResponseResult),
		hookRegistry:     make(HookCallbackRegistry),
		currentPermMode:  permMode,
		currentModel:     model,
	}
	return s
}

// ID 返回会话 ID。ID 在构造时立即确定，始终非空。
func (s *Session) ID() string { return s.id }

// Connect 显式建立连接并完成初始化（发送 initialize 控制请求）。
//
// Send/Stream 会自动调用 Connect，也可以手动调用以提前建立连接（预热）。
// 在 Connect 前调用 SetPermissionMode/SetModel 会修改 CLI 启动参数；
// Connect 后再调用则会发送控制请求。
func (s *Session) Connect(ctx context.Context) error {
	return s.ensureConnected(ctx)
}

// Send 向 CLI 发送用户消息。
// 若尚未连接，自动调用 Connect。
func (s *Session) Send(ctx context.Context, prompt any) error {
	if s.closed.Load() {
		return &CLIConnectionError{Message: "session is closed"}
	}
	if err := s.ensureConnected(ctx); err != nil {
		return err
	}
	s.hasSentMessage.Store(true)
	return sendPrompt(ctx, s.transport, prompt)
}

// Stream 返回消息通道，调用方可 range 迭代。
// 通道在 ResultMessage/ErrorMessage 到来或 Session 关闭后自动关闭。
//
// 每次 Send 后调用一次 Stream 接收本轮响应。
func (s *Session) Stream() <-chan Message {
	return s.messageChannel
}

// ReceiveResponse 从消息通道读取直到收到 ResultMessage 或 ErrorMessage。
// 是 Stream 的便捷封装，适合不需要逐条处理中间消息的场景。
func (s *Session) ReceiveResponse(ctx context.Context) (*ResultMessage, error) {
	for {
		select {
		case msg, ok := <-s.messageChannel:
			if !ok {
				return nil, &CLIConnectionError{Message: "session closed"}
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
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Interrupt 发送中断信号，触发 CLI 停止当前执行。
func (s *Session) Interrupt(ctx context.Context) error {
	s.mu.Lock()
	transport := s.transport
	s.mu.Unlock()
	if transport == nil {
		return &CLIConnectionError{Message: "not connected"}
	}
	request := map[string]any{
		"type":       "control_request",
		"request_id": fmt.Sprintf("interrupt_%d", time.Now().UnixNano()),
		"request": map[string]any{
			"subtype":    "interrupt",
			"session_id": s.id,
		},
	}
	return writeControlResponse(ctx, transport, request)
}

// SetPermissionMode 更新权限模式。
//   - 连接前：修改将传递给 CLI 的启动参数
//   - 连接后：发送 set_permission_mode 控制请求
func (s *Session) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	s.stateMu.Lock()
	s.currentPermMode = mode
	s.stateMu.Unlock()

	s.mu.Lock()
	transport := s.transport
	ready := s.initialized
	s.mu.Unlock()

	if !ready || transport == nil {
		// 连接前：更新 transport 选项（下次 Connect 时生效）
		s.opts.PermissionMode = mode.Ptr()
		return nil
	}

	// 连接后：发送控制请求
	_, err := s.sendControlRequest(ctx, map[string]any{
		"subtype":    "set_permission_mode",
		"session_id": s.id,
		"mode":       string(mode),
	})
	return err
}

// GetPermissionMode 返回当前权限模式（本地状态）。
func (s *Session) GetPermissionMode() PermissionMode {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.currentPermMode
}

// SetModel 更新 AI 模型。
//   - 连接前：修改将传递给 CLI 的启动参数
//   - 连接后：发送 set_model 控制请求
func (s *Session) SetModel(ctx context.Context, model string) error {
	s.stateMu.Lock()
	s.currentModel = model
	s.stateMu.Unlock()

	s.mu.Lock()
	transport := s.transport
	ready := s.initialized
	s.mu.Unlock()

	if !ready || transport == nil {
		// 连接前：更新选项（下次 Connect 时生效）
		s.opts.Model = &model
		return nil
	}

	// 连接后：发送控制请求
	_, err := s.sendControlRequest(ctx, map[string]any{
		"subtype":    "set_model",
		"session_id": s.id,
		"model":      model,
	})
	return err
}

// GetModel 返回当前模型名称（本地状态）。
func (s *Session) GetModel() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.currentModel
}

// MCPServerStatus 查询 MCP 服务器状态列表。
func (s *Session) MCPServerStatus(ctx context.Context) ([]MCPServerStatus, error) {
	resp, err := s.sendControlRequest(ctx, map[string]any{"subtype": "mcp_status"})
	if err != nil {
		return nil, err
	}
	servers, _ := resp["mcp_servers"].([]any)
	result := make([]MCPServerStatus, 0, len(servers))
	for _, sv := range servers {
		m, ok := sv.(map[string]any)
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
// Hook 事件结构（事件/匹配器/ID）在 initialize() 时已确定，此方法仅替换回调函数。
func (s *Session) SetHooks(hooks map[HookEvent][]HookMatcher) {
	_, registry := BuildHooksConfig(hooks)
	s.hookRegistry = registry
}

// SetCanUseTool 动态替换工具权限回调。
// 新回调在下一次 can_use_tool 请求时生效，覆盖 opts.CanUseTool。
func (s *Session) SetCanUseTool(handler CanUseToolFunc) {
	s.canUseToolMu.Lock()
	s.overriddenCanUseTool = handler
	s.canUseToolMu.Unlock()
}

// GetCanUseTool 返回当前生效的权限回调。
// 若已通过 SetCanUseTool 设置了覆盖回调则返回它，否则返回 opts.CanUseTool。
func (s *Session) GetCanUseTool() CanUseToolFunc {
	s.canUseToolMu.Lock()
	override := s.overriddenCanUseTool
	s.canUseToolMu.Unlock()
	if override != nil {
		return override
	}
	return s.opts.CanUseTool
}

// HasPendingHistory 判断 resume 会话是否有待消费的历史消息。
// 仅当：会话通过 ResumeSession 创建 && 尚未调用 Send && 历史尚未通过 Stream 消费 时返回 true。
func (s *Session) HasPendingHistory() bool {
	return s.resumeID != "" && !s.hasSentMessage.Load() && !s.historyConsumed.Load()
}

// GetAvailableModes 从 CLI 获取可用权限模式列表。
// 需要 CLI 支持 get_available_modes 控制请求。
func (s *Session) GetAvailableModes(ctx context.Context) ([]AvailableMode, error) {
	resp, err := s.sendControlRequest(ctx, map[string]any{
		"subtype":    "get_available_modes",
		"session_id": s.id,
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
// 需要 CLI 支持 get_available_models 控制请求。
func (s *Session) GetAvailableModels(ctx context.Context) ([]AvailableModel, error) {
	resp, err := s.sendControlRequest(ctx, map[string]any{"subtype": "get_available_models"})
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

// GetAvailableModelsRaw 从 CLI 获取完整模型配置（包含 capabilities、token limits 等）。
// 需要 CLI 支持 get_available_models 控制请求。
func (s *Session) GetAvailableModelsRaw(ctx context.Context) ([]map[string]any, error) {
	resp, err := s.sendControlRequest(ctx, map[string]any{"subtype": "get_available_models"})
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
// 内部订阅后等待第一条 commands 通知，超时 10 秒。
// 需要 CLI 支持 commands 通道订阅。
func (s *Session) GetAvailableCommands(ctx context.Context) ([]AvailableCommand, error) {
	if err := s.ensureConnected(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	transport := s.transport
	s.mu.Unlock()
	if transport == nil {
		return nil, &CLIConnectionError{Message: "not connected"}
	}

	resultCh := make(chan []AvailableCommand, 1)
	errCh := make(chan error, 1)

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

	_, err := s.sendControlRequest(ctx, map[string]any{
		"subtype": "subscribe",
		"channel": string(SubscriptionChannelCommands),
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
	case err := <-errCh:
		return nil, err
	case <-timeoutCtx.Done():
		transport.OffNotification(SubscriptionChannelCommands, handler)
		return nil, fmt.Errorf("timeout waiting for commands notification")
	}
}

// SubscribeToCommands 持久订阅 commands 通道，每次有命令更新时调用 handler。
// 与 GetAvailableCommands 不同，此方法持续监听，不会在第一次通知后移除 handler。
// 需要 CLI 支持 commands 通道订阅。
func (s *Session) SubscribeToCommands(ctx context.Context, handler NotificationHandler) error {
	if err := s.ensureConnected(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	transport := s.transport
	s.mu.Unlock()
	if transport == nil {
		return &CLIConnectionError{Message: "not connected"}
	}

	transport.OnNotification(SubscriptionChannelCommands, handler)
	_, err := s.sendControlRequest(ctx, map[string]any{
		"subtype": "subscribe",
		"channel": string(SubscriptionChannelCommands),
	})
	if err != nil {
		transport.OffNotification(SubscriptionChannelCommands, handler)
		return err
	}
	return nil
}

// UnsubscribeFromCommands 取消注册 commands 通道的指定 handler。
// 仅移除本地 handler，不向 CLI 发送退订请求。
func (s *Session) UnsubscribeFromCommands(handler NotificationHandler) {
	s.mu.Lock()
	transport := s.transport
	s.mu.Unlock()
	if transport != nil {
		transport.OffNotification(SubscriptionChannelCommands, handler)
	}
}

// Close 关闭 Session，停止后台 goroutine，释放所有资源。
// 幂等，可安全多次调用。实现 io.Closer 接口。
func (s *Session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	// 释放 resume 锁
	if s.lockedResume && s.resumeID != "" {
		releaseSessionLock(s.resumeID)
		s.lockedResume = false
	}

	s.mu.Lock()
	transport := s.transport
	s.transport = nil

	select {
	case <-s.closeCh:
	default:
		close(s.closeCh)
	}
	s.mu.Unlock()

	s.wg.Wait()

	if transport != nil {
		return transport.Close()
	}
	return nil
}

// ---- 内部方法 ----

// ensureConnected 保证连接和初始化只执行一次。
func (s *Session) ensureConnected(ctx context.Context) error {
	s.initOnce.Do(func() {
		s.initErr = s.doConnect(ctx)
	})
	return s.initErr
}

// doConnect 建立传输连接并发送 initialize 控制请求。
func (s *Session) doConnect(ctx context.Context) error {
	// resume 模式：加锁防并发
	if s.resumeID != "" {
		if !acquireSessionLock(s.resumeID) {
			return fmt.Errorf("session %s is already in use", s.resumeID)
		}
		s.lockedResume = true
	}

	// 将会话 ID 写入 Options 以便 transport 构建 --session-id 参数
	s.opts.SessionID = &s.id
	if s.resumeID != "" {
		s.opts.Resume = &s.resumeID
	}

	t := NewSubprocessTransport(s.opts, nil)
	if err := t.Connect(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	s.transport = t
	s.messageChannel = make(chan Message, 100)
	s.closeCh = make(chan struct{})
	s.mu.Unlock()

	// 构建 hooks 配置
	hooksConfig, registry := BuildHooksConfig(s.opts.Hooks)
	s.hookRegistry = registry

	// 构建 initialize 请求
	sdkMCPServers := t.SDKMCPServerNames()
	var sdkMCPServersVal any
	if len(sdkMCPServers) > 0 {
		sdkMCPServersVal = sdkMCPServers
	}

	var systemPrompt, appendSystemPrompt any
	if s.opts.SystemPrompt != nil {
		if s.opts.SystemPrompt.Override != nil {
			systemPrompt = *s.opts.SystemPrompt.Override
		} else if s.opts.SystemPrompt.Append != nil {
			appendSystemPrompt = *s.opts.SystemPrompt.Append
		}
	}

	var agentsConfig any
	if len(s.opts.Agents) > 0 {
		m := make(map[string]any)
		for name, ag := range s.opts.Agents {
			m[name] = map[string]any{
				"description":     ag.Description,
				"prompt":          ag.Prompt,
				"tools":           ag.Tools,
				"disallowedTools": ag.DisallowedTools,
				"model":           ag.Model,
			}
		}
		agentsConfig = m
	}

	initReqID := fmt.Sprintf("init_%d", time.Now().UnixNano())
	initRequest := map[string]any{
		"type":       "control_request",
		"request_id": initReqID,
		"request": map[string]any{
			"subtype":            "initialize",
			"hooks":              hooksConfig,
			"systemPrompt":       systemPrompt,
			"appendSystemPrompt": appendSystemPrompt,
			"agents":             agentsConfig,
			"sdkMcpServers":      sdkMCPServersVal,
			"hasPrompt":          s.hasSentMessage.Load(),
			"capabilities": map[string]any{
				"askUserQuestion": true,
			},
		},
	}

	// 注册 pending channel 先于发送
	initRespCh := make(chan controlResponseResult, 1)
	s.pendingMu.Lock()
	s.pendingResponses[initReqID] = initRespCh
	s.pendingMu.Unlock()

	if err := writeControlResponse(ctx, t, initRequest); err != nil {
		s.pendingMu.Lock()
		delete(s.pendingResponses, initReqID)
		s.pendingMu.Unlock()
		t.Close()
		return err
	}

	// 启动后台读取 goroutine
	s.wg.Add(1)
	go s.backgroundReader(ctx)

	// 等待 initialize 响应（带超时）
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case resp, ok := <-initRespCh:
		if ok && resp.err == nil && resp.response != nil {
			if modelID, ok := resp.response["currentModelId"].(string); ok && modelID != "" {
				s.stateMu.Lock()
				if s.currentModel == "" {
					s.currentModel = modelID
				}
				s.stateMu.Unlock()
			}
		}
	case <-initCtx.Done():
		// 超时忽略，继续
	}

	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	return nil
}

// backgroundReader 在独立 goroutine 中持续读取 transport 消息。
func (s *Session) backgroundReader(ctx context.Context) {
	defer s.wg.Done()
	defer close(s.messageChannel)

	for {
		select {
		case <-s.closeCh:
			s.drainPendingResponses()
			return
		case raw, ok := <-s.transport.ReadMessages():
			if !ok {
				s.drainPendingResponses()
				return
			}
			if raw.Err != nil {
				select {
				case s.messageChannel <- &ErrorMessage{Error: raw.Err.Error()}:
				default:
				}
				s.drainPendingResponses()
				return
			}

			data := raw.Data
			msgType, _ := data["type"].(string)

			switch msgType {
			case "control_response":
				s.routeControlResponse(data)
			case "control_request":
				go s.handleControlRequest(ctx, data)
			default:
				msg := ParseMessage(data)
				if msg == nil {
					continue
				}
				// resume 会话：ResultMessage 到来时标记历史已消费
				if _, isResult := msg.(*ResultMessage); isResult {
					s.historyConsumed.Store(true)
				}
				select {
				case s.messageChannel <- msg:
				case <-s.closeCh:
					s.drainPendingResponses()
					return
				}
			}
		}
	}
}

// routeControlResponse 路由控制响应到等待中的 sendControlRequest 调用。
func (s *Session) routeControlResponse(data map[string]any) {
	resp, _ := data["response"].(map[string]any)
	if resp == nil {
		return
	}
	requestID, _ := resp["request_id"].(string)
	if requestID == "" {
		return
	}

	s.pendingMu.Lock()
	ch, ok := s.pendingResponses[requestID]
	if ok {
		delete(s.pendingResponses, requestID)
	}
	s.pendingMu.Unlock()

	if !ok {
		return
	}

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

// drainPendingResponses 关闭所有 pending 通道，避免 goroutine 泄漏。
func (s *Session) drainPendingResponses() {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	for id, ch := range s.pendingResponses {
		close(ch)
		delete(s.pendingResponses, id)
	}
}

// sendControlRequest 发送控制请求并等待响应。
func (s *Session) sendControlRequest(ctx context.Context, payload map[string]any) (map[string]any, error) {
	s.mu.Lock()
	transport := s.transport
	s.mu.Unlock()
	if transport == nil {
		return nil, &CLIConnectionError{Message: "not connected"}
	}

	requestID := fmt.Sprintf("req_%d_%d", time.Now().UnixNano(), s.reqCounter.Add(1))

	respCh := make(chan controlResponseResult, 1)
	s.pendingMu.Lock()
	s.pendingResponses[requestID] = respCh
	s.pendingMu.Unlock()

	request := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    payload,
	}
	if err := writeControlResponse(ctx, transport, request); err != nil {
		s.pendingMu.Lock()
		delete(s.pendingResponses, requestID)
		s.pendingMu.Unlock()
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
		s.pendingMu.Lock()
		delete(s.pendingResponses, requestID)
		s.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// handleControlRequest 处理来自 CLI 的控制请求（在独立 goroutine 中执行）。
func (s *Session) handleControlRequest(ctx context.Context, data map[string]any) {
	s.mu.Lock()
	transport := s.transport
	s.mu.Unlock()
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
		if r := recover(); r != nil {
			_ = writeControlResponse(ctx, transport, BuildControlErrorResponse(requestID, fmt.Sprintf("panic: %v", r)))
		}
	}()

	switch subtype {
	case "hook_callback":
		callbackID, _ := request["callback_id"].(string)
		hookInput, _ := request["input"].(map[string]any)
		toolUseID := getStringPtrFromMap(request, "tool_use_id")
		output := executeHook(ctx, callbackID, hookInput, toolUseID, s.hookRegistry)
		_ = writeControlResponse(ctx, transport, BuildControlResponse(requestID, output))

	case "can_use_tool":
		// 优先使用动态设置的 overriddenCanUseTool，否则回退到 opts.CanUseTool
		s.canUseToolMu.Lock()
		override := s.overriddenCanUseTool
		s.canUseToolMu.Unlock()
		if override != nil {
			optsCopy := *s.opts
			optsCopy.CanUseTool = override
			handlePermissionRequest(ctx, transport, requestID, request, &optsCopy)
		} else {
			handlePermissionRequest(ctx, transport, requestID, request, s.opts)
		}

	case "mcp_message":
		transport.HandleMCPMessageRequest(ctx, requestID, request)
	}
}

// ---- 辅助函数 ----

// parseAvailableCommands 将 CLI 推送的原始命令列表转为 []AvailableCommand。
func parseAvailableCommands(rawCmds []any) []AvailableCommand {
	result := make([]AvailableCommand, 0, len(rawCmds))
	for _, item := range rawCmds {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := getString(m, "name")
		// 去掉前导 '/'
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}
		cmd := AvailableCommand{
			Name:        name,
			Description: getString(m, "description"),
		}
		if hint := getString(m, "argumentHint"); hint != "" {
			cmd.Input = &CommandInput{Hint: hint}
		}
		result = append(result, cmd)
	}
	return result
}

// generateSessionID は crypto/rand を使って UUID v4 相当のランダム文字列を生成する。
func generateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	// UUID v4 フォーマット: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
