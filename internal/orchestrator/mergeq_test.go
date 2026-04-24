package orchestrator

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Solaxis/aios/internal/engine"
)

// gitInit creates a fresh repo with a single commit on `main` and an
// `aios/staging` branch pointing at the same commit. Returns the repo dir.
func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "x@y.z"},
		{"config", "user.name", "x"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "v0\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	mustGit(t, dir, "branch", "aios/staging")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := osWrite(path, content); err != nil {
		t.Fatal(err)
	}
}

func gitSHA(t *testing.T, dir, ref string) string {
	t.Helper()
	c := exec.Command("git", "rev-parse", ref)
	c.Dir = dir
	var out bytes.Buffer
	c.Stdout = &out
	if err := c.Run(); err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(out.String())
}

func TestMergeQueueFastForward(t *testing.T) {
	dir := gitInit(t)
	mustGit(t, dir, "checkout", "-q", "-b", "aios/task/T1", "aios/staging")
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "T1")

	parent := gitSHA(t, dir, "aios/staging")
	q := NewMergeQueue(dir, "aios/staging", &engine.FakeEngine{Name_: "rev"}, nil)
	go q.Run(context.Background())
	defer q.Close()

	ack := make(chan MergeResult, 1)
	q.Submit(MergeRequest{TaskID: "T1", Branch: "aios/task/T1", ParentSHA: parent, Diff: []byte(""), Ack: ack})
	res := <-ack
	if res.Status != "converged" {
		t.Fatalf("status = %s, reason = %s", res.Status, res.Reason)
	}
	if gitSHA(t, dir, "aios/staging") == parent {
		t.Errorf("staging should have advanced after FF")
	}
}

func TestMergeQueueRebaseConflictBlocks(t *testing.T) {
	dir := gitInit(t)
	// First task lands a change.
	mustGit(t, dir, "checkout", "-q", "-b", "aios/task/T1", "aios/staging")
	mustWrite(t, filepath.Join(dir, "shared.txt"), "T1\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "T1")
	parent := gitSHA(t, dir, "aios/staging")
	q := NewMergeQueue(dir, "aios/staging", &engine.FakeEngine{Name_: "rev"}, nil)
	go q.Run(context.Background())
	defer q.Close()
	ack := make(chan MergeResult, 1)
	q.Submit(MergeRequest{TaskID: "T1", Branch: "aios/task/T1", ParentSHA: parent, Diff: []byte(""), Ack: ack})
	if r := <-ack; r.Status != "converged" {
		t.Fatalf("T1 ff failed: %s", r.Reason)
	}

	// Second task started from the OLD parent and changes the same line.
	mustGit(t, dir, "checkout", "-q", "-b", "aios/task/T2", parent)
	mustWrite(t, filepath.Join(dir, "shared.txt"), "T2\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "T2")
	ack2 := make(chan MergeResult, 1)
	q.Submit(MergeRequest{TaskID: "T2", Branch: "aios/task/T2", ParentSHA: parent, Diff: []byte("ignored"), Ack: ack2})
	r := <-ack2
	if r.Status != "blocked" || r.Reason != "rebase-conflict" {
		t.Fatalf("expected blocked/rebase-conflict, got %s/%s", r.Status, r.Reason)
	}
}

// osWrite is a tiny wrapper to avoid importing "os" twice in test helpers.
func osWrite(path, s string) error {
	return writeFileHelper(path, s)
}
