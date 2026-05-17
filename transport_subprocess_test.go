package codebuddy

import (
	"slices"
	"strings"
	"testing"
)

// newTransportForTest constructs a SubprocessTransport suitable for unit tests.
func newTransportForTest(opts *Options) *SubprocessTransport {
	return NewSubprocessTransport(opts, nil)
}

// ---------------------------------------------------------------------------
// buildArgs tests
// ---------------------------------------------------------------------------

func TestBuildArgs_Defaults(t *testing.T) {
	tr := newTransportForTest(&Options{})
	args := tr.buildArgs()

	// --output-format=stream-json is emitted as a single token.
	assertContainsArg(t, args, "--output-format=stream-json")

	// --setting-sources none (two separate tokens) is the SDK default.
	assertContainsArgs(t, args, "--setting-sources", "none")

	// --verbose must always be present.
	assertContainsArg(t, args, "--verbose")
}

func TestBuildArgs_InputFormat(t *testing.T) {
	tr := newTransportForTest(&Options{})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--input-format=stream-json")
}

func TestBuildArgs_Model(t *testing.T) {
	model := "claude-opus-4"
	tr := newTransportForTest(&Options{Model: &model})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--model", "claude-opus-4")
}

func TestBuildArgs_FallbackModel(t *testing.T) {
	fb := "claude-sonnet-3-5"
	tr := newTransportForTest(&Options{FallbackModel: &fb})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--fallback-model", "claude-sonnet-3-5")
}

func TestBuildArgs_MaxTurns(t *testing.T) {
	n := 5
	tr := newTransportForTest(&Options{MaxTurns: &n})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--max-turns", "5")
}

func TestBuildArgs_PermissionMode(t *testing.T) {
	tr := newTransportForTest(&Options{PermissionMode: PermissionModeBypassPermissions.Ptr()})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--permission-mode", "bypassPermissions")
}

func TestBuildArgs_PermissionModeDefault(t *testing.T) {
	tr := newTransportForTest(&Options{PermissionMode: PermissionModeDefault.Ptr()})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--permission-mode", "default")
}

func TestBuildArgs_AllowedTools(t *testing.T) {
	tr := newTransportForTest(&Options{AllowedTools: []string{"Read", "Glob"}})
	args := tr.buildArgs()
	// CLI receives --allowedTools Read Glob (separate tokens).
	combined := strings.Join(args, " ")
	if !strings.Contains(combined, "Read") || !strings.Contains(combined, "Glob") {
		t.Errorf("expected Read and Glob in args, got: %v", args)
	}
	assertContainsArg(t, args, "--allowedTools")
}

func TestBuildArgs_DisallowedTools(t *testing.T) {
	tr := newTransportForTest(&Options{DisallowedTools: []string{"Bash"}})
	args := tr.buildArgs()
	combined := strings.Join(args, " ")
	if !strings.Contains(combined, "Bash") {
		t.Errorf("expected Bash in args, got: %v", args)
	}
	assertContainsArg(t, args, "--disallowedTools")
}

func TestBuildArgs_SessionID(t *testing.T) {
	sid := "test-session-123"
	tr := newTransportForTest(&Options{SessionID: &sid})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--session-id", "test-session-123")
}

func TestBuildArgs_ContinueConversation(t *testing.T) {
	// ContinueConversation is a plain bool in Options, not a pointer.
	tr := newTransportForTest(&Options{ContinueConversation: true})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--continue")
}

func TestBuildArgs_ContinueConversationFalse(t *testing.T) {
	tr := newTransportForTest(&Options{ContinueConversation: false})
	args := tr.buildArgs()
	for _, a := range args {
		if a == "--continue" {
			t.Error("--continue should not be present when ContinueConversation is false")
		}
	}
}

func TestBuildArgs_Resume(t *testing.T) {
	cp := "checkpoint-abc"
	tr := newTransportForTest(&Options{Resume: &cp})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--resume", "checkpoint-abc")
}

func TestBuildArgs_ForkSession(t *testing.T) {
	tr := newTransportForTest(&Options{ForkSession: true})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--fork-session")
}

func TestBuildArgs_SettingSources_ExplicitUser(t *testing.T) {
	tr := newTransportForTest(&Options{SettingSources: []SettingSource{SettingSourceUser}})
	args := tr.buildArgs()
	// Should NOT contain "none".
	combined := strings.Join(args, " ")
	if strings.Contains(combined, "none") {
		t.Errorf("should not have 'none' when SettingSources is explicitly set, got: %v", args)
	}
	assertContainsArgs(t, args, "--setting-sources", "user")
}

