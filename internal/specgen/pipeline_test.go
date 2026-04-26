package specgen

import (
	"context"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestGenerateHappyPath(t *testing.T) {
	claude := &engine.FakeEngine{
		Name_: "claude",
		Script: []engine.InvokeResponse{
			{Text: "DRAFT_A"},  // stage 1
			{Text: "POLISHED"}, // stage 4
		},
	}
	codex := &engine.FakeEngine{
		Name_: "codex",
		Script: []engine.InvokeResponse{
			{Text: "DRAFT_B"}, // stage 2
			{Text: "MERGED"},  // stage 3
		},
	}

	out, err := Generate(context.Background(), Input{
		UserRequest: "build it",
		Claude:      claude,
		Codex:       codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED" {
		t.Fatalf("Final = %q, want POLISHED", out.Final)
	}
	if out.DraftClaude != "DRAFT_A" || out.DraftCodex != "DRAFT_B" || out.Merged != "MERGED" {
		t.Fatalf("intermediates wrong: claude=%q codex=%q merged=%q", out.DraftClaude, out.DraftCodex, out.Merged)
	}
	if len(out.Stages) != 4 {
		t.Fatalf("Stages len = %d, want 4", len(out.Stages))
	}
	expectedNames := []string{"draft-claude", "draft-codex", "merge", "polish"}
	for i, s := range out.Stages {
		if s.Name != expectedNames[i] {
			t.Fatalf("Stages[%d].Name = %q, want %q", i, s.Name, expectedNames[i])
		}
		if s.Err != "" {
			t.Fatalf("Stages[%d].Err = %q, want empty", i, s.Err)
		}
	}
	// Stage 4's prompt should reference the merged body verbatim.
	stage4Prompt := claude.Received[1].Prompt
	if !strings.Contains(stage4Prompt, "MERGED") {
		t.Fatalf("polish stage prompt did not include merged body; got: %s", stage4Prompt)
	}
}
