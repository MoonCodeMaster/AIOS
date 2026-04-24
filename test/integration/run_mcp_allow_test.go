package integration

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Solaxis/aios/internal/config"
	"github.com/Solaxis/aios/internal/mcp"
	"github.com/Solaxis/aios/internal/spec"
)

// trueBin returns the absolute path to the system "true" binary.
// On macOS it lives at /usr/bin/true; on Linux typically /bin/true.
func trueBin(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("system 'true' binary not found: %v", err)
	}
	return p
}

func TestMCPAllowlistRendersScope(t *testing.T) {
	bin := trueBin(t)
	servers := map[string]config.MCPServer{
		"github": {Binary: bin, AllowedTools: []string{"search_code", "get_pr"}},
		"fs":     {Binary: bin, AllowedTools: []string{"read_file"}},
	}
	tk := &spec.Task{
		ID:       "001",
		MCPAllow: []string{"github"},
		MCPAllowTools: map[string][]string{
			"github": {"search_code"},
		},
	}
	mgr := mcp.NewManager(servers)
	defer mgr.Shutdown(context.Background())
	scope, err := mgr.ScopeFor(tk, t.TempDir())
	if err != nil {
		t.Fatalf("ScopeFor: %v", err)
	}
	if scope == nil {
		t.Fatal("scope nil")
	}
	if len(scope.Allowed) != 1 || scope.Allowed[0] != "github.search_code" {
		t.Errorf("Allowed = %v, want [github.search_code]", scope.Allowed)
	}
	if filepath.Base(scope.ConfigPath) != "mcp-config.json" {
		t.Errorf("ConfigPath base = %q, want mcp-config.json", filepath.Base(scope.ConfigPath))
	}
	// fs server must NOT have been launched (not in mcp_allow).
	if mgr.RunningCount() != 1 {
		t.Errorf("RunningCount = %d, want 1 (only github)", mgr.RunningCount())
	}
}

func TestMCPAllowlistEmpty(t *testing.T) {
	servers := map[string]config.MCPServer{
		"github": {Binary: trueBin(t), AllowedTools: []string{"search_code"}},
	}
	tk := &spec.Task{ID: "002", MCPAllow: nil} // opt out
	mgr := mcp.NewManager(servers)
	defer mgr.Shutdown(context.Background())
	scope, err := mgr.ScopeFor(tk, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if scope != nil {
		t.Errorf("scope = %+v, want nil (no opt-in)", scope)
	}
	if mgr.RunningCount() != 0 {
		t.Errorf("no server should have spawned")
	}
}
