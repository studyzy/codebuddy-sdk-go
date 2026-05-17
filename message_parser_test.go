// message_parser_test.go
package codebuddy

import (
	"encoding/json"
	"os"
	"testing"
)

// loadFixtureMessages reads testdata/messages.json and returns a slice of raw maps.
func loadFixtureMessages(t *testing.T) []map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/messages.json")
	if err != nil {
		t.Fatalf("failed to read testdata/messages.json: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("failed to unmarshal testdata/messages.json: %v", err)
	}
	return items
}

// findFixtureByType returns the first fixture entry with the given "type" value.
func findFixtureByType(fixtures []map[string]any, msgType string) map[string]any {
	for _, f := range fixtures {
		if f["type"] == msgType {
			return f
		}
	}
	return nil
}

// ---- TestParseMessage -------------------------------------------------------

func TestParseMessage(t *testing.T) {
	fixtures := loadFixtureMessages(t)

	tests := []struct {
		name     string
		data     map[string]any
		wantType string
		check    func(t *testing.T, msg Message)
	}{
		{
			name:     "user message",
			data:     findFixtureByType(fixtures, "user"),
			wantType: "user",
			check: func(t *testing.T, msg Message) {
				t.Helper()
				um, ok := msg.(*UserMessage)
				if !ok {
					t.Fatalf("expected *UserMessage, got %T", msg)
				}
				if um.UUID == nil || *um.UUID != "u1" {
					t.Errorf("UUID: got %v, want \"u1\"", um.UUID)
				}
				// Content is a plain string from the fixture ("Hello")
				if s, ok := um.Content.(string); ok {
					if s != "Hello" {
						t.Errorf("Content string: got %q, want \"Hello\"", s)
					}
				} else {
					t.Errorf("Content type: got %T, want string", um.Content)
				}
			},
		},
		{
			name:     "assistant message with text block",
			data:     fixtures[1], // second entry: assistant with text content
			wantType: "assistant",
			check: func(t *testing.T, msg Message) {
				t.Helper()
				am, ok := msg.(*AssistantMessage)
				if !ok {
					t.Fatalf("expected *AssistantMessage, got %T", msg)
				}
				if am.Model != "claude-opus-4-5" {
					t.Errorf("Model: got %q, want \"claude-opus-4-5\"", am.Model)
				}
				if len(am.Content) != 1 {
					t.Fatalf("Content len: got %d, want 1", len(am.Content))
				}
				tb, ok := am.Content[0].(*TextBlock)
				if !ok {
					t.Fatalf("Content[0] type: got %T, want *TextBlock", am.Content[0])
				}
				if tb.Text != "Hi there!" {
					t.Errorf("TextBlock.Text: got %q, want \"Hi there!\"", tb.Text)
				}
			},
		},
		{
			name:     "assistant message with thinking and tool_use blocks",
			data:     fixtures[2], // third entry: assistant with thinking + tool_use
			wantType: "assistant",
			check: func(t *testing.T, msg Message) {
				t.Helper()
				am, ok := msg.(*AssistantMessage)
				if !ok {
					t.Fatalf("expected *AssistantMessage, got %T", msg)
				}
				if len(am.Content) != 2 {
					t.Fatalf("Content len: got %d, want 2", len(am.Content))
				}
				// First block: thinking
				thk, ok := am.Content[0].(*ThinkingBlock)
				if !ok {
					t.Fatalf("Content[0] type: got %T, want *ThinkingBlock", am.Content[0])
				}
				if thk.Thinking != "Let me think..." {
					t.Errorf("ThinkingBlock.Thinking: got %q", thk.Thinking)
				}
				if thk.Signature != "sig123" {
					t.Errorf("ThinkingBlock.Signature: got %q", thk.Signature)
				}
				// Second block: tool_use
				tu, ok := am.Content[1].(*ToolUseBlock)
				if !ok {
					t.Fatalf("Content[1] type: got %T, want *ToolUseBlock", am.Content[1])
				}
				if tu.ID != "tu1" {
					t.Errorf("ToolUseBlock.ID: got %q, want \"tu1\"", tu.ID)
				}
				if tu.Name != "Bash" {
					t.Errorf("ToolUseBlock.Name: got %q, want \"Bash\"", tu.Name)
				}
				if tu.Input["command"] != "ls" {
					t.Errorf("ToolUseBlock.Input[command]: got %v, want \"ls\"", tu.Input["command"])
				}
			},
		},
		{
			name:     "system message",
			data:     findFixtureByType(fixtures, "system"),
			wantType: "system",
			check: func(t *testing.T, msg Message) {
				t.Helper()
				sm, ok := msg.(*SystemMessage)
				if !ok {
					t.Fatalf("expected *SystemMessage, got %T", msg)
				}
				if sm.Subtype != "init" {
					t.Errorf("Subtype: got %q, want \"init\"", sm.Subtype)
				}
				if sm.Data == nil {
					t.Error("Data should not be nil")
				}
			},
		},
		{
			name:     "result message",
			data:     findFixtureByType(fixtures, "result"),
			wantType: "result",
			check: func(t *testing.T, msg Message) {
				t.Helper()
				rm, ok := msg.(*ResultMessage)
				if !ok {
					t.Fatalf("expected *ResultMessage, got %T", msg)
				}
				if rm.Subtype != "success" {
					t.Errorf("Subtype: got %q, want \"success\"", rm.Subtype)
				}
				if rm.DurationMs != 1500 {
					t.Errorf("DurationMs: got %d, want 1500", rm.DurationMs)
				}
				if rm.DurationAPIMs != 1200 {
					t.Errorf("DurationAPIMs: got %d, want 1200", rm.DurationAPIMs)
				}
				if rm.IsError {
					t.Error("IsError: got true, want false")
				}
				if rm.NumTurns != 2 {
					t.Errorf("NumTurns: got %d, want 2", rm.NumTurns)
				}
				if rm.SessionID != "s1" {
					t.Errorf("SessionID: got %q, want \"s1\"", rm.SessionID)
				}
				if rm.StopReason == nil || *rm.StopReason != "end_turn" {
					t.Errorf("StopReason: got %v, want \"end_turn\"", rm.StopReason)
				}
				if rm.TotalCostUSD == nil || *rm.TotalCostUSD != 0.0012 {
					t.Errorf("TotalCostUSD: got %v, want 0.0012", rm.TotalCostUSD)
				}
				if rm.Result == nil || *rm.Result != "Done" {
					t.Errorf("Result: got %v, want \"Done\"", rm.Result)
				}
			},
		},
		{
			name:     "stream_event message",
			data:     findFixtureByType(fixtures, "stream_event"),
			wantType: "stream_event",
			check: func(t *testing.T, msg Message) {
				t.Helper()
				se, ok := msg.(*StreamEvent)
				if !ok {
					t.Fatalf("expected *StreamEvent, got %T", msg)
				}
				if se.UUID != "se1" {
					t.Errorf("UUID: got %q, want \"se1\"", se.UUID)
				}
				if se.SessionID != "s1" {
					t.Errorf("SessionID: got %q, want \"s1\"", se.SessionID)
				}
				if se.Event == nil {
					t.Fatal("Event map should not be nil")
				}
				if se.Event["type"] != "content_block_delta" {
					t.Errorf("Event[type]: got %v, want \"content_block_delta\"", se.Event["type"])
				}
			},
		},
		{
			name:     "error message",
			data:     findFixtureByType(fixtures, "error"),
			wantType: "error",
			check: func(t *testing.T, msg Message) {
				t.Helper()
				em, ok := msg.(*ErrorMessage)
				if !ok {
					t.Fatalf("expected *ErrorMessage, got %T", msg)
				}
				if em.Error != "connection refused" {
					t.Errorf("Error: got %q, want \"connection refused\"", em.Error)
				}
				if em.SessionID == nil || *em.SessionID != "s1" {
					t.Errorf("SessionID: got %v, want \"s1\"", em.SessionID)
				}
			},
		},
		{
			name:     "unknown type returns nil",
			data:     map[string]any{"type": "bogus_type"},
			wantType: "",
			check: func(t *testing.T, msg Message) {
				t.Helper()
				if msg != nil {
					t.Errorf("expected nil for unknown type, got %T", msg)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.data == nil {
				t.Fatal("fixture data is nil — check testdata/messages.json")
			}
			msg := ParseMessage(tc.data)
			if tc.wantType == "" {
				// nil expected
				tc.check(t, msg)
				return
			}
			if msg == nil {
				t.Fatalf("ParseMessage returned nil for type %q", tc.wantType)
			}
			if got := msg.messageType(); got != tc.wantType {
				t.Errorf("messageType(): got %q, want %q", got, tc.wantType)
			}
			tc.check(t, msg)
		})
	}
}

// ---- TestParseMessage_UserContentBlocks ------------------------------------

// TestParseMessage_UserContentBlocks verifies that when a user message carries
// a content array, it is decoded into []ContentBlock rather than left as []any.
func TestParseMessage_UserContentBlocks(t *testing.T) {
	raw := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "hello"},
			},
		},
		"uuid": "abc",
	}
	msg := ParseMessage(raw)
	um, ok := msg.(*UserMessage)
	if !ok {
		t.Fatalf("expected *UserMessage, got %T", msg)
	}
	blocks, ok := um.Content.([]ContentBlock)
	if !ok {
		t.Fatalf("Content type: got %T, want []ContentBlock", um.Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("len(blocks): got %d, want 1", len(blocks))
	}
	tb, ok := blocks[0].(*TextBlock)
	if !ok {
		t.Fatalf("blocks[0] type: got %T, want *TextBlock", blocks[0])
	}
	if tb.Text != "hello" {
		t.Errorf("TextBlock.Text: got %q, want \"hello\"", tb.Text)
	}
}

