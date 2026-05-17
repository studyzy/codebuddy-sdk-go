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
	core connCore // 共享连接核心

	id string // 会话 ID，构造时立即确定

	sessionOpts  *SessionOptions
	resumeID     string // 非空时表示 resume 模式
	lockedResume bool   // 是否已获取 resume 锁

	// 连接状态（Session 特有的 sync.Once 模式）
	initialized bool
	initOnce    sync.Once
	initErr     error

	// 标志
	hasSentMessage  atomic.Bool
	historyConsumed atomic.Bool // resume 会话历史是否已通过 Stream() 消费
	closed          atomic.Bool
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
		id:          id,
		sessionOpts: sessionOpts,
		resumeID:    resumeID,
	}
	initConnCore(&s.core, opts, permMode, model)
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
		return &CLIConnectionError{Message: "会话已关闭"}
	}
	if err := s.ensureConnected(ctx); err != nil {
		return err
	}
	s.hasSentMessage.Store(true)
	return sendPrompt(ctx, s.core.transport, prompt)
}

// Stream 返回消息通道，调用方可 range 迭代。
// 通道在 ResultMessage/ErrorMessage 到来或 Session 关闭后自动关闭。
//
// 每次 Send 后调用一次 Stream 接收本轮响应。
func (s *Session) Stream() <-chan Message {
	return s.core.messageChannel
}

// ReceiveResponse 从消息通道读取直到收到 ResultMessage 或 ErrorMessage。
// 是 Stream 的便捷封装，适合不需要逐条处理中间消息的场景。
func (s *Session) ReceiveResponse(ctx context.Context) (*ResultMessage, error) {
	return s.core.receiveResponse(ctx, "会话已关闭")
}

// Interrupt 发送中断信号，触发 CLI 停止当前执行。
func (s *Session) Interrupt(ctx context.Context) error {
	return s.core.interrupt(ctx, "未连接", map[string]any{
		"session_id": s.id,
	})
}

// SetPermissionMode 更新权限模式。
//   - 连接前：修改将传递给 CLI 的启动参数
//   - 连接后：发送 set_permission_mode 控制请求
func (s *Session) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	s.core.stateMu.Lock()
	s.core.currentPermMode = mode
	s.core.stateMu.Unlock()

	s.core.mu.Lock()
	transport := s.core.transport
	ready := s.initialized
	s.core.mu.Unlock()

	if !ready || transport == nil {
		// 连接前：更新 transport 选项（下次 Connect 时生效）
		s.core.opts.PermissionMode = mode.Ptr()
		return nil
	}

	// 连接后：发送控制请求
	_, err := s.core.sendControlRequest(ctx, map[string]any{
		"subtype":    "set_permission_mode",
		"session_id": s.id,
		"mode":       string(mode),
	}, "未连接")
	return err
}

// GetPermissionMode 返回当前权限模式（本地状态）。
func (s *Session) GetPermissionMode() PermissionMode {
	return s.core.getPermissionMode()
}

// SetModel 更新 AI 模型。
//   - 连接前：修改将传递给 CLI 的启动参数
//   - 连接后：发送 set_model 控制请求
func (s *Session) SetModel(ctx context.Context, model string) error {
	s.core.stateMu.Lock()
	s.core.currentModel = model
	s.core.stateMu.Unlock()

	s.core.mu.Lock()
	transport := s.core.transport
	ready := s.initialized
	s.core.mu.Unlock()

	if !ready || transport == nil {
		// 连接前：更新选项（下次 Connect 时生效）
		s.core.opts.Model = &model
		return nil
	}

	// 连接后：发送控制请求
	_, err := s.core.sendControlRequest(ctx, map[string]any{
		"subtype":    "set_model",
		"session_id": s.id,
		"model":      model,
	}, "未连接")
	return err
}

// GetModel 返回当前模型名称（本地状态）。
func (s *Session) GetModel() string {
	return s.core.getModel()
}

// MCPServerStatus 查询 MCP 服务器状态列表。
func (s *Session) MCPServerStatus(ctx context.Context) ([]MCPServerStatus, error) {
	return s.core.mcpServerStatus(ctx, "未连接")
}

// SetHooks 替换 Hook 回调注册表，不向 CLI 发送任何控制请求。
// Hook 事件结构（事件/匹配器/ID）在 initialize() 时已确定，此方法仅替换回调函数。
func (s *Session) SetHooks(hooks map[HookEvent][]HookMatcher) {
	s.core.setHooks(hooks)
}

// SetCanUseTool 动态替换工具权限回调。
// 新回调在下一次 can_use_tool 请求时生效，覆盖 opts.CanUseTool。
func (s *Session) SetCanUseTool(handler CanUseToolFunc) {
	s.core.setCanUseTool(handler)
}

// GetCanUseTool 返回当前生效的权限回调。
// 若已通过 SetCanUseTool 设置了覆盖回调则返回它，否则返回 opts.CanUseTool。
func (s *Session) GetCanUseTool() CanUseToolFunc {
	return s.core.getCanUseTool()
}

// HasPendingHistory 判断 resume 会话是否有待消费的历史消息。
// 仅当：会话通过 ResumeSession 创建 && 尚未调用 Send && 历史尚未通过 Stream 消费 时返回 true。
func (s *Session) HasPendingHistory() bool {
	return s.resumeID != "" && !s.hasSentMessage.Load() && !s.historyConsumed.Load()
}

// GetAvailableModes 从 CLI 获取可用权限模式列表。
// 需要 CLI 支持 get_available_modes 控制请求。
func (s *Session) GetAvailableModes(ctx context.Context) ([]AvailableMode, error) {
	return s.core.getAvailableModes(ctx, s.id, "未连接")
}

