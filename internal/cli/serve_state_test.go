package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func TestServeState_RoundtripJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewServeState()
	s.Add(42, "run-id-1")
	s.Add(43, "run-id-2")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadServeState(path)
	if err != nil {
		t.Fatalf("LoadServeState: %v", err)
	}
	if len(loaded.Issues) != 2 || loaded.Issues[42].RunID != "run-id-1" {
		t.Errorf("loaded state mismatch: %+v", loaded.Issues)
	}
}

func TestServeState_LoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadServeState(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatalf("LoadServeState (missing): %v", err)
	}
	if len(s.Issues) != 0 {
		t.Errorf("missing-file state should be empty, got %+v", s.Issues)
	}
}

func TestServeState_Reconcile_GitHubOnlyOrphan_ReleasesLabel(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 100, Title: "orphan", Labels: []string{"aios:in-progress"}},
	}}
	s := NewServeState()
	if err := s.Reconcile(context.Background(), host, "aios:do", "aios:in-progress"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := labelsOf(host.Issues, 100)
	if got["aios:in-progress"] {
		t.Errorf("aios:in-progress should be removed, got %v", got)
	}
	if !got["aios:do"] {
		t.Errorf("aios:do should be re-added, got %v", got)
	}
}

func TestServeState_Reconcile_StateOnlyOrphan_RemovesFromState(t *testing.T) {
	host := &githost.FakeHost{}
	s := NewServeState()
	s.Add(99, "orphan-run")
	if err := s.Reconcile(context.Background(), host, "aios:do", "aios:in-progress"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, present := s.Issues[99]; present {
		t.Errorf("state-only orphan #99 should be removed; state = %+v", s.Issues)
	}
}

func labelsOf(issues []githost.Issue, num int) map[string]bool {
	m := map[string]bool{}
	for _, i := range issues {
		if i.Number == num {
			for _, l := range i.Labels {
				m[l] = true
			}
		}
	}
	return m
}
