package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestReplEndToEnd_HappyShip(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}

	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED_FINAL"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}

	shipped := false
	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader("design a thing\n\n/ship\n\n"),
		Out:    &bytes.Buffer{},
		Claude: claude,
		Codex:  codex,
		ShipFn: func(_ context.Context, w string) error {
			data, err := os.ReadFile(filepath.Join(w, ".aios", "project.md"))
			if err != nil {
				return err
			}
			if string(data) != "POLISHED_FINAL" {
				t.Fatalf("ShipFn saw spec = %q, want POLISHED_FINAL", data)
			}
			shipped = true
			return nil
		},
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !shipped {
		t.Fatalf("ShipFn was not called")
	}

	runs, err := os.ReadDir(filepath.Join(wd, ".aios", "runs"))
	if err != nil {
		t.Fatalf("read runs dir: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want exactly 1 run dir, got %d", len(runs))
	}
	for _, name := range []string{"draft-claude.md", "draft-codex.md", "merged.md", "final.md", "stages.json"} {
		if _, err := os.Stat(filepath.Join(wd, ".aios", "runs", runs[0].Name(), "specgen", name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}

	sessions, err := os.ReadDir(filepath.Join(wd, ".aios", "sessions"))
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	sessRaw, err := os.ReadFile(filepath.Join(wd, ".aios", "sessions", sessions[0].Name(), "session.json"))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	var got Session
	if err := json.Unmarshal(sessRaw, &got); err != nil {
		t.Fatalf("unmarshal session.json: %v", err)
	}
	if len(got.Turns) != 1 || got.Turns[0].UserMessage != "design a thing" {
		t.Fatalf("session turns wrong: %+v", got.Turns)
	}
}

func TestReplEndToEnd_MergeFailureWarnsAndContinues(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	codex := &engine.FailOnCallEngine{
		Name_: "codex",
		Script: []engine.InvokeResponse{
			{Text: "DRAFT_B_long_enough_to_be_picked_as_fallback_when_merge_fails"},
		},
		FailOnCall: 2, // second call (the merge) fails
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "A"},        // short draft, loses the longer-draft fallback
		{Text: "POLISHED"}, // polish the fallback
	}}

	out := &bytes.Buffer{}
	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader("idea\n\n/exit\n"),
		Out:    out,
		Claude: claude,
		Codex:  codex,
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Merge step failed") {
		t.Fatalf("expected merge-fallback warning in stdout; got: %s", out.String())
	}
	specBody, _ := os.ReadFile(filepath.Join(wd, ".aios", "project.md"))
	if string(specBody) != "POLISHED" {
		t.Fatalf("spec = %q, want POLISHED", specBody)
	}
}
