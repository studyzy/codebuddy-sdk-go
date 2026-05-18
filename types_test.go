package codebuddy

import (
	"context"
	"testing"
	"time"
)

func TestEffortPtr(t *testing.T) {
	e := EffortHigh
	p := e.Ptr()
	if p == nil {
		t.Fatal("expected non-nil Effort pointer")
	}
	if *p != EffortHigh {
		t.Errorf("got %v, want EffortHigh", *p)
	}
}

func TestPermissionModePtr(t *testing.T) {
	pm := PermissionModeBypassPermissions
	p := pm.Ptr()
	if p == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *p != PermissionModeBypassPermissions {
		t.Errorf("got %v, want PermissionModeBypassPermissions", *p)
	}
}

func TestPermissionResultBehavior(t *testing.T) {
	allow := &PermissionResultAllow{}
	deny := &PermissionResultDeny{Message: "nope"}
	if allow.permissionBehavior() != "allow" {
		t.Errorf("allow.permissionBehavior() = %q, want allow", allow.permissionBehavior())
	}
	if deny.permissionBehavior() != "deny" {
		t.Errorf("deny.permissionBehavior() = %q, want deny", deny.permissionBehavior())
	}
}

// T011: 新增 PermissionMode 常量测试
func TestNewPermissionModeConstants(t *testing.T) {
	tests := []struct {
		mode PermissionMode
		want string
	}{
		{PermissionModeDelegate, "delegate"},
		{PermissionModeDontAsk, "dontAsk"},
		{PermissionModeFullAccess, "fullAccess"},
	}
	for _, tt := range tests {
		if string(tt.mode) != tt.want {
			t.Errorf("PermissionMode %q: got %q", tt.want, string(tt.mode))
		}
		p := tt.mode.Ptr()
		if p == nil || *p != tt.mode {
			t.Errorf("Ptr() for %q failed", tt.want)
		}
	}
}

// T022: 新增 HookEvent 常量和 HookJSONOutput 字段测试
func TestNewHookEventConstants(t *testing.T) {
	if string(HookSessionStart) != "SessionStart" {
		t.Errorf("HookSessionStart = %q, want SessionStart", HookSessionStart)
	}
	if string(HookSessionEnd) != "SessionEnd" {
		t.Errorf("HookSessionEnd = %q, want SessionEnd", HookSessionEnd)
	}
}

func TestHookJSONOutputNewFields(t *testing.T) {
	sysMsg := "test message"
	output := HookJSONOutput{
		SystemMessage:      &sysMsg,
		HookSpecificOutput: map[string]any{"key": "value"},
	}
	if output.SystemMessage == nil || *output.SystemMessage != "test message" {
		t.Error("SystemMessage field not set correctly")
	}
	if output.HookSpecificOutput["key"] != "value" {
		t.Error("HookSpecificOutput field not set correctly")
	}
}

// 新增消息类型 messageType 方法测试
func TestNewMessageTypes(t *testing.T) {
	tests := []struct {
		msg      Message
		wantType string
	}{
		{&TopicMessage{Topic: "test"}, "topic"},
		{&ToolProgressMessage{ToolName: "Bash"}, "tool_progress"},
		{&FileHistorySnapshotMessage{}, "file-history-snapshot"},
		{&CompactBoundaryMessage{}, "system"},
		{&StatusMessage{}, "system"},
	}
	for _, tt := range tests {
		if tt.msg.messageType() != tt.wantType {
			t.Errorf("messageType() = %q, want %q", tt.msg.messageType(), tt.wantType)
		}
	}
}

// 新增 ContentBlock 类型 contentBlockType 方法测试
func TestNewContentBlockTypes(t *testing.T) {
	tests := []struct {
		block    ContentBlock
		wantType string
	}{
		{&RedactedThinkingBlock{Data: "enc"}, "redacted_thinking"},
		{&ImageContentBlock{Source: ImageSource{Type: "url"}}, "image"},
	}
	for _, tt := range tests {
		if tt.block.contentBlockType() != tt.wantType {
			t.Errorf("contentBlockType() = %q, want %q", tt.block.contentBlockType(), tt.wantType)
		}
	}
}

