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
