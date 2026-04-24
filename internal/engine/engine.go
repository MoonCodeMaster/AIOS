package engine

import "context"

type Role string

const (
	RoleCoder    Role = "coder"
	RoleReviewer Role = "reviewer"
)

// Engine abstracts an AI code CLI (Claude, Codex, future MCP adapter).
type Engine interface {
	Name() string
	Invoke(ctx context.Context, req InvokeRequest) (*InvokeResponse, error)
}

type InvokeRequest struct {
	Role       Role
	Prompt     string
	Workdir    string
	TimeoutSec int
	MaxTokens  int
	Mcp        *McpScope // nil = no MCP for this call
}

type InvokeResponse struct {
	Raw         string     // full raw stdout (preserved for audit)
	Text        string     // extracted assistant message
	ToolCalls   []ToolCall // any file edits the CLI performed itself
	UsageTokens int
	ExitCode    int
	McpCalls    []McpCall
}

type ToolCall struct {
	Name string            // e.g. "edit", "run"
	Args map[string]string // engine-specific; preserved verbatim for audit
}

// McpScope is the per-task MCP exposure handed to an engine call.
type McpScope struct {
	ConfigPath string   // path to a temp MCP config file the engine should read
	Allowed    []string // "<server>.<tool>" entries the engine is allowed to call
}

// McpCall is one tool invocation the engine made via MCP during this call.
type McpCall struct {
	Server     string
	Tool       string
	ArgsJSON   []byte
	ResultJSON []byte
	DurationMs int
	Denied     bool
	Error      string
}