// ---- TestParseContentBlock -------------------------------------------------

func TestParseContentBlock(t *testing.T) {
	trueVal := true

	tests := []struct {
		name      string
		data      map[string]any
		wantNil   bool
		blockType string
		check     func(t *testing.T, b ContentBlock)
	}{
		{
			name:      "text block",
			data:      map[string]any{"type": "text", "text": "hello world"},
			blockType: "text",
			check: func(t *testing.T, b ContentBlock) {
				t.Helper()
				tb, ok := b.(*TextBlock)
				if !ok {
					t.Fatalf("expected *TextBlock, got %T", b)
				}
				if tb.Text != "hello world" {
					t.Errorf("Text: got %q, want \"hello world\"", tb.Text)
				}
			},
		},
		{
			name: "thinking block",
			data: map[string]any{
				"type":      "thinking",
				"thinking":  "deep thought",
				"signature": "sig42",
			},
			blockType: "thinking",
			check: func(t *testing.T, b ContentBlock) {
				t.Helper()
				thk, ok := b.(*ThinkingBlock)
				if !ok {
					t.Fatalf("expected *ThinkingBlock, got %T", b)
				}
				if thk.Thinking != "deep thought" {
					t.Errorf("Thinking: got %q", thk.Thinking)
				}
				if thk.Signature != "sig42" {
					t.Errorf("Signature: got %q", thk.Signature)
				}
			},
		},
		{
			name: "tool_use block",
			data: map[string]any{
				"type":  "tool_use",
				"id":    "tu-99",
				"name":  "Read",
				"input": map[string]any{"path": "/tmp/file.txt"},
			},
			blockType: "tool_use",
			check: func(t *testing.T, b ContentBlock) {
				t.Helper()
				tu, ok := b.(*ToolUseBlock)
				if !ok {
					t.Fatalf("expected *ToolUseBlock, got %T", b)
				}
				if tu.ID != "tu-99" {
					t.Errorf("ID: got %q, want \"tu-99\"", tu.ID)
				}
				if tu.Name != "Read" {
					t.Errorf("Name: got %q, want \"Read\"", tu.Name)
				}
				if tu.Input["path"] != "/tmp/file.txt" {
					t.Errorf("Input[path]: got %v", tu.Input["path"])
				}
			},
		},
		{
			name: "tool_use block with nil input defaults to empty map",
			data: map[string]any{
				"type": "tool_use",
				"id":   "tu-00",
				"name": "NoInput",
				// "input" key absent
			},
			blockType: "tool_use",
			check: func(t *testing.T, b ContentBlock) {
				t.Helper()
				tu, ok := b.(*ToolUseBlock)
				if !ok {
					t.Fatalf("expected *ToolUseBlock, got %T", b)
				}
				if tu.Input == nil {
					t.Error("Input should be non-nil empty map when key is absent")
				}
			},
		},
		{
			name: "tool_result block",
			data: map[string]any{
				"type":        "tool_result",
				"tool_use_id": "tu-99",
				"content":     "result text",
				"is_error":    true,
			},
			blockType: "tool_result",
			check: func(t *testing.T, b ContentBlock) {
				t.Helper()
				tr, ok := b.(*ToolResultBlock)
				if !ok {
					t.Fatalf("expected *ToolResultBlock, got %T", b)
				}
				if tr.ToolUseID != "tu-99" {
					t.Errorf("ToolUseID: got %q, want \"tu-99\"", tr.ToolUseID)
				}
				if tr.Content != "result text" {
					t.Errorf("Content: got %v", tr.Content)
				}
				if tr.IsError == nil || *tr.IsError != trueVal {
					t.Errorf("IsError: got %v, want true", tr.IsError)
				}
			},
		},
		{
			name:    "unknown type returns nil",
			data:    map[string]any{"type": "unknown_block"},
			wantNil: true,
			check: func(t *testing.T, b ContentBlock) {
				t.Helper()
				if b != nil {
					t.Errorf("expected nil, got %T", b)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b := ParseContentBlock(tc.data)
			if tc.wantNil {
				tc.check(t, b)
				return
			}
			if b == nil {
				t.Fatalf("ParseContentBlock returned nil for type %q", tc.data["type"])
			}
			if got := b.contentBlockType(); got != tc.blockType {
				t.Errorf("contentBlockType(): got %q, want %q", got, tc.blockType)
			}
			tc.check(t, b)
		})
	}
}

// ---- TestParseContentBlocks ------------------------------------------------

func TestParseContentBlocks(t *testing.T) {
	items := []any{
		map[string]any{"type": "text", "text": "first"},
		map[string]any{"type": "bogus"}, // should be skipped
		"not a map",                     // should be skipped
		map[string]any{"type": "text", "text": "second"},
	}
	blocks := ParseContentBlocks(items)
	if len(blocks) != 2 {
		t.Fatalf("len: got %d, want 2", len(blocks))
	}
	if tb, ok := blocks[0].(*TextBlock); !ok || tb.Text != "first" {
		t.Errorf("blocks[0]: got %v", blocks[0])
	}
	if tb, ok := blocks[1].(*TextBlock); !ok || tb.Text != "second" {
		t.Errorf("blocks[1]: got %v", blocks[1])
	}
}

// ---- TestParseMessage_FromFixtureFile --------------------------------------

// TestParseMessage_FromFixtureFile loads every entry from testdata/messages.json
// and confirms none of them panic or return nil (all are known types).
func TestParseMessage_FromFixtureFile(t *testing.T) {
	fixtures := loadFixtureMessages(t)
	for i, f := range fixtures {
		msgType, _ := f["type"].(string)
		msg := ParseMessage(f)
		if msg == nil {
			t.Errorf("fixture[%d] (type=%q): ParseMessage returned nil", i, msgType)
		}
	}
}

// TestGetBool tests the internal getBool helper.
func TestGetBool(t *testing.T) {
	data := map[string]any{"yes": true, "no": false, "num": 42}
	if !getBool(data, "yes") {
		t.Error("expected true for 'yes'")
	}
	if getBool(data, "no") {
		t.Error("expected false for 'no'")
	}
	if getBool(data, "num") {
		t.Error("expected false for non-bool type")
	}
	if getBool(data, "missing") {
		t.Error("expected false for missing key")
	}
}

// TestGetInt tests the internal getInt helper with all supported types.
func TestGetInt(t *testing.T) {
	data := map[string]any{
		"int":     int(42),
		"float64": float64(3.7),
		"int64":   int64(100),
		"str":     "oops",
	}
	if n := getInt(data, "int"); n != 42 {
		t.Errorf("int: got %d, want 42", n)
	}
	if n := getInt(data, "float64"); n != 3 {
		t.Errorf("float64: got %d, want 3", n)
	}
	if n := getInt(data, "int64"); n != 100 {
		t.Errorf("int64: got %d, want 100", n)
	}
	if n := getInt(data, "str"); n != 0 {
		t.Errorf("str: got %d, want 0", n)
	}
	if n := getInt(data, "missing"); n != 0 {
		t.Errorf("missing: got %d, want 0", n)
	}
}

// ---- T018: ParseContentBlock 新增类型测试 ----

func TestParseContentBlock_RedactedThinking(t *testing.T) {
	data := map[string]any{
		"type": "redacted_thinking",
		"data": "encrypted-data-abc",
	}
	b := ParseContentBlock(data)
	if b == nil {
		t.Fatal("ParseContentBlock returned nil for redacted_thinking")
	}
	rtb, ok := b.(*RedactedThinkingBlock)
	if !ok {
		t.Fatalf("expected *RedactedThinkingBlock, got %T", b)
	}
	if rtb.Data != "encrypted-data-abc" {
		t.Errorf("Data: got %q, want \"encrypted-data-abc\"", rtb.Data)
	}
}

func TestParseContentBlock_ImageBase64(t *testing.T) {
	data := map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": "image/png",
			"data":       "iVBORw0KGgo=",
		},
	}
	b := ParseContentBlock(data)
	if b == nil {
		t.Fatal("ParseContentBlock returned nil for image")
	}
	img, ok := b.(*ImageContentBlock)
	if !ok {
		t.Fatalf("expected *ImageContentBlock, got %T", b)
	}
	if img.Source.Type != "base64" {
		t.Errorf("Source.Type: got %q, want \"base64\"", img.Source.Type)
	}
	if img.Source.MediaType != "image/png" {
		t.Errorf("Source.MediaType: got %q, want \"image/png\"", img.Source.MediaType)
	}
	if img.Source.Data != "iVBORw0KGgo=" {
		t.Errorf("Source.Data: got %q", img.Source.Data)
	}
}

