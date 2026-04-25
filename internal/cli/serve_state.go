package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

type InProgressIssue struct {
	RunID     string    `json:"run_id"`
	ClaimedAt time.Time `json:"claimed_at"`
}

type ServeState struct {
	mu     sync.Mutex
	Issues map[int]InProgressIssue `json:"issues"`
}

func NewServeState() *ServeState { return &ServeState{Issues: map[int]InProgressIssue{}} }

func LoadServeState(path string) (*ServeState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewServeState(), nil
		}
		return nil, err
	}
	s := NewServeState()
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("parse serve state: %w", err)
	}
	if s.Issues == nil {
		s.Issues = map[int]InProgressIssue{}
	}
	return s, nil
}

func (s *ServeState) Save(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func (s *ServeState) Add(issueNum int, runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Issues[issueNum] = InProgressIssue{RunID: runID, ClaimedAt: time.Now()}
}

func (s *ServeState) Remove(issueNum int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Issues, issueNum)
}

func (s *ServeState) Reconcile(ctx context.Context, host githost.Host, doLabel, inProgressLabel string) error {
	githubInflight, err := host.ListLabeled(ctx, inProgressLabel)
	if err != nil {
		return fmt.Errorf("reconcile list: %w", err)
	}
	githubSet := map[int]bool{}
	for _, i := range githubInflight {
		githubSet[i.Number] = true
	}
	s.mu.Lock()
	stateSet := map[int]bool{}
	for n := range s.Issues {
		stateSet[n] = true
	}
	s.mu.Unlock()

	for n := range githubSet {
		if stateSet[n] {
			continue
		}
		if err := host.RemoveLabel(ctx, n, inProgressLabel); err != nil {
			return fmt.Errorf("reconcile remove %s on #%d: %w", inProgressLabel, n, err)
		}
		if err := host.AddLabel(ctx, n, doLabel); err != nil {
			return fmt.Errorf("reconcile add %s on #%d: %w", doLabel, n, err)
		}
	}
	for n := range stateSet {
		if githubSet[n] {
			continue
		}
		s.Remove(n)
	}
	return nil
}
