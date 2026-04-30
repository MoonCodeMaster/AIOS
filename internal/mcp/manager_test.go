package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// makeStubBinary writes a script to dir that just sleeps until killed.
// Returns the binary path.
func makeStubBinary(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "stub-mcp")
	body := "#!/bin/sh\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestManagerLazySpawn(t *testing.T) {
	dir := t.TempDir()
	bin := makeStubBinary(t, dir)
	servers := map[string]config.MCPServer{
		"stub": {Binary: bin, AllowedTools: []string{"noop"}},
	}
	mgr := NewManager(servers)
	defer mgr.Shutdown(context.Background())

	if mgr.RunningCount() != 0 {
		t.Errorf("expected 0 running before ScopeFor")
	}

	tk := &spec.Task{ID: "001", MCPAllow: []string{"stub"}}
	scope, err := mgr.ScopeFor(tk, dir)
	if err != nil {
		t.Fatalf("ScopeFor: %v", err)
	}
	if scope == nil || scope.ConfigPath == "" {
		t.Fatal("expected non-nil scope")
	}
	if mgr.RunningCount() != 1 {
		t.Errorf("expected 1 running, got %d", mgr.RunningCount())
	}

	// Idempotent: second call doesn't double-spawn.
	if _, err := mgr.ScopeFor(tk, dir); err != nil {
		t.Fatal(err)
	}
	if mgr.RunningCount() != 1 {
		t.Errorf("expected still 1 running, got %d", mgr.RunningCount())
	}
}

func TestManagerScopeForNoAllow(t *testing.T) {
	mgr := NewManager(map[string]config.MCPServer{})
	defer mgr.Shutdown(context.Background())
	tk := &spec.Task{ID: "002", MCPAllow: nil}
	scope, err := mgr.ScopeFor(tk, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if scope != nil {
		t.Errorf("expected nil scope when MCPAllow empty")
	}
	if mgr.RunningCount() != 0 {
		t.Errorf("no servers should have spawned")
	}
}

func TestManagerScopeForEngineWritesDistinctConfigs(t *testing.T) {
	dir := t.TempDir()
	bin := makeStubBinary(t, dir)
	servers := map[string]config.MCPServer{
		"stub": {Binary: bin, AllowedTools: []string{"noop"}},
	}
	mgr := NewManager(servers)
	defer mgr.Shutdown(context.Background())
	tk := &spec.Task{ID: "001", MCPAllow: []string{"stub"}}

	claudeScope, err := mgr.ScopeForEngine(tk, dir, "claude")
	if err != nil {
		t.Fatalf("ScopeForEngine claude: %v", err)
	}
	codexScope, err := mgr.ScopeForEngine(tk, dir, "codex")
	if err != nil {
		t.Fatalf("ScopeForEngine codex: %v", err)
	}
	if claudeScope.ConfigPath == codexScope.ConfigPath {
		t.Fatalf("ConfigPath should be distinct, got %q", claudeScope.ConfigPath)
	}
	if len(claudeScope.Allowed) != 1 || claudeScope.Allowed[0] != "claude-stub.noop" {
		t.Errorf("claude Allowed = %v, want [claude-stub.noop]", claudeScope.Allowed)
	}
	if len(codexScope.Allowed) != 1 || codexScope.Allowed[0] != "codex-stub.noop" {
		t.Errorf("codex Allowed = %v, want [codex-stub.noop]", codexScope.Allowed)
	}
	if mgr.RunningCount() != 1 {
		t.Errorf("engine-scoped configs should not double-spawn manager process, got %d", mgr.RunningCount())
	}
}

func TestManagerShutdownStopsServers(t *testing.T) {
	dir := t.TempDir()
	bin := makeStubBinary(t, dir)
	servers := map[string]config.MCPServer{
		"stub": {Binary: bin, AllowedTools: []string{"noop"}},
	}
	mgr := NewManager(servers)
	tk := &spec.Task{ID: "001", MCPAllow: []string{"stub"}}
	if _, err := mgr.ScopeFor(tk, dir); err != nil {
		t.Fatal(err)
	}
	if mgr.RunningCount() != 1 {
		t.Fatal("server did not spawn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := mgr.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	if mgr.RunningCount() != 0 {
		t.Errorf("expected 0 running after Shutdown, got %d", mgr.RunningCount())
	}
}
