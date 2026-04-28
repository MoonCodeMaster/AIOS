package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestGateNone_AlwaysOK(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	if _, err := gateNone(context.Background(), ""); err != nil {
		t.Fatalf("gateNone returned err: %v", err)
	}
}

func TestGateGit_FailsOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	_, err := gateGit(context.Background(), "")
	if err == nil {
		t.Fatal("gateGit should fail outside a git repo")
	}
	if !strings.Contains(err.Error(), "not a git repo") {
		t.Fatalf("error should say 'not a git repo'; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "git init") {
		t.Fatalf("error should hint `git init`; got %q", err.Error())
	}
}

func TestGateGit_PassesInRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)
	if _, err := gateGit(context.Background(), ""); err != nil {
		t.Fatalf("gateGit returned err in repo: %v", err)
	}
}

func TestGateAIOS_FailsWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)
	_, err := gateAIOS(context.Background(), "")
	if err == nil {
		t.Fatal("gateAIOS should fail without .aios/config.toml")
	}
	if !strings.Contains(err.Error(), "not an AIOS repo") {
		t.Fatalf("error should say 'not an AIOS repo'; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "aios init") {
		t.Fatalf("error should hint `aios init`; got %q", err.Error())
	}
}

func TestGateAIOS_LoadsConfigIntoContext(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgBody := "schema_version = 1\n[project]\nname = \"x\"\nbase_branch = \"main\"\nstaging_branch = \"aios/staging\"\n[engines]\ncoder_default = \"claude\"\nreviewer_default = \"codex\"\n[engines.claude]\nbinary = \"claude\"\ntimeout_sec = 600\n[engines.codex]\nbinary = \"codex\"\ntimeout_sec = 600\n"
	if err := os.WriteFile(filepath.Join(dir, ".aios", "config.toml"), []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)

	ctx, err := gateAIOS(context.Background(), "")
	if err != nil {
		t.Fatalf("gateAIOS unexpected error: %v", err)
	}
	cfg, ok := ConfigFromContext(ctx)
	if !ok || cfg == nil {
		t.Fatal("gateAIOS did not stash config in context")
	}
	if cfg.Project.Name != "x" {
		t.Fatalf("loaded config.Project.Name = %q; want \"x\"", cfg.Project.Name)
	}
}

func TestGateAIOS_HonorsCustomConfigPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(dir, "custom.toml")
	cfgBody := "schema_version = 1\n[project]\nname = \"y\"\nbase_branch = \"main\"\nstaging_branch = \"aios/staging\"\n[engines]\ncoder_default = \"claude\"\nreviewer_default = \"codex\"\n[engines.claude]\nbinary = \"claude\"\ntimeout_sec = 600\n[engines.codex]\nbinary = \"codex\"\ntimeout_sec = 600\n"
	if err := os.WriteFile(custom, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)

	ctx, err := gateAIOS(context.Background(), custom)
	if err != nil {
		t.Fatalf("gateAIOS with custom path: %v", err)
	}
	cfg, _ := ConfigFromContext(ctx)
	if cfg.Project.Name != "y" {
		t.Fatalf("custom config Project.Name = %q; want \"y\"", cfg.Project.Name)
	}
}
