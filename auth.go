// auth.go
// 认证模块 - 实现 CodeBuddy CLI 的登录和登出功能。
// 支持两阶段认证：先获取登录 URL，再等待用户完成浏览器授权。

package codebuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// AuthOptions 认证操作的配置选项
type AuthOptions struct {
	// MethodID 认证方法标识符，默认为 "external"
	MethodID string
	// Environment 预定义的环境名称（与 Endpoint 互斥）
	Environment *string
	// Endpoint 自定义端点 URL（与 Environment 互斥）
	Endpoint *string
	// CLIPath CLI 可执行文件路径（优先于环境变量）
	CLIPath *string
	// Env 附加环境变量
	Env map[string]string
	// Timeout 等待用户完成登录的超时时间（秒），默认 300
	Timeout float64
}

// authResult 内部用于传递认证结果的结构
type authResult struct {
	response *AuthenticateResponse
	err      error
}

// AuthFlow 封装异步认证流程，持有登录 URL 并可等待最终结果。
//
// 使用方式：
//
//	flow, _ := codebuddy.Authenticate(ctx, nil)
//	if flow.AuthURL != "" {
//	    fmt.Println("请访问:", flow.AuthURL)
//	}
//	result, err := flow.Wait(ctx)
type AuthFlow struct {
	// AuthURL 供用户访问的登录 URL；已登录时为空字符串
	AuthURL string
	// MethodID 认证方法 ID
	MethodID *string

	// 内部字段
	transport Transport
	resultCh  chan authResult       // 异步等待认证完成
	result    *AuthenticateResponse // 已登录时直接持有结果
	timeout   float64               // 认证等待超时（秒），来自 AuthOptions.Timeout
}

// Wait 阻塞直到用户完成认证或 ctx 超时/取消。
// 若 ctx 没有 deadline 且 AuthOptions.Timeout > 0，则自动应用该超时。
// 已登录时（AuthURL 为空）立即返回结果。
func (f *AuthFlow) Wait(ctx context.Context) (*AuthenticateResponse, error) {
	if f.result != nil {
		return f.result, nil
	}
	if f.resultCh == nil {
		return nil, &AuthenticationError{ErrorType: "auth_failed", Message: "认证流程未正确初始化"}
	}
	// 若 ctx 无 deadline 且配置了 Timeout，则自动设置超时
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(f.timeout*float64(time.Second)))
		defer cancel()
	}
	select {
	case r := <-f.resultCh:
		return r.response, r.err
	case <-ctx.Done():
		f.Cancel()
		return nil, &AuthenticationError{ErrorType: "timeout", Message: "认证超时"}
	}
}

// Cancel 取消认证流程并释放资源。
func (f *AuthFlow) Cancel() error {
	if f.transport != nil {
		return f.transport.Close()
	}
	return nil
}

