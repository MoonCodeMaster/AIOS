package engine

import (
	"os"
	"path/filepath"
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
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
