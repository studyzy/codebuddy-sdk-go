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
	core connCore // 共享连接核心

	// Client 特有字段
	connected     bool
	streamingMode bool

	// 会话状态（从第一条 CLI 消息中捕获）
	sessionID    atomic.Value // string
	hasSentQuery atomic.Bool

	// 权限模式与模型的初始值和待同步标志（Client 特有的 fire-and-forget 逻辑）
	initialPermMode     PermissionMode
	initialModel        string
	permModePendingSync bool
	modelPendingSync    bool
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

	c := &Client{
		streamingMode:   true,
		initialPermMode: initialPerm,
		initialModel:    initialModel,
	}
	initConnCore(&c.core, opts, initialPerm, initialModel)
	return c
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
	optsCopy := *c.core.opts
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
	optsCopy := *c.core.opts
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
	c.core.mu.Lock()
	defer c.core.mu.Unlock()

	if c.connected {
		return &CLIConnectionError{Message: "已连接"}
	}

	var transport Transport
	if injectTransport != nil {
		transport = injectTransport
		if err := transport.Connect(ctx); err != nil {
			return err
		}
	} else {
		t := NewSubprocessTransport(c.core.opts, prompt)
		if err := t.Connect(ctx); err != nil {
			return err
		}
		transport = t
	}
	c.core.transport = transport

	// 重置通道和关闭信号（支持重复使用）
	c.core.messageChannel = make(chan Message, 100)
	c.core.closeCh = make(chan struct{})
	c.streamingMode = true
	if _, ok := prompt.(string); ok {
		c.streamingMode = false
	}

	// 构建 hooks 配置并获取注册表，然后发送 initialize
	hooksConfig, registry := BuildHooksConfig(c.core.opts.Hooks)
	c.core.hookRegistry = registry

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
	c.core.pendingMu.Lock()
	c.core.pendingResponses[initReqID] = initRespCh
	c.core.pendingMu.Unlock()

	b, err := json.Marshal(initRequest)
	if err != nil {
		c.core.pendingMu.Lock()
		delete(c.core.pendingResponses, initReqID)
		c.core.pendingMu.Unlock()
		transport.Close() //nolint:errcheck
		return err
	}
	if err := transport.Write(ctx, string(b)); err != nil {
		c.core.pendingMu.Lock()
		delete(c.core.pendingResponses, initReqID)
		c.core.pendingMu.Unlock()
		transport.Close() //nolint:errcheck
		return err
	}

	c.connected = true

	// 启动后台读取 goroutine
	c.core.wg.Add(1)
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
			c.core.stateMu.Lock()
			if c.core.currentModel == "" {
				c.core.currentModel = modelID
				c.initialModel = modelID
			}
			c.core.stateMu.Unlock()
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
	defer c.core.wg.Done()
	defer close(c.core.messageChannel)

	for {
		select {
		case <-c.core.closeCh:
			// 唤醒所有待处理的控制请求，避免泄漏
			c.core.drainPendingResponses()
			return
		case raw, ok := <-c.core.transport.ReadMessages():
			if !ok {
				// transport 已关闭
				c.core.drainPendingResponses()
				return
			}

			if raw.Err != nil {
				// 尝试把错误传给消费者
				select {
				case c.core.messageChannel <- &ErrorMessage{Error: raw.Err.Error()}:
				default:
				}
				c.core.drainPendingResponses()
				return
			}

			data := raw.Data
			msgType, _ := data["type"].(string)

			switch msgType {
			case "control_response":
				// 路由到等待中的 sendControlRequest 调用
				c.core.routeControlResponse(data)

			case "control_request":
				// 派发到独立 goroutine，不阻塞 backgroundReader
				go c.core.handleControlRequest(ctx, data)

			default:
				// 从第一条有 session_id 的消息捕获 sessionID
				if c.sessionID.Load() == nil || c.sessionID.Load() == "" {
					if sid, ok := data["session_id"].(string); ok && sid != "" {
						c.sessionID.Store(sid)
						// 同步待处理的权限模式/模型变更
						c.core.stateMu.Lock()
						needPerm := c.permModePendingSync
						needModel := c.modelPendingSync
						c.permModePendingSync = false
						c.modelPendingSync = false
						perm := c.core.currentPermMode
						model := c.core.currentModel
						c.core.stateMu.Unlock()

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
				case c.core.messageChannel <- msg:
				case <-c.core.closeCh:
					c.core.drainPendingResponses()
					return
				}
			}
		}
	}
}