func TestParseContentBlock_ImageURL(t *testing.T) {
	data := map[string]any{
		"type": "image",
		"source": map[string]any{
			"type": "url",
			"url":  "https://example.com/image.png",
		},
	}
	b := ParseContentBlock(data)
	img, ok := b.(*ImageContentBlock)
	if !ok {
		t.Fatalf("expected *ImageContentBlock, got %T", b)
	}
	if img.Source.Type != "url" {
		t.Errorf("Source.Type: got %q, want \"url\"", img.Source.Type)
	}
	if img.Source.URL != "https://example.com/image.png" {
		t.Errorf("Source.URL: got %q", img.Source.URL)
	}
}

// ---- T019: ParseMessage 新增消息类型测试 ----

func TestParseMessage_TopicMessage(t *testing.T) {
	data := map[string]any{
		"type":       "topic",
		"topic":      "Session Title",
		"session_id": "abc-123",
	}
	msg := ParseMessage(data)
	tm, ok := msg.(*TopicMessage)
	if !ok {
		t.Fatalf("expected *TopicMessage, got %T", msg)
	}
	if tm.Topic != "Session Title" {
		t.Errorf("Topic: got %q, want \"Session Title\"", tm.Topic)
	}
	if tm.SessionID != "abc-123" {
		t.Errorf("SessionID: got %q", tm.SessionID)
	}
}

