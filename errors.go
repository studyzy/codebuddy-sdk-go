// errors.go 定义了 codebuddy SDK 中所有自定义错误类型。
// 该模块负责统一管理与 CLI 交互过程中可能出现的各类错误，
// 包括连接错误、可执行文件未找到、JSON 解析失败、进程启动失败、
// 运行时错误、执行失败以及认证失败等七种错误类型。
package codebuddy

// CLIConnectionError 表示与 CLI 建立连接时发生的错误。
type CLIConnectionError struct {
	// Message 错误描述信息
	Message string
}

// Error 实现 error 接口，返回错误描述信息。
func (e *CLIConnectionError) Error() string {
	return e.Message
}

// CLINotFoundError 表示在当前平台和架构下未找到 CLI 可执行文件的错误。
type CLINotFoundError struct {
	// Message 错误描述信息
	Message string
	// Platform 当前操作系统平台（如 linux、darwin、windows）
	Platform string
	// Arch 当前 CPU 架构（如 amd64、arm64）
	Arch string
}

// Error 实现 error 接口，返回错误描述信息。
func (e *CLINotFoundError) Error() string {
	return e.Message
}

// CLIJSONDecodeError 表示解析 CLI 输出的 JSON 数据时发生的错误。
type CLIJSONDecodeError struct {
	// Message 错误描述信息
	Message string
}

// Error 实现 error 接口，返回错误描述信息。
func (e *CLIJSONDecodeError) Error() string {
	return e.Message
}

// CLIStartupError 表示 CLI 进程启动失败或意外崩溃时的错误。
type CLIStartupError struct {
	// Message 错误描述信息
	Message string
	// Stderr CLI 进程的标准错误输出内容
	Stderr string
	// ExitCode CLI 进程的退出码，可能为 nil（进程未正常退出时）
	ExitCode *int
}

// Error 实现 error 接口，返回错误描述信息。
func (e *CLIStartupError) Error() string {
	return e.Message
}

// ProcessError 表示 CLI 进程在运行时发生的错误。
type ProcessError struct {
	// Message 错误描述信息
	Message string
}

// Error 实现 error 接口，返回错误描述信息。
func (e *ProcessError) Error() string {
	return e.Message
}

// ControlRequestError 表示 CLI 对控制请求返回了显式错误响应。
type ControlRequestError struct {
	// Message 为 CLI 返回的错误描述。
	Message string
	// Subtype 为触发错误的控制请求子类型。
	Subtype string
}

// Error 实现 error 接口，返回错误描述信息。
func (e *ControlRequestError) Error() string {
	return e.Message
}

// ExecutionError 表示执行失败时的错误，来源于 ResultMessage，
// 涵盖认证错误、API 错误等各类执行阶段的失败情况。
// 构造时：若 Errors 不为空则取第一个元素作为 Message，否则 Message 为 "Execution failed"。
type ExecutionError struct {
	// Message 错误描述信息（取自 Errors[0] 或默认值 "Execution failed"）
	Message string
	// Errors 详细错误列表
	Errors []string
	// Subtype 错误子类型
	Subtype string
}

// NewExecutionError 根据错误列表和子类型构造 ExecutionError。
// 若 errors 不为空，则将第一个元素设为 Message；否则 Message 为 "Execution failed"。
func NewExecutionError(errors []string, subtype string) *ExecutionError {
	msg := "Execution failed"
	if len(errors) > 0 {
		msg = errors[0]
	}
	return &ExecutionError{
		Message: msg,
		Errors:  errors,
		Subtype: subtype,
	}
}

// Error 实现 error 接口，返回错误描述信息。
func (e *ExecutionError) Error() string {
	return e.Message
}

// AuthenticationError 表示认证失败时的错误。
type AuthenticationError struct {
	// Message 错误描述信息
	Message string
	// ErrorType 认证错误的具体类型
	ErrorType string
}

// Error 实现 error 接口，返回错误描述信息。
func (e *AuthenticationError) Error() string {
	return e.Message
}

// ACPError 表示 ACP 协议层面的错误，包括连接失败、HTTP 状态错误、JSON-RPC 错误等。
type ACPError struct {
	// Message 错误描述信息
	Message string
	// Cause 可选的底层原因
	Cause error
}

// Error 实现 error 接口，返回错误描述信息。
func (e *ACPError) Error() string {
	return e.Message
}

// Unwrap 返回底层原因，支持 errors.As/errors.Is 链式匹配。
func (e *ACPError) Unwrap() error {
	return e.Cause
}

// ACPTimeoutError 表示等待 ACP 代理响应超时的错误。
type ACPTimeoutError struct {
	// Message 错误描述信息
	Message string
}

// Error 实现 error 接口，返回错误描述信息。
func (e *ACPTimeoutError) Error() string {
	return e.Message
}
