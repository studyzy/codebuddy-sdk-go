package codebuddy

import (
	"context"
	"errors"
	"testing"
)

func TestSendInitialize(t *testing.T) {
	tr := newMockTransport(10)
	opts := &Options{}
	_, registry, err := sendInitialize(context.Background(), tr, opts, true)
	if err != nil {
		t.Fatalf("sendInitialize error: %v", err)
	}
	if registry == nil {
		t.Fatal("registry should not be nil")
	}
	tr.mu.Lock()
	n := len(tr.written)
	tr.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected 1 written message, got %d", n)
	}
	msg := tr.writtenJSON(0)
	if msg["type"] != "control_request" {
		t.Errorf("type: got %v, want control_request", msg["type"])
	}
	req := msg["request"].(map[string]any)
	if req["subtype"] != "initialize" {
		t.Errorf("subtype: got %v, want initialize", req["subtype"])
	}
	if req["hasPrompt"] != true {
		t.Errorf("hasPrompt: got %v, want true", req["hasPrompt"])
	}
}

func TestSendPrompt_String(t *testing.T) {
	tr := newMockTransport(10)
	err := sendPrompt(context.Background(), tr, "hello world")
	if err != nil {
		t.Fatalf("sendPrompt error: %v", err)
	}
	msg := tr.writtenJSON(0)
	if msg["type"] != "user" {
		t.Errorf("type: got %v, want user", msg["type"])
	}
	msgContent := msg["message"].(map[string]any)
	if msgContent["content"] != "hello world" {
		t.Errorf("content: got %v, want hello world", msgContent["content"])
	}
}

func TestSendPrompt_Nil(t *testing.T) {
	tr := newMockTransport(10)
	err := sendPrompt(context.Background(), tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	tr.mu.Lock()
	n := len(tr.written)
	tr.mu.Unlock()
	if n != 0 {
		t.Errorf("expected 0 writes for nil prompt, got %d", n)
	}
}

func TestGetStringPtrFromMap(t *testing.T) {
	data := map[string]any{"key": "value", "num": 42}
	ptr := getStringPtrFromMap(data, "key")
	if ptr == nil || *ptr != "value" {
		t.Errorf("got %v, want 'value'", ptr)
	}
	nilPtr := getStringPtrFromMap(data, "num")
	if nilPtr != nil {
		t.Errorf("expected nil for non-string value, got %v", nilPtr)
	}
	missingPtr := getStringPtrFromMap(data, "missing")
	if missingPtr != nil {
		t.Errorf("expected nil for missing key, got %v", missingPtr)
	}
}

func TestHandlePermissionRequest_NilHandler(t *testing.T) {
	tr := newMockTransport(10)
	opts := &Options{}
	handlePermissionRequest(context.Background(), tr, "req-1", map[string]any{
		"tool_name":   "Write",
		"input":       map[string]any{},
		"tool_use_id": "tu-1",
	}, opts)
	resp := tr.writtenJSON(0)
	if resp == nil {
		t.Fatal("expected a response")
	}
	response := resp["response"].(map[string]any)
	inner := response["response"].(map[string]any)
	if inner["allowed"] != false {
		t.Errorf("expected allowed=false for nil handler, got %v", inner["allowed"])
	}
}

func TestHandlePermissionRequest_AllowHandler(t *testing.T) {
	tr := newMockTransport(10)
	opts := &Options{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, o CanUseToolOptions) (PermissionResult, error) {
			return &PermissionResultAllow{}, nil
		},
	}
	handlePermissionRequest(context.Background(), tr, "req-2", map[string]any{
		"tool_name":   "Read",
		"input":       map[string]any{},
		"tool_use_id": "tu-2",
	}, opts)
	resp := tr.writtenJSON(0)
	response := resp["response"].(map[string]any)
	inner := response["response"].(map[string]any)
	if inner["allowed"] != true {
		t.Errorf("expected allowed=true, got %v", inner["allowed"])
	}
}

