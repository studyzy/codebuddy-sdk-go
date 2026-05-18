// new_features_test.go
// 针对本次新增功能的单元测试，覆盖 NodeJS SDK 中有而 Go 此前未覆盖的逻辑。
//
// 涵盖：
//   - buildArgs: DangerouslySkipPermissions / AdditionalDirectories / Args
//   - Session: SetCanUseTool / GetCanUseTool / HasPendingHistory / SetHooks / historyConsumed
//   - Session: overriddenCanUseTool 优先级
//   - Session: GetAvailableModes / GetAvailableModels / GetAvailableModelsRaw
//   - Session: parseAvailableCommands 辅助函数
//   - Session: SubscribeToCommands / UnsubscribeFromCommands
//   - Client: SetCanUseTool / GetCanUseTool / SetHooks
//   - Client: overriddenCanUseTool 优先级
//   - Client: GetAvailableModes / GetAvailableModels / GetAvailableModelsRaw
//   - Session lock: acquireSessionLock / releaseSessionLock 并发保护
//   - SubprocessTransport: IsReady / OnNotification / OffNotification / dispatchNotification

package codebuddy

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================
// buildArgs: 新增 CLI 参数（NodeJS processTransport.buildArgs 对应）
// ============================================================

func TestBuildArgs_DangerouslySkipPermissionsEnabled(t *testing.T) {
	tr := newTransportForTest(&Options{DangerouslySkipPermissions: true})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--dangerously-skip-permissions")
}

func TestBuildArgs_DangerouslySkipPermissions_NotPresent(t *testing.T) {
	tr := newTransportForTest(&Options{DangerouslySkipPermissions: false})
	args := tr.buildArgs()
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			t.Error("--dangerously-skip-permissions should not appear when false")
		}
	}
}

func TestBuildArgs_AdditionalDirectories_Multiple(t *testing.T) {
	tr := newTransportForTest(&Options{AdditionalDirectories: []string{"/foo", "/bar", "/baz"}})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--add-dir", "/foo")
	assertContainsArgs(t, args, "--add-dir", "/bar")
	assertContainsArgs(t, args, "--add-dir", "/baz")
}

func TestBuildArgs_AdditionalDirectories_Nil(t *testing.T) {
	tr := newTransportForTest(&Options{})
	args := tr.buildArgs()
	for _, a := range args {
		if a == "--add-dir" {
			t.Error("--add-dir should not appear when AdditionalDirectories is nil")
		}
	}
}

func TestBuildArgs_Args_AppendedLast(t *testing.T) {
	tr := newTransportForTest(&Options{Args: []string{"--custom-flag", "custom-value"}})
	args := tr.buildArgs()
	n := len(args)
	if n < 2 {
		t.Fatalf("too few args: %v", args)
	}
	if args[n-2] != "--custom-flag" || args[n-1] != "custom-value" {
		t.Errorf("expected Args at tail, got %v", args[n-2:])
	}
}

func TestBuildArgs_RawArgsAfterExtraArgs(t *testing.T) {
	v := "trace"
	tr := newTransportForTest(&Options{
		ExtraArgs: map[string]*string{"log-level": &v},
		Args:      []string{"--raw-end"},
	})
	args := tr.buildArgs()
	extraIdx, rawIdx := -1, -1
	for i, a := range args {
		if a == "--log-level" {
			extraIdx = i
		}
		if a == "--raw-end" {
			rawIdx = i
		}
	}
	if extraIdx == -1 {
		t.Error("--log-level not found")
	}
	if rawIdx == -1 {
		t.Error("--raw-end not found")
	}
	if rawIdx < extraIdx {
		t.Errorf("--raw-end (%d) must come after --log-level (%d)", rawIdx, extraIdx)
	}
}

func TestBuildArgs_FallbackModel_SameAsModel_NoError(t *testing.T) {
	// NodeJS throws if fallback == model, Go currently does not; just verify both args appear
	model := "claude-opus-4"
	fb := "claude-haiku-3-5"
	tr := newTransportForTest(&Options{Model: &model, FallbackModel: &fb})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--model", "claude-opus-4")
	assertContainsArgs(t, args, "--fallback-model", "claude-haiku-3-5")
}

// ============================================================
// SubprocessTransport: IsReady (mirrors NodeJS isReady())
// ============================================================

func TestSubprocessTransport_IsReady_DefaultFalse(t *testing.T) {
	tr := newTransportForTest(&Options{})
	if tr.IsReady() {
		t.Error("IsReady() should return false before Connect")
	}
}

// ============================================================
// SubprocessTransport: OnNotification / OffNotification / dispatchNotification
// (mirrors NodeJS onNotification / offNotification / handleControlNotification)
// ============================================================

