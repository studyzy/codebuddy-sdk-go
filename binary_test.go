package codebuddy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestGetCLIPath_EnvVar verifies that CODEBUDDY_CODE_PATH is honoured when it
// points to a file that actually exists on disk.
func TestGetCLIPath_EnvVar(t *testing.T) {
	// Create a real temp file so that os.Stat succeeds inside GetCLIPath.
	tmp, err := os.CreateTemp("", "fake-codebuddy-cli-*")
	if err != nil {
		t.Fatalf("could not create temp file: %v", err)
	}
	defer os.Remove(tmp.Name())
	tmp.Close()

	t.Setenv("CODEBUDDY_CODE_PATH", tmp.Name())

	path, err := GetCLIPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != tmp.Name() {
		t.Errorf("got %q, want %q", path, tmp.Name())
	}
}

// TestGetCLIPath_EnvVarMissingFile verifies that when CODEBUDDY_CODE_PATH
// points to a non-existent file, GetCLIPath falls through to other lookup
// strategies rather than returning an error immediately.
func TestGetCLIPath_EnvVarMissingFile(t *testing.T) {
	t.Setenv("CODEBUDDY_CODE_PATH", "/does/not/exist/codebuddy-headless")

	// The call must not return the non-existent path.  It either finds the CLI
	// via another mechanism (no error) or returns CLINotFoundError.
	path, err := GetCLIPath()
	if err != nil {
		var notFound *CLINotFoundError
		if !errors.As(err, &notFound) {
			t.Errorf("expected CLINotFoundError or nil, got %T: %v", err, err)
		}
		return
	}
	// If a path was returned it must not be the non-existent one.
	if path == "/does/not/exist/codebuddy-headless" {
		t.Error("GetCLIPath returned a path that does not exist on disk")
	}
}

// TestGetCLIPath_NoEnvFallsThrough verifies that when the env var is absent
// GetCLIPath returns either a valid binary path or a CLINotFoundError.
func TestGetCLIPath_NoEnvFallsThrough(t *testing.T) {
	os.Unsetenv("CODEBUDDY_CODE_PATH")

	_, err := GetCLIPath()
	if err != nil {
		var notFound *CLINotFoundError
		if !errors.As(err, &notFound) {
			t.Errorf("expected CLINotFoundError or nil, got %T: %v", err, err)
		}
	}
}

// TestTryCLIPath_ReturnsStringOnSuccess verifies that TryCLIPath returns the
// correct path (not empty) when a valid CLI binary can be located.
func TestTryCLIPath_ReturnsStringOnSuccess(t *testing.T) {
	tmp, err := os.CreateTemp("", "fake-codebuddy-cli-*")
	if err != nil {
		t.Fatalf("could not create temp file: %v", err)
	}
	defer os.Remove(tmp.Name())
	tmp.Close()

	t.Setenv("CODEBUDDY_CODE_PATH", tmp.Name())

	path := TryCLIPath()
	if path == "" {
		t.Error("TryCLIPath returned empty string; expected the temp file path")
	}
}

// TestTryCLIPath_ReturnsEmptyOnFailure verifies that TryCLIPath returns an
// empty string rather than propagating an error when no CLI is found.
func TestTryCLIPath_ReturnsEmptyOnFailure(t *testing.T) {
	// Point env var at a non-existent path so that all lookup strategies fail
	// (unless the host happens to have codebuddy-headless on PATH, which is
	// acceptable — in that case TryCLIPath may return a non-empty string and
	// we just skip the assertion).
	t.Setenv("CODEBUDDY_CODE_PATH", "/no/such/path/codebuddy-headless")

	// Temporarily shadow PATH so exec.LookPath won't find the real binary.
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	path := TryCLIPath()
	if path != "" {
		// A non-empty result is only valid if the binary actually exists.
		if _, err := os.Stat(path); err != nil {
			t.Errorf("TryCLIPath returned non-empty path %q that does not exist", path)
		}
	}
}

// TestGetCLIVersion_EmptyPath verifies that resolveCliVersion with an empty
// path returns the sentinel "unknown" string.
func TestGetCLIVersion_EmptyPath(t *testing.T) {
	got := resolveCliVersion("")
	if got != "unknown" {
		t.Errorf("resolveCliVersion(\"\") = %q, want \"unknown\"", got)
	}
}

// TestGetCLIVersion_NonExistentPath verifies that resolveCliVersion returns
// "unknown" when no metadata/package.json files can be found.
func TestGetCLIVersion_NonExistentPath(t *testing.T) {
	got := resolveCliVersion("/tmp/no-such-dir/bin/codebuddy-headless")
	if got != "unknown" {
		t.Errorf("resolveCliVersion with missing files = %q, want \"unknown\"", got)
	}
}

// TestGetCLIVersion_MetadataJSON verifies that resolveCliVersion reads the
// version from a metadata.json file placed in the expected directory layout.
func TestGetCLIVersion_MetadataJSON(t *testing.T) {
	// Layout:  <root>/bin/codebuddy-headless  (fake binary)
	//          <root>/metadata.json           (version source)
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(binDir, "codebuddy-headless")
	if err := os.WriteFile(fakeBin, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}

	meta := `{"tag": "codebuddy@1.2.3"}`
	if err := os.WriteFile(filepath.Join(root, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveCliVersion(fakeBin)
	if got != "1.2.3" {
		t.Errorf("resolveCliVersion = %q, want \"1.2.3\"", got)
	}
}

// TestGetCLIVersion_PackageJSON verifies that resolveCliVersion falls back to
// package.json when metadata.json is absent.
func TestGetCLIVersion_PackageJSON(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(binDir, "codebuddy-headless")
	if err := os.WriteFile(fakeBin, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}

	pkg := `{"version": "2.5.0"}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveCliVersion(fakeBin)
	if got != "2.5.0" {
		t.Errorf("resolveCliVersion = %q, want \"2.5.0\"", got)
	}
}