// Authenticate 启动认证流程，返回包含登录 URL 的 AuthFlow 对象。
// 如果用户已登录，AuthFlow.AuthURL 为空字符串，直接 Wait() 即可获取用户信息。
//
// 示例:
//
//	flow, err := codebuddy.Authenticate(ctx, nil)
//	if flow.AuthURL != "" {
//	    fmt.Printf("请访问: %s\n", flow.AuthURL)
//	}
//	result, err := flow.Wait(ctx)
func Authenticate(ctx context.Context, opts *AuthOptions) (*AuthFlow, error) {
	if opts == nil {
		opts = &AuthOptions{}
	}
	if opts.MethodID == "" {
		opts.MethodID = "external"
	}

	// 构建最小 Options 用于创建 Transport
	transportOpts := &Options{
		CLIPath: opts.CLIPath,
		Env:     opts.Env,
	}
	transport := NewSubprocessTransport(transportOpts, nil)
	if err := transport.Connect(ctx); err != nil {
		return nil, err
	}

	msgCh := transport.ReadMessages()

	// 发送 initialize 请求，检查是否已登录
	initRequestID := "auth_init"
	var envVal, endpointVal any
	if opts.Environment != nil {
		envVal = *opts.Environment
	}
	if opts.Endpoint != nil {
		endpointVal = *opts.Endpoint
	}
	initReq := map[string]any{
		"type":       "control_request",
		"request_id": initRequestID,
		"request": map[string]any{
			"subtype":     "initialize",
			"environment": envVal,
			"endpoint":    endpointVal,
		},
	}
	b, err := json.Marshal(initReq)
	if err != nil {
		transport.Close()
		return nil, fmt.Errorf("marshal initialize request: %w", err)
	}
	if err := transport.Write(ctx, string(b)); err != nil {
		transport.Close()
		return nil, &CLIConnectionError{Message: fmt.Sprintf("发送 initialize 请求失败: %v", err)}
	}

	// 读取 initialize 响应
	initResp, err := readControlResponse(ctx, msgCh, initRequestID)
	if err != nil {
		transport.Close()
		return nil, err
	}

	// 检查是否已登录（account.userId + token 均非空）
	if account, ok := initResp["account"].(map[string]any); ok {
		userID, _ := account["userId"].(string)
		token, _ := account["token"].(string)
		if userID != "" && token != "" {
			transport.Close()
			userInfo := UserInfoFromMap(account)
			return &AuthFlow{
				AuthURL:   "",
				transport: transport,
				result:    &AuthenticateResponse{UserInfo: userInfo},
			}, nil
		}
	}

	// 未登录：发送 authenticate 请求启动登录流程
	authStartReq := map[string]any{
		"type":       "control_request",
		"request_id": "auth_start",
		"request": map[string]any{
			"subtype":     "authenticate",
			"methodId":    opts.MethodID,
			"environment": envVal,
			"endpoint":    endpointVal,
		},
	}
	b2, err := json.Marshal(authStartReq)
	if err != nil {
		transport.Close()
		return nil, fmt.Errorf("marshal authenticate request: %w", err)
	}
	if err := transport.Write(ctx, string(b2)); err != nil {
		transport.Close()
		return nil, &CLIConnectionError{Message: fmt.Sprintf("发送 authenticate 请求失败: %v", err)}
	}

	// 等待 auth_url_callback 控制请求
	authURL, methodID, err := readAuthURLCallback(ctx, msgCh, transport)
	if err != nil {
		transport.Close()
		return nil, err
	}

	// 创建 resultCh，异步等待 auth_result_callback
	resultCh := make(chan authResult, 1)
	flow := &AuthFlow{
		AuthURL:   authURL,
		MethodID:  methodID,
		transport: transport,
		resultCh:  resultCh,
		timeout:   opts.Timeout,
	}

	go func() {
		defer transport.Close()
		resp, err := waitForAuthResult(ctx, msgCh, transport)
		resultCh <- authResult{response: resp, err: err}
		close(resultCh)
	}()

	return flow, nil
}

// readControlResponse 从消息通道读取直到找到匹配 requestID 的 control_response。
func readControlResponse(ctx context.Context, msgCh <-chan RawMessage, requestID string) (map[string]any, error) {
	for {
		select {
		case raw, ok := <-msgCh:
			if !ok {
				return nil, &CLIConnectionError{Message: "连接在收到响应前关闭"}
			}
			if raw.Err != nil {
				return nil, raw.Err
			}
			data := raw.Data
			if tp, _ := data["type"].(string); tp != "control_response" {
				continue
			}
			resp, _ := data["response"].(map[string]any)
			if resp == nil {
				continue
			}
			if id, _ := resp["request_id"].(string); id != requestID {
				continue
			}
			if subtype, _ := resp["subtype"].(string); subtype == "error" {
				errMsg, _ := resp["error"].(string)
				return nil, &CLIConnectionError{Message: errMsg}
			}
			result, _ := resp["response"].(map[string]any)
			if result == nil {
				result = make(map[string]any)
			}
			return result, nil
		case <-ctx.Done():
			return nil, &AuthenticationError{ErrorType: "timeout", Message: "等待响应超时"}
		}
	}
}

