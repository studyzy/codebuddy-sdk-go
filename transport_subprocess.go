// transport_subprocess.go
// 子进程传输层 - 通过启动 CodeBuddy CLI 子进程实现 Transport 接口，
// 使用 stdin/stdout 进行 JSON 行式通信。

package codebuddy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

// SubprocessTransport 通过子进程启动 CLI 并通过 stdin/stdout 通信的传输实现。
// 实现了 Transport 接口。
type SubprocessTransport struct {
	// options 传输层使用的配置选项
	options *Options
	// prompt 可为 nil（流式模式）、string（单次模式）或 <-chan map[string]any（流式输入）
	prompt any

	// mu 保护 closed 字段的互斥锁
	mu sync.Mutex
	// closed 标识连接是否已关闭
	closed bool
	// cmd 子进程句柄
	cmd *exec.Cmd
	// stdinPipe 写入 CLI stdin 的管道
	stdinPipe io.WriteCloser
	// stdinMu 保护 stdinPipe 写入的互斥锁
	stdinMu sync.Mutex

	// msgCh 接收从 CLI stdout 解析出的消息通道
	msgCh chan RawMessage
	// closeCh 用于通知内部 goroutine 关闭的信号通道
	closeCh chan struct{}

	// stderrBuf stderr 环形缓冲区，最多保留 stderrCap 行
	stderrBuf []string
	// stderrMu 保护 stderrBuf 的互斥锁
	stderrMu sync.Mutex
	// stderrCap stderr 缓冲区容量上限
	stderrCap int

	// sdkMCPServerNames type=sdk 的 MCP 服务器名称列表
	sdkMCPServerNames []string
	// sdkMCPServers 进程内 SDK MCP 服务器实例映射（名称 → *SdkMcpServer）
	sdkMCPServers map[string]*SdkMcpServer

	// cliVersion 已解析的 CLI 版本号，用于构建 User-Agent
	cliVersion string

	// isCliReady 标识 CLI 是否已产生首行有效输出（就绪状态）
	isCliReady atomic.Bool

	// notificationHandlers 按 channel 存储的通知处理器列表
	notificationHandlers map[SubscriptionChannel][]NotificationHandler
	notificationMu       sync.Mutex
}

// NewSubprocessTransport 创建 SubprocessTransport 实例。
// prompt 可为 nil（流式模式）、string（单次模式）或 <-chan map[string]any（流式输入）。
func NewSubprocessTransport(opts *Options, prompt any) *SubprocessTransport {
	if opts == nil {
		opts = &Options{}
	}
	t := &SubprocessTransport{
		options:              opts,
		prompt:               prompt,
		msgCh:                make(chan RawMessage, 100),
		closeCh:              make(chan struct{}),
		stderrCap:            100,
		notificationHandlers: make(map[SubscriptionChannel][]NotificationHandler),
	}
	// 提取 SDK MCP server 名称和实例
	t.sdkMCPServerNames, t.sdkMCPServers = extractSDKMCPServers(opts)
	return t
}

// extractSDKMCPServers 从 MCPServers 配置中提取 McpSdkServerConfig 类型的服务器。
// 返回名称列表和名称→实例的映射。
func extractSDKMCPServers(opts *Options) ([]string, map[string]*SdkMcpServer) {
	servers := make(map[string]*SdkMcpServer)
	if opts.MCPServers == nil {
		return nil, servers
	}
	var names []string
	for name, cfg := range opts.MCPServers {
		if sdkCfg, ok := cfg.(McpSdkServerConfig); ok && sdkCfg.Server != nil {
			names = append(names, name)
			servers[name] = sdkCfg.Server
		}
	}
	return names, servers
}

