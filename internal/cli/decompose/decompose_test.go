package decompose

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// fakeEngine is a minimal engine.Engine for these tests — returns scripted
// responses keyed by call order; errors first if errs is set.
type fakeEngine struct {
	name    string
	returns []engine.InvokeResponse
	errs    []error
	calls   int
}

func (f *fakeEngine) Name() string { return f.name }
func (f *fakeEngine) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i >= len(f.returns) {
		return nil, errors.New("fake engine: no scripted response")
	}
	return &f.returns[i], nil
}

const validTwoTaskMerge = `---
id: 005.1
kind: feature
parent_id: "005"
depth: 0
status: pending
acceptance:
  - c1a
---
sub-task 1 body

===TASK===
---
id: 005.2
kind: feature
parent_id: "005"
depth: 0
status: pending
acceptance:
  - c1b
---
sub-task 2 body
`

func TestRun_HappyPath_TwoChildrenAtParentDepthPlus1(t *testing.T) {
	parent := &spec.Task{ID: "005", Kind: "feature", Depth: 0, Acceptance: []string{"c1"}, Body: "parent body"}
	claude := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "claude proposal A"}}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: "codex proposal B"}}}
	synth := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: validTwoTaskMerge}}}

	out, err := Run(context.Background(), Input{
		Parent:        parent,
		Claude:        claude,
		Codex:         codex,
		Synthesizer:   synth,
		IssuesByRound: [][]string{{"issue x", "issue y"}, {"issue x", "issue y"}, {"issue x", "issue y"}},
		LastDiff:      "diff content",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(out.Children))
	}
	for _, c := range out.Children {
		if c.Depth != parent.Depth+1 {
			t.Errorf("child %s depth = %d, want %d", c.ID, c.Depth, parent.Depth+1)
		}
		if c.ParentID != parent.ID {
			t.Errorf("child %s parent_id = %q, want %q", c.ID, c.ParentID, parent.ID)
		}
	}
	if out.Children[0].ID != "005.1" || out.Children[1].ID != "005.2" {
		t.Errorf("child IDs = [%q, %q], want [005.1, 005.2]", out.Children[0].ID, out.Children[1].ID)
	}
	if !out.UsedSynthesizer {
		t.Error("UsedSynthesizer should be true on happy path")
	}
}

func TestRun_ProposalAFails_FallsBackToBOnly(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	claude := &fakeEngine{name: "claude", errs: []error{errors.New("network")}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: validTwoTaskMerge}}}
	synth := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "should not be called"}}}

	out, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out.Children) != 2 {
		t.Errorf("Children = %d, want 2", len(out.Children))
	}
	if out.UsedSynthesizer {
		t.Error("UsedSynthesizer should be false when one proposal failed (single-source path)")
	}
	if synth.calls != 0 {
		t.Errorf("synthesizer called %d times, want 0", synth.calls)
	}
}

func TestRun_BothProposalsFail_AbandonError(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	claude := &fakeEngine{name: "claude", errs: []error{errors.New("network")}}
	codex := &fakeEngine{name: "codex", errs: []error{errors.New("network")}}
	synth := &fakeEngine{name: "claude"}
	_, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth})
	if err == nil {
		t.Fatal("expected ErrAbandon when both proposals fail")
	}
	if !errors.Is(err, ErrAbandon) {
		t.Errorf("err = %v, want ErrAbandon", err)
	}
}

func TestRun_SynthesizerFails_DeterministicFallbackUnion(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	propA := `---
id: 005.1
kind: feature
parent_id: "005"
status: pending
acceptance:
  - c1a
---
A's split 1

===TASK===
---
id: 005.shared
kind: feature
parent_id: "005"
status: pending
acceptance:
  - shared
---
shared body (A's wording)
`
	propB := `---
id: 005.shared
kind: feature
parent_id: "005"
status: pending
acceptance:
  - shared
---
shared body (B's wording)

===TASK===
---
id: 005.2
kind: feature
parent_id: "005"
status: pending
acceptance:
  - c1b
---
B's split 2
`
	claude := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: propA}}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: propB}}}
	synth := &fakeEngine{name: "codex", errs: []error{errors.New("synth network")}}

	out, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth, SynthesizerName: "codex"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out.Children) != 3 {
		t.Errorf("Children = %d, want 3 (union dedupe)", len(out.Children))
	}
	if out.UsedSynthesizer {
		t.Error("UsedSynthesizer should be false when synth failed and fallback was used")
	}
	for _, c := range out.Children {
		if strings.HasSuffix(c.ID, "shared") && !strings.Contains(c.Body, "B's wording") {
			t.Errorf("collision tiebreak: shared body = %q, want B's wording", c.Body)
		}
	}
}

func TestRun_SingleTaskResult_AbandonError(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	singleTask := `---
id: 005.giveup
kind: feature
parent_id: "005"
status: pending
acceptance:
  - c1
---
single result, give up
`
	claude := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "anything"}}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: "anything"}}}
	synth := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: singleTask}}}
	_, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth})
	if !errors.Is(err, ErrAbandon) {
		t.Errorf("err = %v, want ErrAbandon", err)
	}
}

func TestRun_MalformedOutput_AbandonError(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	claude := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "anything"}}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: "anything"}}}
	synth := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "garbage with no separator markers"}}}
	_, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth})
	if !errors.Is(err, ErrAbandon) {
		t.Errorf("err = %v, want ErrAbandon (malformed)", err)
	}
}