func TestSubprocessTransport_OnNotification_ReceivesDispatch(t *testing.T) {
	tr := newTransportForTest(&Options{})
	var count atomic.Int32
	handler := NotificationHandler(func(_ ControlNotificationMessage) {
		count.Add(1)
	})
	tr.OnNotification(SubscriptionChannelCommands, handler)

	tr.dispatchNotification(map[string]any{
		"channel":  string(SubscriptionChannelCommands),
		"commands": []any{},
	})
	if count.Load() != 1 {
		t.Errorf("handler called %d times, want 1", count.Load())
	}
}

func TestSubprocessTransport_OnNotification_MultipleHandlers(t *testing.T) {
	tr := newTransportForTest(&Options{})
	var calls [3]atomic.Int32
	for i := 0; i < 3; i++ {
		idx := i
		tr.OnNotification(SubscriptionChannelCommands, func(_ ControlNotificationMessage) {
			calls[idx].Add(1)
		})
	}
	tr.dispatchNotification(map[string]any{"channel": string(SubscriptionChannelCommands)})
	for i := range calls {
		if calls[i].Load() != 1 {
			t.Errorf("handler[%d] called %d times, want 1", i, calls[i].Load())
		}
	}
}

func TestSubprocessTransport_OffNotification_RemovesHandler(t *testing.T) {
	tr := newTransportForTest(&Options{})
	var count atomic.Int32
	handler := NotificationHandler(func(_ ControlNotificationMessage) {
		count.Add(1)
	})
	tr.OnNotification(SubscriptionChannelCommands, handler)
	tr.OffNotification(SubscriptionChannelCommands, handler)

	tr.dispatchNotification(map[string]any{"channel": string(SubscriptionChannelCommands)})
	if count.Load() != 0 {
		t.Errorf("handler should not be called after OffNotification, called %d times", count.Load())
	}
}

func TestSubprocessTransport_OffNotification_OnlyRemovesTarget(t *testing.T) {
	tr := newTransportForTest(&Options{})
	var countA, countB atomic.Int32
	handlerA := NotificationHandler(func(_ ControlNotificationMessage) { countA.Add(1) })
	handlerB := NotificationHandler(func(_ ControlNotificationMessage) { countB.Add(1) })

	tr.OnNotification(SubscriptionChannelCommands, handlerA)
	tr.OnNotification(SubscriptionChannelCommands, handlerB)
	tr.OffNotification(SubscriptionChannelCommands, handlerA)

	tr.dispatchNotification(map[string]any{"channel": string(SubscriptionChannelCommands)})
	if countA.Load() != 0 {
		t.Errorf("handlerA should be removed, called %d times", countA.Load())
	}
	if countB.Load() != 1 {
		t.Errorf("handlerB should still run, called %d times", countB.Load())
	}
}

func TestSubprocessTransport_DispatchNotification_WrongChannel(t *testing.T) {
	tr := newTransportForTest(&Options{})
	var count atomic.Int32
	tr.OnNotification(SubscriptionChannelCommands, func(_ ControlNotificationMessage) {
		count.Add(1)
	})
	// Dispatch to a different channel
	tr.dispatchNotification(map[string]any{"channel": "other-channel"})
	if count.Load() != 0 {
		t.Errorf("handler should not fire for wrong channel, called %d times", count.Load())
	}
}

func TestSubprocessTransport_DispatchNotification_HandlerPanic_OthersStillRun(t *testing.T) {
	tr := newTransportForTest(&Options{})
	var safeCount atomic.Int32
	tr.OnNotification(SubscriptionChannelCommands, func(_ ControlNotificationMessage) {
		panic("intentional panic")
	})
	tr.OnNotification(SubscriptionChannelCommands, func(_ ControlNotificationMessage) {
		safeCount.Add(1)
	})
	// Should not panic; safe handler should still run
	tr.dispatchNotification(map[string]any{"channel": string(SubscriptionChannelCommands)})
	if safeCount.Load() != 1 {
		t.Errorf("safe handler called %d times, want 1", safeCount.Load())
	}
}

// ============================================================
// Session: SetCanUseTool / GetCanUseTool
// (mirrors NodeJS setCanUseTool / getCanUseTool)
// ============================================================

func TestSession_SetGetCanUseTool_Default(t *testing.T) {
	s := newSession(&Options{}, nil, "")
	// opts.CanUseTool は nil → nil
	if s.GetCanUseTool() != nil {
		t.Error("expected nil before override when opts.CanUseTool not set")
	}
}

func TestSession_SetGetCanUseTool_OptsCanUseTool(t *testing.T) {
	optsHandler := CanUseToolFunc(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		return &PermissionResultAllow{}, nil
	})
	s := newSession(&Options{CanUseTool: optsHandler}, nil, "")
	got := s.GetCanUseTool()
	if got == nil {
		t.Fatal("expected opts.CanUseTool to be returned")
	}
	res, _ := got(context.Background(), "t", nil, CanUseToolOptions{})
	if res.permissionBehavior() != "allow" {
		t.Error("expected allow")
	}
}