// Connect 实现 Transport 接口：启动 CLI 子进程并建立 stdin/stdout 通信。
func (t *SubprocessTransport) Connect(ctx context.Context) error {
	// 确定 CLI 可执行文件路径
	cliPath := ""
	if t.options.CLIPath != nil {
		cliPath = *t.options.CLIPath
	}
	if cliPath == "" {
		var err error
		cliPath, err = GetCLIPath()
		if err != nil {
			return err
		}
	}
	t.cliVersion = GetCLIVersion(cliPath)

	// 构建命令行参数
	args := t.buildArgs()

	// 确定工作目录
	cwd := ""
	if t.options.Cwd != nil {
		cwd = *t.options.Cwd
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// 构建环境变量
	env := t.buildEnv(cliPath)

	// 创建子进程
	cmd := exec.CommandContext(ctx, cliPath, args...)
	cmd.Dir = cwd
	cmd.Env = env

	// 创建 stdin 管道
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("无法创建 stdin 管道: %v", err)}
	}

	// 创建 stdout 管道
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("无法创建 stdout 管道: %v", err)}
	}

	// 创建 stderr 管道
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("无法创建 stderr 管道: %v", err)}
	}

	// 启动子进程
	if err := cmd.Start(); err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("无法启动 CLI 进程: %v", err)}
	}

	t.cmd = cmd
	t.stdinPipe = stdinPipe

	// 启动 stderr 读取 goroutine
	go t.readStderr(stderrPipe)

	// 启动 stdout 读取 goroutine，将 JSON 行解析后写入 msgCh
	go t.readStdout(stdoutPipe)

	return nil
}

// readStdout 从 stdout 逐行读取 JSON，解析后发送到 msgCh。
// 支持跨行累积不完整 JSON，对应 Python SDK 的 _read_messages_impl。
func (t *SubprocessTransport) readStdout(pipe io.Reader) {
	scanner := bufio.NewScanner(pipe)
	// 设置较大缓冲区，应对长行（最大 1MB）
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// gotOutput 标记是否已收到至少一行有效 JSON 输出
	gotOutput := false
	// jsonBuf 用于跨行累积不完整 JSON
	var jsonBuf strings.Builder

	for scanner.Scan() {
		if t.IsClosed() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// 先尝试直接解析当前行（最常见情况：每行一个完整 JSON）
		if jsonBuf.Len() == 0 {
			var data map[string]any
			if err := json.Unmarshal([]byte(line), &data); err == nil {
				gotOutput = true
				t.isCliReady.Store(true)
				// control_notification 直接分发给注册处理器，不入 msgCh
				if msgType, _ := data["type"].(string); msgType == "control_notification" {
					t.dispatchNotification(data)
					continue
				}
				select {
				case t.msgCh <- RawMessage{Data: data, Raw: json.RawMessage([]byte(line))}:
				case <-t.closeCh:
					return
				}
				continue
			}
		}

		// 累积到缓冲区，尝试解析完整 JSON
		jsonBuf.WriteString(line)
		raw := jsonBuf.String()

		var data map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err == nil {
			// 累积后解析成功
			jsonBuf.Reset()
			gotOutput = true
			t.isCliReady.Store(true)
			// control_notification 直接分发，不入 msgCh
			if msgType, _ := data["type"].(string); msgType == "control_notification" {
				t.dispatchNotification(data)
				continue
			}
			select {
			case t.msgCh <- RawMessage{Data: data, Raw: json.RawMessage([]byte(raw))}:
			case <-t.closeCh:
				return
			}
		}
		// 解析失败则继续累积，等待后续行
	}

	// 尝试 flush 剩余缓冲区中的 JSON
	if jsonBuf.Len() > 0 {
		var data map[string]any
		if err := json.Unmarshal([]byte(jsonBuf.String()), &data); err == nil {
			gotOutput = true
			remaining := jsonBuf.String()
			select {
			case t.msgCh <- RawMessage{Data: data, Raw: json.RawMessage([]byte(remaining))}:
			default:
			}
		}
	}

	// stdout 结束，检查进程退出情况
	if !t.IsClosed() {
		exitCode := t.waitProcess()
		if !gotOutput {
			// 没有任何输出，属于启动失败
			stderr := t.getStderrSnapshot()
			msg := fmt.Sprintf("CLI 进程无输出即退出（退出码 %v）", exitCode)
			if stderr != "" {
				msg += ": " + stderr
			}
			select {
			case t.msgCh <- RawMessage{Err: &CLIStartupError{Message: msg, Stderr: stderr, ExitCode: exitCode}}:
			default:
			}
		} else if exitCode != nil && *exitCode != 0 {
			// 有输出但以非零退出码结束
			stderr := t.getStderrSnapshot()
			msg := fmt.Sprintf("CLI 进程以非零退出码 %d 退出", *exitCode)
			if stderr != "" {
				msg += ": " + stderr
			}
			select {
			case t.msgCh <- RawMessage{Err: &ProcessError{Message: msg}}:
			default:
			}
		}
	}

	// 关闭消息通道，通知上层读取完毕
	close(t.msgCh)
}

