package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClaudeOutput(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-output-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseClaudeOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.UsageTokens != 13 {
		t.Errorf("UsageTokens = %d, want 13", resp.UsageTokens)
	}
	if resp.Raw == "" {
		t.Error("Raw should be preserved")
	}
}

func TestParseClaudeOutput_Large(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-output-large.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseClaudeOutput(raw)
	if err != nil {
		t.Fatalf("parseClaudeOutput: %v", err)
	}
	if resp.UsageTokens != 2560 {
		t.Errorf("UsageTokens = %d, want 2560", resp.UsageTokens)
	}
	if !strings.Contains(resp.Text, "rate limiter") {
		t.Errorf("Text should mention rate limiter, got %q", resp.Text[:50])
	}
	if len(resp.McpCalls) != 1 {
		t.Errorf("McpCalls = %d, want 1 (only mcp__ prefixed tool_calls)", len(resp.McpCalls))
	}
}

func TestParseClaudeOutput_EmptyResult(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-output-empty-result.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseClaudeOutput(raw)
	if err != nil {
		t.Fatalf("parseClaudeOutput: %v", err)
	}
	if resp.Text != "" {
		t.Errorf("Text = %q, want empty", resp.Text)
	}
	if resp.UsageTokens != 5 {
		t.Errorf("UsageTokens = %d, want 5", resp.UsageTokens)
	}
}

func TestParseClaudeOutput_TimeoutText(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-output-timeout.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseClaudeOutput(raw)
	if err == nil {
		t.Fatal("expected parse error for timeout text")
	}
	if !strings.Contains(err.Error(), "output parse") {
		t.Errorf("error should mention output parse, got: %v", err)
	}
}

func TestClaudeContainsTimeout(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"request has timed out after 120 seconds", true},
		{"Connection timeout", true},
		{"Timed Out waiting for response", true},
		{"normal response text", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := containsTimeout(tt.input); got != tt.want {
			t.Errorf("containsTimeout(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestClaudeMcpFlagWiring(t *testing.T) {
	args := buildClaudeArgs(InvokeRequest{
		Prompt: "do x",
		Mcp:    &McpScope{ConfigPath: "/tmp/scope.json"},
	}, []string{"--extra"})
	if !contains(args, "--mcp-config") || !contains(args, "/tmp/scope.json") {
		t.Errorf("expected --mcp-config /tmp/scope.json in args, got %v", args)
	}
}

func TestClaudeNoMcpFlagWhenNil(t *testing.T) {
	args := buildClaudeArgs(InvokeRequest{Prompt: "do x"}, nil)
	for _, a := range args {
		if a == "--mcp-config" {
			t.Fatalf("--mcp-config should not appear when Mcp is nil; got args %v", args)
		}
	}
}

func TestClaudeParseMcpCallsFromOutput(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-output-with-mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseClaudeOutput(raw)
	if err != nil {
		t.Fatalf("parseClaudeOutput: %v", err)
	}
	if len(resp.McpCalls) != 2 {
		t.Errorf("McpCalls = %d, want 2", len(resp.McpCalls))
	}
	if resp.McpCalls[0].Server != "github" {
		t.Errorf("McpCalls[0].Server = %q, want github", resp.McpCalls[0].Server)
	}
	if resp.McpCalls[0].Tool != "search_code" {
		t.Errorf("McpCalls[0].Tool = %q, want search_code", resp.McpCalls[0].Tool)
	}
	if resp.McpCalls[1].Server != "fs-readonly" {
		t.Errorf("McpCalls[1].Server = %q, want fs-readonly", resp.McpCalls[1].Server)
	}
}

func TestClaudeInvoke_RetriesTransientFailures(t *testing.T) {
	helper := buildFakeHelper(t)
	counterFile := filepath.Join(t.TempDir(), "counter")
	eng := &ClaudeEngine{
		Binary: helper,
		Retry:  RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: true},
	}
	t.Setenv("AIOS_FAKE_COUNTER", counterFile)
	t.Setenv("AIOS_FAKE_FAIL_TIMES", "2")
	t.Setenv("AIOS_FAKE_STDERR", "429 Too Many Requests")
	t.Setenv("AIOS_FAKE_STDOUT", `{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":5,"output_tokens":3}}`)

	resp, err := eng.Invoke(context.Background(), InvokeRequest{Prompt: "test"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if resp == nil {
		t.Fatal("resp is nil")
	}
	if resp.Text != "ok" {
		t.Errorf("resp.Text = %q, want ok", resp.Text)
	}
	if len(resp.Attempts) != 2 {
		t.Fatalf("Attempts = %d, want 2", len(resp.Attempts))
	}
	for i, a := range resp.Attempts {
		if a.Error == "" {
			t.Errorf("Attempts[%d].Error is empty", i)
		}
		if a.DurationMs < 0 {
			t.Errorf("Attempts[%d].DurationMs = %d, want >= 0", i, a.DurationMs)
		}
	}
}

func TestClaudeInvoke_PermanentErrorNoRetry(t *testing.T) {
	helper := buildFakeHelper(t)
	counterFile := filepath.Join(t.TempDir(), "counter")
	eng := &ClaudeEngine{
		Binary: helper,
		Retry:  RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: true},
	}
	t.Setenv("AIOS_FAKE_COUNTER", counterFile)
	t.Setenv("AIOS_FAKE_FAIL_TIMES", "99")
	t.Setenv("AIOS_FAKE_STDERR", "auth token expired")
	t.Setenv("AIOS_FAKE_STDOUT", "")

	resp, err := eng.Invoke(context.Background(), InvokeRequest{Prompt: "test"})
	if err == nil {
		t.Fatal("expected error for permanent failure")
	}
	if resp != nil {
		t.Errorf("resp should be nil on permanent error, got %+v", resp)
	}
	count := readCounter(t, counterFile)
	if count != 1 {
		t.Errorf("exec called %d times, want 1", count)
	}
}

func TestClaudeInvoke_ContextCancellation(t *testing.T) {
	helper := buildFakeHelper(t)
	counterFile := filepath.Join(t.TempDir(), "counter")
	eng := &ClaudeEngine{
		Binary: helper,
		Retry:  RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: true},
	}
	t.Setenv("AIOS_FAKE_COUNTER", counterFile)
	t.Setenv("AIOS_FAKE_FAIL_TIMES", "0")
	t.Setenv("AIOS_FAKE_STDOUT", `{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":1,"output_tokens":1}}`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := eng.Invoke(ctx, InvokeRequest{Prompt: "test"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
