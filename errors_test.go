// errors_test.go
package codebuddy

import (
	"errors"
	"testing"
)

func TestErrorTypes(t *testing.T) {

	t.Run("CLIConnectionError", func(t *testing.T) {
		err := &CLIConnectionError{Message: "connection failed"}

		if err.Error() != "connection failed" {
			t.Fatalf("Error(): got %q, want \"connection failed\"", err.Error())
		}

		var target *CLIConnectionError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed for *CLIConnectionError")
		}
		if target.Message != "connection failed" {
			t.Errorf("target.Message: got %q", target.Message)
		}

		// Verify it does NOT match a different error type
		var other *CLINotFoundError
		if errors.As(err, &other) {
			t.Error("errors.As should not match *CLINotFoundError")
		}
	})

	t.Run("CLINotFoundError", func(t *testing.T) {
		err := &CLINotFoundError{
			Message:  "cli not found",
			Platform: "linux",
			Arch:     "amd64",
		}

		if err.Error() != "cli not found" {
			t.Fatalf("Error(): got %q, want \"cli not found\"", err.Error())
		}

		var target *CLINotFoundError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed for *CLINotFoundError")
		}
		if target.Platform != "linux" {
			t.Errorf("Platform: got %q, want \"linux\"", target.Platform)
		}
		if target.Arch != "amd64" {
			t.Errorf("Arch: got %q, want \"amd64\"", target.Arch)
		}
	})

	t.Run("CLIJSONDecodeError", func(t *testing.T) {
		err := &CLIJSONDecodeError{Message: "invalid json"}

		if err.Error() != "invalid json" {
			t.Fatalf("Error(): got %q, want \"invalid json\"", err.Error())
		}

		var target *CLIJSONDecodeError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed for *CLIJSONDecodeError")
		}
	})

	t.Run("CLIStartupError", func(t *testing.T) {
		code := 1
		err := &CLIStartupError{
			Message:  "startup failed",
			Stderr:   "some stderr output",
			ExitCode: &code,
		}

		if err.Error() != "startup failed" {
			t.Fatalf("Error(): got %q, want \"startup failed\"", err.Error())
		}

		var target *CLIStartupError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed for *CLIStartupError")
		}
		if target.Stderr != "some stderr output" {
			t.Errorf("Stderr: got %q", target.Stderr)
		}
		if target.ExitCode == nil || *target.ExitCode != 1 {
			t.Errorf("ExitCode: got %v, want 1", target.ExitCode)
		}
	})

	t.Run("CLIStartupError_NilExitCode", func(t *testing.T) {
		err := &CLIStartupError{Message: "crashed", ExitCode: nil}
		if err.ExitCode != nil {
			t.Error("ExitCode should be nil")
		}
		if err.Error() != "crashed" {
			t.Errorf("Error(): got %q", err.Error())
		}
	})

	t.Run("ProcessError", func(t *testing.T) {
		err := &ProcessError{Message: "process died"}

		if err.Error() != "process died" {
			t.Fatalf("Error(): got %q, want \"process died\"", err.Error())
		}

		var target *ProcessError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed for *ProcessError")
		}
	})

	t.Run("ExecutionError_direct", func(t *testing.T) {
		err := &ExecutionError{
			Message: "exec failed",
			Errors:  []string{"exec failed", "detail"},
			Subtype: "timeout",
		}

		if err.Error() != "exec failed" {
			t.Fatalf("Error(): got %q, want \"exec failed\"", err.Error())
		}

		var target *ExecutionError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed for *ExecutionError")
		}
		if target.Subtype != "timeout" {
			t.Errorf("Subtype: got %q, want \"timeout\"", target.Subtype)
		}
	})

	t.Run("AuthenticationError", func(t *testing.T) {
		err := &AuthenticationError{
			Message:   "auth failed",
			ErrorType: "invalid_token",
		}

		if err.Error() != "auth failed" {
			t.Fatalf("Error(): got %q, want \"auth failed\"", err.Error())
		}

		var target *AuthenticationError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed for *AuthenticationError")
		}
		if target.ErrorType != "invalid_token" {
			t.Errorf("ErrorType: got %q, want \"invalid_token\"", target.ErrorType)
		}
	})
}

// ---- NewExecutionError factory ---------------------------------------------

func TestNewExecutionError(t *testing.T) {

	t.Run("with errors list", func(t *testing.T) {
		err := NewExecutionError([]string{"api error", "detail"}, "api_error")

		if err.Message != "api error" {
			t.Fatalf("Message: got %q, want \"api error\"", err.Message)
		}
		if err.Subtype != "api_error" {
			t.Fatalf("Subtype: got %q, want \"api_error\"", err.Subtype)
		}
		if len(err.Errors) != 2 {
			t.Errorf("Errors len: got %d, want 2", len(err.Errors))
		}
		if err.Error() != "api error" {
			t.Errorf("Error(): got %q", err.Error())
		}
	})

	t.Run("with empty errors list", func(t *testing.T) {
		err := NewExecutionError([]string{}, "unknown")

		if err.Message != "Execution failed" {
			t.Fatalf("Message: got %q, want \"Execution failed\"", err.Message)
		}
		if err.Error() != "Execution failed" {
			t.Errorf("Error(): got %q", err.Error())
		}
	})

	t.Run("with nil errors list", func(t *testing.T) {
		err := NewExecutionError(nil, "none")

		if err.Message != "Execution failed" {
			t.Fatalf("Message: got %q, want \"Execution failed\"", err.Message)
		}
	})

	t.Run("errors.As works on NewExecutionError result", func(t *testing.T) {
		err := NewExecutionError([]string{"boom"}, "crash")
		var target *ExecutionError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Message != "boom" {
			t.Errorf("target.Message: got %q", target.Message)
		}
	})
}

// ---- errors interface conformance ------------------------------------------

// TestErrorInterface makes sure every error type satisfies the error interface
// at the type level. This is a compile-time check bundled as a runtime test.
func TestErrorInterface(t *testing.T) {
	var _ error = (*CLIConnectionError)(nil)
	var _ error = (*CLINotFoundError)(nil)
	var _ error = (*CLIJSONDecodeError)(nil)
	var _ error = (*CLIStartupError)(nil)
	var _ error = (*ProcessError)(nil)
	var _ error = (*ExecutionError)(nil)
	var _ error = (*AuthenticationError)(nil)
	// If the above assignments compile and the test is reached, the check passed.
	t.Log("all 7 error types satisfy the error interface")
}
