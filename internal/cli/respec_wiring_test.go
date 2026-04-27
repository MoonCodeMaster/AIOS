package cli

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
)

func TestStashTasks_MovesMdFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "tasks")
	dst := filepath.Join(root, "old-tasks")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"001.md", "002.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := stashTasks(src, dst); err != nil {
		t.Fatalf("stashTasks: %v", err)
	}

	moved, _ := os.ReadDir(dst)
	var names []string
	for _, e := range moved {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "001.md" || names[1] != "002.md" {
		t.Errorf("dst contents = %v, want [001.md 002.md]", names)
	}

	left, _ := os.ReadDir(src)
	if len(left) != 1 || left[0].Name() != "notes.txt" {
		t.Errorf("non-md files should remain in src; got %v", left)
	}
}

func TestStashTasks_MissingSrcOK(t *testing.T) {
	root := t.TempDir()
	if err := stashTasks(filepath.Join(root, "nope"), filepath.Join(root, "old")); err != nil {
		t.Errorf("stashTasks on missing src should not error, got: %v", err)
	}
}

func TestLatestRunDir_PicksMostRecent(t *testing.T) {
	root := t.TempDir()
	runs := filepath.Join(root, ".aios", "runs")
	for _, id := range []string{"2025-01-01T00-00-00", "2026-04-27T12-00-00", "2026-04-26T23-59-59"} {
		if err := os.MkdirAll(filepath.Join(runs, id), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
	}
	got, err := latestRunDir(root)
	if err != nil {
		t.Fatalf("latestRunDir: %v", err)
	}
	if filepath.Base(got) != "2026-04-27T12-00-00" {
		t.Errorf("latestRunDir = %s, want 2026-04-27T12-00-00", filepath.Base(got))
	}
}

func TestLatestRunDir_NoRunsErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aios", "runs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := latestRunDir(root); err == nil {
		t.Error("expected error when no run dirs present")
	}
}

func TestCollectAbandons_FiltersToBlocked(t *testing.T) {
	captured := map[string]orchestrator.Outcome{
		"a": {Final: orchestrator.StateBlocked, Rounds: []orchestrator.RoundRecord{{N: 1}}},
		"b": {Final: orchestrator.StateConverged},
		"c": {Final: orchestrator.StateBlocked},
	}
	abandons, ids := collectAbandons(captured, &sync.Mutex{})
	if len(abandons) != 2 {
		t.Fatalf("got %d abandons, want 2", len(abandons))
	}
	sort.Strings(ids)
	if ids[0] != "a" || ids[1] != "c" {
		t.Errorf("got ids %v, want [a c]", ids)
	}
}

func TestTaskOutcomeRecorder_RoundTrip(t *testing.T) {
	defer setTaskOutcomeRecorder(nil)
	var got string
	setTaskOutcomeRecorder(func(id string, _ *orchestrator.Outcome) {
		got = id
	})
	recordTaskOutcome("task-7", &orchestrator.Outcome{Final: orchestrator.StateConverged})
	if got != "task-7" {
		t.Errorf("recorder got %q, want task-7", got)
	}
}

func TestRecordTaskOutcome_NilOutcomeNoop(t *testing.T) {
	defer setTaskOutcomeRecorder(nil)
	called := false
	setTaskOutcomeRecorder(func(string, *orchestrator.Outcome) { called = true })
	recordTaskOutcome("x", nil)
	if called {
		t.Error("recorder should not fire on nil outcome")
	}
}

func TestDescribeRespecOutcome(t *testing.T) {
	cases := []struct {
		s    ShipStatus
		want string
	}{
		{ShipMerged, "merged after respec"},
		{ShipPRRed, "PR red after respec"},
		{ShipAbandoned, "abandoned after respec"},
		{ShipUnknown, "unknown after respec"},
	}
	for _, c := range cases {
		if got := describeRespecOutcome(c.s); got != c.want {
			t.Errorf("status %v: got %q, want %q", c.s, got, c.want)
		}
	}
}