// waitProcess 等待子进程退出并返回退出码，若无法获取则返回 nil。
func (t *SubprocessTransport) waitProcess() *int {
	if t.cmd == nil {
		return nil
	}
	// 忽略等待错误（进程可能已被 Kill）
	t.cmd.Wait() //nolint:errcheck
	if t.cmd.ProcessState != nil {
		code := t.cmd.ProcessState.ExitCode()
		return &code
	}
	return nil
}

// readStderr 持续读取 stderr，存入环形缓冲，并调用用户自定义回调。
func (t *SubprocessTransport) readStderr(pipe io.Reader) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()

		// 写入环形缓冲区：超出容量时丢弃最旧的一行
		t.stderrMu.Lock()
		if len(t.stderrBuf) >= t.stderrCap {
			t.stderrBuf = t.stderrBuf[1:]
		}
		t.stderrBuf = append(t.stderrBuf, line)
		t.stderrMu.Unlock()

		// 调用用户自定义 stderr 处理函数
		if t.options.Stderr != nil {
			t.options.Stderr(line)
		}
	}
}

// getStderrSnapshot 返回已捕获的 stderr 内容快照。
func (t *SubprocessTransport) getStderrSnapshot() string {
	t.stderrMu.Lock()
	defer t.stderrMu.Unlock()
	return strings.Join(t.stderrBuf, "\n")
}

// ReadMessages 实现 Transport 接口，返回消息只读通道。
// 通道在 stdout 读取完毕或连接关闭后自动关闭。
func (t *SubprocessTransport) ReadMessages() <-chan RawMessage {
	return t.msgCh
}

// Write 实现 Transport 接口：向 CLI stdin 写入一行 JSON。
func (t *SubprocessTransport) Write(ctx context.Context, data string) error {
	t.stdinMu.Lock()
	defer t.stdinMu.Unlock()
	if t.IsClosed() || t.stdinPipe == nil {
		return nil
	}
	// 使用 fmt.Fprintln 保证末尾有换行符
	_, err := fmt.Fprintln(t.stdinPipe, data)
	if err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("写入 CLI stdin 失败: %v", err)}
	}
	return nil
}

// Close 实现 Transport 接口：关闭连接并终止子进程。
// 幂等操作，多次调用安全。
func (t *SubprocessTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	// 发送关闭信号，通知内部 goroutine 退出
	close(t.closeCh)

	// 关闭 stdin 管道，触发 CLI 收到 EOF
	t.stdinMu.Lock()
	if t.stdinPipe != nil {
		t.stdinPipe.Close() //nolint:errcheck
		t.stdinPipe = nil
	}
	t.stdinMu.Unlock()

	// 强制终止子进程
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill() //nolint:errcheck
		t.cmd.Wait()         //nolint:errcheck
	}
	return nil
}

// IsClosed 实现 Transport 接口，返回连接是否已关闭。
func (t *SubprocessTransport) IsClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// IsReady 实现 Transport 接口，返回 CLI 是否已就绪（已产生首行有效输出）。
func (t *SubprocessTransport) IsReady() bool {
	return !t.IsClosed() && t.isCliReady.Load()
}

// OnNotification 实现 Transport 接口：注册指定 channel 的通知处理器。
func (t *SubprocessTransport) OnNotification(channel SubscriptionChannel, handler NotificationHandler) {
	t.notificationMu.Lock()
	defer t.notificationMu.Unlock()
	t.notificationHandlers[channel] = append(t.notificationHandlers[channel], handler)
}

// OffNotification 实现 Transport 接口：移除指定 channel 的通知处理器。
// 通过 unsafe.Pointer 提取函数指针值进行比较，避免依赖 reflect 包。
func (t *SubprocessTransport) OffNotification(channel SubscriptionChannel, handler NotificationHandler) {
	t.notificationMu.Lock()
	defer t.notificationMu.Unlock()
	handlers := t.notificationHandlers[channel]
	target := funcPointer(handler)
	for i, h := range handlers {
		if funcPointer(h) == target {
			t.notificationHandlers[channel] = append(handlers[:i], handlers[i+1:]...)
			if len(t.notificationHandlers[channel]) == 0 {
				delete(t.notificationHandlers, channel)
			}
			return
		}
	}
}