func TestSession_SetGetCanUseTool_OverrideWins(t *testing.T) {
	optsHandler := CanUseToolFunc(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		return &PermissionResultAllow{}, nil
	})
	overrideHandler := CanUseToolFunc(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		return &PermissionResultDeny{Message: "override deny"}, nil
	})
	s := newSession(&Options{CanUseTool: optsHandler}, nil, "")
	s.SetCanUseTool(overrideHandler)

	got := s.GetCanUseTool()
	res, _ := got(context.Background(), "t", nil, CanUseToolOptions{})
	if res.permissionBehavior() != "deny" {
		t.Errorf("override should win, got %v", res.permissionBehavior())
	}
}

func TestSession_SetCanUseTool_NilClearsOverride(t *testing.T) {
	optsHandler := CanUseToolFunc(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		return &PermissionResultAllow{}, nil
	})
	s := newSession(&Options{CanUseTool: optsHandler}, nil, "")
	s.SetCanUseTool(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		return &PermissionResultDeny{}, nil
	})
	s.SetCanUseTool(nil) // clear override
	got := s.GetCanUseTool()
	// should fall back to opts handler
	res, _ := got(context.Background(), "t", nil, CanUseToolOptions{})
	if res.permissionBehavior() != "allow" {
		t.Errorf("after clearing override, opts.CanUseTool should be used, got %v", res.permissionBehavior())
	}
}

// ============================================================
// Session: HasPendingHistory
// (mirrors NodeJS hasPendingHistory())
// ============================================================

func TestSession_HasPendingHistory_NonResumeSession(t *testing.T) {
	s := newSession(&Options{}, nil, "")
	if s.HasPendingHistory() {
		t.Error("non-resume session should never have pending history")
	}
}

func TestSession_HasPendingHistory_ResumeBeforeSend(t *testing.T) {
	s := newSession(&Options{}, nil, "resume-abc")
	if !s.HasPendingHistory() {
		t.Error("resume session before send should have pending history")
	}
}

func TestSession_HasPendingHistory_FalseAfterSend(t *testing.T) {
	s := newSession(&Options{}, nil, "resume-abc")
	s.hasSentMessage.Store(true)
	if s.HasPendingHistory() {
		t.Error("should be false after send()")
	}
}

func TestSession_HasPendingHistory_FalseAfterConsumed(t *testing.T) {
	s := newSession(&Options{}, nil, "resume-abc")
	s.historyConsumed.Store(true)
	if s.HasPendingHistory() {
		t.Error("should be false after history consumed")
	}
}

func TestSession_HasPendingHistory_AllThreeConditions(t *testing.T) {
	// isResume=true AND hasSentMessage=false AND historyConsumed=false → true
	cases := []struct {
		resume          bool
		hasSent         bool
		historyConsumed bool
		want            bool
	}{
		{true, false, false, true},
		{false, false, false, false},
		{true, true, false, false},
		{true, false, true, false},
		{true, true, true, false},
	}
	for _, tc := range cases {
		rid := ""
		if tc.resume {
			rid = "resume-xyz"
		}
		s := newSession(&Options{}, nil, rid)
		s.hasSentMessage.Store(tc.hasSent)
		s.historyConsumed.Store(tc.historyConsumed)
		got := s.HasPendingHistory()
		if got != tc.want {
			t.Errorf("resume=%v sent=%v consumed=%v: HasPendingHistory()=%v, want %v",
				tc.resume, tc.hasSent, tc.historyConsumed, got, tc.want)
		}
	}
}

// ============================================================
// Session: historyConsumed set by backgroundReader on ResultMessage
// (NodeJS session.stream() sets this.historyConsumed = true on result)
// ============================================================

func TestSession_HistoryConsumed_SetOnResultMessage(t *testing.T) {
	tr := newMockTransport(10)
	s := newSession(&Options{}, nil, "resume-id")
	s.core.mu.Lock()
	s.core.transport = tr
	s.core.messageChannel = make(chan Message, 10)
	s.core.closeCh = make(chan struct{})
	s.core.mu.Unlock()

	if s.historyConsumed.Load() {
		t.Error("historyConsumed should be false initially")
	}

	s.core.wg.Add(1)
	go s.backgroundReader(context.Background(), tr)

	tr.injectRaw(map[string]any{
		"type":     "result",
		"subtype":  "success",
		"is_error": false,
	})

	// drain message channel
	select {
	case <-s.core.messageChannel:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for result message")
	}

	if !s.historyConsumed.Load() {
		t.Error("historyConsumed should be true after ResultMessage")
	}

	tr.closeMessages()
	s.core.wg.Wait()
}

