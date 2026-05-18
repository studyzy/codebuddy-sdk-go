package codebuddy

import (
	"context"
	"errors"
	"testing"
)

func TestReadControlResponse_Success(t *testing.T) {
	ch := make(chan RawMessage, 1)
	ch <- RawMessage{Data: map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": "init",
			"subtype":    "success",
			"response":   map[string]any{"foo": "bar"},
		},
	}}
	resp, err := readControlResponse(context.Background(), ch, "init")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp["foo"] != "bar" {
		t.Errorf("foo: got %v, want bar", resp["foo"])
	}
}

func TestReadControlResponse_SkipsNonMatching(t *testing.T) {
	ch := make(chan RawMessage, 2)
	// First message has wrong request_id
	ch <- RawMessage{Data: map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": "other",
			"subtype":    "success",
			"response":   map[string]any{},
		},
	}}
	// Second message matches
	ch <- RawMessage{Data: map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": "target",
			"subtype":    "success",
			"response":   map[string]any{"result": "ok"},
		},
	}}
	resp, err := readControlResponse(context.Background(), ch, "target")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp["result"] != "ok" {
		t.Errorf("result: got %v, want ok", resp["result"])
	}
}

func TestReadControlResponse_ErrorSubtype(t *testing.T) {
	ch := make(chan RawMessage, 1)
	ch <- RawMessage{Data: map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": "r1",
			"subtype":    "error",
			"error":      "auth failed",
		},
	}}
	_, err := readControlResponse(context.Background(), ch, "r1")
	if err == nil {
		t.Fatal("expected error for error subtype")
	}
	var connErr *CLIConnectionError
	if !errors.As(err, &connErr) {
		t.Errorf("expected CLIConnectionError, got %T", err)
	}
}

func TestReadControlResponse_ChannelClosed(t *testing.T) {
	ch := make(chan RawMessage)
	close(ch)
	_, err := readControlResponse(context.Background(), ch, "r1")
	if err == nil {
		t.Fatal("expected error when channel is closed")
	}
}

func TestReadControlResponse_ContextCancel(t *testing.T) {
	ch := make(chan RawMessage) // never sends
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := readControlResponse(ctx, ch, "r1")
	if err == nil {
		t.Fatal("expected error on canceled context")
	}
}

func TestUserInfoFromMap(t *testing.T) {
	data := map[string]any{
		"userId":       "user-123",
		"email":        "test@example.com",
		"displayName":  "Test User",
		"enterpriseId": "ent-456",
		"enterprise":   "MyOrg",
	}
	info := UserInfoFromMap(data)
	if info.UserID != "user-123" {
		t.Errorf("UserID: got %q, want user-123", info.UserID)
	}
	if info.UserID == "" {
		t.Error("expected non-empty UserID")
	}
	if info.EnterpriseID == nil || *info.EnterpriseID != "ent-456" {
		t.Errorf("EnterpriseID: got %v, want ent-456", info.EnterpriseID)
	}
	if info.Enterprise == nil || *info.Enterprise != "MyOrg" {
		t.Errorf("Enterprise: got %v, want MyOrg", info.Enterprise)
	}
}

func TestAuthFlow_Wait_AlreadyLoggedIn(t *testing.T) {
	resp := &AuthenticateResponse{
		UserInfo: UserInfo{UserID: "u1"},
	}
	flow := &AuthFlow{
		result: resp,
	}
	got, err := flow.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got != resp {
		t.Error("expected same response object")
	}
}

func TestAuthFlow_Wait_Uninitialised(t *testing.T) {
	flow := &AuthFlow{}
	_, err := flow.Wait(context.Background())
	if err == nil {
		t.Fatal("expected error for uninitialised flow")
	}
}

