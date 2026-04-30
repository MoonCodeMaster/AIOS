package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

type CodexEngine struct {
	Binary     string
	ExtraArgs  []string
	TimeoutSec int
	Retry      RetryPolicy
}

func (c *CodexEngine) Name() string { return "codex" }

func (c *CodexEngine) Invoke(ctx context.Context, req InvokeRequest) (*InvokeResponse, error) {
	resp, attempts, err := WithRetry(ctx, c.Retry, func() (*InvokeResponse, error) {
		return c.invoke(ctx, req)
	})
	if resp != nil {
		resp.Attempts = attempts
	}
	return resp, err
}

func (c *CodexEngine) invoke(ctx context.Context, req InvokeRequest) (*InvokeResponse, error) {
	if c.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.TimeoutSec)*time.Second)
		defer cancel()
	}
	args := buildCodexArgs(req, c.ExtraArgs)
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	// Force-close inherited stdio after a short grace period when the
	// context cancels. Without this, descendants of a killed child can
	// keep the stdout pipe open and Wait() blocks until they exit on
	// their own — re-introducing the very hang we're fixing.
	cmd.WaitDelay = 500 * time.Millisecond
	// Run the engine in its own process group so a cancel reaps any MCP
	// servers or sub-tools the CLI spawned, not just the leader.
	setupProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	if req.Workdir != "" {
		cmd.Dir = req.Workdir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("codex timed out after %ds — check `aios doctor` and your codex auth", c.TimeoutSec)
		}
		combined := stderr.String()
		if combined == "" {
			combined = truncateBytes(stdout.Bytes(), 200)
		}
		return nil, fmt.Errorf("codex exec: %w (stderr: %s)", err, combined)
	}
	resp, err := parseCodexOutput(stdout.Bytes())
	if err != nil {
		out := stdout.String()
		if containsTimeout(out) {
			return nil, fmt.Errorf("codex output parse: timeout detected in output: %s", truncateBytes(stdout.Bytes(), 200))
		}
		return nil, err
	}
	resp.ExitCode = cmd.ProcessState.ExitCode()
	return resp, nil
}

func buildCodexArgs(req InvokeRequest, extra []string) []string {
	args := []string{"exec", "--json", "--skip-git-repo-check", req.Prompt}
	if req.Workdir != "" {
		args = append(args, "--cd", req.Workdir)
	}
	if req.Mcp != nil && req.Mcp.ConfigPath != "" {
		args = append(args, "--mcp-config", req.Mcp.ConfigPath)
	}
	args = append(args, extra...)
	return args
}

// codexSingleJSON is the post-T10 single-object format emitted by some Codex versions.
type codexSingleJSON struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content string `json:"content"` // populated for type="error"|"fatal"
	Usage   struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// codexEvent is one line of real Codex CLI NDJSON output.
type codexEvent struct {
	Type         string          `json:"type"`
	Content      string          `json:"content"`
	InputTokens  int             `json:"input_tokens"`
	OutputTokens int             `json:"output_tokens"`
	Server       string          `json:"server"`
	Tool         string          `json:"tool"`
	Args         json.RawMessage `json:"args"`
	Result       json.RawMessage `json:"result"`
	ElapsedMs    int             `json:"elapsed_ms"`
	Error        string          `json:"error"`
}

// isNDJSON reports whether raw contains multiple top-level JSON objects separated
// by newlines. We split on newlines and check that at least two non-empty lines
// each parse as valid JSON.
func isNDJSON(raw []byte) bool {
	lines := bytes.Split(raw, []byte("\n"))
	count := 0
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			return false
		}
		count++
		if count >= 2 {
			return true
		}
	}
	return false
}

func parseCodexOutput(raw []byte) (*InvokeResponse, error) {
	if isNDJSON(raw) {
		return parseCodexOutputNDJSON(raw)
	}
	return parseCodexOutputSingle(raw)
}

// parseCodexOutputNDJSON handles real Codex CLI NDJSON streaming output.
func parseCodexOutputNDJSON(raw []byte) (*InvokeResponse, error) {
	var text string
	var tokens int
	var mcpCalls []McpCall

	lines := bytes.Split(raw, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev codexEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("codex ndjson parse: %w", err)
		}
		switch ev.Type {
		case "response":
			text += ev.Content
		case "usage":
			tokens += ev.InputTokens + ev.OutputTokens
		case "mcp_call":
			mcpCalls = append(mcpCalls, McpCall{
				Server:     ev.Server,
				Tool:       ev.Tool,
				ArgsJSON:   ev.Args,
				ResultJSON: ev.Result,
				DurationMs: ev.ElapsedMs,
				Error:      ev.Error,
			})
		case "error", "fatal":
			// Surface as an error so the retry layer can classify it (a
			// codex-side timeout or rate-limit was previously swallowed as
			// empty success).
			msg := ev.Content
			if msg == "" {
				msg = ev.Error
			}
			return nil, fmt.Errorf("codex %s event: %s", ev.Type, msg)
		}
	}
	return &InvokeResponse{
		Raw:         string(raw),
		Text:        text,
		UsageTokens: tokens,
		McpCalls:    mcpCalls,
	}, nil
}

// parseCodexOutputSingle handles the post-T10 single-object JSON format.
func parseCodexOutputSingle(raw []byte) (*InvokeResponse, error) {
	var j codexSingleJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("codex output parse: %w", err)
	}
	if j.Type == "error" || j.Type == "fatal" {
		// A type=error envelope was previously swallowed as empty success.
		// Surface it so retry classification picks up timeouts/rate-limits.
		msg := j.Content
		if msg == "" {
			msg = j.Text
		}
		return nil, fmt.Errorf("codex %s event: %s", j.Type, msg)
	}
	return &InvokeResponse{
		Raw:         string(raw),
		Text:        j.Text,
		UsageTokens: j.Usage.TotalTokens,
		McpCalls:    parseCodexMcpCallsLocal(raw),
	}, nil
}

// parseCodexMcpCallsLocal mirrors internal/mcp/parse.go's ParseCodexMcpCalls
// to avoid an engine -> mcp -> engine import cycle.
// It handles both single-object and NDJSON formats.
func parseCodexMcpCallsLocal(raw []byte) []McpCall {
	if isNDJSON(raw) {
		return parseCodexMcpCallsNDJSON(raw)
	}
	return parseCodexMcpCallsSingle(raw)
}

// parseCodexMcpCallsSingle extracts MCP calls from a single-object JSON document.
func parseCodexMcpCallsSingle(raw []byte) []McpCall {
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
		return nil
	}
	out := make([]McpCall, 0, len(doc.MCPCalls))
	for _, mc := range doc.MCPCalls {
		out = append(out, McpCall{
			Server:     mc.Server,
			Tool:       mc.Tool,
			ArgsJSON:   mc.Args,
			ResultJSON: mc.Result,
			DurationMs: mc.ElapsedMs,
			Error:      mc.Error,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseCodexMcpCallsNDJSON extracts MCP calls from NDJSON streaming output.
func parseCodexMcpCallsNDJSON(raw []byte) []McpCall {
	var out []McpCall
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev codexEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type == "mcp_call" {
			out = append(out, McpCall{
				Server:     ev.Server,
				Tool:       ev.Tool,
				ArgsJSON:   ev.Args,
				ResultJSON: ev.Result,
				DurationMs: ev.ElapsedMs,
				Error:      ev.Error,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
