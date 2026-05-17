package codebuddy

import "encoding/json"

// =============================================================================
// 消息接口和类型
// =============================================================================

// Message 是所有 CLI 输出消息的公共接口。
type Message interface {
	messageType() string
}

// UserMessage 表示用户发送的消息，type="user"。
type UserMessage struct {
	// Content 是消息内容，可以是 string 或 []ContentBlock。
	Content any
	// UUID 是消息的唯一标识符。
	UUID *string
	// ParentToolUseID 是父工具调用 ID。
	ParentToolUseID *string
	// RawJSON 是保留的 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识。
func (m *UserMessage) messageType() string { return "user" }

// AssistantMessage 表示助手返回的消息，type="assistant"。
type AssistantMessage struct {
	// Content 是消息内容块列表。
	Content []ContentBlock
	// Model 是使用的模型名称。
	Model string
	// ParentToolUseID 是父工具调用 ID。
	ParentToolUseID *string
	// Error 是错误信息（如有）。
	Error *string
	// RawJSON 是保留的 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识。
func (m *AssistantMessage) messageType() string { return "assistant" }

// SystemMessage 表示系统消息，type="system"。
type SystemMessage struct {
	// Subtype 是系统消息子类型。
	Subtype string
	// Data 是系统消息附加数据。
	Data map[string]any
	// RawJSON 是保留的 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识。
func (m *SystemMessage) messageType() string { return "system" }

// ResultMessage 表示最终结果消息，type="result"。
type ResultMessage struct {
	// Subtype 是结果子类型。
	Subtype string
	// DurationMs 是总耗时（毫秒）。
	DurationMs int
	// DurationAPIMs 是 API 调用耗时（毫秒）。
	DurationAPIMs int
	// IsError 表示是否为错误结果。
	IsError bool
	// NumTurns 是对话轮次数。
	NumTurns int
	// SessionID 是会话 ID。
	SessionID string
	// StopReason 是停止原因。
	StopReason *string
	// TotalCostUSD 是总费用（美元）。
	TotalCostUSD *float64
	// Usage 是 Token 用量统计。
	Usage map[string]any
	// Result 是结果文本内容。
	Result *string
	// StructuredOutput 是结构化输出内容。
	StructuredOutput any
	// Errors 是错误列表。
	Errors []string
	// RawJSON 是保留的 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识。
func (m *ResultMessage) messageType() string { return "result" }

// StreamEvent 表示流式事件消息，type="stream_event"。
type StreamEvent struct {
	// UUID 是事件唯一标识符。
	UUID string
	// SessionID 是所属会话 ID。
	SessionID string
	// Event 是事件原始数据。
	Event map[string]any
	// ParentToolUseID 是父工具调用 ID。
	ParentToolUseID *string
	// RawJSON 是保留的 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识。
func (m *StreamEvent) messageType() string { return "stream_event" }

// ErrorMessage 表示错误消息，type="error"。
type ErrorMessage struct {
	// Error 是错误描述。
	Error string
	// SessionID 是所属会话 ID（如有）。
	SessionID *string
	// Errors 是更完整的错误列表。
	Errors []string
	// Subtype 表示错误来源的 result subtype（如有）。
	Subtype *string
	// RawJSON 是保留的 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识。
func (m *ErrorMessage) messageType() string { return "error" }

// AttachRawJSON 将 CLI 原始 JSON 绑定到已解析的消息对象。
func AttachRawJSON(msg Message, raw json.RawMessage) {
	if len(raw) == 0 || msg == nil {
		return
	}
	copied := append(json.RawMessage(nil), raw...)
	switch m := msg.(type) {
	case *UserMessage:
		m.RawJSON = copied
	case *AssistantMessage:
		m.RawJSON = copied
	case *SystemMessage:
		m.RawJSON = copied
	case *ResultMessage:
		m.RawJSON = copied
	case *StreamEvent:
		m.RawJSON = copied
	case *ErrorMessage:
		m.RawJSON = copied
	}
}

// RawJSONOf 返回消息携带的原始 JSON；若无则返回 nil。
func RawJSONOf(msg Message) json.RawMessage {
	switch m := msg.(type) {
	case *UserMessage:
		return m.RawJSON
	case *AssistantMessage:
		return m.RawJSON
	case *SystemMessage:
		return m.RawJSON
	case *ResultMessage:
		return m.RawJSON
	case *StreamEvent:
		return m.RawJSON
	case *ErrorMessage:
		return m.RawJSON
	default:
		return nil
	}
}

// =============================================================================
// ContentBlock 接口和类型
// =============================================================================

// ContentBlock 是消息内容片段的公共接口。
type ContentBlock interface {
	contentBlockType() string
}

// TextBlock 表示纯文本内容块，type="text"。
type TextBlock struct {
	// Text 是文本内容。
	Text string
}

// contentBlockType 返回内容块类型标识。
func (b *TextBlock) contentBlockType() string { return "text" }

// ThinkingBlock 表示模型思考过程内容块，type="thinking"。
type ThinkingBlock struct {
	// Thinking 是思考内容。
	Thinking string
	// Signature 是思考签名。
	Signature string
}

// contentBlockType 返回内容块类型标识。
func (b *ThinkingBlock) contentBlockType() string { return "thinking" }

// ToolUseBlock 表示工具调用内容块，type="tool_use"。
type ToolUseBlock struct {
	// ID 是工具调用唯一标识符。
	ID string
	// Name 是工具名称。
	Name string
	// Input 是工具调用输入参数。
	Input map[string]any
}

// contentBlockType 返回内容块类型标识。
func (b *ToolUseBlock) contentBlockType() string { return "tool_use" }

// ToolResultBlock 表示工具调用结果内容块，type="tool_result"。
type ToolResultBlock struct {
	// ToolUseID 是对应的工具调用 ID。
	ToolUseID string
	// Content 是工具结果内容，可以是 string 或 []map[string]any。
	Content any
	// IsError 表示是否为错误结果。
	IsError *bool
}

// contentBlockType 返回内容块类型标识。
func (b *ToolResultBlock) contentBlockType() string { return "tool_result" }
