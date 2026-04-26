package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		ID:         "session-x",
		Created:    time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		SessionDir: filepath.Join(dir, "session-x"),
		SpecPath:   filepath.Join(dir, "project.md"),
		Turns: []SessionTurn{
			{Timestamp: time.Date(2026, 4, 26, 12, 1, 0, 0, time.UTC), UserMessage: "hello", SpecAfter: "SPEC1", RunID: "run-1"},
		},
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadSession(s.SessionDir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got.ID != s.ID || len(got.Turns) != 1 || got.Turns[0].UserMessage != "hello" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLatestSessionPicksMostRecent(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"2026-04-26T10-00-00", "2026-04-26T11-00-00", "2026-04-26T09-00-00"} {
		s := &Session{ID: id, SessionDir: filepath.Join(dir, id)}
		if err := os.MkdirAll(s.SessionDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LatestSession(dir)
	if err != nil {
		t.Fatalf("LatestSession: %v", err)
	}
	if got.ID != "2026-04-26T11-00-00" {
		t.Fatalf("LatestSession = %q, want 2026-04-26T11-00-00", got.ID)
	}
}