func TestParseMessage_ToolProgressMessage(t *testing.T) {
	data := map[string]any{
		"type":                 "tool_progress",
		"tool_use_id":         "tu-123",
		"tool_name":           "Bash",
		"parent_tool_use_id":  "parent-1",
		"elapsed_time_seconds": 5.2,
		"uuid":                "ev-1",
		"session_id":          "s-1",
	}
	msg := ParseMessage(data)
	tp, ok := msg.(*ToolProgressMessage)
	if !ok {
		t.Fatalf("expected *ToolProgressMessage, got %T", msg)
	}
	if tp.ToolUseID != "tu-123" {
		t.Errorf("ToolUseID: got %q", tp.ToolUseID)
	}
	if tp.ToolName != "Bash" {
		t.Errorf("ToolName: got %q", tp.ToolName)
	}
	if tp.ParentToolUseID == nil || *tp.ParentToolUseID != "parent-1" {
		t.Errorf("ParentToolUseID: got %v", tp.ParentToolUseID)
	}
	if tp.ElapsedTimeSeconds != 5.2 {
		t.Errorf("ElapsedTimeSeconds: got %f, want 5.2", tp.ElapsedTimeSeconds)
	}
}

func TestParseMessage_FileHistorySnapshotMessage(t *testing.T) {
	data := map[string]any{
		"type":             "file-history-snapshot",
		"timestamp":        float64(1700000000),
		"isSnapshotUpdate": true,
		"snapshot": map[string]any{
			"messageId": "msg-1",
		},
		"id":       "snap-1",
		"parentId": "snap-0",
	}
	msg := ParseMessage(data)
	fh, ok := msg.(*FileHistorySnapshotMessage)
	if !ok {
		t.Fatalf("expected *FileHistorySnapshotMessage, got %T", msg)
	}
	if fh.Timestamp != 1700000000 {
		t.Errorf("Timestamp: got %d", fh.Timestamp)
	}
	if !fh.IsSnapshotUpdate {
		t.Error("IsSnapshotUpdate: expected true")
	}
	if fh.ID == nil || *fh.ID != "snap-1" {
		t.Errorf("ID: got %v", fh.ID)
	}
	if fh.ParentID == nil || *fh.ParentID != "snap-0" {
		t.Errorf("ParentID: got %v", fh.ParentID)
	}
}

