package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
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
