package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Solaxis/aios/internal/engine"
	"github.com/Solaxis/aios/internal/orchestrator"
	"github.com/Solaxis/aios/internal/run"
	"github.com/Solaxis/aios/internal/spec"
	"github.com/Solaxis/aios/internal/verify"
	"github.com/Solaxis/aios/internal/worktree"
)

// Drives the live orchestrator + worktree manager with FakeEngines.
func TestRunHappy_WithRealGit(t *testing.T) {
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

	// Make the coder "produce" a file via a filesystem side-effect.
	_ = os.WriteFile(filepath.Join(wt.Path, "hello.txt"), []byte("hi\n"), 0o644)

	dep := &orchestrator.Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier: stubVerifier{[]verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		Diff: func() (string, error) { return wm.Diff(wt, "aios/staging") },
		MaxRounds: 5, MaxTokens: 10000, MaxWall: time.Minute,
	}
	out, err := orchestrator.Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != orchestrator.StateConverged {
		t.Fatalf("final = %s", out.Final)
	}

	// Commit + merge.
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

	// Verify hello.txt now exists on aios/staging.
	primary := &worktree.Git{Dir: repo}
	out2, err := primary.Run("show", "aios/staging:hello.txt")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if out2 != "hi\n" {
		t.Errorf("merged content = %q", out2)
	}

	_ = wm.Remove(wt)
	_ = run.Open
}

type stubVerifier struct {
	r []verify.CheckResult
}

func (s stubVerifier) Run() []verify.CheckResult { return s.r }
