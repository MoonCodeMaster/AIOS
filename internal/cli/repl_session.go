package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Session is the state of one REPL session, persisted between turns and
// restorable across crashes via `aios --resume`.
type Session struct {
	ID         string        `json:"id"`
	Created    time.Time     `json:"created"`
	SessionDir string        `json:"session_dir"`
	SpecPath   string        `json:"spec_path"`
	Turns      []SessionTurn `json:"turns"`
}

type SessionTurn struct {
	Timestamp   time.Time `json:"timestamp"`
	UserMessage string    `json:"user_message"`
	SpecAfter   string    `json:"spec_after"`
	RunID       string    `json:"run_id"`
}

// Save writes the session to <SessionDir>/session.json.
func (s *Session) Save() error {
	if err := os.MkdirAll(s.SessionDir, 0o755); err != nil {
		return fmt.Errorf("mkdir session dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.SessionDir, "session.json"), data, 0o644)
}

// LoadSession reads a session.json from a session directory.
func LoadSession(dir string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("read session.json: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse session.json: %w", err)
	}
	return &s, nil
}

// LatestSession returns the most recent session in sessionsDir, identified
// by the lexicographic ordering of session IDs (timestamp-prefixed).
func LatestSession(sessionsDir string) (*Session, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no sessions in %s", sessionsDir)
	}
	sort.Strings(ids)
	return LoadSession(filepath.Join(sessionsDir, ids[len(ids)-1]))
}

// NewSessionID returns a new timestamp-based session ID.
func NewSessionID() string {
	return time.Now().UTC().Format("2006-01-02T15-04-05")
}