// TestWorktreeHookEventConstants 验证新增的 Worktree Hook 事件常量值与官方文档一致。
func TestWorktreeHookEventConstants(t *testing.T) {
	if string(HookWorktreeCreate) != "WorktreeCreate" {
		t.Errorf("HookWorktreeCreate = %q, want WorktreeCreate", HookWorktreeCreate)
	}
	if string(HookWorktreeRemove) != "WorktreeRemove" {
		t.Errorf("HookWorktreeRemove = %q, want WorktreeRemove", HookWorktreeRemove)
	}
}

// TestBuiltinToolNameConstants 验证补齐的内置工具名常量。
func TestBuiltinToolNameConstants(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{ToolGlob, "Glob"},
		{ToolGrep, "Grep"},
		{ToolBash, "Bash"},
		{ToolTask, "Task"},
		{ToolWebFetch, "WebFetch"},
		{ToolWebSearch, "WebSearch"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("tool constant = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestEnvVarConstants 验证公共环境变量常量值与官方文档一致。
func TestEnvVarConstants(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{EnvAPIKey, "CODEBUDDY_API_KEY"},
		{EnvAuthToken, "CODEBUDDY_AUTH_TOKEN"},
		{EnvInternetEnvironment, "CODEBUDDY_INTERNET_ENVIRONMENT"},
		{EnvCodePath, "CODEBUDDY_CODE_PATH"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("env var constant = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestSendInitialize_AgentTemperatureMaxTokens 验证 AgentDefinition 新字段
// Temperature/MaxTokens 会被序列化进 initialize 请求。
func TestSendInitialize_AgentTemperatureMaxTokens(t *testing.T) {
	tr := newMockTransport(10)
	temp := 0.42
	maxTokens := 1024
	opts := &Options{
		Agents: map[string]AgentDefinition{
			"reviewer": {
				Description: "code reviewer",
				Prompt:      "review code",
				Temperature: &temp,
				MaxTokens:   &maxTokens,
			},
		},
	}
	_, _, err := sendInitialize(context.Background(), tr, opts, false)
	if err != nil {
		t.Fatalf("sendInitialize: %v", err)
	}

	msg := tr.writtenJSON(0)
	req, ok := msg["request"].(map[string]any)
	if !ok {
		t.Fatalf("request payload missing")
	}
	agents, ok := req["agents"].(map[string]any)
	if !ok {
		t.Fatalf("agents payload missing, got %T", req["agents"])
	}
	reviewer, ok := agents["reviewer"].(map[string]any)
	if !ok {
		t.Fatalf("agents[reviewer] missing")
	}
	// JSON unmarshal 会把数值转为 float64
	gotTemp, ok := reviewer["temperature"].(float64)
	if !ok || gotTemp != 0.42 {
		t.Errorf("temperature = %v (%T), want 0.42 (float64)", reviewer["temperature"], reviewer["temperature"])
	}
	gotMax, ok := reviewer["maxTokens"].(float64)
	if !ok || int(gotMax) != 1024 {
		t.Errorf("maxTokens = %v (%T), want 1024", reviewer["maxTokens"], reviewer["maxTokens"])
	}
}

// TestSendInitialize_AgentWithoutOptionalFields 验证未设置 Temperature/MaxTokens
// 时不会污染 initialize 请求（保持向后兼容）。
func TestSendInitialize_AgentWithoutOptionalFields(t *testing.T) {
	tr := newMockTransport(10)
	opts := &Options{
		Agents: map[string]AgentDefinition{
			"basic": {Description: "basic agent", Prompt: "hi"},
		},
	}
	_, _, err := sendInitialize(context.Background(), tr, opts, false)
	if err != nil {
		t.Fatalf("sendInitialize: %v", err)
	}

	msg := tr.writtenJSON(0)
	req := msg["request"].(map[string]any)
	agents := req["agents"].(map[string]any)
	basic := agents["basic"].(map[string]any)
	if _, exists := basic["temperature"]; exists {
		t.Errorf("temperature should be absent when nil, got %v", basic["temperature"])
	}
	if _, exists := basic["maxTokens"]; exists {
		t.Errorf("maxTokens should be absent when nil, got %v", basic["maxTokens"])
	}
}

// TestPartialAssistantMessage_MessageType 验证 PartialAssistantMessage 的 messageType 方法。
func TestPartialAssistantMessage_MessageType(t *testing.T) {
	msg := &PartialAssistantMessage{
		Model: "test-model",
	}
	if msg.messageType() != "partial_assistant" {
		t.Errorf("messageType() = %q, want partial_assistant", msg.messageType())
	}
}

// TestParseMessage_PartialAssistant 验证 ParseMessage 正确解析 partial_assistant 类型。
func TestParseMessage_PartialAssistant(t *testing.T) {
	data := map[string]any{
		"type":  "partial_assistant",
		"model": "deepseek-v3",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "Hello wor"},
			},
		},
		"parent_tool_use_id": "tu_123",
	}

	msg := ParseMessage(data)
	if msg == nil {
		t.Fatal("ParseMessage returned nil for partial_assistant")
	}
	partial, ok := msg.(*PartialAssistantMessage)
	if !ok {
		t.Fatalf("expected *PartialAssistantMessage, got %T", msg)
	}
	if partial.Model != "deepseek-v3" {
		t.Errorf("Model = %q, want deepseek-v3", partial.Model)
	}
	if partial.ParentToolUseID == nil || *partial.ParentToolUseID != "tu_123" {
		t.Error("ParentToolUseID not parsed correctly")
	}
	if len(partial.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(partial.Content))
	}
	tb, ok := partial.Content[0].(*TextBlock)
	if !ok || tb.Text != "Hello wor" {
		t.Errorf("Content[0] = %v, want TextBlock{Hello wor}", partial.Content[0])
	}
}

// TestAttachRawJSON_PartialAssistantMessage 验证 AttachRawJSON 处理 PartialAssistantMessage。
func TestAttachRawJSON_PartialAssistantMessage(t *testing.T) {
	msg := &PartialAssistantMessage{Model: "test"}
	raw := []byte(`{"type":"partial_assistant"}`)
	AttachRawJSON(msg, raw)
	if msg.RawJSON == nil {
		t.Error("RawJSON should be set after AttachRawJSON")
	}

	got := RawJSONOf(msg)
	if got == nil {
		t.Error("RawJSONOf should return non-nil for PartialAssistantMessage")
	}
}

// TestPrompt_ReturnsResult 验证 Prompt 便捷函数通过 mock transport 正常工作。
func TestPrompt_ReturnsResult(t *testing.T) {
	tr := newMockTransport(10)

	// 模拟 CLI 响应：先返回 assistant message，再返回 result
	go func() {
		// 读取 initialize 请求（忽略）
		tr.injectRaw(map[string]any{
			"type":    "assistant",
			"model":   "test-model",
			"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "4"}}},
		})
		resultText := "The answer is 4."
		tr.injectRaw(map[string]any{
			"type":       "result",
			"session_id": "sid-1",
			"result":     resultText,
		})
		tr.closeMessages()
	}()

	ctx := context.Background()
	opts := &Options{}
	// 使用 queryWithTransport 间接测试 Prompt 逻辑
	msgCh, err := queryWithTransport(ctx, "What is 2+2?", opts, tr)
	if err != nil {
		t.Fatalf("queryWithTransport: %v", err)
	}

	// 模拟 Prompt 行为：遍历通道找 ResultMessage
	var result *ResultMessage
	for msg := range msgCh {
		if r, ok := msg.(*ResultMessage); ok {
			if r.IsError && len(r.Errors) > 0 {
				t.Fatalf("unexpected error: %v", r.Errors)
			}
			result = r
			break
		}
	}
	if result == nil {
		t.Fatal("expected ResultMessage")
	}
	if result.Result == nil || *result.Result != "The answer is 4." {
		t.Errorf("Result = %v, want 'The answer is 4.'", result.Result)
	}
}

// TestSendInitialize_ClientFields 验证 sendInitialize 包含 systemPrompt、agents、
// outputFormat、environment、endpoint 等字段。
func TestSendInitialize_ClientFields(t *testing.T) {
	tr := newMockTransport(10)
	override := "You are a helper."
	env := "external"
	endpoint := "https://custom.example.com"
	opts := &Options{
		SystemPrompt: &SystemPromptConfig{Override: &override},
		OutputFormat: &JsonSchemaOutputFormat{
			Type:   "json_schema",
			Schema: map[string]any{"type": "object"},
		},
		Environment: &env,
		Endpoint:    &endpoint,
	}

	_, _, err := sendInitialize(context.Background(), tr, opts, true)
	if err != nil {
		t.Fatalf("sendInitialize: %v", err)
	}

	msg := tr.writtenJSON(0)
	req, _ := msg["request"].(map[string]any)
	if req == nil {
		t.Fatal("request payload missing")
	}

	// systemPrompt
	if sp, ok := req["systemPrompt"].(string); !ok || sp != "You are a helper." {
		t.Errorf("systemPrompt = %v, want 'You are a helper.'", req["systemPrompt"])
	}

	// jsonSchema
	if js, ok := req["jsonSchema"].(map[string]any); !ok || js == nil {
		t.Errorf("jsonSchema = %v, want non-nil", req["jsonSchema"])
	}

	// environment
	if e, ok := req["environment"].(string); !ok || e != "external" {
		t.Errorf("environment = %v, want 'external'", req["environment"])
	}

	// endpoint
	if ep, ok := req["endpoint"].(string); !ok || ep != "https://custom.example.com" {
		t.Errorf("endpoint = %v, want 'https://custom.example.com'", req["endpoint"])
	}
}

// TestSendInitialize_AppendSystemPromptField 验证 appendSystemPrompt 字段。
func TestSendInitialize_AppendSystemPromptField(t *testing.T) {
	tr := newMockTransport(10)
	appendStr := "Additional instructions."
	opts := &Options{
		SystemPrompt: &SystemPromptConfig{Append: &appendStr},
	}

	_, _, err := sendInitialize(context.Background(), tr, opts, true)
	if err != nil {
		t.Fatalf("sendInitialize: %v", err)
	}

	msg := tr.writtenJSON(0)
	req, _ := msg["request"].(map[string]any)

	// systemPrompt should be nil (no override)
	if req["systemPrompt"] != nil {
		t.Errorf("systemPrompt should be nil, got %v", req["systemPrompt"])
	}

	// appendSystemPrompt should be set
	if ap, ok := req["appendSystemPrompt"].(string); !ok || ap != "Additional instructions." {
		t.Errorf("appendSystemPrompt = %v, want 'Additional instructions.'", req["appendSystemPrompt"])
	}
}

// TestConnCore_AccountInfo 验证 accountInfo 方法正确解析响应。
func TestConnCore_AccountInfo(t *testing.T) {
	core := &connCore{}
	tr := newMockTransport(10)
	initConnCore(core, &Options{}, PermissionModeDefault, "")
	core.transport = tr

	// 模拟 CLI 响应
	go func() {
		// 等待 sendControlRequest 的写入
		for {
			tr.mu.Lock()
			n := len(tr.written)
			tr.mu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}

		// 获取 request_id
		reqMsg := tr.writtenJSON(0)
		requestID, _ := reqMsg["request_id"].(string)

		// 注入 control_response
		tr.injectRaw(map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response": map[string]any{
					"account": map[string]any{
						"userId":   "user-123",
						"userName": "testuser",
						"email":    "test@example.com",
					},
				},
			},
		})
	}()

	// 启动后台 reader 来路由 control_response
	core.wg.Add(1)
	go func() {
		defer core.wg.Done()
		for raw := range tr.ReadMessages() {
			if raw.Err != nil {
				return
			}
			data := raw.Data
			msgType, _ := data["type"].(string)
			if msgType == "control_response" {
				core.routeControlResponse(data)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := core.accountInfo(ctx, "未连接")
	if err != nil {
		t.Fatalf("accountInfo: %v", err)
	}

	if info.UserID == nil || *info.UserID != "user-123" {
		t.Errorf("UserID = %v, want 'user-123'", info.UserID)
	}
	if info.UserName == nil || *info.UserName != "testuser" {
		t.Errorf("UserName = %v, want 'testuser'", info.UserName)
	}
	if info.Email == nil || *info.Email != "test@example.com" {
		t.Errorf("Email = %v, want 'test@example.com'", info.Email)
	}

	tr.closeMessages()
	core.wg.Wait()
}

// TestConnCore_SetMaxThinkingTokens 验证 setMaxThinkingTokens 发送正确的控制请求。
func TestConnCore_SetMaxThinkingTokens(t *testing.T) {
	core := &connCore{}
	tr := newMockTransport(10)
	initConnCore(core, &Options{}, PermissionModeDefault, "")
	core.transport = tr

	tokens := 8192

	// 模拟 CLI 响应
	go func() {
		for {
			tr.mu.Lock()
			n := len(tr.written)
			tr.mu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}

		reqMsg := tr.writtenJSON(0)
		requestID, _ := reqMsg["request_id"].(string)

		tr.injectRaw(map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response": map[string]any{
					"updated": map[string]any{"maxThinkingTokens": float64(8192)},
				},
			},
		})
	}()

	// 启动后台 reader
	core.wg.Add(1)
	go func() {
		defer core.wg.Done()
		for raw := range tr.ReadMessages() {
			if raw.Err != nil {
				return
			}
			data := raw.Data
			msgType, _ := data["type"].(string)
			if msgType == "control_response" {
				core.routeControlResponse(data)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := core.setMaxThinkingTokens(ctx, "sess-1", &tokens, "未连接")
	if err != nil {
		t.Fatalf("setMaxThinkingTokens: %v", err)
	}

	// 验证发送的请求中包含 set_config + maxThinkingTokens
	reqMsg := tr.writtenJSON(0)
	req, _ := reqMsg["request"].(map[string]any)
	if req == nil {
		t.Fatal("request payload missing")
	}
	if subtype, _ := req["subtype"].(string); subtype != "set_config" {
		t.Errorf("subtype = %q, want set_config", subtype)
	}
	config, _ := req["config"].(map[string]any)
	if config == nil {
		t.Fatal("config field missing in set_config request")
	}
	if v, _ := config["maxThinkingTokens"].(float64); int(v) != 8192 {
		t.Errorf("maxThinkingTokens = %v, want 8192", config["maxThinkingTokens"])
	}

	tr.closeMessages()
	core.wg.Wait()
}

// TestSession_SetMaxThinkingTokens_BeforeConnect 验证连接前调用 SetMaxThinkingTokens 修改 Options。
func TestSession_SetMaxThinkingTokens_BeforeConnect(t *testing.T) {
	opts := &Options{}
	session := newSession(opts, nil, "")

	budget := 4096
	err := session.SetMaxThinkingTokens(context.Background(), &budget)
	if err != nil {
		t.Fatalf("SetMaxThinkingTokens: %v", err)
	}

	if session.core.opts.Thinking == nil {
		t.Fatal("Thinking should be set")
	}
	if session.core.opts.Thinking.BudgetTokens == nil || *session.core.opts.Thinking.BudgetTokens != 4096 {
		t.Errorf("BudgetTokens = %v, want 4096", session.core.opts.Thinking.BudgetTokens)
	}

	// 设置为 nil 恢复默认
	err = session.SetMaxThinkingTokens(context.Background(), nil)
	if err != nil {
		t.Fatalf("SetMaxThinkingTokens(nil): %v", err)
	}
	if session.core.opts.Thinking.BudgetTokens != nil {
		t.Errorf("BudgetTokens should be nil after reset, got %v", session.core.opts.Thinking.BudgetTokens)
	}
}
