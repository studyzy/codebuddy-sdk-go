# CodeBuddy Golang SDK

CodeBuddy Golang SDK 允许您在 Go 应用程序中集成 CodeBuddy 的 AI 能力。通过此 SDK，您可以轻松实现单次查询、管理多轮对话、配置自定义权限钩子 (Hooks) 以及扩展进程内 MCP 服务器。

## 设计原则

本项目遵循以下核心设计原则，确保代码质量和长期可维护性：

### Golang 规范优先

- 代码必须通过 `go vet` 和 `golangci-lint` 检查，零警告
- 所有导出符号必须有符合 godoc 规范的注释（第一行以符号名称开头）
- 错误处理使用 Go 惯用模式：显式返回 error，不使用 panic 处理业务逻辑
- 代码格式统一使用 `gofmt` / `goimports`
- 接口遵循"小接口"原则，优先定义在消费方
- 并发代码正确使用 context、goroutine 和 channel

### 测试要求

- 所有新增或修改的公共函数/方法必须编写对应的单元测试
- 测试覆盖率必须达到 60% 以上
- 使用标准 `testing` 库 + table-driven tests 模式
- Mock 对象通过接口注入实现
- 集成测试使用 `//go:build integration` 构建标签隔离

### 简洁与可维护性

- YAGNI：不为假设的未来需求增加复杂度
- 公共 API 表面积最小化，只暴露用户真正需要的功能
- 避免过度抽象，依赖最小化，优先使用标准库

## 架构概览

```text
┌───────────────────────────────────────────────────┐
│                 Public API Layer                   │
│                                                    │
│  Query()          Client          Session          │
│  (一次性查询)    (遗留入口)     (推荐多轮对话)      │
│                     │               │              │
│                     └───┬───────────┘              │
│                         │ embeds                   │
│                         ▼                          │
│                    connCore                        │
│              (共享连接核心逻辑)                      │
│                         │                          │
│                         ▼                          │
│                   Transport                        │
│                   (接口层)                          │
│                    ╱       ╲                       │
│        Subprocess        HTTP/ACP                  │
│        Transport         Transport                 │
│       (CLI 子进程)     (HTTP 通信)                  │
└───────────────────────────────────────────────────┘
```

### 核心模块

| 模块 | 文件 | 职责 |
|:-----|:-----|:-----|
| 包入口 | `codebuddy.go` | `Query()` 一次性查询 |
| 客户端 | `client.go` | Client 工厂和遗留 API |
| 会话 | `session.go` | Session 多轮对话管理 |
| 连接核心 | `conn_core.go` | Client/Session 共享的连接管理逻辑 |
| 传输层 | `transport.go`, `transport_subprocess.go`, `transport_http.go` | Transport 接口及实现 |
| 消息类型 | `types_message.go` | Message/ContentBlock 类型定义 |
| 配置选项 | `types_options.go` | Options/SessionOptions 等 |
| MCP | `mcp.go`, `types_mcp.go` | 进程内 MCP Server |
| Hooks | `types_hooks.go` | 事件钩子类型 |
| 认证 | `auth.go` | 两阶段认证流程 |
| 协议 | `protocol.go`, `message_parser.go` | 控制协议和消息解析 |
| 错误 | `errors.go` | 错误类型定义 |

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

## 开发指南

### 构建与测试

```bash
# 编译
make build

# 运行测试
make test

# 覆盖率报告
make cover

# 静态分析
make vet
make lint
```

### 提交规范

提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范：

- `feat:` 新功能
- `fix:` Bug 修复
- `docs:` 文档更新
- `test:` 测试相关
- `refactor:` 代码重构

### 质量门禁

所有提交必须满足：

1. `go vet ./...` 零警告
2. `golangci-lint run ./...` 零警告
3. `go test ./...` 全部通过
4. 测试覆盖率 >= 60%

---

更多详细示例，请参阅项目中的 `examples/` 目录。
