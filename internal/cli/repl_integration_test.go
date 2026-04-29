package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Integration tests for REPL session persistence.
// The bubbletea TUI cannot be driven via stdin strings, so these tests
// verify the session layer directly rather than the full TUI loop.

func TestReplSessionPersistence(t *testing.T) {
	wd := t.TempDir()
	sessionsDir := filepath.Join(wd, ".aios", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a session with one turn.
	id := "2026-04-26T12-00-00"
	s := &Session{
		ID:         id,
		Created:    time.Now().UTC(),
		SessionDir: filepath.Join(sessionsDir, id),
		SpecPath:   filepath.Join(wd, ".aios", "project.md"),
		Turns: []SessionTurn{
			{Timestamp: time.Now().UTC(), UserMessage: "design a thing", SpecAfter: "POLISHED_FINAL", RunID: "run-1"},
		},
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Verify session.json was written correctly.
	sessRaw, err := os.ReadFile(filepath.Join(sessionsDir, id, "session.json"))
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

	// Verify LatestSession picks it up.
	latest, err := LatestSession(sessionsDir)
	if err != nil {
		t.Fatalf("LatestSession: %v", err)
	}
	if latest.ID != id {
		t.Fatalf("LatestSession = %q, want %q", latest.ID, id)
	}
}

func TestReplSessionClear(t *testing.T) {
	wd := t.TempDir()
	sessionsDir := filepath.Join(wd, ".aios", "sessions")
	id := "2026-04-26T12-00-00"
	sessionDir := filepath.Join(sessionsDir, id)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	s := &Session{
		ID:         id,
		Created:    time.Now().UTC(),
		SessionDir: sessionDir,
		SpecPath:   filepath.Join(wd, ".aios", "project.md"),
		Turns: []SessionTurn{
			{Timestamp: time.Now().UTC(), UserMessage: "first", SpecAfter: "SPEC1", RunID: "r1"},
		},
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Clear turns and re-save.
	s.Turns = nil
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Reload and verify.
	reloaded, err := LoadSession(sessionDir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(reloaded.Turns) != 0 {
		t.Fatalf("expected 0 turns after clear, got %d", len(reloaded.Turns))
	}
}
