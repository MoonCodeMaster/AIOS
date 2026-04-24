package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Minimal(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "minimal.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Project.Name != "demo" {
		t.Errorf("Project.Name = %q, want %q", c.Project.Name, "demo")
	}
	if c.Budget.MaxRoundsPerTask != 5 {
		t.Errorf("default MaxRoundsPerTask = %d, want 5", c.Budget.MaxRoundsPerTask)
	}
	if c.Project.StagingBranch != "aios/staging" {
		t.Errorf("default StagingBranch = %q, want aios/staging", c.Project.StagingBranch)
	}
}

func TestLoad_Full(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "full.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Engines.CoderDefault != "claude" || c.Engines.ReviewerDefault != "codex" {
		t.Errorf("engine defaults = %q/%q", c.Engines.CoderDefault, c.Engines.ReviewerDefault)
	}
	if c.Engines.RolesByKind["bugfix"] != "codex" {
		t.Errorf("RolesByKind[bugfix] = %q", c.Engines.RolesByKind["bugfix"])
	}
	if c.Verify.TestCmd != "npm test" {
		t.Errorf("TestCmd = %q", c.Verify.TestCmd)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("does/not/exist.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_SchemaVersionMismatch(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "bad-schema.toml"))
	if err == nil {
		t.Fatal("expected error for bad schema_version")
	}
}

func TestParallelDefaults(t *testing.T) {
	c, err := Load("testdata/minimal.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Parallel.Workers() != 4 {
		t.Errorf("Workers() = %d, want 4 (default)", c.Parallel.Workers())
	}
	if c.Parallel.RunTokenCap() != 1000000 {
		t.Errorf("RunTokenCap() = %d, want 1000000 (default)", c.Parallel.RunTokenCap())
	}
}

func TestParallelExplicit(t *testing.T) {
	c, err := Load("testdata/full.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Parallel.Workers() != 8 {
		t.Errorf("Workers() = %d, want 8", c.Parallel.Workers())
	}
	if c.Parallel.RunTokenCap() != 2000000 {
		t.Errorf("RunTokenCap() = %d, want 2000000", c.Parallel.RunTokenCap())
	}
}

func TestParallelInvalid(t *testing.T) {
	const src = `schema_version = 1
[parallel]
max_parallel_tasks = 0
`
	tmp := t.TempDir() + "/c.toml"
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(tmp); err == nil {
		t.Fatal("expected error for max_parallel_tasks < 1")
	}
}

func TestMCPServers(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok-abc")
	c, err := Load("testdata/full.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	gh, ok := c.MCP.Servers["github"]
	if !ok {
		t.Fatal("github MCP server not found")
	}
	if gh.Binary != "github-mcp-server" {
		t.Errorf("Binary = %q, want github-mcp-server", gh.Binary)
	}
	if gh.Env["GITHUB_TOKEN"] != "tok-abc" {
		t.Errorf("Env[GITHUB_TOKEN] = %q, want interpolated tok-abc", gh.Env["GITHUB_TOKEN"])
	}
	if len(gh.AllowedTools) == 0 {
		t.Errorf("AllowedTools empty")
	}
}

func TestMCPServersEmptyByDefault(t *testing.T) {
	c, err := Load("testdata/minimal.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.MCP.Servers) != 0 {
		t.Errorf("Servers = %v, want empty", c.MCP.Servers)
	}
}

func TestLoad_RejectsSameCoderAndReviewer(t *testing.T) {
	const src = `schema_version = 1
[engines]
coder_default = "claude"
reviewer_default = "claude"
`
	tmp := t.TempDir() + "/c.toml"
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(tmp)
	if err == nil {
		t.Fatal("expected error when coder_default == reviewer_default (single-model review forbidden)")
	}
}

func TestLoad_RejectsUnknownEngineName(t *testing.T) {
	const src = `schema_version = 1
[engines]
coder_default = "gpt-5"
reviewer_default = "codex"
`
	tmp := t.TempDir() + "/c.toml"
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(tmp); err == nil {
		t.Fatal("expected error for unknown engine name")
	}
}

func TestLoad_RejectsUnknownRolesByKindEngine(t *testing.T) {
	const src = `schema_version = 1
[engines]
coder_default = "claude"
reviewer_default = "codex"
[engines.roles_by_kind]
feature = "gpt-5"
`
	tmp := t.TempDir() + "/c.toml"
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(tmp); err == nil {
		t.Fatal("expected error for unknown engine in roles_by_kind")
	}
}

func TestMCPServerMissingBinaryRejected(t *testing.T) {
	const src = `schema_version = 1
[mcp.servers.foo]
args = ["x"]
`
	tmp := t.TempDir() + "/c.toml"
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(tmp); err == nil {
		t.Fatal("expected error for MCP server missing binary")
	}
}
