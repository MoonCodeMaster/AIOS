package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// Two sequential tasks (Workers=1) edit DIFFERENT files. Because the MergeQueue
// processes T1 before T2 starts, T2 must land via rebase (staging has advanced).
// Both must end on staging. Inline FF/rebase logic is removed — the Task
// callback delegates all merge work to RunAll's MergeQueue.
func TestRunAllRebaseClean(t *testing.T) {
	dir := initTestRepo(t)
	// Pre-create files so non-conflicting edits are possible.
	if err := os.WriteFile(filepath.Join(dir, "f1.txt"), []byte("A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f2.txt"), []byte("B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "seed")
	mustRun(t, dir, "git", "branch", "-f", "aios/staging", "main")

	tasks := []*spec.Task{
		{ID: "T1", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T2", Status: "pending", Acceptance: []string{"x"}},
	}
	taskFn := func(ctx context.Context, id orchestrator.TaskID) (orchestrator.TaskResult, *orchestrator.MergeRequest) {
		// Capture staging HEAD before we branch — this becomes the ParentSHA
		// that MergeQueue uses to decide between FF and rebase paths.
		parentOut, err := func() (string, error) {
			c := exec.Command("git", "rev-parse", "aios/staging")
			c.Dir = dir
			out, e := c.Output()
			return strings.TrimSpace(string(out)), e
		}()
		if err != nil {
			t.Errorf("rev-parse staging: %v", err)
			return orchestrator.TaskResult{ID: id, Status: "blocked"}, nil
		}

		mustRun(t, dir, "git", "checkout", "-q", "-b", "aios/task/"+string(id), "aios/staging")
		f := "f1.txt"
		if id == "T2" {
			f = "f2.txt"
		}
		path := filepath.Join(dir, f)
		body, _ := os.ReadFile(path)
		_ = os.WriteFile(path, append(body, []byte(string(id)+"\n")...), 0o644)
		mustRun(t, dir, "git", "add", ".")
		mustRun(t, dir, "git", "commit", "-q", "-m", string(id))

		// Return to staging so MergeQueue can operate on the working tree.
		mustRun(t, dir, "git", "checkout", "-q", "aios/staging")

		// Return the merge request — RunAll will submit it to the MergeQueue
		// and block until an ack is received before this task is considered done.
		return orchestrator.TaskResult{ID: id, Status: "converged"}, &orchestrator.MergeRequest{
			TaskID:    id,
			Branch:    "aios/task/" + string(id),
			ParentSHA: parentOut,
			Diff:      nil,
		}
	}
	rep, err := orchestrator.RunAll(context.Background(), orchestrator.RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       1, // Serialized so T2 sees T1's merged commit in staging.
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Task:          taskFn,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(rep.Blocked) != 0 {
		t.Errorf("expected no blocked, got %v", rep.Blocked)
	}
	// Both messages should be present in staging history.
	out := mustRunOut(t, dir, "git", "log", "--format=%s", "aios/staging")
	if !strings.Contains(out, "T1") || !strings.Contains(out, "T2") {
		t.Errorf("staging log missing T1 or T2:\n%s", out)
	}
}