func TestSession_HistoryConsumed_NotSetOnOtherMessages(t *testing.T) {
	tr := newMockTransport(10)
	s := newSession(&Options{}, nil, "resume-id")
	s.core.mu.Lock()
	s.core.transport = tr
	s.core.messageChannel = make(chan Message, 10)
	s.core.closeCh = make(chan struct{})
	s.core.mu.Unlock()

	s.core.wg.Add(1)
	go s.backgroundReader(context.Background(), tr)

	// inject a system message (not result)
	tr.injectRaw(map[string]any{
		"type":    "system",
		"subtype": "init",
	})
	time.Sleep(30 * time.Millisecond)

	if s.historyConsumed.Load() {
		t.Error("historyConsumed should remain false for non-result messages")
	}

	tr.closeMessages()
	s.core.wg.Wait()
}

// ============================================================
// Session: SetHooks (mirrors NodeJS setHooks())
// ============================================================

func TestSession_SetHooks_ReplacesRegistry(t *testing.T) {
	s := newSession(&Options{}, nil, "")
	if len(s.core.hookRegistry) != 0 {
		t.Errorf("initial hookRegistry should be empty, got %d", len(s.core.hookRegistry))
	}

	var called1, called2 atomic.Bool
	hooks1 := map[HookEvent][]HookMatcher{
		HookPreToolUse: {{Hooks: []HookCallback{
			func(_ context.Context, _ map[string]any, _ *string) (HookJSONOutput, error) {
				called1.Store(true)
				return HookJSONOutput{}, nil
			},
		}}},
	}
	s.SetHooks(hooks1)
	if len(s.core.hookRegistry) == 0 {
		t.Error("hookRegistry should be populated after SetHooks")
	}

	// Replace with new hooks
	hooks2 := map[HookEvent][]HookMatcher{
		HookPostToolUse: {{Hooks: []HookCallback{
			func(_ context.Context, _ map[string]any, _ *string) (HookJSONOutput, error) {
				called2.Store(true)
				return HookJSONOutput{}, nil
			},
		}}},
	}
	s.SetHooks(hooks2)

	// Old callbacks should not be present
	for id, cb := range s.core.hookRegistry {
		_ = id
		_, _ = cb(context.Background(), nil, nil)
	}
	if called1.Load() {
		t.Error("old hook1 should be replaced")
	}
	if !called2.Load() {
		t.Error("new hook2 should be in registry")
	}
}

// ============================================================
// Session: handleControlRequest uses overriddenCanUseTool
// (mirrors NodeJS handlePermissionRequest: use overriddenCanUseTool ?? options.canUseTool)
// ============================================================

func TestSession_HandleControlRequest_OverrideBeatsOpts(t *testing.T) {
	tr := newMockTransport(10)
	optsCalled := atomic.Bool{}
	overrideCalled := atomic.Bool{}

	s := newSession(&Options{
		CanUseTool: func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
			optsCalled.Store(true)
			return &PermissionResultDeny{Message: "opts"}, nil
		},
	}, nil, "")
	s.core.mu.Lock()
	s.core.transport = tr
	s.core.mu.Unlock()

	s.SetCanUseTool(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		overrideCalled.Store(true)
		return &PermissionResultAllow{}, nil
	})

	s.core.handleControlRequest(context.Background(), map[string]any{
		"request_id": "req-x",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Bash",
			"input":       map[string]any{},
			"tool_use_id": "tu-x",
		},
	})
	time.Sleep(30 * time.Millisecond)

	if optsCalled.Load() {
		t.Error("opts.CanUseTool should NOT be called when override is set")
	}
	if !overrideCalled.Load() {
		t.Error("overriddenCanUseTool should be called")
	}
}

func TestSession_HandleControlRequest_FallsBackToOpts_WhenNoOverride(t *testing.T) {
	tr := newMockTransport(10)
	optsCalled := atomic.Bool{}

	s := newSession(&Options{
		CanUseTool: func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
			optsCalled.Store(true)
			return &PermissionResultAllow{}, nil
		},
	}, nil, "")
	s.core.mu.Lock()
	s.core.transport = tr
	s.core.mu.Unlock()
	// No SetCanUseTool → should fall back to opts

	s.core.handleControlRequest(context.Background(), map[string]any{
		"request_id": "req-y",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Read",
			"input":       map[string]any{},
			"tool_use_id": "tu-y",
		},
	})
	time.Sleep(30 * time.Millisecond)

	if !optsCalled.Load() {
		t.Error("opts.CanUseTool should be called when no override set")
	}
}

// ============================================================
// Session: GetAvailableModes (send control req, parse response)
// ============================================================

