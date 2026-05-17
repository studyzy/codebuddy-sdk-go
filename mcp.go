// mcp.go
// 进程内 JSON-RPC 2.0 MCP 服务器实现
// 支持通过 Options.MCPServers 注册 SDK 定义的工具，由 SubprocessTransport 路由消息。

package codebuddy

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolHandler 处理工具调用，接受输入参数 args，返回 *CallToolResult 或 error。
// 若返回 error，SDK 将自动构造 isError=true 的文本错误响应。
// 若希望返回富内容（图片、资源等），可在 CallToolResult.Content 中放置
// *TextContent / *ImageContent / *EmbeddedResource 等值。
type ToolHandler func(ctx context.Context, args map[string]any) (*CallToolResult, error)

// SimpleToolHandler 是仅返回文本的简化版 handler 类型。
// 可通过 WrapSimpleHandler 转换为 ToolHandler。
type SimpleToolHandler func(ctx context.Context, args map[string]any) (string, error)

// WrapSimpleHandler 将 SimpleToolHandler 包装为 ToolHandler，
// 返回值自动封装为单条 TextContent。
func WrapSimpleHandler(h SimpleToolHandler) ToolHandler {
	return func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		text, err := h(ctx, args)
		if err != nil {
			return nil, err
		}
		return &CallToolResult{
			Content: []any{&TextContent{Type: "text", Text: text}},
		}, nil
	}
}

// ToolDefinition 定义一个 MCP 工具。
type ToolDefinition struct {
	// Name 工具名称
	Name string
	// Description 工具功能描述
	Description string
	// InputSchema JSON Schema 对象，描述工具的输入参数
	InputSchema map[string]any
	// Handler 工具调用处理函数
	Handler ToolHandler
}

// SdkMcpServer 是一个进程内 JSON-RPC 2.0 MCP 服务器。
// 通过 RegisterTool 注册工具，HandleMessage 处理来自 CLI 的 MCP 消息。
type SdkMcpServer struct {
	name    string
	version string
	tools   map[string]*ToolDefinition
}

// NewSdkMcpServer 创建一个新的 SdkMcpServer 实例。
func NewSdkMcpServer(name, version string) *SdkMcpServer {
	return &SdkMcpServer{
		name:    name,
		version: version,
		tools:   make(map[string]*ToolDefinition),
	}
}

// RegisterTool 向服务器注册一个工具。
func (s *SdkMcpServer) RegisterTool(def *ToolDefinition) {
	s.tools[def.Name] = def
}

// jsonRPCRequest JSON-RPC 2.0 请求结构体（仅用于解析）
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// HandleMessage 处理一条 JSON-RPC 2.0 消息（原始 JSON 字节），返回响应 JSON 字节。
// 对于 notifications/initialized（通知类）返回 nil, nil，不需要响应。
func (s *SdkMcpServer) HandleMessage(ctx context.Context, msgBytes []byte) ([]byte, error) {
	var req jsonRPCRequest
	if err := json.Unmarshal(msgBytes, &req); err != nil {
		return s.errorResponse(nil, -32700, "Parse error"), nil
	}

	switch req.Method {
	case "initialize":
		result := map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    s.name,
				"version": s.version,
			},
		}
		return s.successResponse(req.ID, result), nil

	case "notifications/initialized":
		// 通知类消息，无需响应
		return nil, nil

	case "tools/list":
		tools := make([]map[string]any, 0, len(s.tools))
		for _, def := range s.tools {
			schema := def.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{
				"name":        def.Name,
				"description": def.Description,
				"inputSchema": schema,
			})
		}
		return s.successResponse(req.ID, map[string]any{"tools": tools}), nil

	case "tools/call":
		return s.handleToolsCall(ctx, req)

	default:
		return s.errorResponse(req.ID, -32601, "Method not found"), nil
	}
}

// handleToolsCall 处理 tools/call 方法调用。
func (s *SdkMcpServer) handleToolsCall(ctx context.Context, req jsonRPCRequest) ([]byte, error) {
	// 解析 params
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, -32602, "Invalid params"), nil
		}
	}

	// 查找工具
	def, ok := s.tools[params.Name]
	if !ok {
		return s.errorResponse(req.ID, -32601, fmt.Sprintf("Tool not found: %s", params.Name)), nil
	}

	// 调用工具处理函数
	args := params.Arguments
	if args == nil {
		args = map[string]any{}
	}
	toolResult, err := def.Handler(ctx, args)
	if err != nil {
		// 工具执行出错，返回 isError=true 的文本错误内容
		result := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": err.Error()},
			},
			"isError": true,
		}
		return s.successResponse(req.ID, result), nil
	}

	// 将 CallToolResult 序列化为响应
	// content 中的元素可能是 *TextContent / *ImageContent / *EmbeddedResource 或 map[string]any
	var contentItems []any
	if toolResult != nil {
		for _, item := range toolResult.Content {
			contentItems = append(contentItems, item)
		}
	}
	if contentItems == nil {
		contentItems = []any{}
	}

	isError := false
	if toolResult != nil {
		isError = toolResult.IsError
	}

	result := map[string]any{
		"content": contentItems,
		"isError": isError,
	}
	return s.successResponse(req.ID, result), nil
}

// successResponse 构建 JSON-RPC 2.0 成功响应。
func (s *SdkMcpServer) successResponse(id any, result any) []byte {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	b, _ := json.Marshal(resp)
	return b
}

// errorResponse 构建 JSON-RPC 2.0 错误响应。
func (s *SdkMcpServer) errorResponse(id any, code int, message string) []byte {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// CreateSDKMCPServer 创建一个 MCP 服务器并注册所有给定工具，
// 同时返回可以放入 Options.MCPServers 的 McpSdkServerConfig。
func CreateSDKMCPServer(name, version string, tools []*ToolDefinition) (*SdkMcpServer, McpSdkServerConfig) {
	server := NewSdkMcpServer(name, version)
	for _, t := range tools {
		server.RegisterTool(t)
	}
	config := McpSdkServerConfig{
		Server: server,
	}
	return server, config
}