// Send 向 CLI 发送新的用户消息（流式模式）。
// 适用于 Connect(ctx, nil) 之后的多轮对话。
func (c *Client) Send(ctx context.Context, prompt any) error {
	c.core.mu.Lock()
	transport := c.core.transport
	connected := c.connected
	c.core.mu.Unlock()

	if !connected || transport == nil {
		return &CLIConnectionError{Message: "未连接，请先调用 Connect"}
	}
	if !c.streamingMode {
		return &CLIConnectionError{Message: "非流式模式下不能调用 Send；Connect 已使用字符串 prompt 参数"}
	}

	c.hasSentQuery.Store(true)
	return sendPrompt(ctx, transport, prompt)
}

// ReceiveMessages 返回消息通道，调用方可 range 迭代所有消息。
// 通道在连接关闭或读取完毕后自动关闭。
func (c *Client) ReceiveMessages() <-chan Message {
	return c.core.messageChannel
}

// ReceiveResponse 从消息通道中读取，直到收到 ResultMessage 或 ErrorMessage 为止。
// 若 ResultMessage.IsError 为 true，则返回 ExecutionError。
func (c *Client) ReceiveResponse(ctx context.Context) (*ResultMessage, error) {
	return c.core.receiveResponse(ctx, "连接已关闭")
}

// Interrupt 发送中断信号，触发 CLI 停止当前执行。
// 这是 fire-and-forget 操作（不等待响应）。
func (c *Client) Interrupt(ctx context.Context) error {
	c.core.mu.Lock()
	connected := c.connected
	c.core.mu.Unlock()
	if !connected {
		return &CLIConnectionError{Message: "未连接"}
	}
	return c.core.interrupt(ctx, "未连接", nil)
}

// SetPermissionMode 更新权限模式。
//   - 若尚未发送任何查询，仅更新本地状态，待第一条 CLI 消息到来后同步
//   - 若已发送查询且有 session_id，立即 fire-and-forget 同步到 CLI
func (c *Client) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	c.core.stateMu.Lock()
	c.core.currentPermMode = mode
	c.core.stateMu.Unlock()

	sidRaw := c.sessionID.Load()
	if !c.hasSentQuery.Load() || sidRaw == nil {
		// 标记待同步
		c.core.stateMu.Lock()
		c.permModePendingSync = true
		c.core.stateMu.Unlock()
		return nil
	}

	sid, _ := sidRaw.(string)
	if sid == "" {
		c.core.stateMu.Lock()
		c.permModePendingSync = true
		c.core.stateMu.Unlock()
		return nil
	}

	go c.fireAndForgetPermMode(ctx, string(mode), sid)
	return nil
}

// GetPermissionMode 返回当前权限模式（本地状态）。
func (c *Client) GetPermissionMode() PermissionMode {
	return c.core.getPermissionMode()
}

// SetModel 更新 AI 模型。
//   - 若尚未发送任何查询，仅更新本地状态，待第一条 CLI 消息到来后同步
//   - 若已发送查询且有 session_id，立即 fire-and-forget 同步到 CLI
func (c *Client) SetModel(ctx context.Context, model string) error {
	c.core.stateMu.Lock()
	c.core.currentModel = model
	c.core.stateMu.Unlock()

	sidRaw := c.sessionID.Load()
	if !c.hasSentQuery.Load() || sidRaw == nil {
		c.core.stateMu.Lock()
		c.modelPendingSync = true
		c.core.stateMu.Unlock()
		return nil
	}

	sid, _ := sidRaw.(string)
	if sid == "" {
		c.core.stateMu.Lock()
		c.modelPendingSync = true
		c.core.stateMu.Unlock()
		return nil
	}

	go c.fireAndForgetModel(ctx, model, sid)
	return nil
}