// readAuthURLCallback 等待 CLI 发来 auth_url_callback 控制请求，返回登录 URL 和方法 ID。
func readAuthURLCallback(ctx context.Context, msgCh <-chan RawMessage, transport Transport) (string, *string, error) {
	for {
		select {
		case raw, ok := <-msgCh:
			if !ok {
				return "", nil, &AuthenticationError{ErrorType: "auth_failed", Message: "连接在收到登录 URL 前关闭"}
			}
			if raw.Err != nil {
				return "", nil, raw.Err
			}
			data := raw.Data
			if tp, _ := data["type"].(string); tp != "control_request" {
				continue
			}
			requestID, _ := data["request_id"].(string)
			request, _ := data["request"].(map[string]any)
			subtype, _ := request["subtype"].(string)
			if subtype != "auth_url_callback" {
				continue
			}
			authState, _ := request["authState"].(map[string]any)
			authURL, _ := authState["authUrl"].(string)
			var methodID *string
			if mid, ok := authState["methodId"].(string); ok && mid != "" {
				methodID = &mid
			}
			// 回复 CLI，确认收到
			_ = writeControlResponse(ctx, transport, BuildControlResponse(requestID, map[string]any{"received": true}))
			return authURL, methodID, nil
		case <-ctx.Done():
			return "", nil, &AuthenticationError{ErrorType: "timeout", Message: "等待登录 URL 超时"}
		}
	}
}

// waitForAuthResult 等待 CLI 发来 auth_result_callback 控制请求，返回认证结果。
func waitForAuthResult(ctx context.Context, msgCh <-chan RawMessage, transport Transport) (*AuthenticateResponse, error) {
	for {
		select {
		case raw, ok := <-msgCh:
			if !ok {
				return nil, &AuthenticationError{ErrorType: "auth_failed", Message: "认证流程意外结束"}
			}
			if raw.Err != nil {
				return nil, raw.Err
			}
			data := raw.Data
			if tp, _ := data["type"].(string); tp != "control_request" {
				continue
			}
			requestID, _ := data["request_id"].(string)
			request, _ := data["request"].(map[string]any)
			if subtype, _ := request["subtype"].(string); subtype != "auth_result_callback" {
				continue
			}
			success, _ := request["success"].(bool)
			userinfoData, _ := request["userinfo"].(map[string]any)
			errData, _ := request["error"].(map[string]any)

			// 确认收到
			_ = writeControlResponse(ctx, transport, BuildControlResponse(requestID, map[string]any{"handled": true}))

			if success && userinfoData != nil {
				return &AuthenticateResponse{UserInfo: UserInfoFromMap(userinfoData)}, nil
			}
			errType := "auth_failed"
			errMsg := "认证失败"
			if errData != nil {
				if t, ok := errData["type"].(string); ok && t != "" {
					errType = t
				}
				if m, ok := errData["message"].(string); ok && m != "" {
					errMsg = m
				}
			}
			return nil, &AuthenticationError{ErrorType: errType, Message: errMsg}
		case <-ctx.Done():
			return nil, &AuthenticationError{ErrorType: "timeout", Message: "等待认证结果超时"}
		}
	}
}

// Logout 清除 CLI 本地缓存的认证 token。
func Logout(ctx context.Context, opts *AuthOptions) error {
	if opts == nil {
		opts = &AuthOptions{}
	}
	transportOpts := &Options{
		CLIPath: opts.CLIPath,
		Env:     opts.Env,
	}
	transport := NewSubprocessTransport(transportOpts, nil)
	if err := transport.Connect(ctx); err != nil {
		return err
	}
	defer transport.Close()

	msgCh := transport.ReadMessages()

	requestID := "logout"
	var envVal, endpointVal any
	if opts.Environment != nil {
		envVal = *opts.Environment
	}
	if opts.Endpoint != nil {
		endpointVal = *opts.Endpoint
	}

	req := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request": map[string]any{
			"subtype":     "logout",
			"environment": envVal,
			"endpoint":    endpointVal,
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal logout request: %w", err)
	}
	if err := transport.Write(ctx, string(b)); err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("发送 logout 请求失败: %v", err)}
	}

	_, err = readControlResponse(ctx, msgCh, requestID)
	return err
}