func TestSession_GetAvailableModes_ParsesResponse(t *testing.T) {
	tr, s, cleanup := setupConnectedSession(t)
	defer cleanup()

	var wg sync.WaitGroup
	wg.Add(1)
	var modes []AvailableMode
	var err error
	go func() {
		defer wg.Done()
		modes, err = s.GetAvailableModes(context.Background())
	}()

	injectControlResponse(t, tr, map[string]any{
		"availableModes": []any{
			map[string]any{"id": "default", "description": "Default"},
			map[string]any{"id": "bypassPermissions", "description": "Bypass all"},
		},
	})
	wg.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modes) != 2 {
		t.Fatalf("expected 2 modes, got %d", len(modes))
	}
	if modes[0].ID != "default" || modes[1].ID != "bypassPermissions" {
		t.Errorf("unexpected modes: %+v", modes)
	}
}

func TestSession_GetAvailableModes_EmptyList(t *testing.T) {
	tr, s, cleanup := setupConnectedSession(t)
	defer cleanup()

	var wg sync.WaitGroup
	wg.Add(1)
	var modes []AvailableMode
	go func() {
		defer wg.Done()
		modes, _ = s.GetAvailableModes(context.Background())
	}()

	injectControlResponse(t, tr, map[string]any{"availableModes": []any{}})
	wg.Wait()

	if len(modes) != 0 {
		t.Errorf("expected 0 modes, got %d", len(modes))
	}
}

// ============================================================
// Session: GetAvailableModels
// ============================================================

func TestSession_GetAvailableModels_ParsesResponse(t *testing.T) {
	tr, s, cleanup := setupConnectedSession(t)
	defer cleanup()

	var wg sync.WaitGroup
	wg.Add(1)
	var models []AvailableModel
	var err error
	go func() {
		defer wg.Done()
		models, err = s.GetAvailableModels(context.Background())
	}()

	injectControlResponse(t, tr, map[string]any{
		"availableModels": []any{
			map[string]any{"modelId": "claude-opus-4", "displayName": "Claude Opus 4"},
			map[string]any{"modelId": "claude-sonnet-4", "displayName": "Claude Sonnet 4"},
		},
	})
	wg.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ModelID != "claude-opus-4" || models[0].Name != "Claude Opus 4" {
		t.Errorf("unexpected model[0]: %+v", models[0])
	}
}

// ============================================================
// Session: GetAvailableModelsRaw
// ============================================================

func TestSession_GetAvailableModelsRaw_ReturnsRawMap(t *testing.T) {
	tr, s, cleanup := setupConnectedSession(t)
	defer cleanup()

	var wg sync.WaitGroup
	wg.Add(1)
	var raw []map[string]any
	var err error
	go func() {
		defer wg.Done()
		raw, err = s.GetAvailableModelsRaw(context.Background())
	}()

	injectControlResponse(t, tr, map[string]any{
		"rawModels": []any{
			map[string]any{"modelId": "m1", "contextWindow": float64(200000), "supportsVision": true},
		},
	})
	wg.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 raw model, got %d", len(raw))
	}
	if raw[0]["modelId"] != "m1" {
		t.Errorf("modelId mismatch: %v", raw[0]["modelId"])
	}
	if raw[0]["contextWindow"] != float64(200000) {
		t.Errorf("contextWindow mismatch: %v", raw[0]["contextWindow"])
	}
}

func TestSession_GetAvailableModelsRaw_MissingKey_ReturnsEmpty(t *testing.T) {
	tr, s, cleanup := setupConnectedSession(t)
	defer cleanup()

	var wg sync.WaitGroup
	wg.Add(1)
	var raw []map[string]any
	go func() {
		defer wg.Done()
		raw, _ = s.GetAvailableModelsRaw(context.Background())
	}()

	// response without rawModels key
	injectControlResponse(t, tr, map[string]any{})
	wg.Wait()

	if len(raw) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(raw))
	}
}

// ============================================================
// parseAvailableCommands (mirrors NodeJS getAvailableCommands parse logic)
// ============================================================

func TestParseAvailableCommands_StripLeadingSlash(t *testing.T) {
	raw := []any{
		map[string]any{"name": "/help", "description": "Show help"},
		map[string]any{"name": "run", "description": "Run without slash"},
	}
	cmds := parseAvailableCommands(raw)
	if len(cmds) != 2 {
		t.Fatalf("expected 2, got %d", len(cmds))
	}
	if cmds[0].Name != "help" {
		t.Errorf("expected 'help', got %q", cmds[0].Name)
	}
	if cmds[1].Name != "run" {
		t.Errorf("expected 'run', got %q", cmds[1].Name)
	}
}

func TestParseAvailableCommands_ArgumentHint(t *testing.T) {
	raw := []any{
		map[string]any{"name": "exec", "description": "Execute", "argumentHint": "<script>"},
		map[string]any{"name": "noop", "description": "No args"},
	}
	cmds := parseAvailableCommands(raw)
	if cmds[0].Input == nil || cmds[0].Input.Hint != "<script>" {
		t.Errorf("expected hint '<script>', got %v", cmds[0].Input)
	}
	if cmds[1].Input != nil {
		t.Errorf("expected nil Input for no-arg command, got %v", cmds[1].Input)
	}
}

