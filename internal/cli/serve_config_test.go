package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServeConfig_Defaults_WhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadServeConfig(filepath.Join(dir, "serve.toml"))
	if err != nil {
		t.Fatalf("LoadServeConfig (missing file): %v", err)
	}
	if cfg.Labels.Do != "aios:do" {
		t.Errorf("Labels.Do = %q, want aios:do", cfg.Labels.Do)
	}
	if cfg.Poll.IntervalSec != 60 {
		t.Errorf("Poll.IntervalSec = %d, want 60", cfg.Poll.IntervalSec)
	}
	if cfg.Concurrency.MaxConcurrentIssues != 1 {
		t.Errorf("Concurrency = %d, want 1 (sequential default for v0.5.0)", cfg.Concurrency.MaxConcurrentIssues)
	}
}

func TestServeConfig_LoadsTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.toml")
	body := `
[repo]
owner = "MoonCodeMaster"
name = "AIOS"

[labels]
do = "aios:please-do"
in_progress = "aios:wip"

[poll]
interval_sec = 30
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServeConfig(path)
	if err != nil {
		t.Fatalf("LoadServeConfig: %v", err)
	}
	if cfg.Repo.Owner != "MoonCodeMaster" || cfg.Repo.Name != "AIOS" {
		t.Errorf("Repo = %+v, want MoonCodeMaster/AIOS", cfg.Repo)
	}
	if cfg.Labels.Do != "aios:please-do" {
		t.Errorf("Labels.Do = %q, want aios:please-do", cfg.Labels.Do)
	}
	if cfg.Labels.InProgress != "aios:wip" {
		t.Errorf("Labels.InProgress = %q, want aios:wip", cfg.Labels.InProgress)
	}
	if cfg.Poll.IntervalSec != 30 {
		t.Errorf("Poll.IntervalSec = %d, want 30", cfg.Poll.IntervalSec)
	}
}
