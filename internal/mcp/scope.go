package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// claudeMcpConfig is the on-disk shape Claude CLI expects via --mcp-config.
type claudeMcpConfig struct {
	MCPServers map[string]claudeServer `json:"mcpServers"`
}

type claudeServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// RenderScope builds an engine.McpScope for a task: it intersects the task's
// mcp_allow with the project's [mcp.servers], writes a temp Claude-format
// config file under outDir/mcp-config.json, and returns the scope.
//
// Returns (nil, nil) when the task opts out of MCP entirely (mcp_allow empty).
func RenderScope(servers map[string]config.MCPServer, tk *spec.Task, outDir string) (*engine.McpScope, error) {
	if len(tk.MCPAllow) == 0 {
		return nil, nil
	}
	cfg := claudeMcpConfig{MCPServers: map[string]claudeServer{}}
	var allowed []string
	for _, name := range tk.MCPAllow {
		srv, ok := servers[name]
		if !ok {
			return nil, fmt.Errorf("task %s mcp_allow references unknown server %q", tk.ID, name)
		}
		cfg.MCPServers[name] = claudeServer{
			Command: srv.Binary,
			Args:    srv.Args,
			Env:     srv.Env,
		}
		toolList := srv.AllowedTools
		if narrowed, ok := tk.MCPAllowTools[name]; ok && len(narrowed) > 0 {
			toolList = intersect(srv.AllowedTools, narrowed)
		}
		for _, tool := range toolList {
			allowed = append(allowed, name+"."+tool)
		}
	}
	sort.Strings(allowed)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("scope dir: %w", err)
	}
	p := filepath.Join(outDir, "mcp-config.json")
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		return nil, fmt.Errorf("write scope: %w", err)
	}
	return &engine.McpScope{ConfigPath: p, Allowed: allowed}, nil
}

func intersect(a, b []string) []string {
	bs := map[string]struct{}{}
	for _, x := range b {
		bs[x] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := bs[x]; ok {
			out = append(out, x)
		}
	}
	return out
}
