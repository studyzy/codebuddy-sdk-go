// codebuddy.go
// CodeBuddy Agent SDK for Go - 包入口文件
// 提供顶层 API：Query（单次查询）、Authenticate（认证）、Logout（登出）

package codebuddy

import (
	"context"
	"encoding/json"
	"fmt"
)

// Prompt 是一次性查询的便捷函数，发送 prompt 并直接返回最终结果。
//
// 内部调用 Query 发起查询，自动遍历消息通道直到收到 ResultMessage。
// 若结果包含错误（ResultMessage.IsError），返回 ExecutionError。
// 适合不需要逐条处理中间消息的简单查询场景。
//
// 参数:
//   - ctx：控制取消和超时
//   - message：查询内容字符串
//   - opts：可选配置，nil 时使用默认值
//
// 返回:
//   - *ResultMessage：查询结果
//   - error：查询失败时返回错误
//
// 示例:
//
//	result, err := codebuddy.Prompt(ctx, "What is 2+2?", nil)
//	if err != nil { log.Fatal(err) }
//	fmt.Println(*result.Result)
func Prompt(ctx context.Context, message string, opts *Options) (*ResultMessage, error) {
	msgCh, err := Query(ctx, message, opts)
	if err != nil {
		return nil, err
	}
	for msg := range msgCh {
		switch m := msg.(type) {
		case *ResultMessage:
			if m.IsError && len(m.Errors) > 0 {
				return nil, NewExecutionError(m.Errors, m.Subtype)
			}
			return m, nil
		case *ErrorMessage:
			return nil, &CLIConnectionError{Message: m.Error}
		}
	}
	return nil, &CLIConnectionError{Message: "未收到结果消息"}
}

// Query 向 CodeBuddy 发送单次查询，返回消息通道。
//
// 适用于无需管理连接生命周期的一次性查询。函数建立连接后立即返回通道，
// 调用方通过 range 迭代接收消息，收到 ResultMessage 或 ErrorMessage 后通道自动关闭。
//
// 参数:
//   - ctx：控制取消和超时
//   - prompt：查询内容，可以是 string 或 <-chan map[string]any（流式输入）
//   - opts：可选配置，nil 时使用默认值
//
// 返回:
//   - <-chan Message：消息通道，调用方 range 迭代
//   - error：连接建立失败时立即返回错误
//
// 示例:
//
//	msgCh, err := codebuddy.Query(ctx, "What is 2+2?", nil)
//	if err != nil { log.Fatal(err) }
//	for msg := range msgCh {
//	    if r, ok := msg.(*codebuddy.ResultMessage); ok {
//	        fmt.Println(r.Result)
//	    }
//	}
func Query(ctx context.Context, prompt any, opts *Options) (<-chan Message, error) {
	return queryWithTransport(ctx, prompt, opts, nil)
}

// queryWithTransport is the internal implementation that accepts an optional injected transport.
// When injectTransport is nil, a SubprocessTransport is created from opts.
func queryWithTransport(ctx context.Context, prompt any, opts *Options, injectTransport Transport) (<-chan Message, error) {
	if opts == nil {
		opts = &Options{}
	}

	var transport Transport
	if injectTransport != nil {
		transport = injectTransport
		if err := transport.Connect(ctx); err != nil {
			return nil, err
		}
	} else {
		t := NewSubprocessTransport(opts, prompt)
		if err := t.Connect(ctx); err != nil {
			return nil, err
		}
		transport = t
	}

	msgCh := make(chan Message, 100)

	go func() {
		defer close(msgCh)
		defer transport.Close()

		// 发送 initialize 控制请求，获取 hook 回调注册表
		hooksConfig, hookRegistry, err := sendInitialize(ctx, transport, opts, true)
		if err != nil {
			// initialize 失败，忽略（CLI 可能不支持）
			_ = hooksConfig
		}
		_ = hookRegistry

		// 发送用户 prompt
		if err := sendPrompt(ctx, transport, prompt); err != nil {
			return
		}

		// 读取消息流
		for raw := range transport.ReadMessages() {
			if raw.Err != nil {
				select {
				case msgCh <- &ErrorMessage{Error: raw.Err.Error(), Errors: []string{raw.Err.Error()}}:
				case <-ctx.Done():
				}
				return
			}

			data := raw.Data
			msgType, _ := data["type"].(string)

			// 控制请求：处理 hook/权限/MCP，不传给调用方
			if msgType == "control_request" {
				handleQueryControlRequest(ctx, transport, data, opts, hookRegistry)
				continue
			}

			msg := ParseMessage(data)
			if msg == nil {
				continue
			}

			// ResultMessage：检查错误，然后发送并结束
			if result, ok := msg.(*ResultMessage); ok {
				if result.IsError && len(result.Errors) > 0 {
					errMsg := result.Errors[0]
					subtype := result.Subtype
					select {
					case msgCh <- &ErrorMessage{Error: errMsg, Errors: result.Errors, Subtype: &subtype, SessionID: &result.SessionID}:
					case <-ctx.Done():
					}
					return
				}
				select {
				case msgCh <- msg:
				case <-ctx.Done():
					return
				}
				return
			}

			select {
			case msgCh <- msg:
			case <-ctx.Done():
				return
			}

			// ErrorMessage：发送后结束
			if _, ok := msg.(*ErrorMessage); ok {
				return
			}
		}
	}()

	return msgCh, nil
}

