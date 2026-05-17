// transport_http_test.go
// httpACPTransport 和 ACPClient 的单元测试，使用 httptest.Server 模拟 ACP 服务端。

package codebuddy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// makeSSEServer 创建一个简单的 ACP mock 服务端。
// sseEvents 是 GET /acp 时通过 SSE 推送的事件列表（JSON 字符串）。
// postResponses 是收到 POST 时返回的 SSE 事件列表（JSON 字符串），按顺序消费。
func makeSSEServer(t *testing.T, sessionToken, connectionID string, sseEvents []string, postResponses []string) *httptest.Server {
	t.Helper()
	postIdx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("acp-session-token", sessionToken)
			w.Header().Set("acp-connection-id", connectionID)
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if ok {
				flusher.Flush()
			}
			for _, ev := range sseEvents {
				fmt.Fprintf(w, "data: %s\n\n", ev)
				if ok {
					flusher.Flush()
				}
			}
			// 持续阻塞，直到客户端断开
			<-r.Context().Done()
			return
		}
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if postIdx < len(postResponses) {
				fmt.Fprintf(w, "data: %s\n\n", postResponses[postIdx])
				postIdx++
			}
			return
		}
		http.NotFound(w, r)
	}))
}

// TestConnect_Success 验证正常握手时 sessionToken 和 connectionID 被正确提取。
func TestConnect_Success(t *testing.T) {
	srv := makeSSEServer(t, "tok-123", "conn-456", nil, nil)
	defer srv.Close()

	transport := newHttpACPTransport(srv.URL, "test-password")
	err := transport.connect(context.Background())
	if err != nil {
		t.Fatalf("connect() 预期成功，得到错误: %v", err)
	}
	defer transport.close()

	if transport.sessionToken != "tok-123" {
		t.Errorf("sessionToken = %q, 预期 %q", transport.sessionToken, "tok-123")
	}
	if transport.connectionID != "conn-456" {
		t.Errorf("connectionID = %q, 预期 %q", transport.connectionID, "conn-456")
	}
}

// TestConnect_MissingSessionToken 验证缺少 acp-session-token 时返回 ACPError。
func TestConnect_MissingSessionToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只设置 connection-id，不设置 session-token
		w.Header().Set("acp-connection-id", "conn-456")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	// 使用带超时的 context，避免等待 30s ResponseHeaderTimeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	transport := newHttpACPTransport(srv.URL, "test-password")
	err := transport.connect(ctx)
	if err == nil {
		t.Fatal("connect() 预期失败，得到 nil")
		transport.close()
	}

	var acpErr *ACPError
	if !errors.As(err, &acpErr) {
		t.Errorf("错误类型 = %T，预期 *ACPError", err)
	}
}

// TestConnect_MissingConnectionID 验证缺少 acp-connection-id 时返回 ACPError。
func TestConnect_MissingConnectionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只设置 session-token，不设置 connection-id
		w.Header().Set("acp-session-token", "tok-123")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	// 使用带超时的 context，避免等待 30s ResponseHeaderTimeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	transport := newHttpACPTransport(srv.URL, "test-password")
	err := transport.connect(ctx)
	if err == nil {
		t.Fatal("connect() 预期失败，得到 nil")
		transport.close()
	}

	var acpErr *ACPError
	if !errors.As(err, &acpErr) {
		t.Errorf("错误类型 = %T，预期 *ACPError", err)
	}
}

// TestConnect_HTTPError 验证服务端返回 401 时返回 ACPError。
func TestConnect_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	transport := newHttpACPTransport(srv.URL, "bad-token")
	err := transport.connect(context.Background())
	if err == nil {
		t.Fatal("connect() 预期失败，得到 nil")
		transport.close()
	}

	var acpErr *ACPError
	if !errors.As(err, &acpErr) {
		t.Errorf("错误类型 = %T，预期 *ACPError", err)
	}
}

