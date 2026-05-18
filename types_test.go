package codebuddy

import (
	"context"
	"testing"
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
