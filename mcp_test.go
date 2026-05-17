// mcp_test.go
package codebuddy

import (
	"context"
	"encoding/json"
	"testing"
)

// newEchoServer creates a fresh SdkMcpServer with a single "echo" tool for reuse
// across subtests.
func newEchoServer() *SdkMcpServer {
	server := NewSdkMcpServer("test-server", "1.0.0")
	server.RegisterTool(&ToolDefinition{
		Name:        "echo",
		Description: "Echo the input",
		InputSchema: map[string]any{"type": "object"},
		Handler: WrapSimpleHandler(func(ctx context.Context, args map[string]any) (string, error) {
			msg, _ := args["message"].(string)
			return "echo: " + msg, nil
		}),
	})
	return server
}

func TestSdkMcpServerHandleMessage(t *testing.T) {
	server := newEchoServer()
	ctx := context.Background()

	// ---- initialize ----------------------------------------------------------
	t.Run("initialize", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
		resp, err := server.HandleMessage(ctx, []byte(req))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("response is nil")
		}

		var out map[string]any
		if e := json.Unmarshal(resp, &out); e != nil {
			t.Fatalf("unmarshal error: %v", e)
		}

		// jsonrpc version
		if out["jsonrpc"] != "2.0" {
			t.Errorf("jsonrpc: got %v, want \"2.0\"", out["jsonrpc"])
		}

		result, ok := out["result"].(map[string]any)
		if !ok {
			t.Fatalf("result: expected map, got %T", out["result"])
		}

		// serverInfo.name
		serverInfo, ok := result["serverInfo"].(map[string]any)
		if !ok {
			t.Fatalf("serverInfo: expected map, got %T", result["serverInfo"])
		}
		if serverInfo["name"] != "test-server" {
			t.Errorf("serverInfo.name: got %v, want \"test-server\"", serverInfo["name"])
		}
		if serverInfo["version"] != "1.0.0" {
			t.Errorf("serverInfo.version: got %v, want \"1.0.0\"", serverInfo["version"])
		}

		// protocolVersion present
		if result["protocolVersion"] == nil {
			t.Error("protocolVersion should be present")
		}

		// capabilities.tools present
		caps, ok := result["capabilities"].(map[string]any)
		if !ok {
			t.Fatalf("capabilities: expected map, got %T", result["capabilities"])
		}
		if caps["tools"] == nil {
			t.Error("capabilities.tools should be present")
		}
	})

	// ---- tools/list ----------------------------------------------------------
	t.Run("tools/list", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
		resp, err := server.HandleMessage(ctx, []byte(req))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("response is nil")
		}

		var out map[string]any
		if e := json.Unmarshal(resp, &out); e != nil {
			t.Fatalf("unmarshal error: %v", e)
		}

		result, ok := out["result"].(map[string]any)
		if !ok {
			t.Fatalf("result: expected map, got %T", out["result"])
		}

		toolsRaw, ok := result["tools"].([]any)
		if !ok {
			t.Fatalf("tools: expected []any, got %T", result["tools"])
		}
		if len(toolsRaw) != 1 {
			t.Fatalf("tools len: got %d, want 1", len(toolsRaw))
		}

		tool, ok := toolsRaw[0].(map[string]any)
		if !ok {
			t.Fatalf("tools[0]: expected map, got %T", toolsRaw[0])
		}
		if tool["name"] != "echo" {
			t.Errorf("tools[0].name: got %v, want \"echo\"", tool["name"])
		}
		if tool["description"] != "Echo the input" {
			t.Errorf("tools[0].description: got %v", tool["description"])
		}
	})

	// ---- tools/call ----------------------------------------------------------
	t.Run("tools/call", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`
		resp, err := server.HandleMessage(ctx, []byte(req))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("response is nil")
		}

		var out map[string]any
		if e := json.Unmarshal(resp, &out); e != nil {
			t.Fatalf("unmarshal error: %v", e)
		}

		result, ok := out["result"].(map[string]any)
		if !ok {
			t.Fatalf("result: expected map, got %T", out["result"])
		}

		contentRaw, ok := result["content"].([]any)
		if !ok {
			t.Fatalf("content: expected []any, got %T", result["content"])
		}
		if len(contentRaw) != 1 {
			t.Fatalf("content len: got %d, want 1", len(contentRaw))
		}

		block, ok := contentRaw[0].(map[string]any)
		if !ok {
			t.Fatalf("content[0]: expected map, got %T", contentRaw[0])
		}
		if block["text"] != "echo: hello" {
			t.Errorf("content[0].text: got %v, want \"echo: hello\"", block["text"])
		}
		if block["type"] != "text" {
			t.Errorf("content[0].type: got %v, want \"text\"", block["type"])
		}

		// isError should be false for a successful call
		if result["isError"] != false {
			t.Errorf("isError: got %v, want false", result["isError"])
		}
	})

	// ---- tools/call with empty message --------------------------------------
	t.Run("tools/call empty message", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"echo","arguments":{}}}`
		resp, err := server.HandleMessage(ctx, []byte(req))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out map[string]any
		if e := json.Unmarshal(resp, &out); e != nil {
			t.Fatalf("unmarshal error: %v", e)
		}

		result := out["result"].(map[string]any)
		content := result["content"].([]any)
		block := content[0].(map[string]any)

		// message arg absent → empty string concatenated
		if block["text"] != "echo: " {
			t.Errorf("content[0].text: got %v, want \"echo: \"", block["text"])
		}
	})

	// ---- tools/call handler error -------------------------------------------
	t.Run("tools/call handler error", func(t *testing.T) {
		errServer := NewSdkMcpServer("err-server", "0.1")
		errServer.RegisterTool(&ToolDefinition{
			Name:        "fail",
			Description: "always fails",
			InputSchema: map[string]any{"type": "object"},
			Handler: WrapSimpleHandler(func(_ context.Context, _ map[string]any) (string, error) {
				return "", &ProcessError{Message: "tool explosion"}
			}),
		})

		req := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"fail","arguments":{}}}`
		resp, err := errServer.HandleMessage(ctx, []byte(req))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out map[string]any
		if e := json.Unmarshal(resp, &out); e != nil {
			t.Fatalf("unmarshal error: %v", e)
		}

		// Handler errors are surfaced as isError:true, not as JSON-RPC errors
		result, ok := out["result"].(map[string]any)
		if !ok {
			t.Fatalf("expected result map, got error: %v", out["error"])
		}
		if result["isError"] != true {
			t.Errorf("isError: got %v, want true", result["isError"])
		}
		content := result["content"].([]any)
		block := content[0].(map[string]any)
		if block["text"] != "tool explosion" {
			t.Errorf("error text: got %v, want \"tool explosion\"", block["text"])
		}
	})

	// ---- notifications/initialized ------------------------------------------
	t.Run("notifications/initialized", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
		resp, err := server.HandleMessage(ctx, []byte(req))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil {
			t.Errorf("resp: expected nil for notification, got %s", resp)
		}
	})

	// ---- unknown method → -32601 --------------------------------------------
	t.Run("unknown method", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":4,"method":"unknown/method","params":{}}`
		resp, err := server.HandleMessage(ctx, []byte(req))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("response is nil")
		}

		var out map[string]any
		if e := json.Unmarshal(resp, &out); e != nil {
			t.Fatalf("unmarshal error: %v", e)
		}

		rpcErr, ok := out["error"].(map[string]any)
		if !ok {
			t.Fatalf("error: expected map, got %T", out["error"])
		}

		// JSON numbers unmarshal as float64
		code, ok := rpcErr["code"].(float64)
		if !ok {
			t.Fatalf("error.code type: got %T", rpcErr["code"])
		}
		if int(code) != -32601 {
			t.Errorf("error.code: got %d, want -32601", int(code))
		}
		if rpcErr["message"] != "Method not found" {
			t.Errorf("error.message: got %v, want \"Method not found\"", rpcErr["message"])
		}
	})

	// ---- invalid JSON → parse error -----------------------------------------
	t.Run("invalid json", func(t *testing.T) {
		resp, err := server.HandleMessage(ctx, []byte(`{not valid json`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("response is nil")
		}

		var out map[string]any
		if e := json.Unmarshal(resp, &out); e != nil {
			t.Fatalf("unmarshal error: %v", e)
		}

		rpcErr, ok := out["error"].(map[string]any)
		if !ok {
			t.Fatalf("error: expected map, got %T", out["error"])
		}
		code := int(rpcErr["code"].(float64))
		if code != -32700 {
			t.Errorf("error.code: got %d, want -32700 (parse error)", code)
		}
	})

	// ---- tools/call unknown tool → -32601 -----------------------------------
	t.Run("tools/call unknown tool", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"no_such_tool","arguments":{}}}`
		resp, err := server.HandleMessage(ctx, []byte(req))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out map[string]any
		if e := json.Unmarshal(resp, &out); e != nil {
			t.Fatalf("unmarshal error: %v", e)
		}

		rpcErr, ok := out["error"].(map[string]any)
		if !ok {
			t.Fatalf("error: expected map, got %T", out["error"])
		}
		code := int(rpcErr["code"].(float64))
		if code != -32601 {
			t.Errorf("error.code: got %d, want -32601", code)
		}
	})
}

// ---- NewSdkMcpServer -------------------------------------------------------

func TestNewSdkMcpServer(t *testing.T) {
	s := NewSdkMcpServer("my-server", "2.0")
	if s == nil {
		t.Fatal("NewSdkMcpServer returned nil")
	}
	if s.name != "my-server" {
		t.Errorf("name: got %q", s.name)
	}
	if s.version != "2.0" {
		t.Errorf("version: got %q", s.version)
	}
	if s.tools == nil {
		t.Error("tools map should be initialized")
	}
}

// ---- RegisterTool ----------------------------------------------------------

func TestRegisterTool(t *testing.T) {
	s := NewSdkMcpServer("s", "1")
	def := &ToolDefinition{
		Name:    "my_tool",
		Handler: WrapSimpleHandler(func(_ context.Context, _ map[string]any) (string, error) { return "", nil }),
	}
	s.RegisterTool(def)

	if _, ok := s.tools["my_tool"]; !ok {
		t.Error("tool was not registered")
	}
	// Overwrite
	def2 := &ToolDefinition{
		Name:    "my_tool",
		Handler: WrapSimpleHandler(func(_ context.Context, _ map[string]any) (string, error) { return "v2", nil }),
	}
	s.RegisterTool(def2)
	if s.tools["my_tool"] != def2 {
		t.Error("RegisterTool should overwrite existing tool with same name")
	}
}

// ---- CreateSDKMCPServer ----------------------------------------------------

func TestCreateSDKMCPServer(t *testing.T) {
	tools := []*ToolDefinition{
		{Name: "a", Handler: WrapSimpleHandler(func(_ context.Context, _ map[string]any) (string, error) { return "a", nil })},
		{Name: "b", Handler: WrapSimpleHandler(func(_ context.Context, _ map[string]any) (string, error) { return "b", nil })},
	}
	server, cfg := CreateSDKMCPServer("bulk-server", "3.0", tools)

	if server == nil {
		t.Fatal("server is nil")
	}
	if cfg.Server != server {
		t.Error("config.Server should point to the returned server")
	}
	if len(server.tools) != 2 {
		t.Errorf("tools len: got %d, want 2", len(server.tools))
	}
	if _, ok := server.tools["a"]; !ok {
		t.Error("tool \"a\" not registered")
	}
	if _, ok := server.tools["b"]; !ok {
		t.Error("tool \"b\" not registered")
	}
}

// TestToolHandler_RichCallToolResult verifies that a ToolHandler returning
// a *CallToolResult with multiple content types is serialised correctly.
func TestToolHandler_RichCallToolResult(t *testing.T) {
	s := NewSdkMcpServer("rich-server", "1.0.0")
	s.RegisterTool(&ToolDefinition{
		Name:        "multi_content",
		Description: "returns text and image",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(_ context.Context, _ map[string]any) (*CallToolResult, error) {
			return &CallToolResult{
				Content: []any{
					&TextContent{Type: "text", Text: "hello"},
					&ImageContent{Type: "image", Data: "base64data==", MIMEType: "image/png"},
				},
				IsError: false,
			}, nil
		},
	})

	ctx := context.Background()
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"multi_content","arguments":{}}}`
	resp, err := s.HandleMessage(ctx, []byte(req))
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	var out map[string]any
	if e := json.Unmarshal(resp, &out); e != nil {
		t.Fatalf("unmarshal error: %v", e)
	}

	result, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("result: expected map, got %T: %v", out["result"], out)
	}

	if result["isError"] != false {
		t.Errorf("isError: got %v, want false", result["isError"])
	}

	content, ok := result["content"].([]any)
	if !ok {
		t.Fatalf("content: expected []any, got %T", result["content"])
	}
	if len(content) != 2 {
		t.Fatalf("content len: got %d, want 2", len(content))
	}

	// First item: TextContent
	textItem, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0]: expected map, got %T", content[0])
	}
	if textItem["type"] != "text" || textItem["text"] != "hello" {
		t.Errorf("content[0]: got %v", textItem)
	}

	// Second item: ImageContent
	imgItem, ok := content[1].(map[string]any)
	if !ok {
		t.Fatalf("content[1]: expected map, got %T", content[1])
	}
	if imgItem["type"] != "image" || imgItem["mimeType"] != "image/png" {
		t.Errorf("content[1]: got %v", imgItem)
	}
}

// TestWrapSimpleHandler verifies the helper produces the expected CallToolResult.
func TestWrapSimpleHandler(t *testing.T) {
	h := WrapSimpleHandler(func(_ context.Context, _ map[string]any) (string, error) {
		return "wrapped text", nil
	})
	result, err := h(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil CallToolResult")
	}
	if len(result.Content) != 1 {
		t.Fatalf("content len: got %d, want 1", len(result.Content))
	}
	tc, ok := result.Content[0].(*TextContent)
	if !ok {
		t.Fatalf("content[0]: expected *TextContent, got %T", result.Content[0])
	}
	if tc.Text != "wrapped text" {
		t.Errorf("text: got %q, want \"wrapped text\"", tc.Text)
	}
}
