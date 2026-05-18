package codebuddy

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient(nil)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.GetPermissionMode() != PermissionModeDefault {
		t.Errorf("default permission mode: got %v, want %v", client.GetPermissionMode(), PermissionModeDefault)
	}
	if client.GetModel() != "" {
		t.Errorf("default model: got %q, want empty", client.GetModel())
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	model := "claude-opus-4"
	client := NewClient(&Options{
		Model:          &model,
		PermissionMode: PermissionModeBypassPermissions.Ptr(),
	})
	if client.GetModel() != model {
		t.Errorf("model: got %q, want %q", client.GetModel(), model)
	}
	if client.GetPermissionMode() != PermissionModeBypassPermissions {
		t.Errorf("permission mode: got %v", client.GetPermissionMode())
	}
}

func TestRouteControlResponse_Success(t *testing.T) {
	client := NewClient(nil)
	respCh := make(chan controlResponseResult, 1)
	client.core.pendingMu.Lock()
	client.core.pendingResponses["req-1"] = respCh
	client.core.pendingMu.Unlock()

	client.core.routeControlResponse(map[string]any{
		"response": map[string]any{
			"request_id": "req-1",
			"subtype":    "success",
			"response":   map[string]any{"foo": "bar"},
		},
	})

	result := <-respCh
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if result.response["foo"] != "bar" {
		t.Errorf("foo: got %v, want bar", result.response["foo"])
	}
}

func TestRouteControlResponse_Error(t *testing.T) {
	client := NewClient(nil)
	respCh := make(chan controlResponseResult, 1)
	client.core.pendingMu.Lock()
	client.core.pendingResponses["req-2"] = respCh
	client.core.pendingMu.Unlock()

	client.core.routeControlResponse(map[string]any{
		"response": map[string]any{
			"request_id": "req-2",
			"subtype":    "error",
			"error":      "something failed",
		},
	})

	result := <-respCh
	if result.err == nil {
		t.Error("expected error result on error response")
	}
}

func TestDrainPendingResponses(t *testing.T) {
	client := NewClient(nil)
	ch1 := make(chan controlResponseResult, 1)
	ch2 := make(chan controlResponseResult, 1)
	client.core.pendingMu.Lock()
	client.core.pendingResponses["r1"] = ch1
	client.core.pendingResponses["r2"] = ch2
	client.core.pendingMu.Unlock()

	client.core.drainPendingResponses()

	_, ok1 := <-ch1
	_, ok2 := <-ch2
	if ok1 || ok2 {
		t.Error("expected all channels to be closed after drain")
	}
	client.core.pendingMu.Lock()
	n := len(client.core.pendingResponses)
	client.core.pendingMu.Unlock()
	if n != 0 {
		t.Errorf("expected empty pendingResponses after drain, got %d", n)
	}
}

// newConnectedClient creates a Client wired to a mockTransport and returns both.
// It pre-injects an initialize control_response so Connect returns immediately.
func newConnectedClient(t *testing.T, bufSize int) (*Client, *mockTransport) {
	t.Helper()
	return connectClientWithMock(t, bufSize, nil)
}

// connectClientWithMock connects a client with a mock transport.
// initResp is an optional extra payload in the initialize response;
// pass nil to use a minimal success response.
func connectClientWithMock(t *testing.T, bufSize int, initExtra map[string]any) (*Client, *mockTransport) {
	t.Helper()
	tr := newMockTransport(bufSize + 10)
	client := NewClient(nil)

	// We need to inject the init response BEFORE Connect reads it.
	// But Connect calls pendingResponses[initReqID] internally, so we can't
	// pre-fill the response. Instead we run connect in a goroutine and inject
	// the response after the request lands in written[].
	ctx := context.Background()
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectWithTransport(ctx, nil, tr)
	}()

	// Wait until the initialize control_request is written, then inject response
	var initReqID string
	for initReqID == "" {
		tr.mu.Lock()
		for _, w := range tr.written {
			var m map[string]any
			if json.Unmarshal([]byte(w), &m) == nil {
				if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "initialize" {
					initReqID, _ = m["request_id"].(string)
				}
			}
		}
		tr.mu.Unlock()
		if initReqID == "" {
			runtime.Gosched()
		}
	}

	// Build the response payload
	responseData := map[string]any{}
	for k, v := range initExtra {
		responseData[k] = v
	}
	tr.injectRaw(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": initReqID,
			"subtype":    "success",
			"response":   responseData,
		},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("connectWithTransport: %v", err)
	}
	return client, tr
}

