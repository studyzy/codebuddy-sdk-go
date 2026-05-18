// examples/interactive-cli/main.go
// 交互式 CLI 工具：综合演示 codebuddy-sdk-go SDK 的流式对话、
// AskUserQuestion 处理、打断控制、Session 管理和文件操作可视化等能力。

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/chzyer/readline"

	"github.com/studyzy/codebuddy-sdk-go"
)

// sessionState 保存单个会话的状态信息。
type sessionState struct {
	// id 会话 ID
	id string
}

// appState 持有 CLI 应用的全局状态。
type appState struct {
	client      *codebuddy.Client
	sessions    map[string]*sessionState
	currentSID  string
	mu          sync.Mutex // 保护 client 和 currentSID
	rl          *readline.Instance
	isStreaming atomic.Bool   // 是否正在等待 AI 响应
	doneCh      chan struct{} // receiveLoop 完成信号
}

// newAppState 创建初始化的 appState。
func newAppState(rl *readline.Instance) *appState {
	return &appState{
		sessions: make(map[string]*sessionState),
		rl:       rl,
	}
}

// printLine 线程安全地向终端输出一行，会自动清除当前输入行再重绘。
// readline 提供的 Stdout() 写入器保证了与输入行的协调。
func (app *appState) printLine(format string, a ...any) {
	fmt.Fprintf(app.rl.Stdout(), format, a...)
}

// connect 建立新的 SDK 连接，使用可选的会话恢复选项。
func (app *appState) connect(ctx context.Context, opts *codebuddy.Options) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.client != nil {
		app.client.Close() //nolint:errcheck
	}

	baseOpts := app.buildOptions(opts)
	client := codebuddy.NewClient(baseOpts)
	if err := client.Connect(ctx, nil); err != nil {
		return err
	}
	app.client = client
	return nil
}

// buildOptions 合并传入的选项与默认 Hook 配置。
func (app *appState) buildOptions(extra *codebuddy.Options) *codebuddy.Options {
	opts := &codebuddy.Options{}
	if extra != nil {
		opts.SessionID = extra.SessionID
		opts.ContinueConversation = extra.ContinueConversation
	}
	// 绕过所有权限检查，避免 AI 使用工具时因无 CanUseTool 回调而被默认拒绝
	mode := codebuddy.PermissionModeBypassPermissions
	opts.CanUseTool = func(ctx context.Context, toolName string, input map[string]any, o codebuddy.CanUseToolOptions) (codebuddy.PermissionResult, error) {
		if toolName == codebuddy.ToolAskUserQuestion {
			data, err := json.Marshal(input)
			if err != nil {
				return &codebuddy.PermissionResultDeny{Message: "解析问题失败", Interrupt: true}, nil
			}
			var q codebuddy.AskUserQuestionInput
			if err := json.Unmarshal(data, &q); err != nil {
				return &codebuddy.PermissionResultDeny{Message: "解析问题失败", Interrupt: true}, nil
			}

			// 展示问题，读取用户回答，将答案填入 UpdatedInput 回传给 AI
			answers := handleAskQuestion(app, q.Questions)
			updatedInput := make(map[string]any)
			for k, v := range input {
				updatedInput[k] = v
			}
			updatedInput["answers"] = answers
			return &codebuddy.PermissionResultAllow{UpdatedInput: updatedInput}, nil
		}
		return &codebuddy.PermissionResultAllow{}, nil
	}
	opts.PermissionMode = &mode
	opts.Hooks = map[codebuddy.HookEvent][]codebuddy.HookMatcher{
		codebuddy.HookPostToolUse: {
			{
				Matcher: nil,
				Hooks:   []codebuddy.HookCallback{app.fileOpHook},
			},
		},
	}
	return opts
}

