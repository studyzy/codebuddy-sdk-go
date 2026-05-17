// transport.go
// 传输层接口定义 - 抽象与 CLI 进程的通信方式，支持自定义实现

package codebuddy

import (
	"context"
	"encoding/json"
)

// RawMessage 包含从 CLI 读取的原始 JSON 数据或读取错误。
// Raw 保留 CLI 输出的原始 JSON 字节，供上层按原格式透传。
type RawMessage struct {
	Data map[string]any
	Raw  json.RawMessage
	Err  error
}

// Transport 是与 CLI 进程通信的抽象接口。
// 默认实现为 SubprocessTransport，可替换为自定义实现用于测试。
type Transport interface {
	// Connect 建立与 CLI 的连接（启动子进程或建立网络连接）
	Connect(ctx context.Context) error
	// ReadMessages 返回原始消息的只读通道，连接关闭时通道自动关闭
	ReadMessages() <-chan RawMessage
	// Write 向 CLI 写入一行 JSON 数据
	Write(ctx context.Context, data string) error
	// Close 关闭连接并释放所有资源
	Close() error
	// IsClosed 返回连接是否已关闭
	IsClosed() bool
	// IsReady 返回 CLI 是否已就绪（已产生首行有效输出）
	IsReady() bool
	// SDKMCPServerNames 返回已注册的 SDK MCP 服务器名称列表
	SDKMCPServerNames() []string
	// HandleMCPMessageRequest 处理 CLI 发来的 mcp_message 控制请求
	HandleMCPMessageRequest(ctx context.Context, requestID string, request map[string]any)
	// OnNotification 注册指定 channel 的通知处理器
	// 每次从 CLI 收到该 channel 的 control_notification 消息时调用 handler
	OnNotification(channel SubscriptionChannel, handler NotificationHandler)
	// OffNotification 移除指定 channel 的通知处理器
	OffNotification(channel SubscriptionChannel, handler NotificationHandler)
	// SendControlRequestNoWait 发送控制请求，不等待响应（fire-and-forget）
	// 适用于不关心响应结果的场景（如 interrupt、set_permission_mode 静默同步等）
	SendControlRequestNoWait(ctx context.Context, payload map[string]any) error
}

// 编译时接口合规性断言
var _ Transport = (*SubprocessTransport)(nil)