func TestClient_AttachesRawJSONToMessages(t *testing.T) {
	client, tr := newConnectedClient(t, 4)
	defer client.Close()

	rawJSON := `{"type":"assistant","session_id":"session-raw-001","message":{"role":"assistant","content":[{"type":"text","text":"hello raw"}]}}`
	var data map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &data); err != nil {
		t.Fatalf("unmarshal rawJSON: %v", err)
	}
	tr.msgCh <- RawMessage{Data: data, Raw: json.RawMessage(rawJSON)}

	select {
	case msg := <-client.ReceiveMessages():
		assistant, ok := msg.(*AssistantMessage)
		if !ok {
			t.Fatalf("expected *AssistantMessage, got %T", msg)
		}
		if string(RawJSONOf(assistant)) != rawJSON {
			t.Fatalf("raw json mismatch: got %s", string(RawJSONOf(assistant)))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for assistant message")
	}
}

func TestClient_SendAndReceiveResponse(t *testing.T) {
	client, tr := newConnectedClient(t, 20)

	ctx := context.Background()
	if err := client.Send(ctx, "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Inject an assistant message then a result message
	tr.injectRaw(map[string]any{
		"type":       "assistant",
		"session_id": "sess-1",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "Hi there!"}},
		},
	})
	tr.injectRaw(map[string]any{
		"type":        "result",
		"session_id":  "sess-1",
		"subtype":     "success",
		"is_error":    false,
		"result":      "done",
		"duration_ms": float64(100),
		"num_turns":   float64(1),
	})
	tr.closeMessages()

	result, err := client.ReceiveResponse(ctx)
	if err != nil {
		t.Fatalf("ReceiveResponse: %v", err)
	}
	if result == nil {
		t.Fatal("expected ResultMessage, got nil")
	}
	client.Close()
}

func TestClient_ReceiveResponse_IsError(t *testing.T) {
	client, tr := newConnectedClient(t, 10)
	ctx := context.Background()

	// Inject a result with is_error=true and errors list
	tr.injectRaw(map[string]any{
		"type":        "result",
		"is_error":    true,
		"subtype":     "api_error",
		"result":      "",
		"duration_ms": float64(0),
		"num_turns":   float64(0),
		"errors":      []any{"API error occurred"},
	})
	tr.closeMessages()

	_, err := client.ReceiveResponse(ctx)
	if err == nil {
		t.Fatal("expected error for is_error result")
	}
	var execErr *ExecutionError
	if !errors.As(err, &execErr) {
		var connErr *CLIConnectionError
		if !errors.As(err, &connErr) {
			t.Errorf("expected ExecutionError or CLIConnectionError, got %T: %v", err, err)
		}
	}
	client.Close()
}

func TestClient_Interrupt(t *testing.T) {
	client, tr := newConnectedClient(t, 10)
	ctx := context.Background()
	defer client.Close()

	if err := client.Interrupt(ctx); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	// Find the interrupt request in written messages (skip initialize)
	tr.mu.Lock()
	var found bool
	for _, w := range tr.written {
		var m map[string]any
		if json.Unmarshal([]byte(w), &m) == nil {
			if req, ok := m["request"].(map[string]any); ok {
				if req["subtype"] == "interrupt" {
					found = true
				}
			}
		}
	}
	tr.mu.Unlock()
	if !found {
		t.Error("expected interrupt control_request to be written")
	}
	tr.closeMessages()
}

func TestClient_SetGetPermissionMode(t *testing.T) {
	client := NewClient(nil)
	client.SetPermissionMode(context.Background(), PermissionModeBypassPermissions) //nolint:errcheck
	if client.GetPermissionMode() != PermissionModeBypassPermissions {
		t.Errorf("got %v, want bypassPermissions", client.GetPermissionMode())
	}
}

func TestClient_SetGetModel(t *testing.T) {
	client := NewClient(nil)
	client.SetModel(context.Background(), "claude-haiku-3") //nolint:errcheck
	if client.GetModel() != "claude-haiku-3" {
		t.Errorf("got %q, want claude-haiku-3", client.GetModel())
	}
}

func TestClient_Disconnect(t *testing.T) {
	client, tr := newConnectedClient(t, 5)
	ctx := context.Background()
	tr.closeMessages()

	if err := client.Disconnect(ctx); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if !tr.IsClosed() {
		t.Error("expected transport to be closed after Disconnect")
	}

	// Second Disconnect should be a no-op
	if err := client.Disconnect(ctx); err != nil {
		t.Fatalf("second Disconnect: %v", err)
	}
}