func TestBuildArgs_SettingSources_Multiple(t *testing.T) {
	tr := newTransportForTest(&Options{
		SettingSources: []SettingSource{SettingSourceUser, SettingSourceProject},
	})
	args := tr.buildArgs()
	combined := strings.Join(args, " ")
	if !strings.Contains(combined, "user") || !strings.Contains(combined, "project") {
		t.Errorf("expected user and project in setting-sources, got: %v", args)
	}
}

func TestBuildArgs_SettingSources_EmptySlice(t *testing.T) {
	// An explicitly empty slice still means "none".
	tr := newTransportForTest(&Options{SettingSources: []SettingSource{}})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--setting-sources", "none")
}

func TestBuildArgs_Effort(t *testing.T) {
	e := EffortHigh
	tr := newTransportForTest(&Options{Effort: &e})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--effort", "high")
}

func TestBuildArgs_EffortLow(t *testing.T) {
	e := EffortLow
	tr := newTransportForTest(&Options{Effort: &e})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--effort", "low")
}

func TestBuildArgs_IncludePartialMessages(t *testing.T) {
	tr := newTransportForTest(&Options{IncludePartialMessages: true})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--include-partial-messages")
}

func TestBuildArgs_SystemPromptOverride(t *testing.T) {
	sp := "You are a helpful assistant."
	tr := newTransportForTest(&Options{SystemPrompt: &SystemPromptConfig{Override: &sp}})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--system-prompt", sp)
}

func TestBuildArgs_SystemPromptAppend(t *testing.T) {
	ap := "Always be concise."
	tr := newTransportForTest(&Options{SystemPrompt: &SystemPromptConfig{Append: &ap}})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--append-system-prompt", ap)
}

func TestBuildArgs_ThinkingAdaptive(t *testing.T) {
	tr := newTransportForTest(&Options{Thinking: &ThinkingConfig{Type: "adaptive"}})
	args := tr.buildArgs()
	// adaptive thinking injects --settings {"alwaysThinkingEnabled":true}
	combined := strings.Join(args, " ")
	if !strings.Contains(combined, "alwaysThinkingEnabled") {
		t.Errorf("expected alwaysThinkingEnabled in args for adaptive thinking, got: %v", args)
	}
}

func TestBuildArgs_ThinkingEnabled(t *testing.T) {
	budget := 1000
	tr := newTransportForTest(&Options{
		Thinking: &ThinkingConfig{Type: "enabled", BudgetTokens: &budget},
	})
	args := tr.buildArgs()
	combined := strings.Join(args, " ")
	if !strings.Contains(combined, "alwaysThinkingEnabled") {
		t.Errorf("expected alwaysThinkingEnabled in args for enabled thinking, got: %v", args)
	}
}

func TestBuildArgs_ExtraArgs_FlagOnly(t *testing.T) {
	tr := newTransportForTest(&Options{
		ExtraArgs: map[string]*string{"debug": nil},
	})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--debug")
}

func TestBuildArgs_ExtraArgs_FlagWithValue(t *testing.T) {
	v := "trace"
	tr := newTransportForTest(&Options{
		ExtraArgs: map[string]*string{"log-level": &v},
	})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--log-level", "trace")
}

// ---------------------------------------------------------------------------
// 新增功能：DangerouslySkipPermissions / AdditionalDirectories / Args
// ---------------------------------------------------------------------------

func TestBuildArgs_DangerouslySkipPermissions(t *testing.T) {
	tr := newTransportForTest(&Options{DangerouslySkipPermissions: true})
	args := tr.buildArgs()
	assertContainsArg(t, args, "--dangerously-skip-permissions")
}

func TestBuildArgs_DangerouslySkipPermissions_False(t *testing.T) {
	tr := newTransportForTest(&Options{DangerouslySkipPermissions: false})
	args := tr.buildArgs()
	if slices.Contains(args, "--dangerously-skip-permissions") {
		t.Error("--dangerously-skip-permissions should not appear when false")
	}
}

func TestBuildArgs_AdditionalDirectories(t *testing.T) {
	tr := newTransportForTest(&Options{AdditionalDirectories: []string{"/foo", "/bar"}})
	args := tr.buildArgs()
	assertContainsArgs(t, args, "--add-dir", "/foo")
	assertContainsArgs(t, args, "--add-dir", "/bar")
}

func TestBuildArgs_AdditionalDirectories_Empty(t *testing.T) {
	tr := newTransportForTest(&Options{AdditionalDirectories: nil})
	args := tr.buildArgs()
	if slices.Contains(args, "--add-dir") {
		t.Error("--add-dir should not appear when AdditionalDirectories is nil")
	}
}

