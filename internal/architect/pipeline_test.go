package architect

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

// scriptedEngine returns scripted responses in call order. Errors in errs[i]
// take precedence over returns[i]. Concurrent-safe.
type scriptedEngine struct {
	name    string
	mu      sync.Mutex
	returns []engine.InvokeResponse
	errs    []error
	calls   int
}

func (s *scriptedEngine) Name() string { return s.name }
func (s *scriptedEngine) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i >= len(s.returns) {
		return nil, errors.New("scriptedEngine: script exhausted")
	}
	return &s.returns[i], nil
}

func bp(title, stance string) string {
	b := Blueprint{
		Title: title, Tagline: title + " tagline", Stance: stance,
		MindMap: "- root: " + title, Sketch: "sketch.", DataFlow: "1. step",
		Tradeoff: "- pro: x\n- con: y", Roadmap: "- M1: ship", Risks: "- risk: r | mitigation: m",
	}
	return Render(b)
}

func threeFinalists() string {
	return bp("Conservative one", "conservative") + bp("Balanced one", "balanced") + bp("Ambitious one", "ambitious")
}

func TestRun_HappyPath_ReturnsThreeFinalists(t *testing.T) {
	claude := &scriptedEngine{
		name: "claude",
		returns: []engine.InvokeResponse{
			{Text: bp("Claude A", "conservative") + bp("Claude B", "balanced")}, // R1 propose
			{Text: "critique of codex content"},                                  // R2 critique on codex
			{Text: bp("Claude A r", "conservative") + bp("Claude B r", "balanced")}, // R3 refine
		},
	}
	codex := &scriptedEngine{
		name: "codex",
		returns: []engine.InvokeResponse{
			{Text: bp("Codex C", "ambitious")},      // R1 propose
			{Text: "critique of claude content"},    // R2 critique on claude
			{Text: bp("Codex C r", "ambitious")},    // R3 refine
		},
	}
	synth := &scriptedEngine{
		name:    "codex",
		returns: []engine.InvokeResponse{{Text: threeFinalists()}}, // R4 synthesize
	}
	out, err := Run(context.Background(), Input{
		Idea: "build a thing", Claude: claude, Codex: codex, Synthesizer: synth,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out.Finalists) != 3 {
		t.Fatalf("Finalists = %d, want 3", len(out.Finalists))
	}
	stances := []string{out.Finalists[0].Stance, out.Finalists[1].Stance, out.Finalists[2].Stance}
	wantStances := []string{"conservative", "balanced", "ambitious"}
	for i, want := range wantStances {
		if stances[i] != want {
			t.Errorf("Finalists[%d].Stance = %q, want %q", i, stances[i], want)
		}
	}
	if out.UsedFallback {
		t.Errorf("UsedFallback = true on happy path")
	}
	for _, key := range []string{"1-claude.txt", "1-codex.txt", "3-claude.txt", "3-codex.txt", "4-synthesis.txt"} {
		if _, ok := out.RawArtifacts[key]; !ok {
			t.Errorf("RawArtifacts missing %q", key)
		}
	}
}

func TestRun_FallbackOnSynthesizerError_UsesRefinedPool(t *testing.T) {
	// Pool contains 3 valid blueprints across both authors → fallback succeeds.
	claude := &scriptedEngine{
		name: "claude",
		returns: []engine.InvokeResponse{
			{Text: bp("Claude A", "conservative") + bp("Claude B", "balanced")},
			{Text: "crit"},
			{Text: bp("Claude A r", "conservative") + bp("Claude B r", "balanced")},
		},
	}
	codex := &scriptedEngine{
		name: "codex",
		returns: []engine.InvokeResponse{
			{Text: bp("Codex C", "ambitious")},
			{Text: "crit"},
			{Text: bp("Codex C r", "ambitious")},
		},
	}
	synth := &scriptedEngine{name: "codex", errs: []error{errors.New("network gone")}}
	out, err := Run(context.Background(), Input{Idea: "x", Claude: claude, Codex: codex, Synthesizer: synth})
	if err != nil {
		t.Fatalf("expected fallback success, got err: %v", err)
	}
	if !out.UsedFallback {
		t.Errorf("UsedFallback = false; want true")
	}
	if len(out.Finalists) != 3 {
		t.Errorf("Finalists = %d, want 3", len(out.Finalists))
	}
	if _, ok := out.RawArtifacts["4-synthesis.err"]; !ok {
		t.Errorf("RawArtifacts missing synthesis error key")
	}
}

func TestRun_SynthesisShortAndPoolShort_ReturnsErr(t *testing.T) {
	// Only 2 valid blueprints in the pool, synthesis returns 1 → ErrSynthesisShort.
	claude := &scriptedEngine{
		name: "claude",
		returns: []engine.InvokeResponse{
			{Text: bp("Claude A", "conservative")},
			{Text: ""},
			{Text: bp("Claude A r", "conservative")},
		},
	}
	codex := &scriptedEngine{
		name: "codex",
		returns: []engine.InvokeResponse{
			{Text: bp("Codex C", "ambitious")},
			{Text: ""},
			{Text: bp("Codex C r", "ambitious")},
		},
	}
	synth := &scriptedEngine{name: "codex", returns: []engine.InvokeResponse{{Text: bp("Only One", "balanced")}}}
	_, err := Run(context.Background(), Input{Idea: "x", Claude: claude, Codex: codex, Synthesizer: synth})
	if !errors.Is(err, ErrSynthesisShort) {
		t.Fatalf("err = %v, want ErrSynthesisShort", err)
	}
}

func TestRun_BothProposersErr_ReturnsErr(t *testing.T) {
	claude := &scriptedEngine{name: "claude", errs: []error{errors.New("boom")}}
	codex := &scriptedEngine{name: "codex", errs: []error{errors.New("boom")}}
	synth := &scriptedEngine{name: "codex"}
	_, err := Run(context.Background(), Input{Idea: "x", Claude: claude, Codex: codex, Synthesizer: synth})
	if err == nil || !strings.Contains(err.Error(), "both proposers errored") {
		t.Fatalf("err = %v, want both-proposers error", err)
	}
}

// Regression: Round-1 errors must land in RawArtifacts so the audit
// trail shows WHY a side is empty (vs. the reader assuming the model
// went silent). The other side still produces 3 finalists via synthesis.
func TestRun_OneSideErrors_RecordsErrInArtifacts(t *testing.T) {
	claude := &scriptedEngine{name: "claude", errs: []error{errors.New("transport hung up")}}
	codex := &scriptedEngine{
		name: "codex",
		returns: []engine.InvokeResponse{
			{Text: bp("Codex C", "ambitious")},
			{Text: ""},
			{Text: bp("Codex C r", "ambitious")},
		},
	}
	synth := &scriptedEngine{name: "codex", returns: []engine.InvokeResponse{{Text: threeFinalists()}}}
	out, err := Run(context.Background(), Input{Idea: "x", Claude: claude, Codex: codex, Synthesizer: synth})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := out.RawArtifacts["1-claude.err"]; got == "" || !strings.Contains(got, "transport hung up") {
		t.Errorf("RawArtifacts[1-claude.err] = %q; want it to record the error", got)
	}
}

func TestRun_NilInput_ReturnsErr(t *testing.T) {
	_, err := Run(context.Background(), Input{Idea: ""})
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}
