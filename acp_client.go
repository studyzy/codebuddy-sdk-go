// acp_client.go
// ACPClient 高层客户端 - 封装 ACP 协议的完整会话生命周期。
//
// ACP（Agent Communication Protocol）是 CodeBuddy 的 HTTP 协议，通过双通道
//（SSE GET + POST JSON-RPC 2.0）与远程 CodeBuddy 实例通信。
//
// 典型用法（一次性任务）：
//
//	result, err := codebuddy.NewACPClient("http://host/acp", "token").RunTask(ctx, "写一段 hello world")
//
// 复用连接（多轮对话）：
//
//	client := codebuddy.NewACPClient("http://host/acp", "token")
//	if err := client.Connect(ctx); err != nil { ... }
//	defer client.Disconnect()
//	updates1, err := client.Prompt(ctx, "第一个问题", nil)
//	updates2, err := client.Prompt(ctx, "第二个问题", nil)

package codebuddy

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// defaultTaskTimeout 默认单次任务超时时间（600 秒）。
const defaultTaskTimeout = 600 * time.Second

// ACPClient 是 ACP HTTP 协议的高层客户端。
// 通过 SSE GET + POST JSON-RPC 2.0 双通道与 CodeBuddy ACP 服务端通信。
//
// 使用 NewACPClient 创建实例，调用 Connect 建立连接，
// 调用 Prompt 发送任务，调用 Disconnect 释放资源。
type ACPClient struct {
	// url ACP 端点地址，如 "http://localhost:8848/acp"
	url string
	// password Bearer Token 认证密钥
	password string
	// taskTimeout 单次 Prompt 的最大等待时间
	taskTimeout time.Duration
	// transport 底层 HTTP/SSE 传输层（nil 表示未连接）
	transport *httpACPTransport
	// sessionID 当前会话 ID，由 session/new 返回
	sessionID string
}

// NewACPClient 创建 ACPClient，使用默认超时（600 秒）。
// url 为 ACP 端点地址（如 "http://localhost:8848/acp"），password 为 Bearer Token。
func NewACPClient(url, password string) *ACPClient {
	return &ACPClient{
		url:         url,
		password:    password,
		taskTimeout: defaultTaskTimeout,
	}
}

// NewACPClientWithTimeout 创建带自定义超时的 ACPClient。
// taskTimeout 为单次 Prompt 的最大等待时间；传入 0 则使用默认值 600 秒。
func NewACPClientWithTimeout(url, password string, taskTimeout time.Duration) *ACPClient {
	if taskTimeout <= 0 {
		taskTimeout = defaultTaskTimeout
	}
	return &ACPClient{
		url:         url,
		password:    password,
		taskTimeout: taskTimeout,
	}
}

// Connect 建立 ACP 连接并完成握手。
//
// 握手流程：GET SSE → initialize → session/new → session/set_mode(bypassPermissions)。
// 成功后可调用 Prompt 发送任务。
func (c *ACPClient) Connect(ctx context.Context) error {
	t := newHttpACPTransport(c.url, c.password)
	if err := t.connect(ctx); err != nil {
		return fmt.Errorf("ACP 连接失败: %w", err)
	}

	// initialize 握手
	initID := t.nextID()
	_, err := t.postRPC(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientInfo": map[string]any{
			"name":    "codebuddy-sdk-go",
			"title":   "CodeBuddy SDK Go",
			"version": Version,
		},
		"clientCapabilities": map[string]any{},
	}, initID)
	if err != nil {
		t.close()
		return fmt.Errorf("ACP initialize 失败: %w", err)
	}

	// 创建会话
	newSessID := t.nextID()
	sessResult, err := t.postRPC(ctx, "session/new", map[string]any{
		"cwd":        "/workspace",
		"mcpServers": []any{},
	}, newSessID)
	if err != nil {
		t.close()
		return fmt.Errorf("ACP session/new 失败: %w", err)
	}

	sessionID, ok := sessResult["sessionId"].(string)
	if !ok || sessionID == "" {
		t.close()
		return &ACPError{Message: "session/new 响应中未找到有效的 sessionId"}
	}

	// 设置权限模式为 bypassPermissions，允许工具调用无需确认
	setModeID := t.nextID()
	_, err = t.postRPC(ctx, "session/set_mode", map[string]any{
		"sessionId": sessionID,
		"modeId":    "bypassPermissions",
	}, setModeID)
	if err != nil {
		t.close()
		return fmt.Errorf("ACP session/set_mode 失败: %w", err)
	}

	c.transport = t
	c.sessionID = sessionID
	return nil
}

// Disconnect 断开连接，释放所有资源。幂等，多次调用安全。
func (c *ACPClient) Disconnect() {
	if c.transport != nil {
		c.transport.close()
		c.transport = nil
		c.sessionID = ""
	}
}

// Prompt 发送提示并等待代理完整回复。
// onUpdate 为可选回调，每收到一个 session/update 事件时实时调用。
// 返回所有 session/update 事件的 update 字段列表。
func (c *ACPClient) Prompt(ctx context.Context, text string, onUpdate func(update map[string]any)) ([]map[string]any, error) {
	if c.transport == nil {
		return nil, &ACPError{Message: "未连接 ACP 服务，请先调用 Connect()"}
	}

	promptID := c.transport.nextID()
	err := c.transport.postRPCNoWait(ctx, "session/prompt", map[string]any{
		"sessionId": c.sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": text}},
	}, promptID)
	if err != nil {
		return nil, fmt.Errorf("ACP session/prompt 发送失败: %w", err)
	}

	return c.transport.waitForUpdates(ctx, c.sessionID, c.taskTimeout, onUpdate)
}

// RunTask 一次性连接、发送任务提示、断开连接，返回代理的文本响应。
//
// 等价于 Connect → Prompt → Disconnect 的便捷封装。
// 适合不需要复用连接的单次任务场景。
func (c *ACPClient) RunTask(ctx context.Context, promptText string) (string, error) {
	if err := c.Connect(ctx); err != nil {
		return "", err
	}
	defer c.Disconnect()

	updates, err := c.Prompt(ctx, promptText, nil)
	if err != nil {
		return "", err
	}
	return extractTextFromUpdates(updates), nil
}

// extractTextFromUpdates 从 session/update 事件列表中提取代理回复文本。
// 拼接所有 agent_message_chunk 类型且 content.type=="text" 的片段。
func extractTextFromUpdates(updates []map[string]any) string {
	var parts []string
	for _, u := range updates {
		if su, _ := u["sessionUpdate"].(string); su != "agent_message_chunk" {
			continue
		}
		content, _ := u["content"].(map[string]any)
		if content == nil {
			continue
		}
		if t, _ := content["type"].(string); t != "text" {
			continue
		}
		if text, _ := content["text"].(string); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "")
}