func TestClient_Send_NotConnected(t *testing.T) {
	client := NewClient(nil)
	err := client.Send(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for Send on unconnected client")
	}
}

func TestClient_ReceiveMessages(t *testing.T) {
	client, tr := newConnectedClient(t, 5)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	ch := client.ReceiveMessages()
	if ch == nil {
		t.Fatal("ReceiveMessages returned nil channel")
	}
}

func TestClient_SendControlRequest(t *testing.T) {
	client, tr := newConnectedClient(t, 20)
	ctx := context.Background()
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	respCh := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := client.core.sendControlRequest(ctx, map[string]any{"subtype": "mcp_status"}, "未连接")
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	var reqID string
	for reqID == "" {
		tr.mu.Lock()
		for _, w := range tr.written {
			var m map[string]any
			if json.Unmarshal([]byte(w), &m) == nil {
				if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "mcp_status" {
					reqID, _ = m["request_id"].(string)
				}
			}
		}
		tr.mu.Unlock()
		if reqID == "" {
			runtime.Gosched()
		}
	}

	tr.injectRaw(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": reqID,
			"subtype":    "success",
			"response":   map[string]any{"result": "ok"},
		},
	})

	select {
	case resp := <-respCh:
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	case err := <-errCh:
		t.Fatalf("sendControlRequest error: %v", err)
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

func TestClient_MCPServerStatus(t *testing.T) {
	client, tr := newConnectedClient(t, 20)
	ctx := context.Background()
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	errCh := make(chan error, 1)
	resultCh := make(chan []MCPServerStatus, 1)
	go func() {
		statuses, err := client.MCPServerStatus(ctx)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- statuses
	}()

	var reqID string
	for reqID == "" {
		tr.mu.Lock()
		for _, w := range tr.written {
			var m map[string]any
			if json.Unmarshal([]byte(w), &m) == nil {
				if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "mcp_status" {
					reqID, _ = m["request_id"].(string)
				}
			}
		}
		tr.mu.Unlock()
		if reqID == "" {
			runtime.Gosched()
		}
	}
	tr.injectRaw(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": reqID,
			"subtype":    "success",
			"response": map[string]any{
				"mcp_servers": []any{
					map[string]any{"name": "test-server", "status": "connected"},
				},
			},
		},
	})

	select {
	case statuses := <-resultCh:
		if len(statuses) != 1 || statuses[0].Name != "test-server" {
			t.Errorf("unexpected statuses: %+v", statuses)
		}
	case err := <-errCh:
		t.Fatalf("MCPServerStatus: %v", err)
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

func TestClient_HandleControlRequest_HookCallback(t *testing.T) {
	hookDone := make(chan struct{})
	client, tr := newConnectedClient(t, 10)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	cbID := "hook_PreToolUse_0_0"
	client.core.hookRegistry[cbID] = func(ctx context.Context, input map[string]any, toolUseID *string) (HookJSONOutput, error) {
		select {
		case <-hookDone:
			// 已关闭，避免重复 close 触发 panic
		default:
			close(hookDone)
		}
		c := true
		return HookJSONOutput{Continue: &c}, nil
	}

	tr.injectRaw(map[string]any{
		"type":       "control_request",
		"request_id": "hook-1",
		"request": map[string]any{
			"subtype":     "hook_callback",
			"callback_id": cbID,
			"input":       map[string]any{},
		},
	})

	select {
	case <-hookDone:
		// 成功调用
	case <-time.After(2 * time.Second):
		t.Error("expected hook to be called within 2s")
	}
}

// TestClient_BackgroundReader_Error tests that an error in the transport
// is propagated to messageChannel.
func TestClient_BackgroundReader_Error(t *testing.T) {
	client, tr := newConnectedClient(t, 10)
	defer client.Close()

	msgCh := client.ReceiveMessages()
	tr.injectErr(errors.New("transport failed"))
	tr.closeMessages()

	var gotErr bool
	for msg := range msgCh {
		if _, ok := msg.(*ErrorMessage); ok {
			gotErr = true
		}
	}
	if !gotErr {
		t.Error("expected ErrorMessage from transport error")
	}
}

// TestClient_BackgroundReader_SessionID tests that session_id is captured
// and pending perm/model syncs are triggered.
func TestClient_BackgroundReader_SessionID(t *testing.T) {
	client, tr := newConnectedClient(t, 20)

	ctx := context.Background()
	// Set perm + model BEFORE hasSentQuery is set, so they are pending
	client.SetPermissionMode(ctx, PermissionModeBypassPermissions) //nolint:errcheck
	client.SetModel(ctx, "claude-model-x")                         //nolint:errcheck

	// Inject a message with a session_id to trigger pending syncs
	tr.injectRaw(map[string]any{
		"type":       "assistant",
		"session_id": "sess-sync-1",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "hi"}},
		},
	})
	tr.injectRaw(map[string]any{
		"type":        "result",
		"session_id":  "sess-sync-1",
		"subtype":     "success",
		"is_error":    false,
		"result":      "done",
		"duration_ms": float64(10),
		"num_turns":   float64(1),
	})
	tr.closeMessages()

	msgCh := client.ReceiveMessages()
	for range msgCh {
	}

	client.Close()

	// Verify session_id was captured
	sidRaw := client.sessionID.Load()
	if sidRaw == nil || sidRaw.(string) != "sess-sync-1" {
		t.Errorf("sessionID: got %v, want sess-sync-1", sidRaw)
	}
}