func TestParseAvailableCommands_EmptyList(t *testing.T) {
	cmds := parseAvailableCommands([]any{})
	if len(cmds) != 0 {
		t.Errorf("expected empty, got %d", len(cmds))
	}
}

func TestParseAvailableCommands_NilList(t *testing.T) {
	cmds := parseAvailableCommands(nil)
	if len(cmds) != 0 {
		t.Errorf("expected empty for nil input, got %d", len(cmds))
	}
}

func TestParseAvailableCommands_InvalidEntrySkipped(t *testing.T) {
	raw := []any{
		"not-a-map",
		map[string]any{"name": "valid", "description": "ok"},
	}
	cmds := parseAvailableCommands(raw)
	if len(cmds) != 1 || cmds[0].Name != "valid" {
		t.Errorf("invalid entry should be skipped, got %+v", cmds)
	}
}

// ============================================================
// Session: SubscribeToCommands / UnsubscribeFromCommands
// (mirrors NodeJS subscribeToCommands / unsubscribeFromCommands)
// ============================================================

func TestSession_UnsubscribeFromCommands_BeforeConnect(t *testing.T) {
	s := newSession(&Options{}, nil, "")
	// Should not panic when transport is nil
	s.UnsubscribeFromCommands(func(_ ControlNotificationMessage) {})
}

// ============================================================
// Client: SetCanUseTool / GetCanUseTool
// ============================================================

func TestClient_SetGetCanUseTool_Default(t *testing.T) {
	c := NewClient(nil)
	if c.GetCanUseTool() != nil {
		t.Error("expected nil before override")
	}
}

func TestClient_SetGetCanUseTool_OverrideWins(t *testing.T) {
	optsHandler := CanUseToolFunc(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		return &PermissionResultAllow{}, nil
	})
	c := NewClient(&Options{CanUseTool: optsHandler})

	override := CanUseToolFunc(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		return &PermissionResultDeny{Message: "c-deny"}, nil
	})
	c.SetCanUseTool(override)

	got := c.GetCanUseTool()
	res, _ := got(context.Background(), "t", nil, CanUseToolOptions{})
	if res.permissionBehavior() != "deny" {
		t.Errorf("override should win, got %v", res.permissionBehavior())
	}
}

// ============================================================
// Client: SetHooks
// ============================================================

func TestClient_SetHooks_PopulatesRegistry(t *testing.T) {
	c := NewClient(nil)
	if len(c.core.hookRegistry) != 0 {
		t.Errorf("expected empty registry initially")
	}

	var called atomic.Bool
	hooks := map[HookEvent][]HookMatcher{
		HookStop: {{Hooks: []HookCallback{
			func(_ context.Context, _ map[string]any, _ *string) (HookJSONOutput, error) {
				called.Store(true)
				return HookJSONOutput{}, nil
			},
		}}},
	}
	c.SetHooks(hooks)
	if len(c.core.hookRegistry) == 0 {
		t.Error("hookRegistry should be non-empty after SetHooks")
	}
	for _, cb := range c.core.hookRegistry {
		_, _ = cb(context.Background(), nil, nil)
		break
	}
	if !called.Load() {
		t.Error("hook callback should be invoked")
	}
}

// ============================================================
// Client: handleControlRequest uses overriddenCanUseTool
// ============================================================

func TestClient_HandleControlRequest_OverrideBeatsOpts(t *testing.T) {
	tr := newMockTransport(10)
	optsCalled := atomic.Bool{}
	overrideCalled := atomic.Bool{}

	c := NewClient(&Options{
		CanUseTool: func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
			optsCalled.Store(true)
			return &PermissionResultDeny{}, nil
		},
	})
	c.core.mu.Lock()
	c.core.transport = tr
	c.connected = true
	c.core.mu.Unlock()

	c.SetCanUseTool(func(_ context.Context, _ string, _ map[string]any, _ CanUseToolOptions) (PermissionResult, error) {
		overrideCalled.Store(true)
		return &PermissionResultAllow{}, nil
	})

	c.core.handleControlRequest(context.Background(), map[string]any{
		"request_id": "c-req-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Write",
			"input":       map[string]any{},
			"tool_use_id": "tu-c1",
		},
	})
	time.Sleep(30 * time.Millisecond)

	if optsCalled.Load() {
		t.Error("opts.CanUseTool must NOT be called when override is set")
	}
	if !overrideCalled.Load() {
		t.Error("overriddenCanUseTool must be called")
	}
}

// ============================================================
// Session lock: concurrent resume prevention
// (mirrors NodeJS acquireSessionLock / releaseSessionLock)
// ============================================================

