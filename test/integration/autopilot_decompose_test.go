package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/cli/decompose"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// fakeFromScript is a one-shot scripted FakeEngine for decompose tests.
func fakeFromScript(name, text string) engine.Engine {
	return &engine.FakeEngine{Name_: name, Script: []engine.InvokeResponse{{Text: text}}}
}

const decomposeMergeOutput = `---
id: 005.1
kind: feature
parent_id: "005"
status: pending
acceptance:
  - sub1
---
sub-task 1

===TASK===
---
id: 005.2
kind: feature
parent_id: "005"
status: pending
acceptance:
  - sub2
---
sub-task 2
`

func TestAutopilotDecompose_HappyPath_SplicesAndConverges(t *testing.T) {
	parent := &spec.Task{
		ID: "005", Kind: "feature", Depth: 0,
		Acceptance: []string{"c1"}, Body: "stuck parent body",
	}
	dependent := &spec.Task{
		ID: "006", DependsOn: []string{"005"}, Acceptance: []string{"c2"},
	}

	out, err := decompose.Run(context.Background(), decompose.Input{
		Parent:          parent,
		Claude:          fakeFromScript("claude", "claude proposal"),
		Codex:           fakeFromScript("codex", "codex proposal"),
		Synthesizer:     fakeFromScript("codex", decomposeMergeOutput),
		SynthesizerName: "codex",
		IssuesByRound:   [][]string{{"x"}, {"x"}, {"x"}},
		LastDiff:        "diff",
	})
	if err != nil {
		t.Fatalf("decompose.Run: %v", err)
	}
	if len(out.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(out.Children))
	}

	s, err := orchestrator.NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-s.Ready(); got != "005" {
		t.Fatalf("first ready = %q, want 005", got)
	}

	s.Done(orchestrator.TaskResult{ID: "005", Status: "decomposed", Children: out.Children})

	enqueued := map[orchestrator.TaskID]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-s.Ready():
			enqueued[id] = true
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("child %d not enqueued (got %v)", i+1, enqueued)
		}
	}
	if !enqueued[orchestrator.TaskID(out.Children[0].ID)] {
		t.Errorf("child %s not enqueued", out.Children[0].ID)
	}

	s.Done(orchestrator.TaskResult{ID: orchestrator.TaskID(out.Children[0].ID), Status: "converged"})
	s.Done(orchestrator.TaskResult{ID: orchestrator.TaskID(out.Children[1].ID), Status: "converged"})
	select {
	case id := <-s.Ready():
		if id != "006" {
			t.Errorf("expected 006 after both children converged, got %q", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("006 did not enqueue after both children converged")
	}

	for _, c := range out.Children {
		if c.ParentID != "005" || c.Depth != 1 {
			t.Errorf("child %s: ParentID=%q Depth=%d, want ParentID=005 Depth=1", c.ID, c.ParentID, c.Depth)
		}
		if !strings.HasPrefix(c.ID, "005.") {
			t.Errorf("child ID = %q, want prefix 005.", c.ID)
		}
	}
}
