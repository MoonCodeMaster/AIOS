package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestParseCodexOutputNDJSON_Large(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex-output-ndjson-large.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseCodexOutput(raw)
	if err != nil {
		t.Fatalf("parseCodexOutput: %v", err)
	}
	if !strings.Contains(resp.Text, "implementation") {
		t.Errorf("Text should contain 'implementation', got %q", resp.Text)
	}
	if resp.UsageTokens != 1280 {
		t.Errorf("UsageTokens = %d, want 1280", resp.UsageTokens)
	}
	if len(resp.McpCalls) != 2 {
		t.Errorf("McpCalls = %d, want 2", len(resp.McpCalls))
	}
	if len(resp.McpCalls) >= 2 {
		if resp.McpCalls[0].Server != "github" {
			t.Errorf("McpCalls[0].Server = %q, want github", resp.McpCalls[0].Server)
		}
		if resp.McpCalls[1].Server != "fs-readonly" {
			t.Errorf("McpCalls[1].Server = %q, want fs-readonly", resp.McpCalls[1].Server)
		}
	}
}

func TestParseCodexOutput_MultiMcp(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex-output-multi-mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseCodexOutput(raw)
	if err != nil {
		t.Fatalf("parseCodexOutput: %v", err)
	}
	if resp.Text != "I reviewed the PR and found 3 issues." {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.UsageTokens != 800 {
		t.Errorf("UsageTokens = %d, want 800", resp.UsageTokens)
	}
	if len(resp.McpCalls) != 3 {
		t.Fatalf("McpCalls = %d, want 3", len(resp.McpCalls))
	}
	if resp.McpCalls[0].Tool != "get_pr" {
		t.Errorf("McpCalls[0].Tool = %q, want get_pr", resp.McpCalls[0].Tool)
	}
	if resp.McpCalls[2].Server != "fs-readonly" {
		t.Errorf("McpCalls[2].Server = %q, want fs-readonly", resp.McpCalls[2].Server)
	}
}

func TestParseCodexOutput_EmptyResult(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex-output-empty-result.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseCodexOutput(raw)
	if err != nil {
		t.Fatalf("parseCodexOutput: %v", err)
	}
	if resp.Text != "" {
		t.Errorf("Text = %q, want empty", resp.Text)
	}
	if resp.UsageTokens != 4 {
		t.Errorf("UsageTokens = %d, want 4", resp.UsageTokens)
	}
}

func TestParseCodexOutput_TimeoutText(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex-output-timeout.txt")
	if err != nil {
		t.Fatal(err)
	}
	// {"type":"error",...} envelopes were previously swallowed as empty
	// success. They must surface as a real error so the retry layer can
	// classify timeouts/rate-limits.
	resp, err := parseCodexOutput(raw)
	if err == nil {
		t.Fatalf("expected error for type=error envelope, got resp=%+v", resp)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should contain upstream message; got: %v", err)
	}
	if !classifyErr(err) {
		t.Errorf("classifyErr should mark this transient (retriable); got false for %v", err)
	}
}

func TestParseCodexOutput_NDJSONError(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex-output-ndjson-error.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := parseCodexOutput(raw)
	if err == nil {
		t.Fatalf("expected error for NDJSON with error event, got resp=%+v", resp)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should propagate event content; got: %v", err)
	}
	if !classifyErr(err) {
		t.Errorf("classifyErr should mark NDJSON timeout transient; got false for %v", err)
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

func TestCodexInvoke_ContextCancellation(t *testing.T) {
	helper := buildFakeHelper(t)
	counterFile := filepath.Join(t.TempDir(), "counter")
	eng := &CodexEngine{
		Binary: helper,
		Retry:  RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: true},
	}
	t.Setenv("AIOS_FAKE_COUNTER", counterFile)
	t.Setenv("AIOS_FAKE_FAIL_TIMES", "0")
	t.Setenv("AIOS_FAKE_STDOUT", `{"type":"final","text":"ok","usage":{"total_tokens":5}}`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := eng.Invoke(ctx, InvokeRequest{Prompt: "test"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
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
