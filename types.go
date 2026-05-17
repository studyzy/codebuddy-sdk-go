// Package codebuddy 定义了 CodeBuddy SDK 的所有公共类型。
// 本文件包含消息类型、内容块类型、枚举类型、配置类型、权限类型、
// Hook 类型、MCP 配置类型以及 Options 结构体等核心数据结构。
package codebuddy

import (
	"context"
	"encoding/json"
)

// =============================================================================
// 消息接口和类型
// =============================================================================

// Message 是所有 CLI 输出消息的公共接口
type Message interface {
	messageType() string
}

// UserMessage 表示用户发送的消息，type="user"
type UserMessage struct {
	// Content 消息内容，可以是 string 或 []ContentBlock
	Content any
	// UUID 消息的唯一标识符
	UUID *string
	// ParentToolUseID 父工具调用 ID
	ParentToolUseID *string
	// RawJSON 保留 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识
func (m *UserMessage) messageType() string { return "user" }

// AssistantMessage 表示助手返回的消息，type="assistant"
type AssistantMessage struct {
	// Content 消息内容块列表
	Content []ContentBlock
	// Model 使用的模型名称
	Model string
	// ParentToolUseID 父工具调用 ID
	ParentToolUseID *string
	// Error 错误信息（如有）
	Error *string
	// RawJSON 保留 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识
func (m *AssistantMessage) messageType() string { return "assistant" }

// SystemMessage 表示系统消息，type="system"
type SystemMessage struct {
	// Subtype 系统消息子类型
	Subtype string
	// Data 系统消息附加数据
	Data map[string]any
	// RawJSON 保留 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识
func (m *SystemMessage) messageType() string { return "system" }

// ResultMessage 表示最终结果消息，type="result"
type ResultMessage struct {
	// Subtype 结果子类型
	Subtype string
	// DurationMs 总耗时（毫秒）
	DurationMs int
	// DurationAPIMs API 调用耗时（毫秒）
	DurationAPIMs int
	// IsError 是否为错误结果
	IsError bool
	// NumTurns 对话轮次数
	NumTurns int
	// SessionID 会话 ID
	SessionID string
	// StopReason 停止原因
	StopReason *string
	// TotalCostUSD 总费用（美元）
	TotalCostUSD *float64
	// Usage Token 用量统计
	Usage map[string]any
	// Result 结果文本内容
	Result *string
	// StructuredOutput 结构化输出内容
	StructuredOutput any
	// Errors 错误列表
	Errors []string
	// RawJSON 保留 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识
func (m *ResultMessage) messageType() string { return "result" }

// StreamEvent 表示流式事件消息，type="stream_event"
type StreamEvent struct {
	// UUID 事件唯一标识符
	UUID string
	// SessionID 所属会话 ID
	SessionID string
	// Event 事件原始数据
	Event map[string]any
	// ParentToolUseID 父工具调用 ID
	ParentToolUseID *string
	// RawJSON 保留 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识
func (m *StreamEvent) messageType() string { return "stream_event" }

// ErrorMessage 表示错误消息，type="error"
type ErrorMessage struct {
	// Error 错误描述
	Error string
	// SessionID 所属会话 ID（如有）
	SessionID *string
	// Errors 为更完整的错误列表。
	Errors []string
	// Subtype 表示错误来源的 result subtype（如有）。
	Subtype *string
	// RawJSON 保留 CLI 原始 JSON，供上层按原格式透传。
	RawJSON json.RawMessage
}

// messageType 返回消息类型标识
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

// ContentBlock 是消息内容片段的公共接口
type ContentBlock interface {
	contentBlockType() string
}

// TextBlock 表示纯文本内容块，type="text"
type TextBlock struct {
	// Text 文本内容
	Text string
}

// contentBlockType 返回内容块类型标识
func (b *TextBlock) contentBlockType() string { return "text" }

// ThinkingBlock 表示模型思考过程内容块，type="thinking"
type ThinkingBlock struct {
	// Thinking 思考内容
	Thinking string
	// Signature 思考签名
	Signature string
}

// contentBlockType 返回内容块类型标识
func (b *ThinkingBlock) contentBlockType() string { return "thinking" }

// ToolUseBlock 表示工具调用内容块，type="tool_use"
type ToolUseBlock struct {
	// ID 工具调用唯一标识符
	ID string
	// Name 工具名称
	Name string
	// Input 工具调用输入参数
	Input map[string]any
}

// contentBlockType 返回内容块类型标识
func (b *ToolUseBlock) contentBlockType() string { return "tool_use" }

// ToolResultBlock 表示工具调用结果内容块，type="tool_result"
type ToolResultBlock struct {
	// ToolUseID 对应的工具调用 ID
	ToolUseID string
	// Content 工具结果内容，可以是 string 或 []map[string]any
	Content any
	// IsError 是否为错误结果
	IsError *bool
}

// contentBlockType 返回内容块类型标识
func (b *ToolResultBlock) contentBlockType() string { return "tool_result" }

// =============================================================================
// 枚举类型
// =============================================================================

// PermissionMode 工具使用权限模式
type PermissionMode string

const (
	// PermissionModeDefault 默认权限模式
	PermissionModeDefault PermissionMode = "default"
	// PermissionModeAcceptEdits 自动接受编辑操作
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	// PermissionModePlan 仅规划模式，不执行操作
	PermissionModePlan PermissionMode = "plan"
	// PermissionModeBypassPermissions 绕过所有权限检查
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

// Ptr 返回 PermissionMode 的指针，便于在 Options 中赋值
func (p PermissionMode) Ptr() *PermissionMode { return &p }

// HookEvent Hook 事件类型
type HookEvent string

const (
	// HookPreToolUse 工具调用前触发
	HookPreToolUse HookEvent = "PreToolUse"
	// HookPostToolUse 工具调用成功后触发
	HookPostToolUse HookEvent = "PostToolUse"
	// HookPostToolUseFailure 工具调用失败后触发
	HookPostToolUseFailure HookEvent = "PostToolUseFailure"
	// HookUserPromptSubmit 用户提交提示词时触发
	HookUserPromptSubmit HookEvent = "UserPromptSubmit"
	// HookStop 对话停止时触发
	HookStop HookEvent = "Stop"
	// HookSubagentStop 子 Agent 停止时触发
	HookSubagentStop HookEvent = "SubagentStop"
	// HookPreCompact 压缩上下文前触发
	HookPreCompact HookEvent = "PreCompact"
	// HookNotification 通知事件触发
	HookNotification HookEvent = "Notification"
	// HookSubagentStart 子 Agent 启动时触发
	HookSubagentStart HookEvent = "SubagentStart"
	// HookPermissionRequest 权限请求时触发
	HookPermissionRequest HookEvent = "PermissionRequest"
)

// SettingSource 配置来源
type SettingSource string

const (
	// SettingSourceUser 用户级别配置
	SettingSourceUser SettingSource = "user"
	// SettingSourceProject 项目级别配置
	SettingSourceProject SettingSource = "project"
	// SettingSourceLocal 本地级别配置
	SettingSourceLocal SettingSource = "local"
)

// Effort 模型思考深度
type Effort string

const (
	// EffortLow 低深度思考
	EffortLow Effort = "low"
	// EffortMedium 中等深度思考
	EffortMedium Effort = "medium"
	// EffortHigh 高深度思考
	EffortHigh Effort = "high"
	// EffortXHigh 超高深度思考
	EffortXHigh Effort = "xhigh"
)

// Ptr 返回 Effort 的指针，便于在 Options 中赋值
func (e Effort) Ptr() *Effort { return &e }

// =============================================================================
// 配置类型
// =============================================================================

// SystemPromptConfig 系统提示配置
type SystemPromptConfig struct {
	// Override 覆盖整个系统提示
	Override *string
	// Append 追加到默认系统提示末尾
	Append *string
}

// ThinkingConfig 模型思考配置
type ThinkingConfig struct {
	// Type 思考类型，可选值："adaptive" | "enabled" | "disabled"
	Type string
	// BudgetTokens 思考预算 Token 数，仅在 Type="enabled" 时有效
	BudgetTokens *int
}

// AgentDefinition 子 Agent 定义配置
type AgentDefinition struct {
	// Description Agent 功能描述
	Description string
	// Prompt Agent 系统提示词
	Prompt string
	// Tools Agent 可用工具列表
	Tools []string
	// DisallowedTools Agent 禁用工具列表
	DisallowedTools []string
	// Model Agent 使用的模型（可选，不指定则继承父级）
	Model *string
}

// =============================================================================
// 权限类型
// =============================================================================

// CanUseToolOptions 权限回调附加选项
type CanUseToolOptions struct {
	// ToolUseID 工具调用唯一标识符
	ToolUseID string
	// AgentID 发起调用的 Agent ID（子 Agent 场景）
	AgentID *string
	// Suggestions 权限建议列表
	Suggestions []map[string]any
	// BlockedPath 被阻止的文件路径（文件操作场景）
	BlockedPath *string
	// DecisionReason 决策原因说明
	DecisionReason *string
}

// PermissionResult 权限决定接口
type PermissionResult interface {
	permissionBehavior() string
}

// PermissionResultAllow 表示允许工具执行的权限决定
type PermissionResultAllow struct {
	// UpdatedInput 修改后的工具输入参数（可选）
	UpdatedInput map[string]any
	// UpdatedPermissions 更新后的权限配置（可选）
	UpdatedPermissions []map[string]any
}

// permissionBehavior 返回权限行为标识
func (p *PermissionResultAllow) permissionBehavior() string { return "allow" }

// PermissionResultDeny 表示拒绝工具执行的权限决定
type PermissionResultDeny struct {
	// Message 拒绝原因说明
	Message string
	// Interrupt 是否中断整个对话流程
	Interrupt bool
}

// permissionBehavior 返回权限行为标识
func (p *PermissionResultDeny) permissionBehavior() string { return "deny" }

// CanUseToolFunc 权限控制回调函数类型
// ctx 上下文，toolName 工具名称，input 工具输入参数，opts 附加选项
type CanUseToolFunc func(ctx context.Context, toolName string, input map[string]any, opts CanUseToolOptions) (PermissionResult, error)

// =============================================================================
// Hook 类型
// =============================================================================

// HookJSONOutput Hook 回调的输出结果
type HookJSONOutput struct {
	// Continue 是否继续执行（false 表示中断）
	Continue *bool
	// SuppressOutput 是否抑制输出
	SuppressOutput *bool
	// StopReason 停止原因说明
	StopReason *string
	// Decision 决策结果，可选值："block"
	Decision *string
	// Reason 决策原因说明
	Reason *string
}

// HookContext 为 Hook 回调附加上下文，主要用于公开类型对齐。
type HookContext struct {
	Signal any
}

// HookCallback Hook 回调函数类型
// ctx 上下文，input Hook 输入数据，toolUseID 关联的工具调用 ID（如有）
type HookCallback func(ctx context.Context, input map[string]any, toolUseID *string) (HookJSONOutput, error)

// HookMatcher Hook 匹配器配置，用于指定哪些工具触发哪些 Hook
type HookMatcher struct {
	// Matcher 工具名称匹配模式（支持通配符），nil 表示匹配所有工具
	Matcher *string
	// Hooks Hook 回调函数列表
	Hooks []HookCallback
	// Timeout Hook 执行超时时间（秒）
	Timeout *float64
}

// =============================================================================
// MCP 配置类型
// =============================================================================

// MCPServerConfig 是所有 MCP 服务器配置的公共接口。
// 实现此接口的类型：McpStdioServerConfig、McpHttpServerConfig、
// McpSseServerConfig、McpRemoteServerConfig、McpSdkServerConfig。
type MCPServerConfig interface {
	mcpServerType() string
}

// MCPServerMap 是 MCP 服务器名称到配置的映射，用于 Options.MCPServers。
type MCPServerMap map[string]MCPServerConfig

// McpToolConfig MCP 工具级别配置
type McpToolConfig struct {
	// DeferLoading 是否延迟加载该工具
	DeferLoading bool
}

// TextContent 表示 MCP 工具返回的文本内容。
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ImageContent 表示 MCP 工具返回的图片内容。
type ImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
}

// EmbeddedResource 表示 MCP 工具返回的内嵌资源。
type EmbeddedResource struct {
	Type     string         `json:"type"`
	Resource map[string]any `json:"resource"`
}

// CallToolResult 是 MCP tools/call 的标准返回体。
type CallToolResult struct {
	Content []any `json:"content,omitempty"`
	IsError bool  `json:"isError,omitempty"`
}

// McpStdioServerConfig MCP stdio 服务器配置
type McpStdioServerConfig struct {
	// Type 服务器类型，固定为 "stdio"（可省略）
	Type string
	// Command 启动服务器的命令
	Command string
	// Args 启动命令的参数列表
	Args []string
	// Env 进程环境变量
	Env map[string]string
	// Description 服务器功能描述
	Description string
	// DeferLoading 是否延迟加载该服务器
	DeferLoading bool
	// Tools 工具级别配置映射
	Tools map[string]McpToolConfig
}

func (c McpStdioServerConfig) mcpServerType() string { return "stdio" }

// McpHttpServerConfig MCP HTTP 服务器配置
type McpHttpServerConfig struct {
	// Type 服务器类型，固定为 "http"
	Type string
	// URL 服务器地址
	URL string
	// Headers HTTP 请求头
	Headers map[string]string
	// Description 服务器功能描述
	Description string
	// DeferLoading 是否延迟加载该服务器
	DeferLoading bool
	// Tools 工具级别配置映射
	Tools map[string]McpToolConfig
}

func (c McpHttpServerConfig) mcpServerType() string { return "http" }

// McpSseServerConfig MCP SSE 服务器配置
type McpSseServerConfig struct {
	// Type 服务器类型，固定为 "sse"
	Type string
	// URL 服务器地址
	URL string
	// Headers HTTP 请求头
	Headers map[string]string
	// Description 服务器功能描述
	Description string
	// DeferLoading 是否延迟加载该服务器
	DeferLoading bool
	// Tools 工具级别配置映射
	Tools map[string]McpToolConfig
}

func (c McpSseServerConfig) mcpServerType() string { return "sse" }

// McpRemoteServerConfig MCP remote 服务器配置
type McpRemoteServerConfig struct {
	// Type 服务器类型，固定为 "remote"
	Type string
	// URL 服务器地址
	URL string
	// Headers HTTP 请求头
	Headers map[string]string
	// Description 服务器功能描述
	Description string
	// DeferLoading 是否延迟加载该服务器
	DeferLoading bool
	// Tools 工具级别配置映射
	Tools map[string]McpToolConfig
}

func (c McpRemoteServerConfig) mcpServerType() string { return "remote" }

// McpSdkServerConfig MCP SDK 进程内服务器配置。
// 该类型的服务器由 SDK 直接在进程内处理，不启动外部子进程。
type McpSdkServerConfig struct {
	// Type 服务器类型，固定为 "sdk"。
	Type string
	// Name 为 SDK MCP server 的名称。
	Name string
	// Server SDK MCP 服务器实例
	Server *SdkMcpServer
	// Description 为可选描述。
	Description string
	// DeferLoading 是否延迟加载。
	DeferLoading bool
}

func (c McpSdkServerConfig) mcpServerType() string { return "sdk" }

// =============================================================================
// 通知与订阅类型
// =============================================================================

// SubscriptionChannel CLI 通知订阅频道
type SubscriptionChannel string

const (
	// SubscriptionChannelCommands commands 频道，CLI 推送可用斜杠命令列表
	SubscriptionChannelCommands SubscriptionChannel = "commands"
)

// ControlNotificationMessage CLI 主动推送的通知消息，type="control_notification"
type ControlNotificationMessage struct {
	// Channel 通知频道
	Channel SubscriptionChannel
	// Data 通知携带的原始数据
	Data map[string]any
}

// NotificationHandler 控制通知处理回调
type NotificationHandler func(notification ControlNotificationMessage)

// AvailableCommand 可用斜杠命令信息
type AvailableCommand struct {
	// Name 命令名称（不含前缀 /）
	Name string
	// Description 命令功能描述
	Description string
	// Input 可选的输入提示信息
	Input *CommandInput
}

// CommandInput 命令输入提示
type CommandInput struct {
	// Hint 参数提示文本
	Hint string
}

// AvailableMode 可用权限模式信息
type AvailableMode struct {
	// ID 模式标识符
	ID string
	// Name 模式显示名称
	Name string
	// Description 模式描述
	Description string
}

// AvailableModel 可用模型信息（简化格式）
type AvailableModel struct {
	// ModelID 模型标识符
	ModelID string
	// Name 模型显示名称
	Name string
	// Description 模型描述（可选）
	Description *string
}

// =============================================================================
// 信息类型
// =============================================================================

// SlashCommand 可用斜线命令信息
type SlashCommand struct {
	// Name 命令名称
	Name string
	// Description 命令功能描述
	Description string
	// ArgumentHint 参数提示（可选）
	ArgumentHint *string
}

// ModelInfo 可用模型信息
type ModelInfo struct {
	// Value 模型标识符
	Value string
	// DisplayName 模型显示名称
	DisplayName string
	// Description 模型功能描述（可选）
	Description *string
}

// AccountInfo 账户信息
type AccountInfo struct {
	// Email 账户邮箱
	Email *string
	// Organization 所属组织
	Organization *string
	// SubscriptionType 订阅类型
	SubscriptionType *string
	// TokenSource Token 来源
	TokenSource *string
	// APIKeySource API Key 来源
	APIKeySource *string
	// UserID 用户唯一标识符
	UserID *string
	// UserName 用户名
	UserName *string
	// UserNickname 用户昵称
	UserNickname *string
	// EnterpriseID 企业 ID
	EnterpriseID *string
	// Enterprise 企业名称
	Enterprise *string
}

// MCPServerStatus MCP 服务器连接状态
type MCPServerStatus struct {
	// Name 服务器名称
	Name string
	// Status 连接状态，可选值："connected" | "failed" | "needs-auth" | "pending"
	Status string
	// ServerInfo 服务器详细信息
	ServerInfo map[string]any
}

// UserInfo 已认证用户信息
type UserInfo struct {
	// UserID 用户唯一标识符
	UserID string
	// UserName 用户名
	UserName string
	// UserNickname 用户昵称
	UserNickname string
	// Token 认证令牌
	Token string
	// EnterpriseID 企业 ID（可选）
	EnterpriseID *string
	// Enterprise 企业名称（可选）
	Enterprise *string
}

// UserInfoFromMap 从 camelCase 键的 map 构建 UserInfo
// 对应 Python SDK 中的 UserInfo.from_dict 方法
func UserInfoFromMap(data map[string]any) UserInfo {
	getString := func(key string) string {
		if v, ok := data[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getStringPtr := func(key string) *string {
		if v, ok := data[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return &s
			}
		}
		return nil
	}
	return UserInfo{
		UserID:       getString("userId"),
		UserName:     getString("userName"),
		UserNickname: getString("userNickname"),
		Token:        getString("token"),
		EnterpriseID: getStringPtr("enterpriseId"),
		Enterprise:   getStringPtr("enterprise"),
	}
}

// AuthState 认证中间状态，包含待完成认证的相关信息
type AuthState struct {
	// AuthURL 用户需要访问的认证 URL
	AuthURL string
	// MethodID 认证方式 ID（可选）
	MethodID *string
}

// AuthenticateResponse 认证成功响应
type AuthenticateResponse struct {
	// UserInfo 认证成功后的用户信息
	UserInfo UserInfo
}

// =============================================================================
// Options 结构体
// =============================================================================

// Options 是驱动 CLI 行为的完整配置集合，
// 涵盖会话管理、工具控制、模型配置、MCP 服务器、权限与 Hook 等所有选项
type Options struct {
	// ---- 会话管理 ----

	// SessionID 指定会话 ID，用于恢复已有会话
	SessionID *string
	// ContinueConversation 是否继续上一次对话
	ContinueConversation bool
	// Resume 从指定检查点恢复对话
	Resume *string
	// ForkSession 是否分叉当前会话
	ForkSession bool
	// MaxTurns 最大对话轮次限制
	MaxTurns *int

	// ---- 工具控制 ----

	// AllowedTools 允许使用的工具列表
	AllowedTools []string
	// DisallowedTools 禁止使用的工具列表
	DisallowedTools []string
	// Tools 工具列表，nil 表示使用所有工具，[] 表示禁用所有工具
	Tools []string

	// ---- 系统提示 ----

	// SystemPrompt 系统提示配置
	SystemPrompt *SystemPromptConfig

	// ---- 模型配置 ----

	// Model 主模型名称
	Model *string
	// FallbackModel 备用模型名称（主模型不可用时使用）
	FallbackModel *string
	// Thinking 模型思考配置
	Thinking *ThinkingConfig
	// Effort 模型思考深度
	Effort *Effort

	// ---- MCP 服务器 ----

	// MCPServers MCP 服务器配置映射，键为服务器名称，值为具体配置类型
	// （McpStdioServerConfig/McpHttpServerConfig/McpSseServerConfig/McpRemoteServerConfig/McpSdkServerConfig）
	MCPServers MCPServerMap

	// ---- 权限与 Hook ----

	// PermissionMode 工具使用权限模式
	PermissionMode *PermissionMode
	// CanUseTool 工具使用权限控制回调
	CanUseTool CanUseToolFunc
	// Hooks 按事件类型注册的 Hook 回调列表
	Hooks map[HookEvent][]HookMatcher

	// ---- 进程配置 ----

	// Cwd 工作目录路径
	Cwd *string
	// CLIPath CLI 可执行文件路径，优先于环境变量配置
	CLIPath *string
	// Env 传递给子进程的环境变量
	Env map[string]string
	// ExtraArgs 额外的 CLI 参数，nil value 表示仅传入 flag 不带值
	ExtraArgs map[string]*string
	// Stderr 标准错误输出处理函数
	Stderr func(string)

	// ---- 高级选项 ----

	// IncludePartialMessages 是否包含流式部分消息
	IncludePartialMessages bool
	// SettingSources 配置来源列表，nil 表示使用 SDK 默认隔离策略
	SettingSources []SettingSource
	// Agents 子 Agent 定义映射
	Agents map[string]AgentDefinition
	// DangerouslySkipPermissions 跳过所有权限检查（对应 --dangerously-skip-permissions）
	// 警告：仅在受控环境中使用，会绕过所有安全提示
	DangerouslySkipPermissions bool
	// AdditionalDirectories 额外允许访问的目录列表（对应多个 --add-dir 参数）
	AdditionalDirectories []string
	// Args 直接追加到 CLI 参数末尾的原始参数列表（优先级最低，位于所有其他参数之后）
	Args []string

	// ---- 废弃字段（仅供兼容性保留）----

	// MaxThinkingTokens 最大思考 Token 数（已废弃，请使用 Thinking.BudgetTokens）
	MaxThinkingTokens *int
}
