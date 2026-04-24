package engine

import (
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
