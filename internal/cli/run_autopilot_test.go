package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

func TestAutopilotRescues_OnlyStall(t *testing.T) {
	cases := []struct {
		code orchestrator.BlockCode
		want bool
	}{
		{orchestrator.CodeStallNoProgress, true},
		{orchestrator.CodeMaxRoundsExceeded, false},
		{orchestrator.CodeMaxTokensExceeded, false},
		{orchestrator.CodeEngineInvokeFailed, false},
		{orchestrator.CodeRebaseConflict, false},
		{orchestrator.CodeUpstreamBlocked, false},
	}
	for _, c := range cases {
		got := autopilotRescues(c.code)
		if got != c.want {
			t.Errorf("autopilotRescues(%q) = %v, want %v", c.code, got, c.want)
		}
	}
}

func TestAutopilotFinalizer_NoConvergedTasksSkipsPR(t *testing.T) {
	host := &githost.FakeHost{}
	res, err := runAutopilotFinalizer(context.Background(), finalizerOpts{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		ConvergedCount: 0,
		Title:          "t", Body: "b",
		ChecksTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("finalizer: %v", err)
	}
	if res.PR != nil {
		t.Errorf("expected no PR opened when nothing converged, got %+v", res.PR)
	}
}

func TestAutopilotFinalizer_GreenChecksMerge(t *testing.T) {
	host := &githost.FakeHost{
		ChecksByPR: map[int]githost.ChecksState{1: githost.ChecksGreen},
	}
	res, err := runAutopilotFinalizer(context.Background(), finalizerOpts{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		ConvergedCount: 1,
		Title:          "t", Body: "b",
		ChecksTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("finalizer: %v", err)
	}
	if res.PR == nil || !host.Merged[res.PR.Number] {
		t.Errorf("expected PR merged on green checks, got %+v", res)
	}
}

func TestAutopilotFinalizer_RedChecksDoesNotMerge(t *testing.T) {
	host := &githost.FakeHost{
		ChecksByPR: map[int]githost.ChecksState{1: githost.ChecksRed},
	}
	res, err := runAutopilotFinalizer(context.Background(), finalizerOpts{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		ConvergedCount: 1,
		Title:          "t", Body: "b",
		ChecksTimeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected error on red checks")
	}
	if res == nil || res.PR == nil {
		t.Error("PR should still be reported even on red — user needs the URL")
	}
	if host.Merged[res.PR.Number] {
		t.Error("must not merge a red PR")
	}
}

func TestAutopilotFinalizer_TimeoutDoesNotMerge(t *testing.T) {
	host := &timeoutHost{}
	_, err := runAutopilotFinalizer(context.Background(), finalizerOpts{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		ConvergedCount: 1,
		Title:          "t", Body: "b",
		ChecksTimeout: 10 * time.Millisecond,
	})
	if !errors.Is(err, githost.ErrChecksTimeout) {
		t.Errorf("err = %v, want ErrChecksTimeout", err)
	}
	if host.merged {
		t.Error("must not merge on timeout")
	}
}

// timeoutHost is a Host that always returns ErrChecksTimeout from WaitForChecks.
type timeoutHost struct {
	merged bool
}

func (*timeoutHost) OpenPR(_ context.Context, base, head, _, _ string) (*githost.PR, error) {
	return &githost.PR{Number: 1, URL: "url", Head: head, Base: base}, nil
}
func (*timeoutHost) WaitForChecks(context.Context, *githost.PR, time.Duration) (githost.ChecksState, error) {
	return "", githost.ErrChecksTimeout
}
func (h *timeoutHost) MergePR(context.Context, *githost.PR, githost.MergeMode) error {
	h.merged = true
	return nil
}
func (*timeoutHost) ListLabeled(context.Context, string) ([]githost.Issue, error) {
	return nil, nil
}
func (*timeoutHost) AddLabel(context.Context, int, string) error    { return nil }
func (*timeoutHost) RemoveLabel(context.Context, int, string) error { return nil }
func (*timeoutHost) AddComment(context.Context, int, string) error  { return nil }
func (*timeoutHost) OpenIssue(context.Context, string, string, []string) (int, error) {
	return 0, nil
}
func (*timeoutHost) CloseIssue(context.Context, int) error { return nil }

func TestAutopilotTailPartitioning(t *testing.T) {
	// Build a synthetic rep.Blocked with all four shapes:
	// - real-blocked task
	// - cascade from real-blocked task (CodeUpstreamBlocked, Upstream=real)
	// - autopilot-abandoned task (CodeAbandonedAutopilot)
	// - cascade from abandoned task (CodeUpstreamBlocked, Upstream=abandoned)
	blocked := map[orchestrator.TaskID]orchestrator.BlockReason{
		"realA":         {Code: orchestrator.CodeMaxTokensExceeded, Detail: "tokens"},
		"realA_dep":     {Code: orchestrator.CodeUpstreamBlocked, Upstream: "realA"},
		"abandoned":     {Code: orchestrator.CodeAbandonedAutopilot, Detail: "stall"},
		"abandoned_dep": {Code: orchestrator.CodeUpstreamBlocked, Upstream: "abandoned"},
	}

	// Replicate the partition logic.
	abandonedIDs := map[orchestrator.TaskID]bool{}
	for id, br := range blocked {
		if br.Code == orchestrator.CodeAbandonedAutopilot {
			abandonedIDs[id] = true
		}
	}
	realBlocked := map[orchestrator.TaskID]orchestrator.BlockReason{}
	for id, br := range blocked {
		if abandonedIDs[id] {
			continue
		}
		if br.Code == orchestrator.CodeUpstreamBlocked && abandonedIDs[br.Upstream] {
			continue
		}
		realBlocked[id] = br
	}

	if !abandonedIDs["abandoned"] {
		t.Error("abandoned task should be in abandonedIDs")
	}
	if abandonedIDs["realA"] {
		t.Error("realA must not be classified as abandoned")
	}
	if _, ok := realBlocked["realA"]; !ok {
		t.Error("realA must remain in realBlocked")
	}
	if _, ok := realBlocked["realA_dep"]; !ok {
		t.Error("realA_dep (cascade from real block) must remain in realBlocked")
	}
	if _, ok := realBlocked["abandoned"]; ok {
		t.Error("abandoned must be filtered out of realBlocked")
	}
	if _, ok := realBlocked["abandoned_dep"]; ok {
		t.Error("abandoned_dep (cascade from abandon) must be filtered out of realBlocked")
	}
}

func TestPersistFinalStatus_AbandonedNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	// Build a fake task file on disk with status: abandoned (as the autopilot
	// rescue path leaves it).
	path := filepath.Join(dir, "001.md")
	body := "---\nid: 001\nkind: feature\nstatus: abandoned\nacceptance:\n  - c1\n---\nbody\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tk := &spec.Task{ID: "001", Kind: "feature", Status: "abandoned", Path: path, Acceptance: []string{"c1"}}
	taskByID := map[string]*spec.Task{"001": tk}

	rep := &orchestrator.RunReport{
		Blocked: map[orchestrator.TaskID]orchestrator.BlockReason{
			"001": {Code: orchestrator.CodeAbandonedAutopilot, Detail: "stall"},
		},
	}
	persistFinalStatus(taskByID, rep)

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "status: abandoned") {
		t.Errorf("abandoned frontmatter must not be overwritten by persistFinalStatus; file = %q", raw)
	}
}

func TestPersistFinalStatus_ConvergedAndBlocked(t *testing.T) {
	dir := t.TempDir()
	makeTask := func(id, status string) *spec.Task {
		t.Helper()
		path := filepath.Join(dir, id+".md")
		body := "---\nid: " + id + "\nkind: feature\nstatus: pending\nacceptance:\n  - c1\n---\nbody\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return &spec.Task{ID: id, Kind: "feature", Status: status, Path: path, Acceptance: []string{"c1"}}
	}
	tConv := makeTask("conv", "pending")
	tBlock := makeTask("block", "pending")
	taskByID := map[string]*spec.Task{"conv": tConv, "block": tBlock}

	rep := &orchestrator.RunReport{
		Converged: []orchestrator.TaskID{"conv"},
		Blocked: map[orchestrator.TaskID]orchestrator.BlockReason{
			"block": {Code: orchestrator.CodeRebaseConflict, Detail: "conflict"},
		},
	}
	persistFinalStatus(taskByID, rep)

	convRaw, _ := os.ReadFile(tConv.Path)
	if !strings.Contains(string(convRaw), "status: converged") {
		t.Errorf("converged task frontmatter not updated; got %q", convRaw)
	}
	blockRaw, _ := os.ReadFile(tBlock.Path)
	if !strings.Contains(string(blockRaw), "status: blocked") {
		t.Errorf("blocked task frontmatter not updated; got %q", blockRaw)
	}
}
