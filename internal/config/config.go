package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/BurntSushi/toml"
)

const CurrentSchemaVersion = 1

type Config struct {
	SchemaVersion int      `toml:"schema_version"`
	Project       Project  `toml:"project"`
	Engines       Engines  `toml:"engines"`
	Budget        Budget   `toml:"budget"`
	Verify        Verify   `toml:"verify"`
	Runtime       Runtime  `toml:"runtime"`
	Parallel      Parallel `toml:"parallel"`
	MCP           MCP      `toml:"mcp"`
}

type MCP struct {
	Servers map[string]MCPServer `toml:"servers"`
}

type MCPServer struct {
	Binary       string            `toml:"binary"`
	Args         []string          `toml:"args"`
	Env          map[string]string `toml:"env"`
	AllowedTools []string          `toml:"allowed_tools"`
}

type Project struct {
	Name          string `toml:"name"`
	BaseBranch    string `toml:"base_branch"`
	StagingBranch string `toml:"staging_branch"`
}

type Engines struct {
	CoderDefault    string            `toml:"coder_default"`
	ReviewerDefault string            `toml:"reviewer_default"`
	RolesByKind     map[string]string `toml:"roles_by_kind"`
	Claude          EngineBinary      `toml:"claude"`
	Codex           EngineBinary      `toml:"codex"`
}

type EngineBinary struct {
	Binary           string   `toml:"binary"`
	ExtraArgs        []string `toml:"extra_args"`
	TimeoutSec       int      `toml:"timeout_sec"`
	RetryMaxAttempts int      `toml:"retry_max_attempts"`
	RetryBaseMs      int      `toml:"retry_base_ms"`
	RetryEnabled     *bool    `toml:"retry_enabled"`
}

type Budget struct {
	MaxRoundsPerTask      int `toml:"max_rounds_per_task"`
	MaxTokensPerTask      int `toml:"max_tokens_per_task"`
	MaxWallMinutesPerTask int `toml:"max_wall_minutes_per_task"`
	// StallThreshold is the number of consecutive review rounds with an
	// identical issue fingerprint that triggers stall detection. Default 3.
	// Lower values converge faster on hopeless tasks at the cost of ending
	// borderline cases that might have resolved in one more round.
	StallThreshold int `toml:"stall_threshold"`
	// MaxEscalations is the number of "hard-constraint retry" rounds the
	// orchestrator will run after stall detection fires, before blocking
	// with stall_no_progress. Each escalation re-runs the coder with a
	// prompt that surfaces the reviewer's outstanding issues as hard
	// constraints. Default 1 when unset. Set to 0 in config to disable
	// escalation entirely (strict pre-P0 behavior). Pointer is used so
	// "0" (disable) is distinguishable from "unset" (→ default 1).
	MaxEscalations *int `toml:"max_escalations"`
	// MaxDecomposeDepth is the maximum recursion depth for auto-decompose.
	// 0 = use default (2). Hard-capped at 3 in code regardless of config —
	// runaway decomposition is almost always a sign of a spec problem the
	// model can't solve.
	MaxDecomposeDepth int `toml:"max_decompose_depth"`
}

type Verify struct {
	TestCmd      string          `toml:"test_cmd"`
	LintCmd      string          `toml:"lint_cmd"`
	TypecheckCmd string          `toml:"typecheck_cmd"`
	BuildCmd     string          `toml:"build_cmd"`
	Skipped      map[string]bool `toml:"skipped"`
}

type Runtime struct {
	SandboxImage string `toml:"sandbox_image"`
	WorktreeRoot string `toml:"worktree_root"`
}

type Parallel struct {
	MaxParallelTasks *int `toml:"max_parallel_tasks"`
	MaxTokensPerRun  *int `toml:"max_tokens_per_run"`
}

// Load reads a TOML config from path, applies defaults, and validates the schema version.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if _, err := toml.Decode(string(raw), &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.SchemaVersion == 0 {
		return nil, errors.New("config missing schema_version")
	}
	if c.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("schema_version %d not supported (want %d)", c.SchemaVersion, CurrentSchemaVersion)
	}
	applyDefaults(&c)
	if c.Parallel.MaxParallelTasks != nil && *c.Parallel.MaxParallelTasks < 1 {
		return nil, fmt.Errorf("parallel.max_parallel_tasks must be >= 1, got %d", *c.Parallel.MaxParallelTasks)
	}
	if c.Parallel.MaxTokensPerRun != nil && *c.Parallel.MaxTokensPerRun < 1 {
		return nil, fmt.Errorf("parallel.max_tokens_per_run must be >= 1, got %d", *c.Parallel.MaxTokensPerRun)
	}
	if err := validateCrossModelReview(&c.Engines); err != nil {
		return nil, err
	}
	for name, srv := range c.MCP.Servers {
		if srv.Binary == "" {
			return nil, fmt.Errorf("mcp.servers.%s missing 'binary'", name)
		}
		for k, v := range srv.Env {
			srv.Env[k] = interpolateEnv(v)
		}
		c.MCP.Servers[name] = srv
	}
	return &c, nil
}

