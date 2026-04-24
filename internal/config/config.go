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
	Binary     string   `toml:"binary"`
	ExtraArgs  []string `toml:"extra_args"`
	TimeoutSec int      `toml:"timeout_sec"`
}

type Budget struct {
	MaxRoundsPerTask      int `toml:"max_rounds_per_task"`
	MaxTokensPerTask      int `toml:"max_tokens_per_task"`
	MaxWallMinutesPerTask int `toml:"max_wall_minutes_per_task"`
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

var envRefRE = regexp.MustCompile(`\$\{env:([A-Z_][A-Z0-9_]*)\}`)

func interpolateEnv(s string) string {
	return envRefRE.ReplaceAllStringFunc(s, func(m string) string {
		match := envRefRE.FindStringSubmatch(m)
		return os.Getenv(match[1])
	})
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
	if c.Budget.MaxRoundsPerTask == 0 {
		c.Budget.MaxRoundsPerTask = 5
	}
	if c.Budget.MaxTokensPerTask == 0 {
		c.Budget.MaxTokensPerTask = 200000
	}
	if c.Budget.MaxWallMinutesPerTask == 0 {
		c.Budget.MaxWallMinutesPerTask = 30
	}
	if c.Runtime.WorktreeRoot == "" {
		c.Runtime.WorktreeRoot = ".aios/worktrees"
	}
}
