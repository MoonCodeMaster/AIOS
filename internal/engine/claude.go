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

type ClaudeEngine struct {
	Binary     string
	ExtraArgs  []string
	TimeoutSec int
	Retry      RetryPolicy
}

func (c *ClaudeEngine) Name() string { return "claude" }

func (c *ClaudeEngine) Invoke(ctx context.Context, req InvokeRequest) (*InvokeResponse, error) {
	resp, attempts, err := WithRetry(ctx, c.Retry, func() (*InvokeResponse, error) {
		return c.invoke(ctx, req)
	})
	if resp != nil {
		resp.Attempts = attempts
	}
	return resp, err
}

func (c *ClaudeEngine) invoke(ctx context.Context, req InvokeRequest) (*InvokeResponse, error) {
	if c.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.TimeoutSec)*time.Second)
		defer cancel()
	}
	args := buildClaudeArgs(req, c.ExtraArgs)
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
			return nil, fmt.Errorf("claude timed out after %ds — check `aios doctor` and your auth (ANTHROPIC_AUTH_TOKEN / login)", c.TimeoutSec)
		}
		// Include both stderr and stdout snippet in the error so that
		// classifyErr can detect timeout messages the CLI wrote to stdout
		// (e.g. "request has timed out") before exiting non-zero.
		return nil, fmt.Errorf("claude exec: %w (%s)", err, execOutputDetail(stdout.Bytes(), stderr.Bytes()))
	}
	resp, err := parseClaudeOutput(stdout.Bytes())
	if err != nil {
		// If stdout contains a timeout indicator but isn't valid JSON, surface
		// the timeout so classifyErr can mark it transient and trigger a retry.
		out := stdout.String()
		if containsTimeout(out) {
			return nil, fmt.Errorf("claude output parse: timeout detected in output: %s", truncateBytes(stdout.Bytes(), 200))
		}
		return nil, err
	}
	resp.ExitCode = cmd.ProcessState.ExitCode()
	return resp, nil
}

// containsTimeout checks if the output contains timeout-related messages
// from the claude CLI (e.g. "request has timed out", "timed out").
func containsTimeout(s string) bool {
	lower := bytes.ToLower([]byte(s))
	return bytes.Contains(lower, []byte("timed out")) ||
		bytes.Contains(lower, []byte("timeout")) ||
		bytes.Contains(lower, []byte("request has timed out"))
}

// truncateBytes returns the first n bytes of b as a string, appending "…"
// if truncated.
func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

func execOutputDetail(stdout, stderr []byte) string {
	stderrText := truncateBytes(stderr, 600)
	stdoutText := truncateBytes(stdout, 600)
	if stderrText == "" && stdoutText == "" {
		return "stderr: " + stdoutText
	}
	if stderrText == "" {
		return "stdout: " + stdoutText
	}
	if stdoutText == "" {
		return "stderr: " + stderrText
	}
	return "stderr: " + stderrText + "; stdout: " + stdoutText
}

func buildClaudeArgs(req InvokeRequest, extra []string) []string {
	args := []string{
		"-p", req.Prompt,
		"--output-format", "json",
		"--permission-mode", "bypassPermissions",
	}
	if req.Mcp != nil && req.Mcp.ConfigPath != "" {
		args = append(args, "--mcp-config", req.Mcp.ConfigPath)
	}
	args = append(args, extra...)
	return args
}

type claudeJSON struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Result  string `json:"result"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func parseClaudeOutput(raw []byte) (*InvokeResponse, error) {
	var j claudeJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("claude output parse: %w", err)
	}
	return &InvokeResponse{
		Raw:         string(raw),
		Text:        j.Result,
		UsageTokens: j.Usage.InputTokens + j.Usage.OutputTokens,
		McpCalls:    parseClaudeMcpCallsLocal(raw),
	}, nil
}

// parseClaudeMcpCallsLocal is duplicated from internal/mcp/parse.go to avoid
// an engine -> mcp -> engine import cycle. Keep the two implementations in
// sync if the JSON shape changes.
func parseClaudeMcpCallsLocal(raw []byte) []McpCall {
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
		return nil
	}
	var out []McpCall
	for _, tc := range doc.ToolCalls {
		if len(tc.Name) < 5 || tc.Name[:5] != "mcp__" {
			continue
		}
		rest := tc.Name[5:]
		idx := -1
		for i := 0; i+2 <= len(rest); i++ {
			if rest[i] == '_' && rest[i+1] == '_' {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		out = append(out, McpCall{
			Server:     rest[:idx],
			Tool:       rest[idx+2:],
			ArgsJSON:   tc.Input,
			ResultJSON: tc.Output,
			DurationMs: tc.DurationMs,
			Error:      tc.Error,
		})
	}
	return out
}
