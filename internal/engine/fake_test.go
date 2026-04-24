package engine

import (
	"context"
	"testing"
)

func TestFakeEngine_ScriptedResponses(t *testing.T) {
	f := &FakeEngine{
		Name_: "claude",
		Script: []InvokeResponse{
			{Text: "first response", UsageTokens: 100, ExitCode: 0},
			{Text: "second response", UsageTokens: 120, ExitCode: 0},
		},
	}

	r1, err := f.Invoke(context.Background(), InvokeRequest{Role: RoleCoder, Prompt: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Text != "first response" {
		t.Errorf("r1 = %q", r1.Text)
	}

	r2, err := f.Invoke(context.Background(), InvokeRequest{Role: RoleReviewer, Prompt: "p2"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Text != "second response" {
		t.Errorf("r2 = %q", r2.Text)
	}

	if len(f.Received) != 2 {
		t.Errorf("Received len = %d", len(f.Received))
	}
	if f.Received[0].Role != RoleCoder || f.Received[1].Role != RoleReviewer {
		t.Errorf("roles not captured: %v / %v", f.Received[0].Role, f.Received[1].Role)
	}
}

func TestFakeEngine_UnexpectedCall(t *testing.T) {
	f := &FakeEngine{Name_: "claude", Script: []InvokeResponse{{Text: "only one"}}}
	_, err := f.Invoke(context.Background(), InvokeRequest{Role: RoleCoder, Prompt: "a"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Invoke(context.Background(), InvokeRequest{Role: RoleCoder, Prompt: "b"})
	if err == nil {
		t.Fatal("expected error on extra call")
	}
}

func TestFakeEngineRecordsMcpScope(t *testing.T) {
	scope := &McpScope{ConfigPath: "/tmp/x.json", Allowed: []string{"github.search_code"}}
	f := &FakeEngine{
		Name_: "fake",
		Script: []InvokeResponse{
			{Text: "ok", McpCalls: []McpCall{{Server: "github", Tool: "search_code", DurationMs: 12}}},
		},
	}
	resp, err := f.Invoke(context.Background(), InvokeRequest{Role: RoleCoder, Mcp: scope})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Received[0].Mcp; got == nil || got.ConfigPath != "/tmp/x.json" {
		t.Errorf("Received Mcp = %+v, want path /tmp/x.json", got)
	}
	if len(resp.McpCalls) != 1 || resp.McpCalls[0].Tool != "search_code" {
		t.Errorf("McpCalls = %+v, want one search_code", resp.McpCalls)
	}
}