func (p Parallel) Workers() int {
	if p.MaxParallelTasks == nil {
		return 4
	}
	return *p.MaxParallelTasks
}

func (p Parallel) RunTokenCap() int {
	if p.MaxTokensPerRun == nil {
		return 1_000_000
	}
	return *p.MaxTokensPerRun
}

// Escalations returns the configured MaxEscalations value, resolving the
// unset case to the default (1). Callers should use this instead of
// dereferencing Budget.MaxEscalations directly so the default is applied
// consistently even when configuration pre-dates the field.
func (b Budget) Escalations() int {
	if b.MaxEscalations == nil {
		return 1
	}
	return *b.MaxEscalations
}

// DecomposeDepthCap returns the effective recursion limit for auto-decompose.
// Default 2 when unset (zero value). Hard-capped at 3 — any larger value in
// config is silently clamped.
func (b Budget) DecomposeDepthCap() int {
	const hardCap = 3
	if b.MaxDecomposeDepth == 0 {
		return 2
	}
	if b.MaxDecomposeDepth > hardCap {
		return hardCap
	}
	return b.MaxDecomposeDepth
}

var envRefRE = regexp.MustCompile(`\$\{env:([A-Z_][A-Z0-9_]*)\}`)

func interpolateEnv(s string) string {
	return envRefRE.ReplaceAllStringFunc(s, func(m string) string {
		match := envRefRE.FindStringSubmatch(m)
		return os.Getenv(match[1])
	})
}

// validateCrossModelReview enforces that code and review are performed by
// different engines. Self-review by the same model reliably misses the same
// class of errors it just produced; this check fails closed so that a
// misconfigured project cannot silently fall back to single-model review.
func validateCrossModelReview(e *Engines) error {
	if e.CoderDefault == e.ReviewerDefault {
		return fmt.Errorf(
			"engines.coder_default and engines.reviewer_default must be different engines "+
				"(both are %q); cross-model review is mandatory so one engine's blind spots "+
				"are caught by the other", e.CoderDefault)
	}
	known := map[string]bool{"claude": true, "codex": true}
	if !known[e.CoderDefault] {
		return fmt.Errorf("engines.coder_default %q is not a known engine (want claude|codex)", e.CoderDefault)
	}
	if !known[e.ReviewerDefault] {
		return fmt.Errorf("engines.reviewer_default %q is not a known engine (want claude|codex)", e.ReviewerDefault)
	}
	for kind, coder := range e.RolesByKind {
		if !known[coder] {
			return fmt.Errorf("engines.roles_by_kind[%q] = %q is not a known engine (want claude|codex)", kind, coder)
		}
	}
	return nil
}

func applyDefaults(c *Config) {
	if c.Project.BaseBranch == "" {
		c.Project.BaseBranch = "main"
	}
	if c.Project.StagingBranch == "" {
		c.Project.StagingBranch = "aios/staging"
	}
	if c.Engines.CoderDefault == "" {
		c.Engines.CoderDefault = "claude"
	}
	if c.Engines.ReviewerDefault == "" {
		c.Engines.ReviewerDefault = "codex"
	}
	if c.Engines.Claude.Binary == "" {
		c.Engines.Claude.Binary = "claude"
	}
	if c.Engines.Claude.TimeoutSec == 0 {
		c.Engines.Claude.TimeoutSec = 600
	}
	if c.Engines.Codex.Binary == "" {
		c.Engines.Codex.Binary = "codex"
	}
	if c.Engines.Codex.TimeoutSec == 0 {
		c.Engines.Codex.TimeoutSec = 600
	}
	if c.Engines.Claude.RetryMaxAttempts == 0 {
		c.Engines.Claude.RetryMaxAttempts = 3
	}
	if c.Engines.Claude.RetryBaseMs == 0 {
		c.Engines.Claude.RetryBaseMs = 1000
	}
	if c.Engines.Claude.RetryEnabled == nil {
		b := true
		c.Engines.Claude.RetryEnabled = &b
	}
	if c.Engines.Codex.RetryMaxAttempts == 0 {
		c.Engines.Codex.RetryMaxAttempts = 3
	}
	if c.Engines.Codex.RetryBaseMs == 0 {
		c.Engines.Codex.RetryBaseMs = 1000
	}
	if c.Engines.Codex.RetryEnabled == nil {
		b := true
		c.Engines.Codex.RetryEnabled = &b
	}
	if c.Budget.MaxRoundsPerTask == 0 {
		c.Budget.MaxRoundsPerTask = 5
	}
	if c.Budget.MaxTokensPerTask == 0 {
		c.Budget.MaxTokensPerTask = 200000
	}
	if c.Budget.MaxWallMinutesPerTask == 0 {
		c.Budget.MaxWallMinutesPerTask = 30
	}
	if c.Budget.StallThreshold == 0 {
		c.Budget.StallThreshold = 3
	}
	if c.Budget.MaxEscalations == nil {
		n := 1
		c.Budget.MaxEscalations = &n
	}
	if c.Runtime.WorktreeRoot == "" {
		c.Runtime.WorktreeRoot = ".aios/worktrees"
	}
}