// TestClient_SetPermissionMode_WithSession tests fire-and-forget path
// (when session_id is already set and hasSentQuery is true).
func TestClient_SetPermissionMode_WithSession(t *testing.T) {
	client, tr := newConnectedClient(t, 20)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	// Simulate that a query has been sent and session_id is known
	client.hasSentQuery.Store(true)
	client.sessionID.Store("session-abc")

	ctx := context.Background()
	if err := client.SetPermissionMode(ctx, PermissionModeBypassPermissions); err != nil {
		t.Fatalf("SetPermissionMode: %v", err)
	}

	// Give the goroutine time to run
	for i := 0; i < 100; i++ {
		tr.mu.Lock()
		found := false
		for _, w := range tr.written {
			var m map[string]any
			if json.Unmarshal([]byte(w), &m) == nil {
				if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "set_permission_mode" {
					found = true
				}
			}
		}
		tr.mu.Unlock()
		if found {
			break
		}
		runtime.Gosched()
	}

	tr.mu.Lock()
	var found bool
	for _, w := range tr.written {
		var m map[string]any
		if json.Unmarshal([]byte(w), &m) == nil {
			if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "set_permission_mode" {
				found = true
			}
		}
	}
	tr.mu.Unlock()
	if !found {
		t.Error("expected set_permission_mode control_request to be written")
	}
}

// TestClient_SetModel_WithSession tests fire-and-forget path for SetModel.
func TestClient_SetModel_WithSession(t *testing.T) {
	client, tr := newConnectedClient(t, 20)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	client.hasSentQuery.Store(true)
	client.sessionID.Store("session-xyz")

	ctx := context.Background()
	if err := client.SetModel(ctx, "claude-test-model"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}

	for i := 0; i < 100; i++ {
		tr.mu.Lock()
		found := false
		for _, w := range tr.written {
			var m map[string]any
			if json.Unmarshal([]byte(w), &m) == nil {
				if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "set_model" {
					found = true
				}
			}
		}
		tr.mu.Unlock()
		if found {
			break
		}
		runtime.Gosched()
	}

	tr.mu.Lock()
	var found bool
	for _, w := range tr.written {
		var m map[string]any
		if json.Unmarshal([]byte(w), &m) == nil {
			if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "set_model" {
				found = true
			}
		}
	}
	tr.mu.Unlock()
	if !found {
		t.Error("expected set_model control_request to be written")
	}
}

// TestClient_ReceiveResponse_ContextCancel tests that ReceiveResponse
// returns when context is canceled.
func TestClient_ReceiveResponse_ContextCancel(t *testing.T) {
	client, tr := newConnectedClient(t, 10)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.ReceiveResponse(ctx)
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
}

// TestClient_SendControlRequest_ContextCancel tests that sendControlRequest
// returns when context is canceled before any response.
func TestClient_SendControlRequest_ContextCancel(t *testing.T) {
	client, tr := newConnectedClient(t, 20)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := client.core.sendControlRequest(ctx, map[string]any{"subtype": "test_cancel"}, "未连接")
		errCh <- err
	}()

	// Wait until the request is written, then cancel
	for i := 0; i < 200; i++ {
		tr.mu.Lock()
		for _, w := range tr.written {
			var m map[string]any
			if json.Unmarshal([]byte(w), &m) == nil {
				if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "test_cancel" {
					tr.mu.Unlock()
					cancel()
					goto done
				}
			}
		}
		tr.mu.Unlock()
		runtime.Gosched()
	}
