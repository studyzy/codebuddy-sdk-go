// protocol_test.go
package codebuddy

import (
	"context"
	"fmt"
	"testing"
)

// ---- BuildControlResponse --------------------------------------------------

func TestBuildControlResponse(t *testing.T) {
	resp := BuildControlResponse("req1", map[string]any{"foo": "bar"})

	// top-level type
	if got, ok := resp["type"].(string); !ok || got != "control_response" {
		t.Errorf("type: got %v, want \"control_response\"", resp["type"])
	}

	inner, ok := resp["response"].(map[string]any)
	if !ok {
		t.Fatalf("response: expected map[string]any, got %T", resp["response"])
	}

	// subtype
	if got, ok := inner["subtype"].(string); !ok || got != "success" {
		t.Errorf("response.subtype: got %v, want \"success\"", inner["subtype"])
	}

	// request_id
	if got, ok := inner["request_id"].(string); !ok || got != "req1" {
		t.Errorf("response.request_id: got %v, want \"req1\"", inner["request_id"])
	}

	// nested response payload
	payload, ok := inner["response"].(map[string]any)
	if !ok {
		t.Fatalf("response.response: expected map[string]any, got %T", inner["response"])
	}
	if got, ok := payload["foo"].(string); !ok || got != "bar" {
		t.Errorf("response.response.foo: got %v, want \"bar\"", payload["foo"])
	}
}

// TestBuildControlResponse_NilPayload verifies that a nil payload is preserved.
// Note: in Go, a typed-nil map[string]any stored in an interface is NOT == nil
// at the interface level, so we use a type assertion to extract and compare.
func TestBuildControlResponse_NilPayload(t *testing.T) {
	resp := BuildControlResponse("req-nil", nil)
	inner := resp["response"].(map[string]any)
	// inner["response"] holds a typed nil map[string]any; extract via assertion.
	payload, _ := inner["response"].(map[string]any)
	if payload != nil {
		t.Errorf("response.response: expected nil map, got %v", payload)
	}
}

// ---- BuildControlErrorResponse ---------------------------------------------

func TestBuildControlErrorResponse(t *testing.T) {
	resp := BuildControlErrorResponse("req2", "something went wrong")

	// top-level type
	if got, ok := resp["type"].(string); !ok || got != "control_response" {
		t.Errorf("type: got %v, want \"control_response\"", resp["type"])
	}

	inner, ok := resp["response"].(map[string]any)
	if !ok {
		t.Fatalf("response: expected map[string]any, got %T", resp["response"])
	}

	// subtype
	if got, ok := inner["subtype"].(string); !ok || got != "error" {
		t.Errorf("response.subtype: got %v, want \"error\"", inner["subtype"])
	}

	// request_id
	if got, ok := inner["request_id"].(string); !ok || got != "req2" {
		t.Errorf("response.request_id: got %v, want \"req2\"", inner["request_id"])
	}

	// error message
	if got, ok := inner["error"].(string); !ok || got != "something went wrong" {
		t.Errorf("response.error: got %v, want \"something went wrong\"", inner["error"])
	}

	// must NOT contain "response" key
	if _, exists := inner["response"]; exists {
		t.Error("error response should not have a \"response\" key")
	}
}

// ---- BuildHooksConfig ------------------------------------------------------

func TestBuildHooksConfig_Nil(t *testing.T) {
	config, registry := BuildHooksConfig(nil)

	if config != nil {
		t.Errorf("config: expected nil, got %v", config)
	}
	if registry == nil {
		t.Error("registry should not be nil")
	}
	if len(registry) != 0 {
		t.Errorf("registry len: got %d, want 0", len(registry))
	}
}

func TestBuildHooksConfig_EmptyMap(t *testing.T) {
	config, registry := BuildHooksConfig(map[HookEvent][]HookMatcher{})

	// Empty map → same as nil: no config
	if config != nil {
		t.Errorf("config: expected nil for empty hooks map, got %v", config)
	}
	if len(registry) != 0 {
		t.Errorf("registry len: got %d, want 0", len(registry))
	}
}