func TestHandlePermissionRequest_DenyHandler(t *testing.T) {
	tr := newMockTransport(10)
	opts := &Options{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, o CanUseToolOptions) (PermissionResult, error) {
			return &PermissionResultDeny{Message: "nope"}, nil
		},
	}
	handlePermissionRequest(context.Background(), tr, "req-3", map[string]any{
		"tool_name":   "Bash",
		"input":       map[string]any{},
		"tool_use_id": "tu-3",
	}, opts)
	resp := tr.writtenJSON(0)
	response := resp["response"].(map[string]any)
	inner := response["response"].(map[string]any)
	if inner["allowed"] != false {
		t.Errorf("expected allowed=false, got %v", inner["allowed"])
	}
	if inner["reason"] != "nope" {
		t.Errorf("expected reason='nope', got %v", inner["reason"])
	}
}

func TestExecuteHook_NotFound(t *testing.T) {
	registry := HookCallbackRegistry{}
	output := executeHook(context.Background(), "nonexistent", nil, nil, registry)
	if output["continue"] != true {
		t.Errorf("expected continue=true for missing hook, got %v", output["continue"])
	}
}

func TestExecuteHook_Found(t *testing.T) {
	cont := false
	reason := "blocked"
	registry := HookCallbackRegistry{
		"hook_PreToolUse_0_0": func(ctx context.Context, input map[string]any, toolUseID *string) (HookJSONOutput, error) {
			return HookJSONOutput{Continue: &cont, StopReason: &reason}, nil
		},
	}
	output := executeHook(context.Background(), "hook_PreToolUse_0_0", nil, nil, registry)
	if output["continue"] != false {
		t.Errorf("expected continue=false, got %v", output["continue"])
	}
	if output["stopReason"] != "blocked" {
		t.Errorf("expected stopReason='blocked', got %v", output["stopReason"])
	}
}

func TestQuery_WithMockTransport(t *testing.T) {
	tr := newMockTransport(20)
	ctx := context.Background()

	// Inject: assistant message + result message, then close
	tr.injectRaw(map[string]any{
		"type":       "assistant",
		"session_id": "s1",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "Hello!"}},
		},
	})
	tr.injectRaw(map[string]any{
		"type":        "result",
		"session_id":  "s1",
		"subtype":     "success",
		"is_error":    false,
		"result":      "ok",
		"duration_ms": float64(50),
		"num_turns":   float64(1),
	})
	tr.closeMessages()

	msgCh, err := queryWithTransport(ctx, "test prompt", nil, tr)
	if err != nil {
		t.Fatalf("queryWithTransport: %v", err)
	}

	var msgs []Message
	for msg := range msgCh {
		msgs = append(msgs, msg)
	}

	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	// Last message should be ResultMessage
	last := msgs[len(msgs)-1]
	if _, ok := last.(*ResultMessage); !ok {
		t.Errorf("last message type: got %T, want *ResultMessage", last)
	}
}

func TestQuery_ErrorMessage(t *testing.T) {
	tr := newMockTransport(10)
	ctx := context.Background()

	// Inject an error message from the transport
	tr.injectErr(errors.New("connection reset"))
	tr.closeMessages()

	msgCh, err := queryWithTransport(ctx, "prompt", nil, tr)
	if err != nil {
		t.Fatalf("queryWithTransport: %v", err)
	}

	var got []Message
	for msg := range msgCh {
		got = append(got, msg)
	}
	if len(got) == 0 {
		t.Fatal("expected error message, got nothing")
	}
	if _, ok := got[0].(*ErrorMessage); !ok {
		t.Errorf("expected ErrorMessage, got %T", got[0])
	}
}

func TestQuery_HookCallback(t *testing.T) {
	hookCalled := false
	opts := &Options{
		Hooks: map[HookEvent][]HookMatcher{
			HookPreToolUse: {
				{
					Matcher: nil,
					Hooks: []HookCallback{
						func(ctx context.Context, input map[string]any, toolUseID *string) (HookJSONOutput, error) {
							hookCalled = true
							c := true
							return HookJSONOutput{Continue: &c}, nil
						},
					},
				},
			},
		},
	}

	tr := newMockTransport(20)
	ctx := context.Background()

	// Inject a hook_callback control_request, then a result
	tr.injectRaw(map[string]any{
		"type":       "control_request",
		"request_id": "hook-req-1",
		"request": map[string]any{
			"subtype":     "hook_callback",
			"callback_id": "hook_PreToolUse_0_0",
			"input":       map[string]any{"command": "ls"},
		},
	})
	tr.injectRaw(map[string]any{
		"type":        "result",
		"is_error":    false,
		"result":      "done",
		"duration_ms": float64(10),
		"num_turns":   float64(1),
	})
	tr.closeMessages()

	msgCh, err := queryWithTransport(ctx, "prompt", opts, tr)
	if err != nil {
		t.Fatalf("queryWithTransport: %v", err)
	}
	for range msgCh {
	}

	if !hookCalled {
		t.Error("expected hook callback to be called")
	}
}