// sendInitialize 发送 initialize 控制请求，告知 CLI SDK 能力和 Hook 配置。
// 返回 hooksConfig、hookRegistry 和可能的错误。
func sendInitialize(ctx context.Context, transport Transport, opts *Options, hasPrompt bool) (map[string]any, HookCallbackRegistry, error) {
	hooksConfig, registry := BuildHooksConfig(opts.Hooks)

	sdkMCPServers := transport.SDKMCPServerNames()
	var sdkMCPServersVal any
	if len(sdkMCPServers) > 0 {
		sdkMCPServersVal = sdkMCPServers
	}

	// 解析系统提示配置
	var systemPrompt any
	var appendSystemPrompt any
	if opts.SystemPrompt != nil {
		if opts.SystemPrompt.Override != nil {
			systemPrompt = *opts.SystemPrompt.Override
		} else if opts.SystemPrompt.Append != nil {
			appendSystemPrompt = *opts.SystemPrompt.Append
		}
	}

	// 解析 agents 配置
	var agentsConfig any
	if len(opts.Agents) > 0 {
		m := make(map[string]any)
		for name, ag := range opts.Agents {
			entry := map[string]any{
				"description":     ag.Description,
				"prompt":          ag.Prompt,
				"tools":           ag.Tools,
				"disallowedTools": ag.DisallowedTools,
				"model":           ag.Model,
			}
			if ag.Temperature != nil {
				entry["temperature"] = *ag.Temperature
			}
			if ag.MaxTokens != nil {
				entry["maxTokens"] = *ag.MaxTokens
			}
			m[name] = entry
		}
		agentsConfig = m
	}

	requestID := fmt.Sprintf("init_%p", opts)
	initPayload := map[string]any{
		"subtype":            "initialize",
		"hooks":              hooksConfig,
		"systemPrompt":       systemPrompt,
		"appendSystemPrompt": appendSystemPrompt,
		"agents":             agentsConfig,
		"sdkMcpServers":      sdkMCPServersVal,
		"hasPrompt":          hasPrompt,
		"capabilities": map[string]any{
			"askUserQuestion": true,
		},
	}
	// 注入新增的 initialize 字段
	if opts.OutputFormat != nil {
		initPayload["jsonSchema"] = opts.OutputFormat.Schema
	}
	if opts.Environment != nil {
		initPayload["environment"] = *opts.Environment
	}
	if opts.Endpoint != nil {
		initPayload["endpoint"] = *opts.Endpoint
	}
	request := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    initPayload,
	}

	if err := writeControlResponse(ctx, transport, request); err != nil {
		return nil, registry, err
	}
	return nil, registry, nil
}

// sendPrompt 将用户 prompt 发送给 CLI。
// prompt 可以是 string（直接发送）或 <-chan map[string]any（流式发送）。
func sendPrompt(ctx context.Context, transport Transport, prompt any) error {
	if prompt == nil {
		return nil
	}
	switch p := prompt.(type) {
	case string:
		msg := map[string]any{
			"type":       "user",
			"session_id": "",
			"message": map[string]any{
				"role":    "user",
				"content": p,
			},
			"parent_tool_use_id": nil,
		}
		b, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshal user message: %w", err)
		}
		return transport.Write(ctx, string(b))
	case <-chan map[string]any:
		go func() {
			for m := range p {
				b, err := json.Marshal(m)
				if err != nil {
					continue
				}
				_ = transport.Write(ctx, string(b))
			}
		}()
	}
	return nil
}