func TestBuildHooksConfig_WithHooks(t *testing.T) {
	// Three distinct callbacks; only cb0 increments a counter so the compiler
	// doesn't complain about unused variables.
	callCount := 0
	cb0 := HookCallback(func(_ context.Context, _ map[string]any, _ *string) (HookJSONOutput, error) {
		callCount++
		return HookJSONOutput{}, nil
	})
	cb1 := HookCallback(func(_ context.Context, _ map[string]any, _ *string) (HookJSONOutput, error) {
		return HookJSONOutput{}, nil
	})
	cb2 := HookCallback(func(_ context.Context, _ map[string]any, _ *string) (HookJSONOutput, error) {
		return HookJSONOutput{}, nil
	})

	matcher0 := "Bash"
	hooks := map[HookEvent][]HookMatcher{
		HookPreToolUse: {
			{
				Matcher: &matcher0,
				Hooks:   []HookCallback{cb0, cb1},
			},
			{
				Matcher: nil,
				Hooks:   []HookCallback{cb2},
			},
		},
	}

	config, registry := BuildHooksConfig(hooks)

	if config == nil {
		t.Fatal("config should not be nil")
	}

	// Verify top-level key equals the HookEvent string
	eventKey := string(HookPreToolUse)
	matcherList, ok := config[eventKey].([]any)
	if !ok {
		t.Fatalf("config[%q] type: got %T, want []any", eventKey, config[eventKey])
	}
	if len(matcherList) != 2 {
		t.Fatalf("matcherList len: got %d, want 2", len(matcherList))
	}

	// matcher 0 → 2 hooks → IDs hook_PreToolUse_0_0 and hook_PreToolUse_0_1
	m0, ok := matcherList[0].(map[string]any)
	if !ok {
		t.Fatalf("matcherList[0] type: got %T", matcherList[0])
	}
	cbIDs0, ok := m0["hookCallbackIds"].([]string)
	if !ok {
		t.Fatalf("hookCallbackIds type: got %T", m0["hookCallbackIds"])
	}
	wantIDs0 := []string{
		fmt.Sprintf("hook_%s_0_0", eventKey),
		fmt.Sprintf("hook_%s_0_1", eventKey),
	}
	for i, want := range wantIDs0 {
		if i >= len(cbIDs0) || cbIDs0[i] != want {
			t.Errorf("cbIDs0[%d]: got %q, want %q", i, cbIDs0[i], want)
		}
	}

	// matcher 1 → 1 hook → ID hook_PreToolUse_1_0
	m1, ok := matcherList[1].(map[string]any)
	if !ok {
		t.Fatalf("matcherList[1] type: got %T", matcherList[1])
	}
	cbIDs1, ok := m1["hookCallbackIds"].([]string)
	if !ok {
		t.Fatalf("hookCallbackIds type: got %T", m1["hookCallbackIds"])
	}
	wantID1 := fmt.Sprintf("hook_%s_1_0", eventKey)
	if len(cbIDs1) != 1 || cbIDs1[0] != wantID1 {
		t.Errorf("cbIDs1[0]: got %v, want %q", cbIDs1, wantID1)
	}

	// Registry must contain exactly 3 entries
	if len(registry) != 3 {
		t.Errorf("registry len: got %d, want 3", len(registry))
	}
	for _, id := range append(wantIDs0, wantID1) {
		if _, exists := registry[id]; !exists {
			t.Errorf("registry missing key %q", id)
		}
	}
}

func TestBuildHooksConfig_CallbackIDFormat(t *testing.T) {
	// Verify that the ID format is exactly hook_{event}_{i}_{j} for all
	// combinations by using multiple events.
	events := []HookEvent{HookPreToolUse, HookPostToolUse}
	noopCB := HookCallback(func(_ context.Context, _ map[string]any, _ *string) (HookJSONOutput, error) {
		return HookJSONOutput{}, nil
	})

	hooks := map[HookEvent][]HookMatcher{}
	for _, ev := range events {
		hooks[ev] = []HookMatcher{
			{Hooks: []HookCallback{noopCB}},
		}
	}

	_, registry := BuildHooksConfig(hooks)

	for _, ev := range events {
		expectedID := fmt.Sprintf("hook_%s_0_0", string(ev))
		if _, ok := registry[expectedID]; !ok {
			t.Errorf("registry missing %q", expectedID)
		}
	}
}