// ---- T020: system subtype 分派与 permission_denials 测试 ----

func TestParseMessage_CompactBoundaryMessage(t *testing.T) {
	data := map[string]any{
		"type":       "system",
		"subtype":    "compact_boundary",
		"uuid":       "cb-1",
		"session_id": "s-1",
		"compact_metadata": map[string]any{
			"trigger":    "auto",
			"pre_tokens": float64(50000),
		},
	}
	msg := ParseMessage(data)
	cb, ok := msg.(*CompactBoundaryMessage)
	if !ok {
		t.Fatalf("expected *CompactBoundaryMessage, got %T", msg)
	}
	if cb.UUID != "cb-1" {
		t.Errorf("UUID: got %q", cb.UUID)
	}
	if cb.CompactMetadata["trigger"] != "auto" {
		t.Errorf("CompactMetadata[trigger]: got %v", cb.CompactMetadata["trigger"])
	}
}

func TestParseMessage_StatusMessage(t *testing.T) {
	data := map[string]any{
		"type":       "system",
		"subtype":    "status",
		"status":     "compacting",
		"uuid":       "st-1",
		"session_id": "s-1",
	}
	msg := ParseMessage(data)
	sm, ok := msg.(*StatusMessage)
	if !ok {
		t.Fatalf("expected *StatusMessage, got %T", msg)
	}
	if sm.Status == nil || *sm.Status != "compacting" {
		t.Errorf("Status: got %v, want \"compacting\"", sm.Status)
	}
}

