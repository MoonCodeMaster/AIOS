package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecorder_WriteRound(t *testing.T) {
	root := t.TempDir()
	rec, err := Open(root, "2026-04-23T14-02-11")
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.WriteFile("brainstorm.md", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := rec.WriteRoundFile("task-001", 1, "coder-prompt.md", []byte("prompt")); err != nil {
		t.Fatal(err)
	}
	if err := rec.WriteRoundFile("task-001", 1, "verify.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(filepath.Join(root, "2026-04-23T14-02-11", "brainstorm.md"))
	if string(b) != "hi" {
		t.Errorf("brainstorm = %q", b)
	}
	p, _ := os.ReadFile(filepath.Join(root, "2026-04-23T14-02-11", "task-001", "round-1", "coder-prompt.md"))
	if string(p) != "prompt" {
		t.Errorf("coder-prompt = %q", p)
	}
}

func TestRecorderAppendFile(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AppendFile("merge-queue.log", []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendFile("merge-queue.log", []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "run-1", "merge-queue.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first\nsecond\n" {
		t.Errorf("file = %q, want \"first\\nsecond\\n\"", string(got))
	}
}

func TestRecorderWriteJSON(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir, "run-2")
	if err != nil {
		t.Fatal(err)
	}
	type budgetReport struct{ Used int }
	if err := r.WriteJSON("budget.json", budgetReport{Used: 42}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "run-2", "budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"Used": 42`) {
		t.Errorf("file = %s, missing Used:42", string(got))
	}
}