// fileOpHook 文件操作可视化 Hook 回调，在工具调用完成后触发。
func (app *appState) fileOpHook(_ context.Context, input map[string]any, _ *string) (codebuddy.HookJSONOutput, error) {
	t := true
	toolName, _ := input["tool_name"].(string)
	toolInput, _ := input["tool_input"].(map[string]any)
	if toolInput == nil {
		return codebuddy.HookJSONOutput{Continue: &t}, nil
	}

	filePath, _ := toolInput["file_path"].(string)
	if filePath == "" {
		filePath, _ = toolInput["notebook_path"].(string)
	}
	content, _ := toolInput["content"].(string)
	if content == "" {
		content, _ = toolInput["new_string"].(string)
	}

	fileOps := map[string]bool{
		codebuddy.ToolWrite:        true,
		codebuddy.ToolEdit:         true,
		codebuddy.ToolMultiEdit:    true,
		codebuddy.ToolRead:         true,
		codebuddy.ToolNotebookEdit: true,
	}
	if !fileOps[toolName] || filePath == "" {
		return codebuddy.HookJSONOutput{Continue: &t}, nil
	}

	boxWidth := 50
	header := fmt.Sprintf("┌─ 📁 [%s] ", toolName)
	padding := max(boxWidth-len([]rune(header))-1, 0)
	headerLine := header + strings.Repeat("─", padding) + "┐"

	lines := []string{headerLine, fmt.Sprintf("│ 路径: %s", filePath)}
	if content != "" {
		lines = append(lines, "│ 内容:")
		contentLines := strings.Split(content, "\n")
		total := len(contentLines)
		if total > 20 {
			contentLines = contentLines[:20]
		}
		for _, cl := range contentLines {
			lines = append(lines, "│   "+cl)
		}
		if total > 20 {
			lines = append(lines, fmt.Sprintf("│   ... (共 %d 行，已截断)", total))
		}
	}
	lines = append(lines, "└"+strings.Repeat("─", boxWidth-1)+"┘")

	app.printLine("\n")
	for _, l := range lines {
		app.printLine("%s\n", l)
	}
	return codebuddy.HookJSONOutput{Continue: &t}, nil
}

// receiveLoop 在独立 goroutine 中运行，持续读取 SDK 消息。
func receiveLoop(ctx context.Context, app *appState) {
	defer close(app.doneCh)
	app.isStreaming.Store(true)
	defer app.isStreaming.Store(false)

	app.mu.Lock()
	client := app.client
	app.mu.Unlock()

	for msg := range client.ReceiveMessages() {
		switch m := msg.(type) {
		case *codebuddy.AssistantMessage:
			for _, block := range m.Content {
				switch b := block.(type) {
				case *codebuddy.TextBlock:
					app.printLine("%s", b.Text)
				case *codebuddy.ThinkingBlock:
					app.printLine("\n💭 [思考]\n%s\n", b.Thinking)
				case *codebuddy.ToolUseBlock:
					inputJSON, _ := json.MarshalIndent(b.Input, "  ", "  ")
					app.printLine("\n🔧 [工具调用] %s (id: %s)\n  输入: %s\n", b.Name, b.ID, string(inputJSON))
				case *codebuddy.ToolResultBlock:
					resultStr := ""
					switch v := b.Content.(type) {
					case string:
						resultStr = v
					default:
						if data, err := json.MarshalIndent(v, "  ", "  "); err == nil {
							resultStr = string(data)
						}
					}
					errFlag := ""
					if b.IsError != nil && *b.IsError {
						errFlag = " ❌"
					}
					app.printLine("\n📤 [工具结果]%s (tool_use_id: %s)\n  %s\n", errFlag, b.ToolUseID, resultStr)
				}
			}

		case *codebuddy.ResultMessage:
			sid := client.GetSessionID()
			app.mu.Lock()
			if sid != "" {
				app.currentSID = sid
				app.sessions[sid] = &sessionState{id: sid}
			}
			app.mu.Unlock()
			app.printLine("\n[Session: %s] [耗时: %dms]\n", sid, m.DurationMs)
			return

		case *codebuddy.ErrorMessage:
			app.printLine("\n❌ 错误: %s\n", m.Error)
			return
		}
	}
}

// handleAskQuestion 展示 AI 问题并读取用户回答，返回 map[string]string（key 为问题索引）。
// 支持多个问题，用户可以用空格分隔多个答案一次性输入。
func handleAskQuestion(app *appState, questions []codebuddy.AskUserQuestionQuestion) map[string]string {
	answers := make(map[string]string)
	app.printLine("\n╔══ 🤔 AI 有问题要问你 ══╗\n")
	for i, question := range questions {
		if question.Header != "" {
			app.printLine("║ 【%s】\n", question.Header)
		}
		app.printLine("║ %d. %s\n", i+1, question.Question)
		if len(question.Options) > 0 {
			app.printLine("║ 选项:\n")
			for j, opt := range question.Options {
				if opt.Description != "" {
					app.printLine("║   %d) %s - %s\n", j+1, opt.Label, opt.Description)
				} else {
					app.printLine("║   %d) %s\n", j+1, opt.Label)
				}
			}
		}
	}
	app.printLine("╚══════════════════════════╝\n")

	// 提示用户输入
	if len(questions) == 1 {
		app.rl.SetPrompt("你的回答 > ")
	} else {
		app.rl.SetPrompt(fmt.Sprintf("请依次回答 %d 个问题（空格分隔）> ", len(questions)))
	}
	line, _ := app.rl.Readline()
	app.rl.SetPrompt("> ")
	line = strings.TrimSpace(line)

	// 解析用户输入，按空格分隔
	inputs := strings.Fields(line)

	// 处理每个问题的答案
	for i, question := range questions {
		var ans string
		if i < len(inputs) {
			ans = inputs[i]
		}
		// 如果有选项且用户输入了数字序号，转换为对应的 label
		if len(question.Options) > 0 {
			var idx int
			if n, err := fmt.Sscanf(ans, "%d", &idx); n == 1 && err == nil && idx >= 1 && idx <= len(question.Options) {
				ans = question.Options[idx-1].Label
			}
		}
		answers[fmt.Sprintf("%d", i)] = ans
	}
	return answers
}

