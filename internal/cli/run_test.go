package cli

import (
	"context"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestWithMcpScopeOnlyAttachesToCoder(t *testing.T) {
	scope := &engine.McpScope{ConfigPath: "/tmp/mcp-config-claude.json", Allowed: []string{"claude-github.search_code"}}
	fake := &engine.FakeEngine{
		Name_: "claude",
		Script: []engine.InvokeResponse{
			{Text: "coded"},
			{Text: "reviewed"},
		},
	}
	wrapped := withMcpScope(fake, scope)

	if _, err := wrapped.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: "code"}); err != nil {
		t.Fatalf("coder Invoke: %v", err)
	}
	if _, err := wrapped.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: "review"}); err != nil {
		t.Fatalf("reviewer Invoke: %v", err)
	}
	if got := fake.Received[0].Mcp; got == nil || got.ConfigPath != scope.ConfigPath {
		t.Errorf("coder Mcp = %+v, want scope path %q", got, scope.ConfigPath)
	}
	if got := fake.Received[1].Mcp; got != nil {
		t.Errorf("reviewer Mcp = %+v, want nil", got)
	}
}
