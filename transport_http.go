// transport_http.go
// ACP HTTP 传输层 - 通过 HTTP/SSE 双通道实现与 CodeBuddy ACP 服务端的通信。
//
// 协议: 双通道 SSE + JSON-RPC 2.0
//   - GET  /acp  → 持久 SSE 流（服务端→客户端事件）
//   - POST /acp  → JSON-RPC 2.0 请求；响应可通过 SSE 或直接响应体到达
//
// 认证: Authorization: Bearer <password>
// POST 必需请求头:
//   - acp-session-token   (从 GET /acp 响应头获取)
//   - acp-connection-id   (从 GET /acp 响应头获取)

package codebuddy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// httpACPTransport 管理 ACP HTTP/SSE 双通道连接（包私有）。
// 对应 Python 实现中的 _AcpHttpTransport。
type httpACPTransport struct {
	// url ACP 端点地址，如 "http://localhost:8848/acp"
	url string
	// authHeader Bearer Token 认证头值，如 "Bearer xxx"
	authHeader string
	// sessionToken 从 GET /acp 响应头 acp-session-token 获取
	sessionToken string
	// connectionID 从 GET /acp 响应头 acp-connection-id 获取
	connectionID string
	// reqCounter JSON-RPC 请求 ID 单调递增计数器
	reqCounter atomic.Int64
	// sseResp 持久 SSE GET 响应（连接期间持有）
	sseResp *http.Response
	// eventQueue 后台 SSE 监听 goroutine 写入的事件队列
	eventQueue chan map[string]any
	// ctx 连接生命周期 context
	ctx context.Context
	// cancel 取消函数，用于关闭连接
	cancel context.CancelFunc
	// wg 等待后台 goroutine 退出
	wg sync.WaitGroup
	// closeOnce 保证 close() 幂等
	closeOnce sync.Once
}

// newHttpACPTransport 创建 httpACPTransport 实例。
// url 为 ACP 端点地址，password 为 Bearer Token。
func newHttpACPTransport(url, password string) *httpACPTransport {
	return &httpACPTransport{
		url:        url,
		authHeader: "Bearer " + password,
	}
}

// connect 建立持久 SSE GET 连接，获取 session-token 和 connection-id，启动后台监听。
func (t *httpACPTransport) connect(ctx context.Context) error {
	// 创建带 cancel 的子 context，控制连接生命周期
	t.ctx, t.cancel = context.WithCancel(ctx)

	// 使用带 ResponseHeaderTimeout 的 Transport，防止握手挂起
	httpClient := &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}

	req, err := http.NewRequestWithContext(t.ctx, http.MethodGet, t.url, nil)
	if err != nil {
		t.cancel()
		return &ACPError{Message: fmt.Sprintf("创建 SSE 请求失败: %v", err), Cause: err}
	}
	req.Header.Set("Authorization", t.authHeader)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.cancel()
		return &ACPError{Message: fmt.Sprintf("建立 SSE 连接失败: %v", err), Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.cancel()
		return &ACPError{Message: fmt.Sprintf("ACP 服务端返回非 200 状态: %d", resp.StatusCode)}
	}

	// 从响应头提取会话令牌和连接标识
	t.sessionToken = resp.Header.Get("acp-session-token")
	t.connectionID = resp.Header.Get("acp-connection-id")
	if t.sessionToken == "" {
		resp.Body.Close()
		t.cancel()
		return &ACPError{Message: "ACP 服务端未返回 acp-session-token 响应头"}
	}
	if t.connectionID == "" {
		resp.Body.Close()
		t.cancel()
		return &ACPError{Message: "ACP 服务端未返回 acp-connection-id 响应头"}
	}

	t.sseResp = resp
	// 初始化事件队列，缓冲 100 个事件
	t.eventQueue = make(chan map[string]any, 100)

	// 启动后台 SSE 监听 goroutine
	t.wg.Add(1)
	go t.sseListener()

	return nil
}