// funcPointer 提取函数值的代码指针，用于比较两个函数变量是否指向同一函数。
// Go 的函数值内部结构为一个指向 funcval 的指针，funcval 首字段是代码指针。
func funcPointer(fn NotificationHandler) uintptr {
	return **(**uintptr)(unsafe.Pointer(&fn))
}

// dispatchNotification 将 control_notification 消息分发给注册的处理器。
func (t *SubprocessTransport) dispatchNotification(data map[string]any) {
	channelStr, _ := data["channel"].(string)
	channel := SubscriptionChannel(channelStr)
	notification := ControlNotificationMessage{
		Channel: channel,
		Data:    data,
	}
	t.notificationMu.Lock()
	handlers := make([]NotificationHandler, len(t.notificationHandlers[channel]))
	copy(handlers, t.notificationHandlers[channel])
	t.notificationMu.Unlock()
	for _, h := range handlers {
		func(handler NotificationHandler) {
			defer func() { recover() }() //nolint:errcheck
			handler(notification)
		}(h)
	}
}

// SendControlRequestNoWait 实现 Transport 接口：发送控制请求，不等待响应。
func (t *SubprocessTransport) SendControlRequestNoWait(ctx context.Context, payload map[string]any) error {
	requestID := fmt.Sprintf("nowait_%p", &payload)
	request := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    payload,
	}
	b, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal control request: %w", err)
	}
	return t.Write(ctx, string(b))
}

// SDKMCPServerNames 实现 Transport 接口，返回已注册的 SDK MCP 服务器名称列表。
func (t *SubprocessTransport) SDKMCPServerNames() []string {
	return t.sdkMCPServerNames
}

// HandleMCPMessageRequest 处理 CLI 发来的 mcp_message 控制请求。
// 将消息路由到对应的 SdkMcpServer，并将响应写回 CLI。
func (t *SubprocessTransport) HandleMCPMessageRequest(ctx context.Context, requestID string, request map[string]any) {
	serverName, _ := request["server_name"].(string)

	// 将 request["message"] 序列化为 JSON 字节以传入 HandleMessage
	msgBytes, err := json.Marshal(request["message"])
	if err != nil {
		resp, _ := json.Marshal(BuildControlErrorResponse(requestID, fmt.Sprintf("marshal message failed: %v", err)))
		t.Write(context.Background(), string(resp)) //nolint:errcheck
		return
	}

	// 查找 SDK MCP 服务器
	server, ok := t.sdkMCPServers[serverName]
	if !ok {
		resp, _ := json.Marshal(BuildControlErrorResponse(requestID, fmt.Sprintf("SDK MCP server not found: %s", serverName)))
		t.Write(context.Background(), string(resp)) //nolint:errcheck
		return
	}

	// 调用服务器处理消息
	responseBytes, err := server.HandleMessage(ctx, msgBytes)
	if err != nil {
		resp, _ := json.Marshal(BuildControlErrorResponse(requestID, err.Error()))
		t.Write(context.Background(), string(resp)) //nolint:errcheck
		return
	}

	// notifications/initialized 等无响应通知，直接返回空成功响应
	if responseBytes == nil {
		resp, _ := json.Marshal(BuildControlResponse(requestID, map[string]any{}))
		t.Write(context.Background(), string(resp)) //nolint:errcheck
		return
	}

	// 将响应 JSON 反序列化后包装在控制响应信封中
	var parsedResponse any
	if err := json.Unmarshal(responseBytes, &parsedResponse); err != nil {
		resp, _ := json.Marshal(BuildControlErrorResponse(requestID, fmt.Sprintf("unmarshal response failed: %v", err)))
		t.Write(context.Background(), string(resp)) //nolint:errcheck
		return
	}

	resp, _ := json.Marshal(BuildControlResponse(requestID, map[string]any{
		"mcp_response": parsedResponse,
	}))
	t.Write(context.Background(), string(resp)) //nolint:errcheck
}

