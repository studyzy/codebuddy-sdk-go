package codebuddy

// =============================================================================
// 枚举类型
// =============================================================================

// PermissionMode 表示工具使用权限模式。
type PermissionMode string

const (
	// PermissionModeDefault 表示默认权限模式。
	PermissionModeDefault PermissionMode = "default"
	// PermissionModeAcceptEdits 表示自动接受编辑操作。
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	// PermissionModePlan 表示仅规划模式，不执行操作。
	PermissionModePlan PermissionMode = "plan"
	// PermissionModeBypassPermissions 表示绕过所有权限检查。
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

// Ptr 返回 PermissionMode 的指针，便于在 Options 中赋值。
func (p PermissionMode) Ptr() *PermissionMode { return &p }

// HookEvent 表示 Hook 事件类型。
type HookEvent string

const (
	// HookPreToolUse 表示工具调用前触发的事件。
	HookPreToolUse HookEvent = "PreToolUse"
	// HookPostToolUse 表示工具调用成功后触发的事件。
	HookPostToolUse HookEvent = "PostToolUse"
	// HookPostToolUseFailure 表示工具调用失败后触发的事件。
	HookPostToolUseFailure HookEvent = "PostToolUseFailure"
	// HookUserPromptSubmit 表示用户提交提示词时触发的事件。
	HookUserPromptSubmit HookEvent = "UserPromptSubmit"
	// HookStop 表示对话停止时触发的事件。
	HookStop HookEvent = "Stop"
	// HookSubagentStop 表示子 Agent 停止时触发的事件。
	HookSubagentStop HookEvent = "SubagentStop"
	// HookPreCompact 表示压缩上下文前触发的事件。
	HookPreCompact HookEvent = "PreCompact"
	// HookNotification 表示通知事件触发的事件。
	HookNotification HookEvent = "Notification"
	// HookSubagentStart 表示子 Agent 启动时触发的事件。
	HookSubagentStart HookEvent = "SubagentStart"
	// HookPermissionRequest 表示权限请求时触发的事件。
	HookPermissionRequest HookEvent = "PermissionRequest"
)

// SettingSource 表示配置来源。
type SettingSource string

const (
	// SettingSourceUser 表示用户级别配置。
	SettingSourceUser SettingSource = "user"
	// SettingSourceProject 表示项目级别配置。
	SettingSourceProject SettingSource = "project"
	// SettingSourceLocal 表示本地级别配置。
	SettingSourceLocal SettingSource = "local"
)

// Effort 表示模型思考深度。
type Effort string

const (
	// EffortLow 表示低深度思考。
	EffortLow Effort = "low"
	// EffortMedium 表示中等深度思考。
	EffortMedium Effort = "medium"
	// EffortHigh 表示高深度思考。
	EffortHigh Effort = "high"
	// EffortXHigh 表示超高深度思考。
	EffortXHigh Effort = "xhigh"
)

// Ptr 返回 Effort 的指针，便于在 Options 中赋值。
func (e Effort) Ptr() *Effort { return &e }

// =============================================================================
// 通知与订阅类型
// =============================================================================

// SubscriptionChannel 表示 CLI 通知订阅频道。
type SubscriptionChannel string

const (
	// SubscriptionChannelCommands 表示 commands 频道，CLI 推送可用斜杠命令列表。
	SubscriptionChannelCommands SubscriptionChannel = "commands"
)

// ControlNotificationMessage 表示 CLI 主动推送的通知消息，type="control_notification"。
type ControlNotificationMessage struct {
	// Channel 是通知频道。
	Channel SubscriptionChannel
	// Data 是通知携带的原始数据。
	Data map[string]any
}

// NotificationHandler 是控制通知处理回调函数类型。
type NotificationHandler func(notification ControlNotificationMessage)

// AvailableCommand 表示可用斜杠命令信息。
type AvailableCommand struct {
	// Name 是命令名称（不含前缀 /）。
	Name string
	// Description 是命令功能描述。
	Description string
	// Input 是可选的输入提示信息。
	Input *CommandInput
}

// CommandInput 表示命令输入提示。
type CommandInput struct {
	// Hint 是参数提示文本。
	Hint string
}

// AvailableMode 表示可用权限模式信息。
type AvailableMode struct {
	// ID 是模式标识符。
	ID string
	// Name 是模式显示名称。
	Name string
	// Description 是模式描述。
	Description string
}

// AvailableModel 表示可用模型信息（简化格式）。
type AvailableModel struct {
	// ModelID 是模型标识符。
	ModelID string
	// Name 是模型显示名称。
	Name string
	// Description 是模型描述（可选）。
	Description *string
}

// =============================================================================
// 信息类型
// =============================================================================

// SlashCommand 表示可用斜线命令信息。
type SlashCommand struct {
	// Name 是命令名称。
	Name string
	// Description 是命令功能描述。
	Description string
	// ArgumentHint 是参数提示（可选）。
	ArgumentHint *string
}

// ModelInfo 表示可用模型信息。
type ModelInfo struct {
	// Value 是模型标识符。
	Value string
	// DisplayName 是模型显示名称。
	DisplayName string
	// Description 是模型功能描述（可选）。
	Description *string
}

// AccountInfo 表示账户信息。
type AccountInfo struct {
	// Email 是账户邮箱。
	Email *string
	// Organization 是所属组织。
	Organization *string
	// SubscriptionType 是订阅类型。
	SubscriptionType *string
	// TokenSource 是 Token 来源。
	TokenSource *string
	// APIKeySource 是 API Key 来源。
	APIKeySource *string
	// UserID 是用户唯一标识符。
	UserID *string
	// UserName 是用户名。
	UserName *string
	// UserNickname 是用户昵称。
	UserNickname *string
	// EnterpriseID 是企业 ID。
	EnterpriseID *string
	// Enterprise 是企业名称。
	Enterprise *string
}

// MCPServerStatus 表示 MCP 服务器连接状态。
type MCPServerStatus struct {
	// Name 是服务器名称。
	Name string
	// Status 是连接状态，可选值："connected" | "failed" | "needs-auth" | "pending"。
	Status string
	// ServerInfo 是服务器详细信息。
	ServerInfo map[string]any
}

// UserInfo 表示已认证用户信息。
type UserInfo struct {
	// UserID 是用户唯一标识符。
	UserID string
	// UserName 是用户名。
	UserName string
	// UserNickname 是用户昵称。
	UserNickname string
	// Token 是认证令牌。
	Token string
	// EnterpriseID 是企业 ID（可选）。
	EnterpriseID *string
	// Enterprise 是企业名称（可选）。
	Enterprise *string
}

// UserInfoFromMap 从 camelCase 键的 map 构建 UserInfo。
// 对应 Python SDK 中的 UserInfo.from_dict 方法。
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

// AuthState 表示认证中间状态，包含待完成认证的相关信息。
type AuthState struct {
	// AuthURL 是用户需要访问的认证 URL。
	AuthURL string
	// MethodID 是认证方式 ID（可选）。
	MethodID *string
}

// AuthenticateResponse 表示认证成功响应。
type AuthenticateResponse struct {
	// UserInfo 是认证成功后的用户信息。
	UserInfo UserInfo
}