// sendMessage 向 AI 发送消息并启动 receiveLoop。
func sendMessage(ctx context.Context, app *appState, msg string) {
	app.mu.Lock()
	client := app.client
	app.mu.Unlock()

	if err := client.Send(ctx, msg); err != nil {
		app.printLine("❌ 发送失败: %v\n", err)
		close(app.doneCh)
		return
	}
	go receiveLoop(ctx, app)
}

// printHelp 打印帮助信息。
func printHelp(app *appState) {
	app.printLine(`
╭─ 帮助 ──────────────────────────────────────────────╮
│  命令列表：                                           │
│  /q            打断当前 AI 响应                       │
│  /new          创建新会话                             │
│  /session <id> 恢复指定会话（自动处理待回答问题）     │
│  /help         显示此帮助                             │
│  /exit 或 exit 退出程序                               │
│  其他内容      发送给 AI                              │
╰──────────────────────────────────────────────────────╯
`)
}

func main() {
	ctx := context.Background()

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "> ",
		HistoryFile:     "/tmp/codebuddy-cli-history",
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 初始化终端失败: %v\n", err)
		os.Exit(1)
	}
	defer rl.Close()

	app := newAppState(rl)

	if err := app.connect(ctx, nil); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 连接失败: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		app.mu.Lock()
		c := app.client
		app.mu.Unlock()
		if c != nil {
			c.Close() //nolint:errcheck
		}
	}()

	app.mu.Lock()
	sid := app.client.GetSessionID()
	app.mu.Unlock()

	app.printLine("🚀 CodeBuddy 交互式 CLI 已启动（输入 /help 查看命令）\n")
	app.printLine("[Session: %s]\n", sid)

	// 初始化已关闭的 doneCh，使主循环可以立即进入提示符
	app.doneCh = make(chan struct{})
	close(app.doneCh)

	for {
		// 等待当前响应完成
		<-app.doneCh

		// 读取用户输入（readline 处理原始模式、历史记录、Ctrl+C 等）
		line, err := rl.Readline()
		if err != nil { // io.EOF 或 readline.ErrInterrupt
			app.printLine("👋 再见！\n")
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			app.doneCh = make(chan struct{})
			close(app.doneCh)
			continue
		}

		switch {
		case line == "/exit" || line == "exit":
			app.printLine("👋 再见！\n")
			return

		case line == "/help":
			printHelp(app)
			app.doneCh = make(chan struct{})
			close(app.doneCh)

		case line == "/q":
			app.mu.Lock()
			client := app.client
			app.mu.Unlock()
			if client != nil && app.isStreaming.Load() {
				if err := client.Interrupt(ctx); err != nil {
					app.printLine("❌ 发送打断信号失败: %v\n", err)
				} else {
					app.printLine("⚡ 已发送打断信号\n")
				}
				// 等待 receiveLoop 自然结束，不重置 doneCh
			} else {
				app.printLine("当前无进行中的响应\n")
				app.doneCh = make(chan struct{})
				close(app.doneCh)
			}

		case line == "/new":
			if err := app.connect(ctx, nil); err != nil {
				app.printLine("❌ 创建新会话失败: %v\n", err)
			} else {
				app.mu.Lock()
				sid := app.client.GetSessionID()
				app.mu.Unlock()
				app.printLine("✨ 新会话已创建 [Session: %s]\n", sid)
			}
			app.doneCh = make(chan struct{})
			close(app.doneCh)

		case strings.HasPrefix(line, "/session "):
			id := strings.TrimSpace(strings.TrimPrefix(line, "/session "))
			if id == "" {
				app.printLine("⚠️  用法: /session <session-id>\n")
				app.doneCh = make(chan struct{})
				close(app.doneCh)
			} else {
				// 先重连（建立新的 client 连接，用于后续普通对话）
				opts := &codebuddy.Options{SessionID: &id}
				if err := app.connect(ctx, opts); err != nil {
					app.printLine("❌ 恢复会话失败: %v\n", err)
					app.doneCh = make(chan struct{})
					close(app.doneCh)
				} else {
					app.mu.Lock()
					app.currentSID = id
					app.mu.Unlock()
					app.printLine("🔄 已恢复会话 [Session: %s]\n", id)

					app.doneCh = make(chan struct{})
					close(app.doneCh)
				}
			}

		default:
			app.doneCh = make(chan struct{})
			sendMessage(ctx, app, line)
		}
	}
}
