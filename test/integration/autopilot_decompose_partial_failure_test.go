package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/cli/decompose"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// fakeErrEngine returns an error on every Invoke.
type fakeErrEngine struct{ name string }

func (f *fakeErrEngine) Name() string { return f.name }
func (f *fakeErrEngine) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	return nil, errors.New("fake transport failure")
}

func twoTaskProposal(parentID string) string {
	return `---
id: ` + parentID + `.1
kind: feature
parent_id: "` + parentID + `"
status: pending
acceptance:
  - c1a
---
body 1

===TASK===
---
id: ` + parentID + `.2
kind: feature
parent_id: "` + parentID + `"
status: pending
acceptance:
  - c1b
---
body 2
`
}

// TestAutopilotDecompose_SingleSourceFallback_StillSplices proves that when one
// proposal errors and the other succeeds, the surviving proposal's sub-tasks
// still splice into the scheduler correctly.
func TestAutopilotDecompose_SingleSourceFallback_StillSplices(t *testing.T) {
	parent := &spec.Task{ID: "007", Acceptance: []string{"c1"}, Body: "x"}
	dependent := &spec.Task{ID: "008", DependsOn: []string{"007"}, Acceptance: []string{"c1"}}

	out, err := decompose.Run(context.Background(), decompose.Input{
		Parent:          parent,
		Claude:          &fakeErrEngine{name: "claude"},
		Codex:           fakeFromScript("codex", twoTaskProposal("007")),
		Synthesizer:     fakeFromScript("codex", "should not be called"),
		SynthesizerName: "codex",
	})
	if err != nil {
		t.Fatalf("decompose.Run: %v", err)
	}
	if out.UsedSynthesizer {
		t.Error("UsedSynthesizer should be false on single-source fallback")
	}
	if len(out.Children) != 2 {
		t.Fatalf("Children = %d, want 2 (B's proposal)", len(out.Children))
	}

	s, err := orchestrator.NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Ready()
	s.Done(orchestrator.TaskResult{ID: "007", Status: "decomposed", Children: out.Children})

	for i := 0; i < 2; i++ {
		select {
		case <-s.Ready():
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("child %d not enqueued", i+1)
		}
	}
	for _, c := range out.Children {
		s.Done(orchestrator.TaskResult{ID: orchestrator.TaskID(c.ID), Status: "converged"})
	}
	select {
	case id := <-s.Ready():
		if id != "008" {
			t.Errorf("expected 008, got %q", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("008 did not enqueue after both fallback children converged")
	}
}

// TestAutopilotDecompose_BothProposalsFail_ReturnsAbandon proves the all-fail
// path bubbles ErrAbandon to the caller.
func TestAutopilotDecompose_BothProposalsFail_ReturnsAbandon(t *testing.T) {
	parent := &spec.Task{ID: "009", Acceptance: []string{"c1"}, Body: "x"}
	_, err := decompose.Run(context.Background(), decompose.Input{
		Parent:          parent,
		Claude:          &fakeErrEngine{name: "claude"},
		Codex:           &fakeErrEngine{name: "codex"},
		Synthesizer:     fakeFromScript("claude", "unused"),
		SynthesizerName: "claude",
	})
	if !errors.Is(err, decompose.ErrAbandon) {
		t.Errorf("err = %v, want ErrAbandon", err)
	}
}