func TestQuery_PermissionRequest(t *testing.T) {
	permCalled := false
	opts := &Options{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, o CanUseToolOptions) (PermissionResult, error) {
			permCalled = true
			return &PermissionResultAllow{}, nil
		},
	}

	tr := newMockTransport(20)
	ctx := context.Background()

	tr.injectRaw(map[string]any{
		"type":       "control_request",
		"request_id": "perm-req-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Read",
			"input":       map[string]any{},
			"tool_use_id": "tu-1",
		},
	})
	tr.injectRaw(map[string]any{
		"type":        "result",
		"is_error":    false,
		"result":      "done",
		"duration_ms": float64(10),
		"num_turns":   float64(1),
	})
	tr.closeMessages()

	msgCh, err := queryWithTransport(ctx, "prompt", opts, tr)
	if err != nil {
		t.Fatalf("queryWithTransport: %v", err)
	}
	for range msgCh {
	}

	if !permCalled {
		t.Error("expected CanUseTool callback to be called")
	}
}

func TestQuery_ContextCancel(t *testing.T) {
	tr := newMockTransport(5)
	ctx, cancel := context.WithCancel(context.Background())

	msgCh, err := queryWithTransport(ctx, "prompt", nil, tr)
	if err != nil {
		t.Fatalf("queryWithTransport: %v", err)
	}

	cancel() // cancel before any messages arrive
	tr.closeMessages()

	// Drain the channel — should close without hanging
	for range msgCh {
	}
}

func TestSendInitialize_WithOptions(t *testing.T) {
	tr := newMockTransport(10)
	override := "custom system prompt"
	opts := &Options{
		SystemPrompt: &SystemPromptConfig{Override: &override},
		Agents: map[string]AgentDefinition{
			"helper": {Description: "helps", Prompt: "you are helpful"},
		},
	}
	_, _, err := sendInitialize(context.Background(), tr, opts, false)
	if err != nil {
		t.Fatalf("sendInitialize: %v", err)
	}
	msg := tr.writtenJSON(0)
	req := msg["request"].(map[string]any)
	if req["systemPrompt"] != "custom system prompt" {
		t.Errorf("systemPrompt: got %v", req["systemPrompt"])
	}
	if req["agents"] == nil {
		t.Error("expected agents to be set")
	}
}

func TestSendInitialize_AppendSystemPrompt(t *testing.T) {
	tr := newMockTransport(10)
	appendStr := "extra instructions"
	opts := &Options{
		SystemPrompt: &SystemPromptConfig{Append: &appendStr},
	}
	_, _, err := sendInitialize(context.Background(), tr, opts, true)
	if err != nil {
		t.Fatalf("sendInitialize: %v", err)
	}
	msg := tr.writtenJSON(0)
	req := msg["request"].(map[string]any)
	if req["appendSystemPrompt"] != "extra instructions" {
		t.Errorf("appendSystemPrompt: got %v", req["appendSystemPrompt"])
	}
	if req["systemPrompt"] != nil {
		t.Errorf("systemPrompt should be nil, got %v", req["systemPrompt"])
	}
}

func TestSendPrompt_Channel(t *testing.T) {
	tr := newMockTransport(10)
	ch := make(chan map[string]any, 1)
	ch <- map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "hi"}}
	close(ch)

	err := sendPrompt(context.Background(), tr, (<-chan map[string]any)(ch))
	if err != nil {
		t.Fatalf("sendPrompt channel: %v", err)
	}
	// The goroutine writes asynchronously; wait briefly
	for i := 0; i < 100; i++ {
		tr.mu.Lock()
		n := len(tr.written)
		tr.mu.Unlock()
		if n > 0 {
			break
		}
		// small yield
		_ = i
	}
}

