package codebuddy

import "context"

// =============================================================================
// 权限类型
// =============================================================================

// CanUseToolOptions 表示权限回调的附加选项。
type CanUseToolOptions struct {
	// ToolUseID 是工具调用唯一标识符。
	ToolUseID string
	// AgentID 是发起调用的 Agent ID（子 Agent 场景）。
	AgentID *string
	// Suggestions 是权限建议列表。
	Suggestions []map[string]any
	// BlockedPath 是被阻止的文件路径（文件操作场景）。
	BlockedPath *string
	// DecisionReason 是决策原因说明。
	DecisionReason *string
}

// PermissionResult 是权限决定的接口。
type PermissionResult interface {
	permissionBehavior() string
}

// PermissionResultAllow 表示允许工具执行的权限决定。
type PermissionResultAllow struct {
	// UpdatedInput 是修改后的工具输入参数（可选）。
	UpdatedInput map[string]any
	// UpdatedPermissions 是更新后的权限配置（可选）。
	UpdatedPermissions []map[string]any
}

// permissionBehavior 返回权限行为标识。
func (p *PermissionResultAllow) permissionBehavior() string { return "allow" }

// PermissionResultDeny 表示拒绝工具执行的权限决定。
type PermissionResultDeny struct {
	// Message 是拒绝原因说明。
	Message string
	// Interrupt 表示是否中断整个对话流程。
	Interrupt bool
}

// permissionBehavior 返回权限行为标识。
func (p *PermissionResultDeny) permissionBehavior() string { return "deny" }

// CanUseToolFunc 是权限控制回调函数类型。
// ctx 上下文，toolName 工具名称，input 工具输入参数，opts 附加选项。
type CanUseToolFunc func(ctx context.Context, toolName string, input map[string]any, opts CanUseToolOptions) (PermissionResult, error)

// =============================================================================
// Hook 类型
// =============================================================================

// HookJSONOutput 表示 Hook 回调的输出结果。
type HookJSONOutput struct {
	// Continue 表示是否继续执行（false 表示中断）。
	Continue *bool
	// SuppressOutput 表示是否抑制输出。
	SuppressOutput *bool
	// StopReason 是停止原因说明。
	StopReason *string
	// Decision 是决策结果，可选值："approve" | "block"。
	Decision *string
	// Reason 是决策原因说明。
	Reason *string
	// SystemMessage 是 Hook 注入的系统消息文本。
	SystemMessage *string
	// HookSpecificOutput 是 Hook 特定的额外输出数据。
	HookSpecificOutput map[string]any
}

// HookContext 表示 Hook 回调附加上下文，主要用于公开类型对齐。
type HookContext struct {
	// Signal 是上下文信号数据。
	Signal any
}

// HookCallback 是 Hook 回调函数类型。
// ctx 上下文，input Hook 输入数据，toolUseID 关联的工具调用 ID（如有）。
type HookCallback func(ctx context.Context, input map[string]any, toolUseID *string) (HookJSONOutput, error)

// HookMatcher 表示 Hook 匹配器配置，用于指定哪些工具触发哪些 Hook。
type HookMatcher struct {
	// Matcher 是工具名称匹配模式（支持通配符），nil 表示匹配所有工具。
	Matcher *string
	// Hooks 是 Hook 回调函数列表。
	Hooks []HookCallback
	// Timeout 是 Hook 执行超时时间（秒）。
	Timeout *float64
}
