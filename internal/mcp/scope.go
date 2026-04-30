package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

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

// ScopeOptions controls how a task MCP scope is written for one engine
// invocation. Namespace, when non-empty, prefixes MCP server names in the
// rendered config and uses a distinct config filename so two engines can run
// concurrently without sharing server identifiers or config files.
type ScopeOptions struct {
	Namespace string
}

// RenderScope builds an engine.McpScope for a task: it intersects the task's
// mcp_allow with the project's [mcp.servers], writes a temp Claude-format
// config file under outDir/mcp-config.json, and returns the scope.
//
// Returns (nil, nil) when the task opts out of MCP entirely (mcp_allow empty).
func RenderScope(servers map[string]config.MCPServer, tk *spec.Task, outDir string) (*engine.McpScope, error) {
	return RenderScopeWithOptions(servers, tk, outDir, ScopeOptions{})
}

// RenderScopeForEngine writes a scope isolated for one engine's invocation.
// The task still opts into original server names (e.g. "github"), while the
// config given to the CLI contains namespaced server names (e.g.
// "claude-github"). The namespace avoids ambiguous MCP tool names when two
// engine processes inspect the same task at the same time.
func RenderScopeForEngine(servers map[string]config.MCPServer, tk *spec.Task, outDir, engineName string) (*engine.McpScope, error) {
	return RenderScopeWithOptions(servers, tk, outDir, ScopeOptions{Namespace: engineName})
}

func RenderScopeWithOptions(servers map[string]config.MCPServer, tk *spec.Task, outDir string, opts ScopeOptions) (*engine.McpScope, error) {
	if len(tk.MCPAllow) == 0 {
		return nil, nil
	}
	cfg := claudeMcpConfig{MCPServers: map[string]claudeServer{}}
	var allowed []string
	namespace := sanitizeNamespace(opts.Namespace)
	for _, name := range tk.MCPAllow {
		srv, ok := servers[name]
		if !ok {
			return nil, fmt.Errorf("task %s mcp_allow references unknown server %q", tk.ID, name)
		}
		renderedName := scopedServerName(namespace, name)
		cfg.MCPServers[renderedName] = claudeServer{
			Command: srv.Binary,
			Args:    srv.Args,
			Env:     srv.Env,
		}
		toolList := srv.AllowedTools
		if narrowed, ok := tk.MCPAllowTools[name]; ok && len(narrowed) > 0 {
			toolList = intersect(srv.AllowedTools, narrowed)
		}
		for _, tool := range toolList {
			allowed = append(allowed, renderedName+"."+tool)
		}
	}
	sort.Strings(allowed)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("scope dir: %w", err)
	}
	p := filepath.Join(outDir, scopeFilename(namespace))
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		return nil, fmt.Errorf("write scope: %w", err)
	}
	return &engine.McpScope{ConfigPath: p, Allowed: allowed}, nil
}

func scopedServerName(namespace, server string) string {
	if namespace == "" {
		return server
	}
	return namespace + "-" + server
}

func scopeFilename(namespace string) string {
	if namespace == "" {
		return "mcp-config.json"
	}
	return "mcp-config-" + namespace + ".json"
}

func sanitizeNamespace(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
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