// sseListener 后台持续读取 SSE GET 流，将事件写入 eventQueue。
// 遇到 session/request_permission 时自动批准，其他事件入队。
func (t *httpACPTransport) sseListener() {
	defer t.wg.Done()

	scanner := bufio.NewScanner(t.sseResp.Body)
	for scanner.Scan() {
		// 检查 context 是否已取消
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" {
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal([]byte(payload), &obj); err != nil {
			continue
		}

		// 权限请求由独立 goroutine 自动批准，不入队
		if method, _ := obj["method"].(string); method == "session/request_permission" {
			go t.approvePermission(obj)
			continue
		}

		// 将事件写入队列（阻塞写入，背压机制）
		select {
		case t.eventQueue <- obj:
		case <-t.ctx.Done():
			return
		}
	}
}

// close 关闭连接，取消 context，等待后台 goroutine 退出。幂等操作。
func (t *httpACPTransport) close() {
	t.closeOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
		if t.sseResp != nil {
			t.sseResp.Body.Close()
		}
		t.wg.Wait()
	})
}

// nextID 返回下一个单调递增的 JSON-RPC 请求 ID。
func (t *httpACPTransport) nextID() int64 {
	return t.reqCounter.Add(1)
}

// parseSSEEvents 解析原始 SSE 响应体，返回所有 data: 行解析出的 JSON 对象列表。
func parseSSEEvents(body []byte) []map[string]any {
	var events []map[string]any
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(payload), &obj); err == nil {
			events = append(events, obj)
		}
	}
	return events
}

// postRPC 发送 JSON-RPC 2.0 POST 请求并等待匹配响应。
// 从响应体（SSE 格式）中找到 id 匹配的事件，返回其 result 字段。
func (t *httpACPTransport) postRPC(ctx context.Context, method string, params map[string]any, id int64) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	})
	if err != nil {
		return nil, &ACPError{Message: fmt.Sprintf("序列化 JSON-RPC 请求失败: %v", err), Cause: err}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, &ACPError{Message: fmt.Sprintf("创建 POST 请求失败: %v", err), Cause: err}
	}
	req.Header.Set("Authorization", t.authHeader)
	req.Header.Set("acp-session-token", t.sessionToken)
	req.Header.Set("acp-connection-id", t.connectionID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &ACPError{Message: fmt.Sprintf("POST 请求失败: %v", err), Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ACPError{Message: fmt.Sprintf("ACP 服务端返回 HTTP %d", resp.StatusCode)}
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ACPError{Message: fmt.Sprintf("读取响应体失败: %v", err), Cause: err}
	}

	events := parseSSEEvents(respBody)
	for _, ev := range events {
		// 匹配响应 ID（JSON number 可能被解析为 float64）
		evID, ok := ev["id"]
		if !ok {
			continue
		}
		var evIDInt int64
		switch v := evID.(type) {
		case float64:
			evIDInt = int64(v)
		case int64:
			evIDInt = v
		default:
			continue
		}
		if evIDInt != id {
			continue
		}
		if errField, hasErr := ev["error"]; hasErr {
			return nil, &ACPError{Message: fmt.Sprintf("JSON-RPC 错误: %v", errField)}
		}
		if result, ok := ev["result"].(map[string]any); ok {
			return result, nil
		}
		return map[string]any{}, nil
	}
	return nil, &ACPError{Message: fmt.Sprintf("响应中未找到 id=%d 的 JSON-RPC 结果，收到事件: %v", id, events)}
}

