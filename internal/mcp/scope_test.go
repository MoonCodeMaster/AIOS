package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

func TestScopeAllowedSubset(t *testing.T) {
	servers := map[string]config.MCPServer{
		"github":      {Binary: "gh", AllowedTools: []string{"search_code", "get_pr", "list_issues"}},
		"fs-readonly": {Binary: "fs", AllowedTools: []string{"read_file"}},
	}
	tk := &spec.Task{
		ID:       "001-x",
		MCPAllow: []string{"github"},
		MCPAllowTools: map[string][]string{
			"github": {"search_code"},
		},
	}
	dir := t.TempDir()
	scope, err := RenderScope(servers, tk, dir)
	if err != nil {
		t.Fatalf("RenderScope: %v", err)
	}
	want := []string{"github.search_code"}
	if got := scope.Allowed; !equalStringSlice(got, want) {
		t.Errorf("Allowed = %v, want %v", got, want)
	}
	if scope.ConfigPath == "" {
		t.Fatal("ConfigPath empty")
	}
	raw, err := os.ReadFile(scope.ConfigPath)
	if err != nil {
		t.Fatalf("read scope file: %v", err)
	}
	var written struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatalf("unmarshal scope file: %v", err)
	}
	if _, ok := written.MCPServers["github"]; !ok {
		t.Errorf("github not in scope file: %s", string(raw))
	}
	if _, ok := written.MCPServers["fs-readonly"]; ok {
		t.Errorf("fs-readonly should NOT be in scope file (not allowed)")
	}
}

func TestScopeNoAllow(t *testing.T) {
	tk := &spec.Task{ID: "002-no", MCPAllow: nil}
	scope, err := RenderScope(map[string]config.MCPServer{}, tk, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if scope != nil {
		t.Errorf("expected nil scope when MCPAllow empty, got %+v", scope)
	}
}

func TestScopeAllowAllToolsByDefault(t *testing.T) {
	servers := map[string]config.MCPServer{
		"fs": {Binary: "fs", AllowedTools: []string{"read_file", "list_dir"}},
	}
	tk := &spec.Task{ID: "003-all", MCPAllow: []string{"fs"}}
	scope, err := RenderScope(servers, tk, t.TempDir())
	if err != nil {
		t.Fatalf("RenderScope: %v", err)
	}
	want := []string{"fs.list_dir", "fs.read_file"}
	got := append([]string(nil), scope.Allowed...)
	sort.Strings(got)
	if !equalStringSlice(got, want) {
		t.Errorf("Allowed = %v, want %v (all tools)", got, want)
	}
}

func TestScopeForEngineNamespacesServerNames(t *testing.T) {
	servers := map[string]config.MCPServer{
		"github": {Binary: "gh", AllowedTools: []string{"search_code"}},
	}
	tk := &spec.Task{ID: "005", MCPAllow: []string{"github"}}
	scope, err := RenderScopeForEngine(servers, tk, t.TempDir(), "claude")
	if err != nil {
		t.Fatalf("RenderScopeForEngine: %v", err)
	}
	if filepath.Base(scope.ConfigPath) != "mcp-config-claude.json" {
		t.Errorf("ConfigPath base = %q, want mcp-config-claude.json", filepath.Base(scope.ConfigPath))
	}
	if got, want := scope.Allowed, []string{"claude-github.search_code"}; !equalStringSlice(got, want) {
		t.Errorf("Allowed = %v, want %v", got, want)
	}
	raw, err := os.ReadFile(scope.ConfigPath)
	if err != nil {
		t.Fatalf("read scope file: %v", err)
	}
	var written struct {
		MCPServers map[string]struct{} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatalf("unmarshal scope file: %v", err)
	}
	if _, ok := written.MCPServers["claude-github"]; !ok {
		t.Errorf("namespaced github server missing from scope file: %s", string(raw))
	}
	if _, ok := written.MCPServers["github"]; ok {
		t.Errorf("unscoped github server should not be rendered in engine scope: %s", string(raw))
	}
}

func TestScopeUnknownServerRejected(t *testing.T) {
	tk := &spec.Task{ID: "004", MCPAllow: []string{"missing"}}
	if _, err := RenderScope(map[string]config.MCPServer{}, tk, t.TempDir()); err == nil {
		t.Fatal("expected error for unknown MCP server in mcp_allow")
	}
}

// helpers
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ = filepath.Join // keep import
