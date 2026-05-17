package codebuddy

// =============================================================================
// 配置类型
// =============================================================================

// SystemPromptConfig 表示系统提示配置。
type SystemPromptConfig struct {
	// Override 是覆盖整个系统提示的文本。
	Override *string
	// Append 是追加到默认系统提示末尾的文本。
	Append *string
}

// ThinkingConfig 表示模型思考配置。
type ThinkingConfig struct {
	// Type 是思考类型，可选值："adaptive" | "enabled" | "disabled"。
	Type string
	// BudgetTokens 是思考预算 Token 数，仅在 Type="enabled" 时有效。
	BudgetTokens *int
}

// AgentDefinition 表示子 Agent 定义配置。
type AgentDefinition struct {
	// Description 是 Agent 功能描述。
	Description string
	// Prompt 是 Agent 系统提示词。
	Prompt string
	// Tools 是 Agent 可用工具列表。
	Tools []string
	// DisallowedTools 是 Agent 禁用工具列表。
	DisallowedTools []string
	// Model 是 Agent 使用的模型（可选，不指定则继承父级）。
	Model *string
}

// SandboxSettings 表示沙箱执行环境配置。
type SandboxSettings struct {
	// Enabled 表示是否启用沙箱（nil 表示使用默认值）。
	Enabled *bool
	// AutoAllowBashIfSandboxed 表示沙箱模式下是否自动允许 Bash 命令。
	AutoAllowBashIfSandboxed *bool
	// Network 是沙箱网络权限配置。
	Network *NetworkSettings
}

// NetworkSettings 表示沙箱网络权限配置。
type NetworkSettings struct {
	// AllowLocalBinding 表示是否允许本地端口绑定。
	AllowLocalBinding *bool
	// AllowUnixSockets 是允许的 Unix socket 路径列表。
	AllowUnixSockets []string
}

// JsonSchemaOutputFormat 表示 JSON Schema 结构化输出格式。
type JsonSchemaOutputFormat struct {
	// Type 是输出格式类型，固定为 "json_schema"。
	Type string
	// Schema 是 JSON Schema 定义。
	Schema map[string]any
}

// SetConfigResult 表示动态配置更新的结果。
type SetConfigResult struct {
	// Updated 是已成功更新的配置键值对。
	Updated map[string]any
	// Errors 是更新失败的配置项及错误信息。
	Errors map[string]string
}

// =============================================================================
// Options 结构体
// =============================================================================

// Options 是驱动 CLI 行为的完整配置集合，
// 涵盖会话管理、工具控制、模型配置、MCP 服务器、权限与 Hook 等所有选项。
type Options struct {
	// ---- 会话管理 ----

	// SessionID 是指定的会话 ID，用于恢复已有会话。
	SessionID *string
	// ContinueConversation 表示是否继续上一次对话。
	ContinueConversation bool
	// Resume 是从指定检查点恢复对话的检查点标识。
	Resume *string
	// ForkSession 表示是否分叉当前会话。
	ForkSession bool
	// MaxTurns 是最大对话轮次限制。
	MaxTurns *int

	// ---- 工具控制 ----

	// AllowedTools 是允许使用的工具列表。
	AllowedTools []string
	// DisallowedTools 是禁止使用的工具列表。
	DisallowedTools []string
	// Tools 是工具列表，nil 表示使用所有工具，[] 表示禁用所有工具。
	Tools []string

	// ---- 系统提示 ----

	// SystemPrompt 是系统提示配置。
	SystemPrompt *SystemPromptConfig

	// ---- 模型配置 ----

	// Model 是主模型名称。
	Model *string
	// FallbackModel 是备用模型名称（主模型不可用时使用）。
	FallbackModel *string
	// Thinking 是模型思考配置。
	Thinking *ThinkingConfig
	// Effort 是模型思考深度。
	Effort *Effort

	// ---- MCP 服务器 ----

	// MCPServers 是 MCP 服务器配置映射，键为服务器名称，值为具体配置类型
	// （McpStdioServerConfig/McpHttpServerConfig/McpSseServerConfig/McpRemoteServerConfig/McpSdkServerConfig）。
	MCPServers MCPServerMap

	// ---- 权限与 Hook ----

	// PermissionMode 是工具使用权限模式。
	PermissionMode *PermissionMode
	// CanUseTool 是工具使用权限控制回调。
	CanUseTool CanUseToolFunc
	// Hooks 是按事件类型注册的 Hook 回调列表。
	Hooks map[HookEvent][]HookMatcher

	// ---- 进程配置 ----

	// Cwd 是工作目录路径。
	Cwd *string
	// CLIPath 是 CLI 可执行文件路径，优先于环境变量配置。
	CLIPath *string
	// Env 是传递给子进程的环境变量。
	Env map[string]string
	// ExtraArgs 是额外的 CLI 参数，nil value 表示仅传入 flag 不带值。
	ExtraArgs map[string]*string
	// Stderr 是标准错误输出处理函数。
	Stderr func(string)

	// ---- 高级选项 ----

	// IncludePartialMessages 表示是否包含流式部分消息。
	IncludePartialMessages bool
	// SettingSources 是配置来源列表，nil 表示使用 SDK 默认隔离策略。
	SettingSources []SettingSource
	// Agents 是子 Agent 定义映射。
	Agents map[string]AgentDefinition
	// DangerouslySkipPermissions 表示是否跳过所有权限检查（对应 --dangerously-skip-permissions）。
	// 警告：仅在受控环境中使用，会绕过所有安全提示。
	DangerouslySkipPermissions bool
	// AdditionalDirectories 是额外允许访问的目录列表（对应多个 --add-dir 参数）。
	AdditionalDirectories []string
	// Args 是直接追加到 CLI 参数末尾的原始参数列表（优先级最低，位于所有其他参数之后）。
	Args []string

	// ---- 废弃字段（仅供兼容性保留）----

	// MaxThinkingTokens 是最大思考 Token 数（已废弃，请使用 Thinking.BudgetTokens）。
	MaxThinkingTokens *int

	// ---- 新增高级选项（对齐 Node.js SDK）----

	// MaxBudgetUsd 是最大预算限制（美元）。
	MaxBudgetUsd *float64
	// OutputFormat 是结构化输出格式配置（JSON Schema）。
	OutputFormat *JsonSchemaOutputFormat
	// PersistSession 控制会话持久化，false 表示禁用持久化（nil 表示使用默认值）。
	PersistSession *bool
	// EnableFileCheckpointing 表示是否启用文件检查点。
	EnableFileCheckpointing bool
	// RequestTimeoutMs 是控制请求超时时间（毫秒），SDK 内部使用。
	RequestTimeoutMs *int
	// Sandbox 是沙箱执行环境配置。
	Sandbox *SandboxSettings
	// Environment 是认证环境标识（如 "external"、"internal"、"ioa"、"cloudhosted"）。
	Environment *string
	// Endpoint 是自定义端点 URL（用于自托管环境）。
	Endpoint *string
	// ResumeSessionAt 是指定恢复到的消息 ID。
	ResumeSessionAt *string
	// PermissionPromptToolName 是 MCP 工具名称，用于权限提示。
	PermissionPromptToolName *string
	// TraceId 是分布式追踪 ID，传播到 CLI 和模型请求。
	TraceId *string
	// ParentSpanId 是父 Span ID，用于将 CLI 追踪链接为上游调用的子 span。
	ParentSpanId *string
	// StrictMcpConfig 表示是否启用 MCP 配置严格校验模式。
	StrictMcpConfig bool
}
