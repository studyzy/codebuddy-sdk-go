package codebuddy

// =============================================================================
// MCP 配置类型
// =============================================================================

// MCPServerConfig 是所有 MCP 服务器配置的公共接口。
// 实现此接口的类型：McpStdioServerConfig、McpHttpServerConfig、
// McpSseServerConfig、McpRemoteServerConfig、McpSdkServerConfig。
type MCPServerConfig interface {
	mcpServerType() string
}

// MCPServerMap 是 MCP 服务器名称到配置的映射类型，用于 Options.MCPServers。
type MCPServerMap map[string]MCPServerConfig

// McpToolConfig 表示 MCP 工具级别配置。
type McpToolConfig struct {
	// DeferLoading 表示是否延迟加载该工具。
	DeferLoading bool
}

// TextContent 表示 MCP 工具返回的文本内容。
type TextContent struct {
	// Type 是内容类型标识。
	Type string `json:"type"`
	// Text 是文本内容。
	Text string `json:"text"`
}

// ImageContent 表示 MCP 工具返回的图片内容。
type ImageContent struct {
	// Type 是内容类型标识。
	Type string `json:"type"`
	// Data 是 Base64 编码的图片数据。
	Data string `json:"data"`
	// MIMEType 是图片的 MIME 类型。
	MIMEType string `json:"mimeType"`
}

// EmbeddedResource 表示 MCP 工具返回的内嵌资源。
type EmbeddedResource struct {
	// Type 是内容类型标识。
	Type string `json:"type"`
	// Resource 是资源详情。
	Resource map[string]any `json:"resource"`
}

// CallToolResult 表示 MCP tools/call 的标准返回体。
type CallToolResult struct {
	// Content 是返回内容列表。
	Content []any `json:"content,omitempty"`
	// IsError 表示是否为错误结果。
	IsError bool `json:"isError,omitempty"`
}

// McpStdioServerConfig 表示 MCP stdio 服务器配置。
type McpStdioServerConfig struct {
	// Type 是服务器类型，固定为 "stdio"（可省略）。
	Type string
	// Command 是启动服务器的命令。
	Command string
	// Args 是启动命令的参数列表。
	Args []string
	// Env 是进程环境变量。
	Env map[string]string
	// Description 是服务器功能描述。
	Description string
	// DeferLoading 表示是否延迟加载该服务器。
	DeferLoading bool
	// Tools 是工具级别配置映射。
	Tools map[string]McpToolConfig
}

// mcpServerType 返回 MCP 服务器类型标识。
func (c McpStdioServerConfig) mcpServerType() string { return "stdio" }

// McpHttpServerConfig 表示 MCP HTTP 服务器配置。
type McpHttpServerConfig struct {
	// Type 是服务器类型，固定为 "http"。
	Type string
	// URL 是服务器地址。
	URL string
	// Headers 是 HTTP 请求头。
	Headers map[string]string
	// Description 是服务器功能描述。
	Description string
	// DeferLoading 表示是否延迟加载该服务器。
	DeferLoading bool
	// Tools 是工具级别配置映射。
	Tools map[string]McpToolConfig
}

// mcpServerType 返回 MCP 服务器类型标识。
func (c McpHttpServerConfig) mcpServerType() string { return "http" }

// McpSseServerConfig 表示 MCP SSE 服务器配置。
type McpSseServerConfig struct {
	// Type 是服务器类型，固定为 "sse"。
	Type string
	// URL 是服务器地址。
	URL string
	// Headers 是 HTTP 请求头。
	Headers map[string]string
	// Description 是服务器功能描述。
	Description string
	// DeferLoading 表示是否延迟加载该服务器。
	DeferLoading bool
	// Tools 是工具级别配置映射。
	Tools map[string]McpToolConfig
}

// mcpServerType 返回 MCP 服务器类型标识。
func (c McpSseServerConfig) mcpServerType() string { return "sse" }

// McpRemoteServerConfig 表示 MCP remote 服务器配置。
type McpRemoteServerConfig struct {
	// Type 是服务器类型，固定为 "remote"。
	Type string
	// URL 是服务器地址。
	URL string
	// Headers 是 HTTP 请求头。
	Headers map[string]string
	// Description 是服务器功能描述。
	Description string
	// DeferLoading 表示是否延迟加载该服务器。
	DeferLoading bool
	// Tools 是工具级别配置映射。
	Tools map[string]McpToolConfig
}

// mcpServerType 返回 MCP 服务器类型标识。
func (c McpRemoteServerConfig) mcpServerType() string { return "remote" }

// McpSdkServerConfig 表示 MCP SDK 进程内服务器配置。
// 该类型的服务器由 SDK 直接在进程内处理，不启动外部子进程。
type McpSdkServerConfig struct {
	// Type 是服务器类型，固定为 "sdk"。
	Type string
	// Name 是 SDK MCP server 的名称。
	Name string
	// Server 是 SDK MCP 服务器实例。
	Server *SdkMcpServer
	// Description 是可选的服务器功能描述。
	Description string
	// DeferLoading 表示是否延迟加载。
	DeferLoading bool
}

// mcpServerType 返回 MCP 服务器类型标识。
func (c McpSdkServerConfig) mcpServerType() string { return "sdk" }
