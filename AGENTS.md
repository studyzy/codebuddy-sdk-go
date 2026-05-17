# AGENTS.md - AI 编程助手开发指南

本文件指导 AI 编程工具（CodeBuddy、Cursor、Copilot 等）在本项目中进行开发时
应遵循的规范和约束。

## 项目概述

这是 CodeBuddy 的官方 Go SDK (`github.com/studyzy/codebuddy-sdk-go`)，
通过子进程或 HTTP 与 CodeBuddy CLI 通信，为 Go 应用提供 AI 编程能力。

- **语言**: Go 1.24+
- **包名**: `codebuddy`（单一扁平包，无子包）
- **模块路径**: `github.com/studyzy/codebuddy-sdk-go`
- **外部依赖**: 仅 `github.com/chzyer/readline`（用于 examples）

## 架构约束

### 分层架构

```text
Public API Layer
  ├── Query()              一次性查询入口 (codebuddy.go)
  ├── Client               遗留全功能入口 (client.go)
  └── Session              推荐的多轮对话管理 (session.go)
        │
        ▼
  connCore                 共享连接核心 (conn_core.go)
        │
        ▼
  Transport (interface)    抽象通信层 (transport.go)
  ├── SubprocessTransport  CLI 子进程通信 (transport_subprocess.go)
  └── httpACPTransport     HTTP/ACP 通信 (transport_http.go)
```

### 文件职责

| 文件 | 职责 | 备注 |
|------|------|------|
| `codebuddy.go` | 包入口，`Query()` 函数 | 保持简洁 |
| `client.go` | Client 结构体，工厂方法 | 仅 Client 特有逻辑 |
| `session.go` | Session 结构体，多轮对话 | 仅 Session 特有逻辑 |
| `conn_core.go` | Client/Session 共享的连接管理 | 不可导出的 connCore 类型 |
| `transport.go` | Transport 接口定义 + 合规性断言 | 小接口原则 |
| `transport_subprocess.go` | 子进程通信实现 | |
| `transport_http.go` | HTTP 通信实现 | |
| `types_message.go` | Message/ContentBlock 类型 | |
| `types_options.go` | Options/SessionOptions 配置 | |
| `types_mcp.go` | MCP Server 配置类型 | |
| `types_hooks.go` | Hooks/CanUseTool 类型 | |
| `types_enums.go` | 枚举常量 | |
| `auth.go` | 认证流程 | |
| `protocol.go` | 控制协议辅助函数 | |
| `message_parser.go` | JSON 消息解析 | |
| `mcp.go` | 进程内 MCP Server | |
| `plugin.go` | 插件/市场管理 | |
| `acp_client.go` | ACP 高级客户端 | |
| `binary.go` | CLI 二进制发现 | |
| `errors.go` | 自定义错误类型 | |
| `tools.go` | 工具名称常量 | |
| `session_lock.go` | 会话锁 | |
| `version.go` | 版本常量 | |

## 编码规范（必须遵守）

### 1. Go 语言规范

- 代码必须通过 `go vet` 和 `golangci-lint`，零警告
- 代码格式必须使用 `gofmt` / `goimports`
- 错误处理使用显式返回 error，禁止用 panic 处理业务逻辑
- 接口设计遵循"小接口"原则
- 并发代码必须正确使用 context 传播、goroutine 生命周期管理
- 所有 Transport 接口实现必须有编译时合规性断言：
  ```go
  var _ Transport = (*SubprocessTransport)(nil)
  ```

### 2. 注释规范

- 所有导出的类型、函数、方法、常量必须有 godoc 注释
- godoc 注释第一行必须以被注释的标识符名称开头（Go 规范要求）
- 后续说明使用中文
- 复杂业务逻辑必须有行内中文注释
- 示例：
  ```go
  // Client 是 CodeBuddy SDK 的入口对象，持有全局配置并提供 Session 工厂方法。
  // 通过 NewClient 创建实例，支持连接管理和多轮对话。
  type Client struct { ... }

  // NewClient 创建一个新的 Client 实例。
  // opts 为 nil 时使用默认配置。
  func NewClient(opts *Options) *Client { ... }
  ```

### 3. 测试规范

- 所有新增或修改的公共函数/方法必须编写对应的单元测试
- 测试覆盖率必须 >= 60%
- 使用标准 `testing` 包，禁止引入第三方测试框架
- 优先使用 table-driven tests 模式
- Mock 对象通过接口注入（参考 `mock_transport_test.go`）
- 测试文件与被测文件同包同目录，命名 `*_test.go`
- 测试必须可独立运行，不依赖外部服务
- 集成测试使用 `//go:build integration` 构建标签隔离

### 4. API 兼容性

- 禁止破坏现有导出 API（类型、函数、方法签名）
- 新增 API 必须保持最小化，只暴露用户真正需要的功能
- 破坏性变更必须递增主版本号（语义化版本）

### 5. 依赖管理

- 外部依赖必须保持最小化
- 新增依赖需要明确说明必要性
- 优先使用标准库

### 6. 错误消息

- 错误消息统一使用中文（面向中文开发者）
- 错误类型定义在 `errors.go` 中

## 开发工作流

### 修改代码前

1. 确认理解当前架构分层
2. 确认修改不会破坏现有导出 API
3. 确认新增代码放入正确的文件（按职责划分）

### 修改代码后

必须依次通过以下检查：

```bash
# 1. 静态分析
go vet ./...

# 2. 编译检查（包括 examples）
go build ./...
go build ./examples/...

# 3. 单元测试
go test -timeout 60s ./...

# 4. 覆盖率检查
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1
# 确认总覆盖率 >= 60%
```

### 提交规范

使用 Conventional Commits 格式：

- `feat:` 新功能
- `fix:` Bug 修复
- `docs:` 文档更新
- `test:` 测试相关
- `refactor:` 代码重构
- `chore:` 构建/工具变更

## 常见任务指引

### 新增公共函数

1. 在对应职责的文件中添加函数
2. 添加符合规范的 godoc 注释（标识符名称开头 + 中文说明）
3. 在对应的 `*_test.go` 中添加测试
4. 运行完整验证流程

### 新增类型

1. 根据类型职责选择正确的文件：
   - 消息相关 → `types_message.go`
   - 配置相关 → `types_options.go`
   - MCP 相关 → `types_mcp.go`
   - Hooks 相关 → `types_hooks.go`
   - 枚举常量 → `types_enums.go`
2. 添加 godoc 注释
3. 如实现接口，添加编译时合规性断言

### 修改 Transport 层

1. 确保修改不破坏 Transport 接口合约
2. SubprocessTransport 和 httpACPTransport 必须保持接口合规性
3. 测试使用 `mock_transport_test.go` 中的 mock 实现

### 新增错误类型

1. 定义在 `errors.go` 中
2. 错误消息使用中文
3. 实现 `error` 接口

## 禁止事项

- 禁止引入子包（保持单一 `codebuddy` 包）
- 禁止修改 `go.mod` 的 module 路径
- 禁止使用 `reflect` 进行函数指针比较
- 禁止使用 `panic` 处理业务逻辑
- 禁止引入第三方测试框架
- 禁止在单个文件中超过 500 行代码
- 禁止在 Client 和 Session 之间复制粘贴逻辑（应放入 connCore）