func TestAuthFlow_Wait_ContextCancelled(t *testing.T) {
	flow := &AuthFlow{
		resultCh: make(chan authResult), // never receives
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := flow.Wait(ctx)
	if err == nil {
		t.Fatal("expected error on canceled context")
	}
}

func TestAuthFlow_Wait_ResultReceived(t *testing.T) {
	ch := make(chan authResult, 1)
	flow := &AuthFlow{resultCh: ch}
	resp := &AuthenticateResponse{UserInfo: UserInfo{UserID: "u2"}}
	ch <- authResult{response: resp}

	got, err := flow.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got != resp {
		t.Error("expected same response")
	}
}

func TestAuthFlow_Wait_ResultError(t *testing.T) {
	ch := make(chan authResult, 1)
	flow := &AuthFlow{resultCh: ch}
	ch <- authResult{err: &AuthenticationError{ErrorType: "timeout", Message: "timed out"}}

	_, err := flow.Wait(context.Background())
	if err == nil {
		t.Fatal("expected error from resultCh")
	}
}

func TestAuthFlow_Cancel_NilTransport(t *testing.T) {
	flow := &AuthFlow{}
	if err := flow.Cancel(); err != nil {
		t.Errorf("Cancel with nil transport: %v", err)
	}
}

func TestAuthFlow_Cancel_WithTransport(t *testing.T) {
	tr := newMockTransport(5)
	flow := &AuthFlow{transport: tr}
	if err := flow.Cancel(); err != nil {
		t.Errorf("Cancel: %v", err)
	}
	if !tr.IsClosed() {
		t.Error("expected transport to be closed after Cancel")
	}
}

func TestReadAuthURLCallback_Success(t *testing.T) {
	tr := newMockTransport(5)
	ch := make(chan RawMessage, 1)
	authURL := "https://example.com/auth"
	ch <- RawMessage{Data: map[string]any{
		"type":       "control_request",
		"request_id": "auth-1",
		"request": map[string]any{
			"subtype": "auth_url_callback",
			"authState": map[string]any{
				"authUrl":  authURL,
				"methodId": "external",
			},
		},
	}}

	url, methodID, err := readAuthURLCallback(context.Background(), ch, tr)
	if err != nil {
		t.Fatalf("readAuthURLCallback: %v", err)
	}
	if url != authURL {
		t.Errorf("url: got %q, want %q", url, authURL)
	}
	if methodID == nil || *methodID != "external" {
		t.Errorf("methodID: got %v, want external", methodID)
	}
}

func TestReadAuthURLCallback_ClosedChannel(t *testing.T) {
	tr := newMockTransport(5)
	ch := make(chan RawMessage)
	close(ch)
	_, _, err := readAuthURLCallback(context.Background(), ch, tr)
	if err == nil {
		t.Fatal("expected error for closed channel")
	}
}

func TestReadAuthURLCallback_ContextCancel(t *testing.T) {
	tr := newMockTransport(5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan RawMessage)
	_, _, err := readAuthURLCallback(ctx, ch, tr)
	if err == nil {
		t.Fatal("expected error on canceled context")
	}
}

func TestReadAuthURLCallback_SkipsNonMatchingType(t *testing.T) {
	tr := newMockTransport(5)
	ch := make(chan RawMessage, 2)
	// First: not a control_request
	ch <- RawMessage{Data: map[string]any{"type": "assistant"}}
	// Second: matching
	ch <- RawMessage{Data: map[string]any{
		"type":       "control_request",
		"request_id": "auth-2",
		"request": map[string]any{
			"subtype": "auth_url_callback",
			"authState": map[string]any{
				"authUrl": "https://example.com/auth2",
			},
		},
	}}

	url, _, err := readAuthURLCallback(context.Background(), ch, tr)
	if err != nil {
		t.Fatalf("readAuthURLCallback: %v", err)
	}
	if url != "https://example.com/auth2" {
		t.Errorf("url: got %q", url)
	}
}

func TestReadAuthURLCallback_ErrorInChannel(t *testing.T) {
	tr := newMockTransport(5)
	ch := make(chan RawMessage, 1)
	ch <- RawMessage{Err: errors.New("connection error")}
	_, _, err := readAuthURLCallback(context.Background(), ch, tr)
	if err == nil {
		t.Fatal("expected error for channel error")
	}
}

func TestWaitForAuthResult_Success(t *testing.T) {
	tr := newMockTransport(5)
	ch := make(chan RawMessage, 1)
	ch <- RawMessage{Data: map[string]any{
		"type":       "control_request",
		"request_id": "res-1",
		"request": map[string]any{
			"subtype": "auth_result_callback",
			"success": true,
			"userinfo": map[string]any{
				"userId":   "u1",
				"username": "testuser",
			},
		},
	}}

	resp, err := waitForAuthResult(context.Background(), ch, tr)
	if err != nil {
		t.Fatalf("waitForAuthResult: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.UserInfo.UserID != "u1" {
		t.Errorf("UserID: got %q, want u1", resp.UserInfo.UserID)
	}
}

func TestWaitForAuthResult_Failure(t *testing.T) {
	tr := newMockTransport(5)
	ch := make(chan RawMessage, 1)
	ch <- RawMessage{Data: map[string]any{
		"type":       "control_request",
		"request_id": "res-2",
		"request": map[string]any{
			"subtype": "auth_result_callback",
			"success": false,
			"error": map[string]any{
				"type":    "auth_denied",
				"message": "user denied",
			},
		},
	}}

	_, err := waitForAuthResult(context.Background(), ch, tr)
	if err == nil {
		t.Fatal("expected error for failed auth")
	}
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Errorf("expected AuthenticationError, got %T", err)
	}
}

func TestWaitForAuthResult_ClosedChannel(t *testing.T) {
	tr := newMockTransport(5)
	ch := make(chan RawMessage)
	close(ch)
	_, err := waitForAuthResult(context.Background(), ch, tr)
	if err == nil {
		t.Fatal("expected error for closed channel")
	}
}

func TestWaitForAuthResult_ContextCancel(t *testing.T) {
	tr := newMockTransport(5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan RawMessage)
	_, err := waitForAuthResult(ctx, ch, tr)
	if err == nil {
		t.Fatal("expected error on canceled context")
	}
}

func TestWaitForAuthResult_SkipsNonMatching(t *testing.T) {
	tr := newMockTransport(5)
	ch := make(chan RawMessage, 2)
	// First: unrelated subtype
	ch <- RawMessage{Data: map[string]any{
		"type":       "control_request",
		"request_id": "x",
		"request":    map[string]any{"subtype": "other"},
	}}
	// Second: matching
	ch <- RawMessage{Data: map[string]any{
		"type":       "control_request",
		"request_id": "res-3",
		"request": map[string]any{
			"subtype": "auth_result_callback",
			"success": true,
			"userinfo": map[string]any{
				"userId": "u3",
			},
		},
	}}

	resp, err := waitForAuthResult(context.Background(), ch, tr)
	if err != nil {
		t.Fatalf("waitForAuthResult: %v", err)
	}
	if resp.UserInfo.UserID != "u3" {
		t.Errorf("UserID: got %q, want u3", resp.UserInfo.UserID)
	}
}
