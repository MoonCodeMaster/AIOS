package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexMcpFlagWiring(t *testing.T) {
	args := buildCodexArgs(InvokeRequest{
		Prompt:  "do x",
		Workdir: "/tmp/wd",
		Mcp:     &McpScope{ConfigPath: "/tmp/scope.json"},
	}, []string{"--extra"})
	if !containsCodex(args, "--mcp-config") || !containsCodex(args, "/tmp/scope.json") {
		t.Errorf("expected --mcp-config /tmp/scope.json in args, got %v", args)
	}
}

func TestCodexNoMcpFlagWhenNil(t *testing.T) {
	args := buildCodexArgs(InvokeRequest{Prompt: "do x", Workdir: "/tmp/wd"}, nil)
	for _, a := range args {
		if a == "--mcp-config" {
			t.Fatalf("--mcp-config should not appear when Mcp is nil; got args %v", args)
		}
	}
}

func TestCodexParseMcpCallsFromOutput(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex-output-with-mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseCodexOutput(raw)
	if err != nil {
		t.Fatalf("parseCodexOutput: %v", err)
	}
	if len(resp.McpCalls) != 1 {
		t.Errorf("McpCalls = %d, want 1", len(resp.McpCalls))
	}
}

func TestParseCodexOutputNDJSON(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex-output-ndjson.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseCodexOutput(raw)
	if err != nil {
		t.Fatalf("parseCodexOutput: %v", err)
	}
	if resp.Text != "hello world" {
		t.Errorf("Text = %q, want \"hello world\"", resp.Text)
	}
	if resp.UsageTokens != 16 {
		t.Errorf("UsageTokens = %d, want 16", resp.UsageTokens)
	}
	if len(resp.McpCalls) != 1 {
		t.Errorf("McpCalls = %d, want 1", len(resp.McpCalls))
	}
	if len(resp.McpCalls) > 0 && resp.McpCalls[0].Tool != "search_code" {
		t.Errorf("McpCalls[0].Tool = %q, want search_code", resp.McpCalls[0].Tool)
	}
}

func TestCodexInvoke_RetriesTransientFailures(t *testing.T) {
	helper := buildFakeHelper(t)
	counterFile := filepath.Join(t.TempDir(), "counter")
	eng := &CodexEngine{
		Binary: helper,
		Retry:  RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: true},
	}
	t.Setenv("AIOS_FAKE_COUNTER", counterFile)
	t.Setenv("AIOS_FAKE_FAIL_TIMES", "2")
	t.Setenv("AIOS_FAKE_STDERR", "429 Too Many Requests")
	t.Setenv("AIOS_FAKE_STDOUT", `{"type":"final","text":"ok","usage":{"total_tokens":10}}`)

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

func TestCodexInvoke_PermanentErrorNoRetry(t *testing.T) {
	helper := buildFakeHelper(t)
	counterFile := filepath.Join(t.TempDir(), "counter")
	eng := &CodexEngine{
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

func containsCodex(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func TestParseCodexOutput(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "codex-output-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseCodexOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello world" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.UsageTokens != 16 {
		t.Errorf("UsageTokens = %d, want 16", resp.UsageTokens)
	}
}