func TestQuery_WithMockTransport_IsError(t *testing.T) {
	tr := newMockTransport(10)
	ctx := context.Background()

	tr.injectRaw(map[string]any{
		"type":        "result",
		"session_id":  "s2",
		"subtype":     "api_error",
		"is_error":    true,
		"result":      "",
		"duration_ms": float64(10),
		"num_turns":   float64(0),
		"errors":      []any{"API limit exceeded"},
	})
	tr.closeMessages()

	msgCh, err := queryWithTransport(ctx, "prompt", nil, tr)
	if err != nil {
		t.Fatalf("queryWithTransport: %v", err)
	}

	var msgs []Message
	for msg := range msgCh {
		msgs = append(msgs, msg)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	if _, ok := msgs[0].(*ErrorMessage); !ok {
		t.Errorf("expected ErrorMessage, got %T", msgs[0])
	}
}

func TestHandlePermissionRequest_ErrorHandler(t *testing.T) {
	tr := newMockTransport(10)
	opts := &Options{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, o CanUseToolOptions) (PermissionResult, error) {
			return nil, errors.New("handler failure")
		},
	}
	handlePermissionRequest(context.Background(), tr, "req-err", map[string]any{
		"tool_name":   "Bash",
		"input":       map[string]any{},
		"tool_use_id": "tu-err",
	}, opts)
	resp := tr.writtenJSON(0)
	if resp == nil {
		t.Fatal("expected response")
	}
	response := resp["response"].(map[string]any)
	inner := response["response"].(map[string]any)
	if inner["allowed"] != false {
		t.Errorf("expected allowed=false on error, got %v", inner["allowed"])
	}
	if inner["reason"] != "handler failure" {
		t.Errorf("expected reason='handler failure', got %v", inner["reason"])
	}
}

func TestHandlePermissionRequest_UnknownResult(t *testing.T) {
	tr := newMockTransport(10)

	// Use PermissionResultAllow but simulate unknown by using the default branch in handlePermissionRequest.
	// Since we can't create a truly unknown type (interface is unexported), test the allow path with updatedInput.
	opts := &Options{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, o CanUseToolOptions) (PermissionResult, error) {
			return &PermissionResultAllow{UpdatedInput: map[string]any{"modified": true}}, nil
		},
	}
	handlePermissionRequest(context.Background(), tr, "req-allow2", map[string]any{
		"tool_name":   "Read",
		"input":       map[string]any{},
		"tool_use_id": "tu-allow2",
	}, opts)
	resp := tr.writtenJSON(0)
	response := resp["response"].(map[string]any)
	inner := response["response"].(map[string]any)
	if inner["allowed"] != true {
		t.Errorf("expected allowed=true, got %v", inner["allowed"])
	}
}

func TestExecuteHook_Error(t *testing.T) {
	registry := HookCallbackRegistry{
		"hook_PreToolUse_0_0": func(ctx context.Context, input map[string]any, toolUseID *string) (HookJSONOutput, error) {
			return HookJSONOutput{}, errors.New("hook error")
		},
	}
	output := executeHook(context.Background(), "hook_PreToolUse_0_0", nil, nil, registry)
	if output["continue"] != false {
		t.Errorf("expected continue=false on error, got %v", output["continue"])
	}
	if output["stopReason"] != "hook error" {
		t.Errorf("expected stopReason='hook error', got %v", output["stopReason"])
	}
}

func TestHandleQueryControlRequest_MCPMessage(t *testing.T) {
	// When transport is not SubprocessTransport, mcp_message is a no-op
	tr := newMockTransport(10)
	opts := &Options{}
	handleQueryControlRequest(context.Background(), tr, map[string]any{
		"type":       "control_request",
		"request_id": "mcp-1",
		"request": map[string]any{
			"subtype": "mcp_message",
			"method":  "tools/list",
		},
	}, opts, nil)
	// Should not panic and not write anything
	tr.mu.Lock()
	n := len(tr.written)
	tr.mu.Unlock()
	if n != 0 {
		t.Errorf("expected 0 writes for mcp_message on non-subprocess, got %d", n)
	}
}
