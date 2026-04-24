package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Solaxis/aios/internal/engine"
)

// ParseClaudeMcpCalls extracts MCP tool calls from Claude CLI JSON output.
// Claude reports MCP tools as tool_calls with names "mcp__<server>__<tool>".
func ParseClaudeMcpCalls(raw []byte) ([]engine.McpCall, error) {
	var doc struct {
		ToolCalls []struct {
			Name       string          `json:"name"`
			Input      json.RawMessage `json:"input"`
			Output     json.RawMessage `json:"output"`
			DurationMs int             `json:"duration_ms"`
			Error      string          `json:"error"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse claude mcp calls: %w", err)
	}
	var out []engine.McpCall
	for _, tc := range doc.ToolCalls {
		if !strings.HasPrefix(tc.Name, "mcp__") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(tc.Name, "mcp__"), "__", 2)
		if len(parts) != 2 {
			continue
		}
		out = append(out, engine.McpCall{
			Server:     parts[0],
			Tool:       parts[1],
			ArgsJSON:   tc.Input,
			ResultJSON: tc.Output,
			DurationMs: tc.DurationMs,
			Error:      tc.Error,
		})
	}
	return out, nil
}

// ParseCodexMcpCalls extracts MCP tool calls from Codex CLI JSON output.
// Codex reports them under "mcp_calls" with explicit server+tool fields.
func ParseCodexMcpCalls(raw []byte) ([]engine.McpCall, error) {
	var doc struct {
		MCPCalls []struct {
			Server    string          `json:"server"`
			Tool      string          `json:"tool"`
			Args      json.RawMessage `json:"args"`
			Result    json.RawMessage `json:"result"`
			ElapsedMs int             `json:"elapsed_ms"`
			Error     string          `json:"error"`
		} `json:"mcp_calls"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse codex mcp calls: %w", err)
	}
	var out []engine.McpCall
	for _, mc := range doc.MCPCalls {
		out = append(out, engine.McpCall{
			Server:     mc.Server,
			Tool:       mc.Tool,
			ArgsJSON:   mc.Args,
			ResultJSON: mc.Result,
			DurationMs: mc.ElapsedMs,
			Error:      mc.Error,
		})
	}
	return out, nil
}