// TestClose_Idempotent 验证多次调用 close() 不会 panic。
func TestClose_Idempotent(t *testing.T) {
	srv := makeSSEServer(t, "tok-123", "conn-456", nil, nil)
	defer srv.Close()

	transport := newHttpACPTransport(srv.URL, "test-password")
	if err := transport.connect(context.Background()); err != nil {
		t.Fatalf("connect() 失败: %v", err)
	}

	// 多次调用 close() 不应 panic
	transport.close()
	transport.close()
	transport.close()
}

// TestParseSSEEvents 验证 parseSSEEvents 能正确解析 SSE 格式。
func TestParseSSEEvents(t *testing.T) {
	input := []byte("data: {\"id\":1,\"result\":{}}\n\ndata: {\"method\":\"session/update\"}\n\n")
	events := parseSSEEvents(input)
	if len(events) != 2 {
		t.Errorf("解析事件数 = %d，预期 2", len(events))
	}
}

// TestParseSSEEvents_InvalidJSON 验证无效 JSON 被跳过。
func TestParseSSEEvents_InvalidJSON(t *testing.T) {
	input := []byte("data: not-json\ndata: {\"id\":1}\n")
	events := parseSSEEvents(input)
	if len(events) != 1 {
		t.Errorf("解析事件数 = %d，预期 1", len(events))
	}
}

// TestExtractTextFromUpdates 验证文本提取逻辑。
func TestExtractTextFromUpdates(t *testing.T) {
	updates := []map[string]any{
		{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "Hello, "},
		},
		{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "World!"},
		},
		{
			"sessionUpdate": "session_end",
		},
	}

	result := extractTextFromUpdates(updates)
	if result != "Hello, World!" {
		t.Errorf("extractTextFromUpdates() = %q, 预期 %q", result, "Hello, World!")
	}
}

// TestExtractTextFromUpdates_Empty 验证空列表返回空字符串。
func TestExtractTextFromUpdates_Empty(t *testing.T) {
	result := extractTextFromUpdates(nil)
	if result != "" {
		t.Errorf("extractTextFromUpdates(nil) = %q, 预期空字符串", result)
	}
}

// TestExtractTextFromUpdates_NoText 验证无文本内容时返回空字符串。
func TestExtractTextFromUpdates_NoText(t *testing.T) {
	updates := []map[string]any{
		{"sessionUpdate": "session_end"},
	}
	result := extractTextFromUpdates(updates)
	if result != "" {
		t.Errorf("extractTextFromUpdates() = %q, 预期空字符串", result)
	}
}

// TestWaitForUpdates_SessionEnd 验证收到 session_end 后正常返回。
func TestWaitForUpdates_SessionEnd(t *testing.T) {
	transport := &httpACPTransport{
		eventQueue: make(chan map[string]any, 10),
	}
	transport.ctx, transport.cancel = context.WithCancel(context.Background())
	defer transport.cancel()

	// 注入 session/update 事件
	go func() {
		transport.eventQueue <- map[string]any{
			"method": "session/update",
			"params": map[string]any{
				"sessionId": "sess-1",
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]any{"type": "text", "text": "hi"},
				},
			},
		}
		transport.eventQueue <- map[string]any{
			"method": "session/update",
			"params": map[string]any{
				"sessionId": "sess-1",
				"update": map[string]any{
					"sessionUpdate": "session_end",
				},
			},
		}
	}()

	updates, err := transport.waitForUpdates(context.Background(), "sess-1", 10*time.Second, nil)
	if err != nil {
		t.Fatalf("waitForUpdates() 错误: %v", err)
	}
	if len(updates) != 2 {
		t.Errorf("updates 长度 = %d，预期 2", len(updates))
	}
}

// TestWaitForUpdates_Timeout 验证超时时返回 ACPTimeoutError。
func TestWaitForUpdates_Timeout(t *testing.T) {
	transport := &httpACPTransport{
		eventQueue: make(chan map[string]any, 10),
	}
	transport.ctx, transport.cancel = context.WithCancel(context.Background())
	defer transport.cancel()

	// 不发送任何事件，触发超时
	_, err := transport.waitForUpdates(context.Background(), "sess-1", 100*time.Millisecond, nil)
	if err == nil {
		t.Fatal("waitForUpdates() 预期超时错误，得到 nil")
	}

	var timeoutErr *ACPTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Errorf("错误类型 = %T，预期 *ACPTimeoutError", err)
	}
}

