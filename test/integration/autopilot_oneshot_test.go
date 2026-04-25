package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/githost"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
	"github.com/MoonCodeMaster/AIOS/internal/verify"
	"github.com/MoonCodeMaster/AIOS/internal/worktree"
)

// TestAutopilotOneShot_HappyPath drives one trivially-converging task through
// the orchestrator with FakeEngines, then runs the autopilot finalizer with
// a green FakeHost. Asserts: PR opened, MergePR called.
func TestAutopilotOneShot_HappyPath(t *testing.T) {
	repo := seedRepo(t)

	approve := `{"approved":true,"criteria":[{"id":"c1","status":"satisfied"}],"issues":[]}`
	coder := &engine.FakeEngine{Name_: "claude",
		Script: []engine.InvokeResponse{{Text: "coded"}}}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{{Text: approve}}}

	wm := &worktree.Manager{RepoDir: repo, Root: filepath.Join(repo, ".aios", "worktrees")}
	task := &spec.Task{ID: "001-a", Kind: "feature", Status: "pending", Acceptance: []string{"c1"}}

	wt, err := wm.Create(task.ID, "aios/staging")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(wt.Path, "hello.txt"), []byte("hi\n"), 0o644)

	dep := &orchestrator.Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier:  stubVerifier{[]verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		Diff:      func() (string, error) { return wm.Diff(wt, "aios/staging") },
		MaxRounds: 5, MaxTokens: 10000, MaxWall: time.Minute,
	}
	out, err := orchestrator.Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != orchestrator.StateConverged {
		t.Fatalf("orchestrator final = %s", out.Final)
	}

	g := &worktree.Git{Dir: wt.Path}
	if _, err := g.Run("add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-m", "aios: converged task 001-a"); err != nil {
		t.Fatal(err)
	}
	if err := wm.MergeFF(wt, "aios/staging"); err != nil {
		t.Fatal(err)
	}

	// Now exercise the finalizer.
	host := &githost.FakeHost{ChecksByPR: map[int]githost.ChecksState{1: githost.ChecksGreen}}
	rec, err := run.Open(filepath.Join(repo, ".aios", "runs"), "test-run")
	if err != nil {
		t.Fatal(err)
	}
	res, err := cli.RunAutopilotFinalizerForTest(context.Background(), cli.FinalizerOptsForTest{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		Title:          "aios: 001-a",
		Body:           "test body",
		ConvergedCount: 1,
		ChecksTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("finalizer: %v", err)
	}
	if res.PR == nil {
		t.Fatal("expected PR opened")
	}
	if !host.Merged[res.PR.Number] {
		t.Errorf("PR #%d should be merged", res.PR.Number)
	}
	if !res.Merged {
		t.Error("finalizer result should mark Merged=true")
	}

	if err := cli.WriteAutopilotSummaryForTest(rec, res, nil); err != nil {
		t.Fatalf("WriteAutopilotSummary: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(rec.Root(), "autopilot-summary.md"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(body), "Merged: true") {
		t.Errorf("summary missing 'Merged: true': %s", body)
	}
	_ = wm.Remove(wt)
}