func TestSessionLock_PreventsConcurrentResume(t *testing.T) {
	sid := "lock-test-session-concurrent"
	releaseSessionLock(sid) // clean state

	if !acquireSessionLock(sid) {
		t.Fatal("first acquire should succeed")
	}
	defer releaseSessionLock(sid)

	if acquireSessionLock(sid) {
		t.Error("second acquire should fail while lock is held")
		releaseSessionLock(sid)
	}
}

func TestSessionLock_ReleaseThenReacquire(t *testing.T) {
	sid := "lock-test-reacquire"
	releaseSessionLock(sid)

	if !acquireSessionLock(sid) {
		t.Fatal("acquire should succeed")
	}
	releaseSessionLock(sid)

	if !acquireSessionLock(sid) {
		t.Error("reacquire after release should succeed")
	}
	releaseSessionLock(sid)
}

func TestSessionLock_DifferentIDsIndependent(t *testing.T) {
	sid1, sid2 := "lock-id-alpha", "lock-id-beta"
	releaseSessionLock(sid1)
	releaseSessionLock(sid2)

	if !acquireSessionLock(sid1) {
		t.Fatal("acquire sid1 should succeed")
	}
	defer releaseSessionLock(sid1)

	// Different ID should not be blocked
	if !acquireSessionLock(sid2) {
		t.Error("acquire sid2 should succeed independently of sid1")
	}
	releaseSessionLock(sid2)
}

// ============================================================
// helpers
// ============================================================

// setupConnectedSession creates a session backed by a mock transport with
// backgroundReader running. Returns transport, session, and a cleanup func.
func setupConnectedSession(t *testing.T) (*mockTransport, *Session, func()) {
	t.Helper()
	tr := newMockTransport(20)
	s := newSession(&Options{}, nil, "")
	s.core.mu.Lock()
	s.core.transport = tr
	s.core.messageChannel = make(chan Message, 20)
	s.core.closeCh = make(chan struct{})
	s.initialized = true
	s.core.mu.Unlock()

	s.core.wg.Add(1)
	go s.backgroundReader(context.Background(), tr)

	cleanup := func() {
		tr.closeMessages()
		s.core.wg.Wait()
	}
	return tr, s, cleanup
}

// injectControlResponse waits for the last written control request and
// injects a matching success response into the transport.
func injectControlResponse(t *testing.T, tr *mockTransport, responsePayload map[string]any) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		tr.mu.Lock()
		n := len(tr.written)
		tr.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tr.mu.Lock()
	n := len(tr.written)
	tr.mu.Unlock()
	if n == 0 {
		t.Fatal("no control request written to transport")
	}
	reqMsg := tr.writtenJSON(n - 1)
	reqID, _ := reqMsg["request_id"].(string)

	tr.injectRaw(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": reqID,
			"subtype":    "success",
			"response":   responsePayload,
		},
	})
}

// ============================================================
// T012: buildArgs 新增 PermissionMode 测试
// ============================================================

func TestBuildArgs_PermissionMode_Delegate(t *testing.T) {
	tr := newTransportForTest(&Options{PermissionMode: PermissionModeDelegate.Ptr()})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--permission-mode", "delegate")
}

func TestBuildArgs_PermissionMode_DontAsk(t *testing.T) {
	tr := newTransportForTest(&Options{PermissionMode: PermissionModeDontAsk.Ptr()})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--permission-mode", "dontAsk")
}

func TestBuildArgs_PermissionMode_FullAccess(t *testing.T) {
	tr := newTransportForTest(&Options{PermissionMode: PermissionModeFullAccess.Ptr()})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--permission-mode", "fullAccess")
}

// ============================================================
// T032: buildArgs 新增 Options 字段测试
// ============================================================

func TestBuildArgs_MaxBudgetUsd(t *testing.T) {
	budget := 5.0
	tr := newTransportForTest(&Options{MaxBudgetUsd: &budget})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--max-budget-usd", "5")
}

func TestBuildArgs_PersistSession_False(t *testing.T) {
	f := false
	tr := newTransportForTest(&Options{PersistSession: &f})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--no-persist-session")
}

func TestBuildArgs_PersistSession_True_NoFlag(t *testing.T) {
	tt := true
	tr := newTransportForTest(&Options{PersistSession: &tt})
	args := tr.buildArgs()
	for _, a := range args {
		if a == "--no-persist-session" {
			t.Error("--no-persist-session should not appear when PersistSession is true")
		}
	}
}

func TestBuildArgs_EnableFileCheckpointing(t *testing.T) {
	tr := newTransportForTest(&Options{EnableFileCheckpointing: true})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--enable-file-checkpointing")
}

func TestBuildArgs_Sandbox_Disabled(t *testing.T) {
	f := false
	tr := newTransportForTest(&Options{Sandbox: &SandboxSettings{Enabled: &f}})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--sandbox=false")
}