done:
	cancel() // ensure cancel is always called

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error when context is canceled")
		}
	case <-ctx.Done():
	}
}

// TestClient_HandleControlRequest_CanUseTool tests the can_use_tool path in handleControlRequest.
func TestClient_HandleControlRequest_CanUseTool(t *testing.T) {
	permDone := make(chan struct{})
	client, tr := connectClientWithMock(t, 20, nil)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	client.core.mu.Lock()
	client.core.opts = &Options{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, o CanUseToolOptions) (PermissionResult, error) {
			select {
			case <-permDone:
			default:
				close(permDone)
			}
			return &PermissionResultAllow{}, nil
		},
	}
	client.core.mu.Unlock()

	tr.injectRaw(map[string]any{
		"type":       "control_request",
		"request_id": "perm-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Write",
			"input":       map[string]any{},
			"tool_use_id": "tu-1",
		},
	})

	select {
	case <-permDone:
		// 成功调用
	case <-time.After(2 * time.Second):
		t.Error("expected CanUseTool to be called within 2s")
	}
}

// TestClient_ReceiveMessages_ResultMessage tests that ResultMessage terminates ReceiveResponse.
func TestClient_ReceiveMessages_ResultMessage(t *testing.T) {
	client, tr := newConnectedClient(t, 20)
	defer client.Close()

	ctx := context.Background()
	tr.injectRaw(map[string]any{
		"type":        "result",
		"session_id":  "sess-3",
		"subtype":     "success",
		"is_error":    false,
		"result":      "final",
		"duration_ms": float64(50),
		"num_turns":   float64(2),
	})
	tr.closeMessages()

	result, err := client.ReceiveResponse(ctx)
	if err != nil {
		t.Fatalf("ReceiveResponse: %v", err)
	}
	if result == nil {
		t.Fatal("expected ResultMessage")
	}
	if result.Result == nil || *result.Result != "final" {
		t.Errorf("result: got %v, want final", result.Result)
	}
}

// TestConnectWithTransport_AlreadyConnected tests that a second connect returns an error.
func TestConnectWithTransport_AlreadyConnected(t *testing.T) {
	client, tr := newConnectedClient(t, 10)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	tr2 := newMockTransport(5)
	err := client.connectWithTransport(context.Background(), nil, tr2)
	if err == nil {
		t.Fatal("expected error for already-connected client")
	}
}

// TestConnectWithTransport_WithModelInInitResp tests that the model from init response is captured.
func TestConnectWithTransport_WithModelInInitResp(t *testing.T) {
	client, _ := connectClientWithMock(t, 20, map[string]any{
		"currentModelId": "claude-special-model",
	})
	defer client.Close()

	if client.GetModel() != "claude-special-model" {
		t.Errorf("model: got %q, want claude-special-model", client.GetModel())
	}
}

// TestClient_SendControlRequest_ChannelClosed tests sendControlRequest when transport closes.
func TestClient_SendControlRequest_ChannelClosed(t *testing.T) {
	client, tr := newConnectedClient(t, 20)
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := client.core.sendControlRequest(context.Background(), map[string]any{"subtype": "test_close"}, "未连接")
		errCh <- err
	}()

	// Wait for request to be written then close transport
	for i := 0; i < 200; i++ {
		tr.mu.Lock()
		found := false
		for _, w := range tr.written {
			var m map[string]any
			if json.Unmarshal([]byte(w), &m) == nil {
				if req, ok := m["request"].(map[string]any); ok && req["subtype"] == "test_close" {
					found = true
				}
			}
		}
		tr.mu.Unlock()
		if found {
			break
		}
		runtime.Gosched()
	}

	tr.closeMessages() // close transport → drainPendingResponses

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error when transport closes")
		}
	case <-context.Background().Done():
		t.Fatal("timeout")
	}
}

// TestClient_HandleControlRequest_MCPMessage tests mcp_message dispatch in handleControlRequest.
func TestClient_HandleControlRequest_MCPMessage(t *testing.T) {
	client, tr := newConnectedClient(t, 20)
	defer func() {
		tr.closeMessages()
		client.Close()
	}()

	// Inject an mcp_message control_request — client.transport is a mockTransport
	// which is not a SubprocessTransport, so this should be a no-op
	tr.injectRaw(map[string]any{
		"type":       "control_request",
		"request_id": "mcp-req-1",
		"request": map[string]any{
			"subtype": "mcp_message",
			"method":  "tools/list",
		},
	})
	// Just ensure it doesn't panic — let it process
	runtime.Gosched()
	runtime.Gosched()
}