// GetAvailableModels 从 CLI 获取可用模型列表（简化格式）。
// 需要 CLI 支持 get_available_models 控制请求。
func (s *Session) GetAvailableModels(ctx context.Context) ([]AvailableModel, error) {
	return s.core.getAvailableModels(ctx, "未连接")
}

// GetAvailableModelsRaw 从 CLI 获取完整模型配置（包含 capabilities、token limits 等）。
// 需要 CLI 支持 get_available_models 控制请求。
func (s *Session) GetAvailableModelsRaw(ctx context.Context) ([]map[string]any, error) {
	return s.core.getAvailableModelsRaw(ctx, "未连接")
}

// GetAvailableCommands 订阅 commands 通道并返回当前可用命令列表（一次性获取）。
// 内部订阅后等待第一条 commands 通知，超时 10 秒。
// 需要 CLI 支持 commands 通道订阅。
func (s *Session) GetAvailableCommands(ctx context.Context) ([]AvailableCommand, error) {
	return s.core.getAvailableCommands(ctx, "", "未连接", s.ensureConnected)
}

// SubscribeToCommands 持久订阅 commands 通道，每次有命令更新时调用 handler。
// 与 GetAvailableCommands 不同，此方法持续监听，不会在第一次通知后移除 handler。
// 需要 CLI 支持 commands 通道订阅。
func (s *Session) SubscribeToCommands(ctx context.Context, handler NotificationHandler) error {
	return s.core.subscribeToCommands(ctx, handler, "", "未连接", s.ensureConnected)
}

// UnsubscribeFromCommands 取消注册 commands 通道的指定 handler。
// 仅移除本地 handler，不向 CLI 发送退订请求。
func (s *Session) UnsubscribeFromCommands(handler NotificationHandler) {
	s.core.unsubscribeFromCommands(handler)
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

	s.core.mu.Lock()
	transport := s.core.transport
	s.core.transport = nil

	select {
	case <-s.core.closeCh:
	default:
		close(s.core.closeCh)
	}
	s.core.mu.Unlock()

	s.core.wg.Wait()

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
	s.core.opts.SessionID = &s.id
	if s.resumeID != "" {
		s.core.opts.Resume = &s.resumeID
	}

	t := NewSubprocessTransport(s.core.opts, nil)
	if err := t.Connect(ctx); err != nil {
		return err
	}

	s.core.mu.Lock()
	s.core.transport = t
	s.core.messageChannel = make(chan Message, 100)
	s.core.closeCh = make(chan struct{})
	s.core.mu.Unlock()

	// 构建 hooks 配置
	hooksConfig, registry := BuildHooksConfig(s.core.opts.Hooks)
	s.core.hookRegistry = registry

	// 构建 initialize 请求
	sdkMCPServers := t.SDKMCPServerNames()
	var sdkMCPServersVal any
	if len(sdkMCPServers) > 0 {
		sdkMCPServersVal = sdkMCPServers
	}

	var systemPrompt, appendSystemPrompt any
	if s.core.opts.SystemPrompt != nil {
		if s.core.opts.SystemPrompt.Override != nil {
			systemPrompt = *s.core.opts.SystemPrompt.Override
		} else if s.core.opts.SystemPrompt.Append != nil {
			appendSystemPrompt = *s.core.opts.SystemPrompt.Append
		}
	}

	var agentsConfig any
	if len(s.core.opts.Agents) > 0 {
		m := make(map[string]any)
		for name, ag := range s.core.opts.Agents {
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
	s.core.pendingMu.Lock()
	s.core.pendingResponses[initReqID] = initRespCh
	s.core.pendingMu.Unlock()

	if err := writeControlResponse(ctx, t, initRequest); err != nil {
		s.core.pendingMu.Lock()
		delete(s.core.pendingResponses, initReqID)
		s.core.pendingMu.Unlock()
		t.Close()
		return err
	}

	// 启动后台读取 goroutine
	s.core.wg.Add(1)
	go s.backgroundReader(ctx)

	// 等待 initialize 响应（带超时）
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case resp, ok := <-initRespCh:
		if ok && resp.err == nil && resp.response != nil {
			if modelID, ok := resp.response["currentModelId"].(string); ok && modelID != "" {
				s.core.stateMu.Lock()
				if s.core.currentModel == "" {
					s.core.currentModel = modelID
				}
				s.core.stateMu.Unlock()
			}
		}
	case <-initCtx.Done():
		// 超时忽略，继续
	}

	s.core.mu.Lock()
	s.initialized = true
	s.core.mu.Unlock()

	return nil
}

// backgroundReader 在独立 goroutine 中持续读取 transport 消息。
func (s *Session) backgroundReader(ctx context.Context) {
	defer s.core.wg.Done()
	defer close(s.core.messageChannel)

	for {
		select {
		case <-s.core.closeCh:
			s.core.drainPendingResponses()
			return
		case raw, ok := <-s.core.transport.ReadMessages():
			if !ok {
				s.core.drainPendingResponses()
				return
			}
			if raw.Err != nil {
				select {
				case s.core.messageChannel <- &ErrorMessage{Error: raw.Err.Error()}:
				default:
				}
				s.core.drainPendingResponses()
				return
			}

			data := raw.Data
			msgType, _ := data["type"].(string)

			switch msgType {
			case "control_response":
				s.core.routeControlResponse(data)
			case "control_request":
				go s.core.handleControlRequest(ctx, data)
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
				case s.core.messageChannel <- msg:
				case <-s.core.closeCh:
					s.core.drainPendingResponses()
					return
				}
			}
		}
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

// generateSessionID 使用 crypto/rand 生成 UUID v4 格式的随机会话 ID。
func generateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	// UUID v4 格式: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
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