// TestPostRPC_Success 验证 postRPC 能正确解析响应。
func TestPostRPC_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("acp-session-token", "tok")
			w.Header().Set("acp-connection-id", "conn")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"sessionId\":\"abc\"}}\n\n")
	}))
	defer srv.Close()

	transport := newHttpACPTransport(srv.URL, "token")
	if err := transport.connect(context.Background()); err != nil {
		t.Fatalf("connect(): %v", err)
	}
	defer transport.close()

	result, err := transport.postRPC(context.Background(), "session/new", map[string]any{}, 1)
	if err != nil {
		t.Fatalf("postRPC() 错误: %v", err)
	}
	if result["sessionId"] != "abc" {
		t.Errorf("result[sessionId] = %v，预期 abc", result["sessionId"])
	}
}

// TestPrompt_AutoApprovePermission 验证自动批准权限请求。
func TestPrompt_AutoApprovePermission(t *testing.T) {
	approvals := make(chan struct{}, 5)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("acp-session-token", "tok")
			w.Header().Set("acp-connection-id", "conn")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			// 推送一个权限请求事件
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":99,\"method\":\"session/request_permission\",\"params\":{}}\n\n")
			flusher.Flush()
			<-r.Context().Done()
			return
		}
		if r.Method == http.MethodPost {
			// 记录收到的 POST（权限批准）
			approvals <- struct{}{}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
		}
	}))
	defer srv.Close()

	transport := newHttpACPTransport(srv.URL, "token")
	if err := transport.connect(context.Background()); err != nil {
		t.Fatalf("connect(): %v", err)
	}
	defer transport.close()

	// 等待 approvePermission 被调用
	select {
	case <-approvals:
		// 权限批准 POST 被发送，测试通过
	case <-time.After(3 * time.Second):
		t.Error("超时：未收到权限批准请求")
	}
}

// TestACPClient_Prompt_NotConnected 验证未连接时调用 Prompt 返回 ACPError。
func TestACPClient_Prompt_NotConnected(t *testing.T) {
	client := NewACPClient("http://localhost:9999/acp", "token")
	_, err := client.Prompt(context.Background(), "hello", nil)
	if err == nil {
		t.Fatal("Prompt() 预期错误，得到 nil")
	}

	var acpErr *ACPError
	if !errors.As(err, &acpErr) {
		t.Errorf("错误类型 = %T，预期 *ACPError", err)
	}

	if !strings.Contains(err.Error(), "未连接") {
		t.Errorf("错误信息 = %q，预期包含 '未连接'", err.Error())
	}
}

// makeFullACPServer 创建完整的 ACP mock 服务端，支持完整握手流程。
func makeFullACPServer(t *testing.T) (*httptest.Server, chan string) {
	t.Helper()
	receivedMethods := make(chan string, 20)

	postCallCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("acp-session-token", "full-tok")
			w.Header().Set("acp-connection-id", "full-conn")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if r.Method == http.MethodPost {
			postCallCount++
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			// 根据调用顺序返回不同响应
			switch postCallCount {
			case 1: // initialize
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":1}}\n\n")
				receivedMethods <- "initialize"
			case 2: // session/new
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"sessionId\":\"test-session-id\"}}\n\n")
				receivedMethods <- "session/new"
			case 3: // session/set_mode
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{}}\n\n")
				receivedMethods <- "session/set_mode"
			default:
				// session/prompt 或其他
				receivedMethods <- fmt.Sprintf("call-%d", postCallCount)
			}
		}
	}))
	return srv, receivedMethods
}

