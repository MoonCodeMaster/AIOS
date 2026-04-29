package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestReplBootSessionCreatesNew(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader(""),
		Out:    &bytes.Buffer{},
		Claude: &engine.FakeEngine{Name_: "claude"},
		Codex:  &engine.FakeEngine{Name_: "codex"},
	}
	if err := r.bootSession(); err != nil {
		t.Fatalf("bootSession: %v", err)
	}
	if r.session == nil {
		t.Fatal("session is nil after bootSession")
	}
	if r.session.ID == "" {
		t.Fatal("session ID is empty")
	}
	// Verify session.json was written.
	if _, err := os.Stat(filepath.Join(r.session.SessionDir, "session.json")); err != nil {
		t.Fatalf("session.json not written: %v", err)
	}
}

func TestReplBootSessionResumesExisting(t *testing.T) {
	wd := t.TempDir()
	sessionID := "2026-04-26T10-00-00"
	sessionDir := filepath.Join(wd, ".aios", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prior := &Session{
		ID:         sessionID,
		Created:    time.Now().UTC(),
		SessionDir: sessionDir,
		SpecPath:   filepath.Join(wd, ".aios", "project.md"),
		Turns: []SessionTurn{
			{Timestamp: time.Now().UTC(), UserMessage: "first", SpecAfter: "OLD_SPEC", RunID: "r1"},
		},
	}
	if err := prior.Save(); err != nil {
		t.Fatal(err)
	}

	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader(""),
		Out:    &bytes.Buffer{},
		Claude: &engine.FakeEngine{Name_: "claude"},
		Codex:  &engine.FakeEngine{Name_: "codex"},
	}
	if err := r.bootSession(); err != nil {
		t.Fatalf("bootSession: %v", err)
	}
	if r.session.ID != sessionID {
		t.Fatalf("session ID = %q, want %q", r.session.ID, sessionID)
	}
	if len(r.session.Turns) != 1 || r.session.Turns[0].UserMessage != "first" {
		t.Fatalf("turns not restored; got %+v", r.session.Turns)
	}
}

func TestReplBootSessionResumesSpecificID(t *testing.T) {
	wd := t.TempDir()
	sessionID := "2026-04-26T10-00-00"
	sessionDir := filepath.Join(wd, ".aios", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prior := &Session{
		ID:         sessionID,
		Created:    time.Now().UTC(),
		SessionDir: sessionDir,
		SpecPath:   filepath.Join(wd, ".aios", "project.md"),
	}
	if err := prior.Save(); err != nil {
		t.Fatal(err)
	}

	r := &Repl{
		Wd:       wd,
		In:       strings.NewReader(""),
		Out:      &bytes.Buffer{},
		Claude:   &engine.FakeEngine{Name_: "claude"},
		Codex:    &engine.FakeEngine{Name_: "codex"},
		ResumeID: sessionID,
	}
	if err := r.bootSession(); err != nil {
		t.Fatalf("bootSession: %v", err)
	}
	if r.session.ID != sessionID {
		t.Fatalf("session ID = %q, want %q", r.session.ID, sessionID)
	}
}

func TestReplRefusesWhenCLIMissing(t *testing.T) {
	wd := t.TempDir()
	r := &Repl{
		Wd:           wd,
		In:           strings.NewReader(""),
		Out:          &bytes.Buffer{},
		ClaudeBinary: "this-binary-does-not-exist-aios-test",
		CodexBinary:  "codex",
		LookPath:     exec.LookPath,
	}
	err := r.Run(context.Background())
	if err == nil {
		t.Fatalf("Run should have returned an error when claude binary is missing")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("error should mention missing claude; got: %v", err)
	}
}

func TestSlashCommandParsing(t *testing.T) {
	tests := []struct {
		input string
		want  SlashCommand
	}{
		{"", SlashNone},
		{"hello", SlashNone},
		{"/exit", SlashExit},
		{"/quit", SlashExit},
		{"/help", SlashHelp},
		{"/show", SlashShow},
		{"/clear", SlashClear},
		{"/ship", SlashShip},
		{"/unknown", SlashUnknown},
	}
	for _, tt := range tests {
		got := ParseSlash(tt.input)
		if got != tt.want {
			t.Errorf("ParseSlash(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