// postRPCNoWait 发送 JSON-RPC 2.0 POST 请求，收到 HTTP 2xx 即返回，不等待响应体。
// 真实响应通过持久 SSE GET 频道到达。
func (t *httpACPTransport) postRPCNoWait(ctx context.Context, method string, params map[string]any, id int64) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	})
	if err != nil {
		return &ACPError{Message: fmt.Sprintf("序列化 JSON-RPC 请求失败: %v", err), Cause: err}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return &ACPError{Message: fmt.Sprintf("创建 POST 请求失败: %v", err), Cause: err}
	}
	req.Header.Set("Authorization", t.authHeader)
	req.Header.Set("acp-session-token", t.sessionToken)
	req.Header.Set("acp-connection-id", t.connectionID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return &ACPError{Message: fmt.Sprintf("POST 请求失败: %v", err), Cause: err}
	}
	// 不读取响应体，但需关闭以释放连接
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ACPError{Message: fmt.Sprintf("ACP 服务端返回 HTTP %d", resp.StatusCode)}
	}
	return nil
}

// approvePermission 自动批准服务端发出的 session/request_permission 请求。
// 发送 allow_always 响应，使工具调用无需人工干预。
func (t *httpACPTransport) approvePermission(request map[string]any) {
	reqID := request["id"]
	reply := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"result": map[string]any{
			"outcome": map[string]any{
				"outcome":  "selected",
				"optionId": "allow_always",
			},
		},
	}
	body, err := json.Marshal(reply)
	if err != nil {
		log.Printf("ACP: 序列化权限批准响应失败: %v", err)
		return
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		log.Printf("ACP: 创建权限批准请求失败: %v", err)
		return
	}
	req.Header.Set("Authorization", t.authHeader)
	req.Header.Set("acp-session-token", t.sessionToken)
	req.Header.Set("acp-connection-id", t.connectionID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ACP: 发送权限批准失败: %v", err)
		return
	}
	resp.Body.Close()
}

// waitForUpdates 等待指定会话的 session/update 事件流，直到代理回合结束。
// 使用 15 秒块间静默超时，总超时由 timeout 参数控制。
// onUpdate 为可选回调，每收到一个事件时调用。
func (t *httpACPTransport) waitForUpdates(ctx context.Context, sessionID string, timeout time.Duration, onUpdate func(map[string]any)) ([]map[string]any, error) {
	var updates []map[string]any
	deadline := time.Now().Add(timeout)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if len(updates) > 0 {
				// 有部分响应，视为完成
				break
			}
			return nil, &ACPTimeoutError{
				Message: fmt.Sprintf("等待 ACP 响应超时（%.0f 秒内未收到任何响应）", timeout.Seconds()),
			}
		}

		// 块间超时取剩余时间和 15s 的较小值
		chunkTimeout := min(15*time.Second, remaining)

		timer := time.NewTimer(chunkTimeout)
		select {
		case <-ctx.Done():
			timer.Stop()
			return updates, ctx.Err()

		case <-timer.C:
			// 块间超时
			if len(updates) > 0 {
				return updates, nil
			}
			return nil, &ACPTimeoutError{
				Message: fmt.Sprintf("等待 ACP 响应超时（%.0f 秒内无新事件）", timeout.Seconds()),
			}

		case obj, ok := <-t.eventQueue:
			timer.Stop()
			if !ok {
				// 队列已关闭
				return updates, nil
			}

			method, _ := obj["method"].(string)
			if method != "session/update" {
				continue
			}

			params, _ := obj["params"].(map[string]any)
			if params == nil {
				continue
			}

			if params["sessionId"] != sessionID {
				continue
			}

			update, _ := params["update"].(map[string]any)
			if update == nil {
				continue
			}

			updates = append(updates, update)
			if onUpdate != nil {
				onUpdate(update)
			}

			// 重置块间超时
			deadline = time.Now().Add(15 * time.Second)

			su, _ := update["sessionUpdate"].(string)
			if su == "session_end" {
				return updates, nil
			}
			if su == "session_info_update" {
				meta, _ := update["_meta"].(map[string]any)
				permResolved, _ := meta["codebuddy.ai/permissionResolved"].(bool)
				if !permResolved {
					return updates, nil
				}
			}
		}
	}

	return updates, nil
}