// TestACPClient_Connect_Success 验证完整握手流程。
func TestACPClient_Connect_Success(t *testing.T) {
	srv, methods := makeFullACPServer(t)
	defer srv.Close()

	client := NewACPClient(srv.URL, "token")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() 错误: %v", err)
	}
	defer client.Disconnect()

	// 验证握手顺序
	expected := []string{"initialize", "session/new", "session/set_mode"}
	for _, exp := range expected {
		select {
		case got := <-methods:
			if got != exp {
				t.Errorf("握手方法 = %q，预期 %q", got, exp)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("超时：未收到方法 %q", exp)
		}
	}
}

// TestACPClient_Disconnect_Idempotent 验证多次调用 Disconnect 不会 panic。
func TestACPClient_Disconnect_Idempotent(t *testing.T) {
	client := NewACPClient("http://localhost:9999/acp", "token")
	// 未连接时调用 Disconnect
	client.Disconnect()
	client.Disconnect()
	client.Disconnect()
}

// TestACPClient_MultiplePrompts_SameSession 验证多次 Prompt 使用相同 sessionID。
func TestACPClient_MultiplePrompts_SameSession(t *testing.T) {
	sessionIDs := make(chan string, 10)
	postCallCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("acp-session-token", "tok")
			w.Header().Set("acp-connection-id", "conn")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// 为每次 prompt 推送 session_end 事件
			flusher := w.(http.Flusher)
			for i := 0; i < 2; i++ {
				<-time.After(100 * time.Millisecond)
				fmt.Fprintf(w, "data: {\"method\":\"session/update\",\"params\":{\"sessionId\":\"my-session\",\"update\":{\"sessionUpdate\":\"session_end\"}}}\n\n")
				flusher.Flush()
			}
			<-r.Context().Done()
			return
		}
		if r.Method == http.MethodPost {
			postCallCount++
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			switch postCallCount {
			case 1: // initialize
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":1}}\n\n")
			case 2: // session/new
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"sessionId\":\"my-session\"}}\n\n")
			case 3: // session/set_mode
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{}}\n\n")
			default:
				// session/prompt - 记录 sessionId 参数（从请求体解析）
				sessionIDs <- "my-session"
			}
		}
	}))
	defer srv.Close()

	client := NewACPClient(srv.URL, "token")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() 错误: %v", err)
	}
	defer client.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 第一次 Prompt
	_, _ = client.Prompt(ctx, "第一个问题", nil)
	// 第二次 Prompt
	_, _ = client.Prompt(ctx, "第二个问题", nil)
}

// TestACPClient_RunTask_Success 验证 RunTask 返回拼接文本。
func TestACPClient_RunTask_Success(t *testing.T) {
	postCallCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("acp-session-token", "tok")
			w.Header().Set("acp-connection-id", "conn")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			// 推送文本事件然后 session_end
			<-time.After(200 * time.Millisecond)
			fmt.Fprintf(w, "data: {\"method\":\"session/update\",\"params\":{\"sessionId\":\"run-sess\",\"update\":{\"sessionUpdate\":\"agent_message_chunk\",\"content\":{\"type\":\"text\",\"text\":\"Hello World\"}}}}\n\n")
			flusher.Flush()
			<-time.After(50 * time.Millisecond)
			fmt.Fprintf(w, "data: {\"method\":\"session/update\",\"params\":{\"sessionId\":\"run-sess\",\"update\":{\"sessionUpdate\":\"session_end\"}}}\n\n")
			flusher.Flush()
			<-r.Context().Done()
			return
		}
		if r.Method == http.MethodPost {
			postCallCount++
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			switch postCallCount {
			case 1:
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":1}}\n\n")
			case 2:
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"sessionId\":\"run-sess\"}}\n\n")
			case 3:
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{}}\n\n")
			}
		}
	}))
	defer srv.Close()

	client := NewACPClientWithTimeout(srv.URL, "token", 10*time.Second)
	result, err := client.RunTask(context.Background(), "说 hello")
	if err != nil {
		t.Fatalf("RunTask() 错误: %v", err)
	}
	if result != "Hello World" {
		t.Errorf("RunTask() = %q，预期 %q", result, "Hello World")
	}
}