// buildArgs 根据 Options 构建 CLI 命令行参数列表。
// 完整对应 Python SDK 的 _build_args() 方法。
func (t *SubprocessTransport) buildArgs() []string {
	// 基础参数：流式 JSON 输入输出模式
	args := []string{
		"--input-format=stream-json",
		"--output-format=stream-json",
		"--verbose",
	}
	opts := t.options

	// 模型相关参数
	if opts.Model != nil {
		args = append(args, "--model", *opts.Model)
	}
	if opts.FallbackModel != nil {
		args = append(args, "--fallback-model", *opts.FallbackModel)
	}

	// 权限模式
	if opts.PermissionMode != nil {
		args = append(args, "--permission-mode", string(*opts.PermissionMode))
	}

	// 对话轮次限制
	if opts.MaxTurns != nil {
		args = append(args, "--max-turns", fmt.Sprintf("%d", *opts.MaxTurns))
	}

	// 会话管理参数
	if opts.SessionID != nil {
		args = append(args, "--session-id", *opts.SessionID)
	}
	if opts.ContinueConversation {
		args = append(args, "--continue")
	}
	if opts.Resume != nil {
		args = append(args, "--resume", *opts.Resume)
	}
	if opts.ForkSession {
		args = append(args, "--fork-session")
	}

	// 工具控制参数
	if len(opts.AllowedTools) > 0 {
		args = append(args, append([]string{"--allowedTools"}, opts.AllowedTools...)...)
	}
	if len(opts.DisallowedTools) > 0 {
		args = append(args, append([]string{"--disallowedTools"}, opts.DisallowedTools...)...)
	}
	if opts.Tools != nil {
		args = append(args, "--tools", strings.Join(opts.Tools, ","))
	}

	// MCP 服务器配置：过滤掉 McpSdkServerConfig 类型的服务器，只传常规服务器给 CLI
	if len(opts.MCPServers) > 0 {
		// 过滤掉 SDK 进程内服务器，构建常规服务器的 map[string]any 供序列化
		regular := make(map[string]any)
		for name, cfg := range opts.MCPServers {
			if cfg.mcpServerType() == "sdk" {
				continue
			}
			regular[name] = cfg
		}
		if len(regular) > 0 {
			b, _ := json.Marshal(map[string]any{"mcpServers": regular})
			args = append(args, "--mcp-config", string(b))
		}
	}

	// 配置来源：nil 默认使用 none（SDK 隔离模式），空切片也表示 none
	if opts.SettingSources != nil {
		if len(opts.SettingSources) == 0 {
			args = append(args, "--setting-sources", "none")
		} else {
			sources := make([]string, len(opts.SettingSources))
			for i, s := range opts.SettingSources {
				sources[i] = string(s)
			}
			args = append(args, "--setting-sources", strings.Join(sources, ","))
		}
	} else {
		args = append(args, "--setting-sources", "none")
	}

	// 流式部分消息
	if opts.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}

	// 系统提示：Override 覆盖整个系统提示，Append 追加到末尾
	if opts.SystemPrompt != nil {
		if opts.SystemPrompt.Override != nil {
			args = append(args, "--system-prompt", *opts.SystemPrompt.Override)
		} else if opts.SystemPrompt.Append != nil {
			args = append(args, "--append-system-prompt", *opts.SystemPrompt.Append)
		}
	}

	// 思考深度
	if opts.Effort != nil {
		args = append(args, "--effort", string(*opts.Effort))
	}

	// 思考模式：adaptive 或 enabled 时注入 alwaysThinkingEnabled=true 设置
	if opts.Thinking != nil && (opts.Thinking.Type == "adaptive" || opts.Thinking.Type == "enabled") {
		args = injectSetting(args, "alwaysThinkingEnabled", true)
	}

	// 用户自定义额外参数：nil value 表示仅传入 flag 不带值
	for flag, value := range opts.ExtraArgs {
		if value == nil {
			args = append(args, "--"+flag)
		} else {
			args = append(args, "--"+flag, *value)
		}
	}

	// 跳过权限确认（危险模式）
	if opts.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	// 预算限制
	if opts.MaxBudgetUsd != nil {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%g", *opts.MaxBudgetUsd))
	}

	// 会话持久化控制
	if opts.PersistSession != nil && !*opts.PersistSession {
		args = append(args, "--no-persist-session")
	}

	// 文件检查点
	if opts.EnableFileCheckpointing {
		args = append(args, "--enable-file-checkpointing")
	}

	// 沙箱设置
	if opts.Sandbox != nil && opts.Sandbox.Enabled != nil && !*opts.Sandbox.Enabled {
		args = append(args, "--sandbox=false")
	}

	// 认证环境
	if opts.Environment != nil {
		args = append(args, "--environment", *opts.Environment)
	}

	// 自定义端点
	if opts.Endpoint != nil {
		args = append(args, "--endpoint", *opts.Endpoint)
	}

	// 指定恢复位置
	if opts.ResumeSessionAt != nil {
		args = append(args, "--resume-session-at", *opts.ResumeSessionAt)
	}

	// 权限提示工具名
	if opts.PermissionPromptToolName != nil {
		args = append(args, "--permission-prompt-tool-name", *opts.PermissionPromptToolName)
	}

	// MCP 配置严格校验
	if opts.StrictMcpConfig {
		args = append(args, "--strict-mcp-config")
	}

	// 附加工作目录
	for _, dir := range opts.AdditionalDirectories {
		args = append(args, "--add-dir", dir)
	}

	// 用户透传的原始 CLI 参数（追加到末尾）
	args = append(args, opts.Args...)

	return args
}

