package codebuddy

import "testing"

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