func TestBuildArgs_Args_Passthrough(t *testing.T) {
	tr := newTransportForTest(&Options{Args: []string{"--foo", "bar", "--baz"}})
	args := tr.buildArgs()
	// Args should appear at the end
	n := len(args)
	if n < 3 {
		t.Fatalf("args too short: %v", args)
	}
	tail := args[n-3:]
	if tail[0] != "--foo" || tail[1] != "bar" || tail[2] != "--baz" {
		t.Errorf("expected tail [--foo bar --baz], got %v", tail)
	}
}

func TestBuildArgs_Args_AfterExtraArgs(t *testing.T) {
	flagVal := "v"
	tr := newTransportForTest(&Options{
		ExtraArgs: map[string]*string{"extra": &flagVal},
		Args:      []string{"--raw-last"},
	})
	args := tr.buildArgs()
	// --raw-last must come after --extra
	extraIdx, rawIdx := -1, -1
	for i, a := range args {
		if a == "--extra" {
			extraIdx = i
		}
		if a == "--raw-last" {
			rawIdx = i
		}
	}
	if extraIdx == -1 {
		t.Error("--extra not found")
	}
	if rawIdx == -1 {
		t.Error("--raw-last not found")
	}
	if rawIdx < extraIdx {
		t.Errorf("--raw-last (%d) should come after --extra (%d)", rawIdx, extraIdx)
	}
}

func TestBuildArgs_MCPServers_StdioConfig(t *testing.T) {
	tr := newTransportForTest(&Options{
		MCPServers: MCPServerMap{
			"my-server": McpStdioServerConfig{Command: "npx", Args: []string{"mcp-server"}},
		},
	})
	args := tr.buildArgs()
	found := false
	for i, a := range args {
		if a == "--mcp-config" && i+1 < len(args) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --mcp-config flag in args, got: %v", args)
	}
}

func TestBuildArgs_NoModel_NoModelFlag(t *testing.T) {
	tr := newTransportForTest(&Options{})
	args := tr.buildArgs()
	for _, a := range args {
		if a == "--model" {
			t.Error("--model flag should not be present when Model is nil")
		}
	}
}

// ---------------------------------------------------------------------------
// buildEnv tests
// ---------------------------------------------------------------------------

func TestBuildEnv_SDKEntrypoint(t *testing.T) {
	tr := newTransportForTest(&Options{})
	env := tr.buildEnv("")
	assertEnvContains(t, env, "CODEBUDDY_CODE_ENTRYPOINT=sdk-go")
}

func TestBuildEnv_DisableAutoupdater(t *testing.T) {
	tr := newTransportForTest(&Options{})
	env := tr.buildEnv("")
	assertEnvContains(t, env, "DISABLE_AUTOUPDATER=1")
}

func TestBuildEnv_DisableAutoMemory(t *testing.T) {
	tr := newTransportForTest(&Options{})
	env := tr.buildEnv("")
	assertEnvContains(t, env, "CODEBUDDY_DISABLE_AUTO_MEMORY=1")
}

func TestBuildEnv_CustomUserEnv(t *testing.T) {
	tr := newTransportForTest(&Options{
		Env: map[string]string{"MY_CUSTOM_VAR": "hello"},
	})
	env := tr.buildEnv("")
	assertEnvContains(t, env, "MY_CUSTOM_VAR=hello")
}

func TestBuildEnv_CustomUserEnvOverridesDefault(t *testing.T) {
	// User can override CODEBUDDY_DISABLE_AUTO_MEMORY via Options.Env.
	tr := newTransportForTest(&Options{
		Env: map[string]string{"CODEBUDDY_DISABLE_AUTO_MEMORY": "0"},
	})
	env := tr.buildEnv("")
	assertEnvContains(t, env, "CODEBUDDY_DISABLE_AUTO_MEMORY=0")
}

func TestBuildEnv_MaxThinkingTokens(t *testing.T) {
	budget := 8000
	tr := newTransportForTest(&Options{
		Thinking: &ThinkingConfig{Type: "enabled", BudgetTokens: &budget},
	})
	env := tr.buildEnv("")
	assertEnvContains(t, env, "MAX_THINKING_TOKENS=8000")
}

func TestBuildEnv_MaxThinkingTokensDeprecated(t *testing.T) {
	n := 4096
	tr := newTransportForTest(&Options{MaxThinkingTokens: &n})
	env := tr.buildEnv("")
	assertEnvContains(t, env, "MAX_THINKING_TOKENS=4096")
}