// injectSetting 向 --settings JSON 注入键值对。
// 若参数列表中已存在 --settings，则合并进去；否则追加新的 --settings 参数。
func injectSetting(args []string, key string, value any) []string {
	for i, arg := range args {
		if arg == "--settings" && i+1 < len(args) {
			var settings map[string]any
			if err := json.Unmarshal([]byte(args[i+1]), &settings); err == nil {
				settings[key] = value
				b, _ := json.Marshal(settings)
				args[i+1] = string(b)
				return args
			}
		}
	}
	// 不存在时追加新的 --settings 参数
	b, _ := json.Marshal(map[string]any{key: value})
	return append(args, "--settings", string(b))
}

// buildEnv 构建子进程的环境变量列表。
// 在继承当前进程环境的基础上，注入 SDK 所需变量，并应用用户自定义变量。
func (t *SubprocessTransport) buildEnv(_ string) []string {
	// 继承当前进程环境，构建 map 方便覆盖
	envMap := make(map[string]string)
	for _, e := range os.Environ() {
		if k, v, ok := strings.Cut(e, "="); ok {
			envMap[k] = v
		}
	}

	// 标记该进程由 Go SDK 启动
	envMap["CODEBUDDY_CODE_ENTRYPOINT"] = "sdk-go"
	// 禁止子进程自动更新
	envMap["DISABLE_AUTOUPDATER"] = "1"
	// 禁用交互式 TUI，防止子进程将终端置于 raw 模式
	envMap["CI"] = "1"

	// 默认禁用自动内存（用户未明确设置时）
	if _, exists := envMap["CODEBUDDY_DISABLE_AUTO_MEMORY"]; !exists {
		envMap["CODEBUDDY_DISABLE_AUTO_MEMORY"] = "1"
	}

	// 思考 Token 预算
	if t.options.Thinking != nil && t.options.Thinking.Type == "enabled" && t.options.Thinking.BudgetTokens != nil {
		envMap["MAX_THINKING_TOKENS"] = fmt.Sprintf("%d", *t.options.Thinking.BudgetTokens)
	} else if t.options.MaxThinkingTokens != nil && *t.options.MaxThinkingTokens > 0 {
		envMap["MAX_THINKING_TOKENS"] = fmt.Sprintf("%d", *t.options.MaxThinkingTokens)
	}

	// 构建 SDK User-Agent 并注入自定义请求头
	goVersion := runtime.Version()
	sdkUA := fmt.Sprintf("User-Agent: CodeBuddy Agent SDK/%s (Go/%s) CodeBuddy Code/%s", Version, goVersion, t.cliVersion)
	if existing, exists := envMap["CODEBUDDY_CUSTOM_HEADERS"]; exists && existing != "" {
		// 追加到现有自定义头部之前
		envMap["CODEBUDDY_CUSTOM_HEADERS"] = sdkUA + "\n" + existing
	} else {
		envMap["CODEBUDDY_CUSTOM_HEADERS"] = sdkUA
	}

	// 应用用户自定义环境变量（最高优先级，可覆盖上述所有设置）
	for k, v := range t.options.Env {
		envMap[k] = v
	}

	// 将 map 转换回 []string 格式
	result := make([]string, 0, len(envMap))
	for k, v := range envMap {
		result = append(result, k+"="+v)
	}
	return result
}