// GetModel 返回当前模型名称（本地状态）。
func (c *Client) GetModel() string {
	return c.core.getModel()
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
	return c.core.mcpServerStatus(ctx, "未连接")
}

// SetHooks 替换 Hook 回调注册表，不向 CLI 发送任何控制请求。
func (c *Client) SetHooks(hooks map[HookEvent][]HookMatcher) {
	c.core.setHooks(hooks)
}

// SetCanUseTool 动态替换工具权限回调。
func (c *Client) SetCanUseTool(handler CanUseToolFunc) {
	c.core.setCanUseTool(handler)
}

// GetCanUseTool 返回当前生效的权限回调。
func (c *Client) GetCanUseTool() CanUseToolFunc {
	return c.core.getCanUseTool()
}

// GetAvailableModes 从 CLI 获取可用权限模式列表。
func (c *Client) GetAvailableModes(ctx context.Context) ([]AvailableMode, error) {
	sid := c.GetSessionID()
	return c.core.getAvailableModes(ctx, sid, "未连接")
}

// GetAvailableModels 从 CLI 获取可用模型列表（简化格式）。
func (c *Client) GetAvailableModels(ctx context.Context) ([]AvailableModel, error) {
	return c.core.getAvailableModels(ctx, "未连接")
}

// GetAvailableModelsRaw 从 CLI 获取完整模型配置。
func (c *Client) GetAvailableModelsRaw(ctx context.Context) ([]map[string]any, error) {
	return c.core.getAvailableModelsRaw(ctx, "未连接")
}

// GetAvailableCommands 订阅 commands 通道并返回当前可用命令列表（一次性获取）。
func (c *Client) GetAvailableCommands(ctx context.Context) ([]AvailableCommand, error) {
	c.core.mu.Lock()
	connected := c.connected
	c.core.mu.Unlock()
	if !connected {
		return nil, &CLIConnectionError{Message: "未连接"}
	}
	sid := c.GetSessionID()
	return c.core.getAvailableCommands(ctx, sid, "未连接", nil)
}

// SubscribeToCommands 持久订阅 commands 通道。
func (c *Client) SubscribeToCommands(ctx context.Context, handler NotificationHandler) error {
	c.core.mu.Lock()
	connected := c.connected
	c.core.mu.Unlock()
	if !connected {
		return &CLIConnectionError{Message: "未连接"}
	}
	sid := c.GetSessionID()
	return c.core.subscribeToCommands(ctx, handler, sid, "未连接", nil)
}

// UnsubscribeFromCommands 取消注册 commands 通道的指定 handler。
func (c *Client) UnsubscribeFromCommands(handler NotificationHandler) {
	c.core.unsubscribeFromCommands(handler)
}

// Disconnect 断开连接，停止后台 goroutine 并释放资源。
func (c *Client) Disconnect(ctx context.Context) error {
	c.core.mu.Lock()
	if !c.connected {
		c.core.mu.Unlock()
		return nil
	}
	c.connected = false
	transport := c.core.transport
	c.core.transport = nil

	// 发送关闭信号
	select {
	case <-c.core.closeCh:
		// 已关闭
	default:
		close(c.core.closeCh)
	}
	c.core.mu.Unlock()

	// 等待 backgroundReader 退出
	c.core.wg.Wait()

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
	c.core.mu.Lock()
	transport := c.core.transport
	c.core.mu.Unlock()
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
	c.core.mu.Lock()
	transport := c.core.transport
	c.core.mu.Unlock()
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