// handleQueryControlRequest 处理 CLI 发来的控制请求（query 模式，单 goroutine 处理）。
func handleQueryControlRequest(ctx context.Context, transport Transport, data map[string]any, opts *Options, registry HookCallbackRegistry) {
	requestID, _ := data["request_id"].(string)
	request, _ := data["request"].(map[string]any)
	subtype, _ := request["subtype"].(string)

	switch subtype {
	case "hook_callback":
		callbackID, _ := request["callback_id"].(string)
		hookInput, _ := request["input"].(map[string]any)
		toolUseID := getStringPtrFromMap(request, "tool_use_id")

		output := executeHook(ctx, callbackID, hookInput, toolUseID, registry)
		_ = writeControlResponse(ctx, transport, BuildControlResponse(requestID, output))

	case "can_use_tool":
		handlePermissionRequest(ctx, transport, requestID, request, opts)

	case "mcp_message":
		if st, ok := transport.(*SubprocessTransport); ok {
			st.HandleMCPMessageRequest(ctx, requestID, request)
		}
	}
}

// executeHook 从注册表查找并执行 Hook 回调。
func executeHook(ctx context.Context, callbackID string, hookInput map[string]any, toolUseID *string, registry HookCallbackRegistry) map[string]any {
	hook, ok := registry[callbackID]
	if !ok {
		return map[string]any{"continue": true}
	}
	result, err := hook(ctx, hookInput, toolUseID)
	if err != nil {
		return map[string]any{"continue": false, "stopReason": err.Error()}
	}
	output := map[string]any{}
	if result.Continue != nil {
		output["continue"] = *result.Continue
	}
	if result.SuppressOutput != nil {
		output["suppressOutput"] = *result.SuppressOutput
	}
	if result.StopReason != nil {
		output["stopReason"] = *result.StopReason
	}
	if result.Decision != nil {
		output["decision"] = *result.Decision
	}
	if result.Reason != nil {
		output["reason"] = *result.Reason
	}
	if result.SystemMessage != nil {
		output["systemMessage"] = *result.SystemMessage
	}
	if len(result.HookSpecificOutput) > 0 {
		output["hookSpecificOutput"] = result.HookSpecificOutput
	}
	return output
}

// handlePermissionRequest 处理 CLI 的 can_use_tool 权限请求。
// 若未配置 CanUseTool 回调，默认拒绝所有权限请求。
func handlePermissionRequest(ctx context.Context, transport Transport, requestID string, request map[string]any, opts *Options) {
	toolName, _ := request["tool_name"].(string)
	inputData, _ := request["input"].(map[string]any)
	toolUseID, _ := request["tool_use_id"].(string)
	agentID := getStringPtrFromMap(request, "agent_id")

	if opts.CanUseTool == nil {
		_ = writeControlResponse(ctx, transport, BuildControlResponse(requestID, map[string]any{
			"allowed":     false,
			"reason":      "No permission handler provided",
			"tool_use_id": toolUseID,
		}))
		return
	}

	var suggestions []map[string]any
	if sl, ok := request["permission_suggestions"].([]any); ok {
		for _, s := range sl {
			if m, ok := s.(map[string]any); ok {
				suggestions = append(suggestions, m)
			}
		}
	}

	callbackOpts := CanUseToolOptions{
		ToolUseID:      toolUseID,
		AgentID:        agentID,
		Suggestions:    suggestions,
		BlockedPath:    getStringPtrFromMap(request, "blocked_path"),
		DecisionReason: getStringPtrFromMap(request, "decision_reason"),
	}

	result, err := opts.CanUseTool(ctx, toolName, inputData, callbackOpts)
	var respData map[string]any
	if err != nil {
		respData = map[string]any{
			"allowed":     false,
			"reason":      err.Error(),
			"tool_use_id": toolUseID,
		}
	} else {
		switch r := result.(type) {
		case *PermissionResultAllow:
			respData = map[string]any{
				"allowed":      true,
				"updatedInput": r.UpdatedInput,
				"tool_use_id":  toolUseID,
			}
		case *PermissionResultDeny:
			respData = map[string]any{
				"allowed":     false,
				"reason":      r.Message,
				"interrupt":   r.Interrupt,
				"tool_use_id": toolUseID,
			}
		default:
			respData = map[string]any{
				"allowed":     false,
				"reason":      "unknown permission result",
				"tool_use_id": toolUseID,
			}
		}
	}
	_ = writeControlResponse(ctx, transport, BuildControlResponse(requestID, respData))
}

// getStringPtrFromMap 从 map 中安全获取字符串指针。
func getStringPtrFromMap(data map[string]any, key string) *string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return &s
		}
	}
	return nil
}
