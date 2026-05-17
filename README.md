# CodeBuddy Golang SDK

CodeBuddy Golang SDK 允许您在 Go 应用程序中集成 CodeBuddy 的 AI 能力。通过此 SDK，您可以轻松实现单次查询、管理多轮对话、配置自定义权限钩子 (Hooks) 以及扩展进程内 MCP 服务器。

## 安装

在您的 Go 项目中，使用 `go get` 安装 SDK：

```bash
go get github.com/studyzy/codebuddy-sdk-go
```

> **注意**：确保您的环境中已安装 CodeBuddy CLI 可执行文件，SDK 将通过子进程与之通信。

## 快速开始

以下是一个简单的单次查询示例：

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/studyzy/codebuddy-sdk-go"
)

func main() {
	ctx := context.Background()

	// 发起单次查询
	msgCh, err := codebuddy.Query(ctx, "帮我写一个 Go 语言的 Hello World", nil)
	if err != nil {
		log.Fatal(err)
	}

	// 处理返回的消息流
	for msg := range msgCh {
		switch m := msg.(type) {
		case *codebuddy.AssistantMessage:
			for _, block := range m.Content {
				if tb, ok := block.(*codebuddy.TextBlock); ok {
					fmt.Print(tb.Text) // 打印 AI 生成的内容
				}
			}
		case *codebuddy.ResultMessage:
			fmt.Printf("\n[查询完成] 耗时: %dms\n", m.DurationMs)
		case *codebuddy.ErrorMessage:
			log.Fatalf("发生错误: %s", m.Error)
		}
	}
}
```

## 身份认证

SDK 支持两阶段认证流程：获取登录 URL 并等待用户授权。

```go
func handleAuth(ctx context.Context) {
	// 启动认证流程
	flow, err := codebuddy.Authenticate(ctx, &codebuddy.AuthOptions{
		Timeout: 300, // 等待用户登录的超时时间（秒）
	})
	if err != nil {
		log.Fatal(err)
	}

	if flow.AuthURL != "" {
		fmt.Printf("请访问以下链接完成登录: %s\n", flow.AuthURL)
	}

	// 阻塞等待认证结果
	result, err := flow.Wait(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("登录成功，用户: %s\n", result.UserInfo.UserName)
}

// 登出
func handleLogout(ctx context.Context) {
	err := codebuddy.Logout(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
}
```

## 多轮对话 (Client)

使用 `Client` 可以精细化管理连接生命周期，支持保持上下文的连续对话。

```go
func multiTurnConversation(ctx context.Context) {
	client := codebuddy.NewClient(nil)
	if err := client.Connect(ctx, nil); err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// 第一轮
	client.Send(ctx, "谁是世界上最伟大的程序员？")
	// ... 接收消息 (参考快速开始中的消息循环)

	// 第二轮（基于上下文）
	client.Send(ctx, "他有哪些著名的贡献？")
	// ... 接收消息
}
```

## 高级配置 (Options)

`codebuddy.Options` 结构体提供了丰富的配置项，用于控制 CLI 行为：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `Model` | `*string` | 指定主模型名称。 |
| `CLIPath` | `*string` | CLI 可执行文件的绝对路径。 |
| `SystemPrompt` | `*SystemPromptConfig` | 覆盖或追加全局系统提示词。 |
| `Agents` | `map[string]AgentDefinition` | 定义子 Agent 及其能力。 |
| `MCPServers` | `any` | 配置 MCP 服务器（支持 stdio, http, sse, sdk 等）。 |
| `PermissionMode` | `*PermissionMode` | 权限模式：`default`, `acceptEdits`, `bypassPermissions` 等。 |
| `CanUseTool` | `CanUseToolFunc` | 自定义工具执行权限控制回调。 |
| `Hooks` | `map[HookEvent][]HookMatcher` | 注册事件钩子（如工具调用前后的干预）。 |

### 权限控制回调 (CanUseTool)

您可以拦截工具调用并决定是否允许：

```go
opts := &codebuddy.Options{
    CanUseTool: func(ctx context.Context, toolName string, input map[string]any, opts codebuddy.CanUseToolOptions) (codebuddy.PermissionResult, error) {
        if toolName == "Bash" && strings.Contains(input["command"].(string), "rm") {
            return &codebuddy.PermissionResultDeny{Message: "禁止执行删除命令"}, nil
        }
        return &codebuddy.PermissionResultAllow{}, nil
    },
}
```

### 钩子 (Hooks)

Hook 允许您在特定事件发生时执行自定义逻辑：

```go
opts := &codebuddy.Options{
    Hooks: map[codebuddy.HookEvent][]codebuddy.HookMatcher{
        codebuddy.HookPreToolUse: {{
            Matcher: nil, // 匹配所有工具
            Hooks: []codebuddy.HookCallback{
                func(ctx context.Context, input map[string]any, id *string) (codebuddy.HookJSONOutput, error) {
                    fmt.Println("准备执行工具...")
                    return codebuddy.HookJSONOutput{}, nil
                },
            },
        }},
    },
}
```

## 核心类型参考

### 消息类型 (Message)

所有消息均实现 `Message` 接口。常见类型包括：
- `*AssistantMessage`: 包含 AI 生成的内容块（`ContentBlock`）。
- `*ResultMessage`: 包含会话最终状态、统计信息及结果字符串。
- `*ErrorMessage`: 包含错误描述。
- `*UserMessage`: 表示用户输入的消息。

### 内容块 (ContentBlock)

`AssistantMessage` 中的 `Content` 字段由多个块组成：
- `*TextBlock`: 纯文本。
- `*ThinkingBlock`: 模型的思考过程。
- `*ToolUseBlock`: AI 发起的工具调用请求。
- `*ToolResultBlock`: 工具执行后的结果反馈。

---

更多详细示例，请参阅项目中的 `examples/` 目录。