func TestParseMessage_SystemInit_StillWorksAsSystemMessage(t *testing.T) {
	data := map[string]any{
		"type":    "system",
		"subtype": "init",
	}
	msg := ParseMessage(data)
	_, ok := msg.(*SystemMessage)
	if !ok {
		t.Fatalf("expected *SystemMessage for subtype=init, got %T", msg)
	}
}

func TestParseMessage_ResultWithPermissionDenials(t *testing.T) {
	data := map[string]any{
		"type":          "result",
		"subtype":       "success",
		"session_id":    "s-1",
		"duration_ms":   float64(1000),
		"is_error":      false,
		"num_turns":     float64(1),
		"result":        "ok",
		"total_cost_usd": float64(0.01),
		"usage":         map[string]any{},
		"permission_denials": []any{
			map[string]any{
				"tool_name":   "Bash",
				"tool_use_id": "tu-1",
				"tool_input":  map[string]any{"command": "rm -rf /"},
			},
			map[string]any{
				"tool_name":   "Write",
				"tool_use_id": "tu-2",
				"tool_input":  map[string]any{"path": "/etc/passwd"},
			},
		},
	}
	msg := ParseMessage(data)
	rm, ok := msg.(*ResultMessage)
	if !ok {
		t.Fatalf("expected *ResultMessage, got %T", msg)
	}
	if len(rm.PermissionDenials) != 2 {
		t.Fatalf("PermissionDenials: got %d, want 2", len(rm.PermissionDenials))
	}
	if rm.PermissionDenials[0].ToolName != "Bash" {
		t.Errorf("PermissionDenials[0].ToolName: got %q", rm.PermissionDenials[0].ToolName)
	}
	if rm.PermissionDenials[0].ToolUseID != "tu-1" {
		t.Errorf("PermissionDenials[0].ToolUseID: got %q", rm.PermissionDenials[0].ToolUseID)
	}
	if rm.PermissionDenials[1].ToolName != "Write" {
		t.Errorf("PermissionDenials[1].ToolName: got %q", rm.PermissionDenials[1].ToolName)
	}
}

// ---- T034: ResultMessage PermissionDenials 复杂场景测试 ----

func TestParseMessage_ResultWithEmptyPermissionDenials(t *testing.T) {
	data := map[string]any{
		"type":               "result",
		"subtype":            "success",
		"session_id":         "s-1",
		"duration_ms":        float64(100),
		"is_error":           false,
		"num_turns":          float64(1),
		"permission_denials": []any{},
	}
	msg := ParseMessage(data)
	rm, ok := msg.(*ResultMessage)
	if !ok {
		t.Fatalf("expected *ResultMessage, got %T", msg)
	}
	if rm.PermissionDenials != nil {
		t.Errorf("expected nil PermissionDenials for empty array, got %v", rm.PermissionDenials)
	}
}

func TestParseMessage_ResultWithNoPermissionDenials(t *testing.T) {
	data := map[string]any{
		"type":       "result",
		"subtype":    "success",
		"session_id": "s-1",
		"duration_ms": float64(100),
		"is_error":   false,
		"num_turns":  float64(1),
	}
	msg := ParseMessage(data)
	rm := msg.(*ResultMessage)
	if rm.PermissionDenials != nil {
		t.Errorf("expected nil PermissionDenials when field absent, got %v", rm.PermissionDenials)
	}
}