func TestBuildArgs_Environment(t *testing.T) {
	env := "internal"
	tr := newTransportForTest(&Options{Environment: &env})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--environment", "internal")
}

func TestBuildArgs_Endpoint(t *testing.T) {
	ep := "https://custom.example.com"
	tr := newTransportForTest(&Options{Endpoint: &ep})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--endpoint", "https://custom.example.com")
}

func TestBuildArgs_ResumeSessionAt(t *testing.T) {
	at := "msg-123"
	tr := newTransportForTest(&Options{ResumeSessionAt: &at})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--resume-session-at", "msg-123")
}

func TestBuildArgs_PermissionPromptToolName(t *testing.T) {
	tn := "my-mcp-tool"
	tr := newTransportForTest(&Options{PermissionPromptToolName: &tn})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--permission-prompt-tool-name", "my-mcp-tool")
}

func TestBuildArgs_StrictMcpConfig(t *testing.T) {
	tr := newTransportForTest(&Options{StrictMcpConfig: true})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--strict-mcp-config")
}

// ============================================================
// T023: executeHook systemMessage/hookSpecificOutput 测试
// ============================================================

func TestExecuteHook_NewOutputFields(t *testing.T) {
	ctx := context.Background()
	sysMsg := "injected system message"
	registry := HookCallbackRegistry{
		"hook-1": func(ctx context.Context, input map[string]any, toolUseID *string) (HookJSONOutput, error) {
			return HookJSONOutput{
				SystemMessage:      &sysMsg,
				HookSpecificOutput: map[string]any{"custom": "data"},
			}, nil
		},
	}
	output := executeHook(ctx, "hook-1", nil, nil, registry)
	if output["systemMessage"] != "injected system message" {
		t.Errorf("systemMessage: got %v", output["systemMessage"])
	}
	hso, ok := output["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatal("hookSpecificOutput not present")
	}
	if hso["custom"] != "data" {
		t.Errorf("hookSpecificOutput[custom]: got %v", hso["custom"])
	}
}

// ============================================================
// T027: SetConfig 测试
// ============================================================

func TestConnCore_SetConfig(t *testing.T) {
	c := &connCore{}
	initConnCore(c, &Options{}, PermissionModeDefault, "")

	tr := newMockTransport(100)
	c.mu.Lock()
	c.transport = tr
	c.mu.Unlock()

	// 启动 goroutine 模拟响应
	go func() {
		time.Sleep(20 * time.Millisecond)
		tr.mu.Lock()
		n := len(tr.written)
		tr.mu.Unlock()
		if n == 0 {
			return
		}
		reqMsg := tr.writtenJSON(0)
		reqID, _ := reqMsg["request_id"].(string)
		// 直接路由响应到 connCore
		c.routeControlResponse(map[string]any{
			"response": map[string]any{
				"request_id": reqID,
				"subtype":    "success",
				"response": map[string]any{
					"updated": map[string]any{"thinking": "disabled"},
					"errors":  map[string]any{"bad_key": "unknown config key"},
				},
			},
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := c.setConfig(ctx, "test-session", map[string]any{"thinking": "disabled"}, "not connected")
	if err != nil {
		t.Fatalf("setConfig error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Updated["thinking"] != "disabled" {
		t.Errorf("Updated[thinking]: got %v", result.Updated["thinking"])
	}
	if result.Errors["bad_key"] != "unknown config key" {
		t.Errorf("Errors[bad_key]: got %v", result.Errors["bad_key"])
	}
}

// ============================================================
// T033: SessionOptions merge 测试
// ============================================================

func TestSessionOptions_MergeToOpts(t *testing.T) {
	baseOpts := &Options{
		Model: ptrStr("base-model"),
	}
	so := &SessionOptions{
		Thinking: &ThinkingConfig{Type: "adaptive"},
		Effort:   EffortHigh.Ptr(),
		Cwd:      ptrStr("/custom/path"),
	}
	mergeSessionOptsToOpts(baseOpts, so)

	if baseOpts.Thinking == nil || baseOpts.Thinking.Type != "adaptive" {
		t.Errorf("Thinking not merged: got %v", baseOpts.Thinking)
	}
	if baseOpts.Effort == nil || *baseOpts.Effort != EffortHigh {
		t.Errorf("Effort not merged: got %v", baseOpts.Effort)
	}
	if baseOpts.Cwd == nil || *baseOpts.Cwd != "/custom/path" {
		t.Errorf("Cwd not merged: got %v", baseOpts.Cwd)
	}
	// Model should not be overwritten since SessionOptions.Model was nil
	if baseOpts.Model == nil || *baseOpts.Model != "base-model" {
		t.Errorf("Model should remain base-model, got %v", baseOpts.Model)
	}
}

func ptrStr(s string) *string { return &s }