func TestBuildEnv_CustomHeadersUserAgentPresent(t *testing.T) {
	tr := newTransportForTest(&Options{})
	env := tr.buildEnv("")
	var found bool
	for _, e := range env {
		if strings.HasPrefix(e, "CODEBUDDY_CUSTOM_HEADERS=") && strings.Contains(e, "CodeBuddy Agent SDK") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CODEBUDDY_CUSTOM_HEADERS with SDK User-Agent not found in env: %v", env)
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// assertContainsArgs checks that args contains the pair [flag, value] as
// consecutive elements.
func assertContainsArgs(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Errorf("args %v does not contain %q %q", args, flag, value)
}

// assertContainsArg checks that args contains the given flag as a standalone
// element (no associated value).
func assertContainsArg(t *testing.T, args []string, flag string) {
	t.Helper()
	if slices.Contains(args, flag) {
		return
	}
	t.Errorf("args %v does not contain %q", args, flag)
}

// assertEnvContains checks that the env slice contains the given KEY=VALUE
// entry.
func assertEnvContains(t *testing.T, env []string, entry string) {
	t.Helper()
	if slices.Contains(env, entry) {
		return
	}
	t.Errorf("env does not contain %q; env = %v", entry, env)
}

// TestInjectSetting_NewSettings tests that injectSetting appends --settings when none exist.
func TestInjectSetting_NewSettings(t *testing.T) {
	args := []string{"--output-format", "json"}
	result := injectSetting(args, "myKey", "myValue")
	if !slices.Contains(result, "--settings") {
		t.Error("expected --settings flag to be added")
	}
	idx := slices.Index(result, "--settings")
	if idx < 0 || idx+1 >= len(result) {
		t.Fatal("--settings has no value")
	}
	if !strings.Contains(result[idx+1], "myKey") {
		t.Errorf("settings value %q does not contain myKey", result[idx+1])
	}
}

// TestInjectSetting_MergeIntoExisting tests that injectSetting merges into existing --settings.
func TestInjectSetting_MergeIntoExisting(t *testing.T) {
	args := []string{"--settings", `{"existingKey":"existingValue"}`}
	result := injectSetting(args, "newKey", "newValue")
	idx := slices.Index(result, "--settings")
	if idx < 0 || idx+1 >= len(result) {
		t.Fatal("--settings not found")
	}
	val := result[idx+1]
	if !strings.Contains(val, "existingKey") {
		t.Errorf("merged settings %q missing existingKey", val)
	}
	if !strings.Contains(val, "newKey") {
		t.Errorf("merged settings %q missing newKey", val)
	}
}

// TestExtractSDKMCPServers_NilOptions tests extractSDKMCPServers with nil MCPServers.
func TestExtractSDKMCPServers_NilOptions(t *testing.T) {
	opts := &Options{}
	names, servers := extractSDKMCPServers(opts)
	if len(names) != 0 {
		t.Errorf("expected no names, got %v", names)
	}
	if len(servers) != 0 {
		t.Errorf("expected no servers, got %v", servers)
	}
}

// TestExtractSDKMCPServers_EmptyMap tests extractSDKMCPServers with empty MCPServers.
func TestExtractSDKMCPServers_NonMapType(t *testing.T) {
	opts := &Options{MCPServers: MCPServerMap{}}
	names, servers := extractSDKMCPServers(opts)
	if len(names) != 0 {
		t.Errorf("expected no names, got %v", names)
	}
	if len(servers) != 0 {
		t.Errorf("expected no servers, got %v", servers)
	}
}

// TestExtractSDKMCPServers_WithSDKServer tests extractSDKMCPServers with SDK server config.
func TestExtractSDKMCPServers_WithSDKServer(t *testing.T) {
	sdkServer, sdkConfig := CreateSDKMCPServer("test-server", "1.0.0", nil)
	opts := &Options{
		MCPServers: MCPServerMap{
			"my-sdk": sdkConfig,
			"other":  McpStdioServerConfig{Command: "echo"},
		},
	}
	names, servers := extractSDKMCPServers(opts)
	if len(names) != 1 || names[0] != "my-sdk" {
		t.Errorf("names: got %v, want [my-sdk]", names)
	}
	if servers["my-sdk"] != sdkServer {
		t.Errorf("server: got %v, want %v", servers["my-sdk"], sdkServer)
	}
}

// TestNewSubprocessTransport_WithSDKMCP tests that SDK MCP servers are extracted.
func TestNewSubprocessTransport_WithSDKMCP(t *testing.T) {
	sdkServer, sdkConfig := CreateSDKMCPServer("helper", "0.1.0", nil)
	_ = sdkServer
	opts := &Options{
		MCPServers: MCPServerMap{
			"helper": sdkConfig,
		},
	}
	tr := NewSubprocessTransport(opts, nil)
	names := tr.SDKMCPServerNames()
	if !slices.Contains(names, "helper") {
		t.Errorf("expected 'helper' in SDKMCPServerNames, got %v", names)
	}
}
