package mcp

import (
	"os"
	"testing"
)

func TestParseClaudeMcpCalls(t *testing.T) {
	raw, err := os.ReadFile("../engine/testdata/claude-output-with-mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	calls, err := ParseClaudeMcpCalls(raw)
	if err != nil {
		t.Fatalf("ParseClaudeMcpCalls: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	c0 := calls[0]
	if c0.Server != "github" || c0.Tool != "search_code" {
		t.Errorf("call[0] = %s.%s, want github.search_code", c0.Server, c0.Tool)
	}
	if c0.DurationMs != 42 {
		t.Errorf("call[0].DurationMs = %d, want 42", c0.DurationMs)
	}
	if len(c0.ArgsJSON) == 0 {
		t.Errorf("call[0].ArgsJSON empty")
	}
	if calls[1].Server != "fs-readonly" || calls[1].Tool != "read_file" {
		t.Errorf("call[1] = %s.%s, want fs-readonly.read_file", calls[1].Server, calls[1].Tool)
	}
}

func TestParseCodexMcpCalls(t *testing.T) {
	raw, err := os.ReadFile("../engine/testdata/codex-output-with-mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	calls, err := ParseCodexMcpCalls(raw)
	if err != nil {
		t.Fatalf("ParseCodexMcpCalls: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Server != "github" || calls[0].Tool != "search_code" {
		t.Errorf("call[0] = %s.%s", calls[0].Server, calls[0].Tool)
	}
	if calls[0].DurationMs != 42 {
		t.Errorf("DurationMs = %d, want 42", calls[0].DurationMs)
	}
}

func TestParseClaudeMcpCallsEmpty(t *testing.T) {
	calls, err := ParseClaudeMcpCalls([]byte(`{"type":"result","result":"hi","usage":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Errorf("expected no calls, got %d", len(calls))
	}
}
