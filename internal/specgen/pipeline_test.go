package specgen

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
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

// multiTimingEngine returns scripted responses in order; each Invoke call
// records its start time and shares the same delay.
type multiTimingEngine struct {
	name      string
	delay     time.Duration
	responses []string
	startedAt []int64
	mu        sync.Mutex
	idx       int
}

func (e *multiTimingEngine) Name() string { return e.name }
func (e *multiTimingEngine) Invoke(ctx context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	e.mu.Lock()
	if e.idx >= len(e.responses) {
		e.mu.Unlock()
		return nil, errors.New("multiTimingEngine exhausted")
	}
	now := time.Now().UnixNano()
	e.startedAt = append(e.startedAt, now)
	r := e.responses[e.idx]
	e.idx++
	e.mu.Unlock()
	time.Sleep(e.delay)
	return &engine.InvokeResponse{Text: r}, nil
}

func TestGenerateDraftsConcurrent(t *testing.T) {
	claude := &multiTimingEngine{name: "claude", delay: 80 * time.Millisecond, responses: []string{"DRAFT_A", "POLISHED"}}
	codex := &multiTimingEngine{name: "codex", delay: 80 * time.Millisecond, responses: []string{"DRAFT_B", "MERGED"}}

	start := time.Now()
	out, err := Generate(context.Background(), Input{UserRequest: "x", Claude: claude, Codex: codex})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED" {
		t.Fatalf("Final = %q", out.Final)
	}
	// Sequential lower bound: 4 * 80ms = 320ms. With stages 1+2 parallel,
	// expect ~3 * 80ms = 240ms. Fail only above 350ms (proves sequential).
	if elapsed > 350*time.Millisecond {
		t.Fatalf("Generate took %v — stages 1 and 2 ran sequentially (expected parallel)", elapsed)
	}
	// Both first-call start times should be within 30ms of each other.
	if len(claude.startedAt) < 1 || len(codex.startedAt) < 1 {
		t.Fatalf("missing start times")
	}
	skew := claude.startedAt[0] - codex.startedAt[0]
	if skew < 0 {
		skew = -skew
	}
	if skew > int64(30*time.Millisecond) {
		t.Fatalf("draft start skew = %v, want < 30ms", time.Duration(skew))
	}
}

func TestGeneratePersistsIntermediates(t *testing.T) {
	dir := t.TempDir()
	rec, err := run.Open(dir, "test-run")
	if err != nil {
		t.Fatalf("run.Open: %v", err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}

	_, err = Generate(context.Background(), Input{
		UserRequest: "x", Claude: claude, Codex: codex, Recorder: rec,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := map[string]string{
		"specgen/draft-claude.md": "DRAFT_A",
		"specgen/draft-codex.md":  "DRAFT_B",
		"specgen/merged.md":       "MERGED",
		"specgen/final.md":        "POLISHED",
	}
	for rel, body := range want {
		p := filepath.Join(dir, "test-run", rel)
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != body {
			t.Fatalf("%s = %q, want %q", rel, got, body)
		}
	}

	stagesPath := filepath.Join(dir, "test-run", "specgen", "stages.json")
	raw, err := os.ReadFile(stagesPath)
	if err != nil {
		t.Fatalf("read stages.json: %v", err)
	}
	var stages []StageMetric
	if err := json.Unmarshal(raw, &stages); err != nil {
		t.Fatalf("unmarshal stages.json: %v", err)
	}
	if len(stages) != 4 {
		t.Fatalf("stages.json had %d entries, want 4", len(stages))
	}
}

// errEngine returns the same error on every call.
type errEngine struct {
	name string
	err  error
}

func (e *errEngine) Name() string { return e.name }
func (e *errEngine) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	return nil, e.err
}

func TestGenerateClaudeDraftFailsThenSingleDraftFlow(t *testing.T) {
	claude := &errEngine{name: "claude", err: errors.New("claude offline")}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"},
		{Text: "POLISHED_BY_CODEX"}, // codex stands in for polish too
	}}

	out, err := Generate(context.Background(), Input{
		UserRequest: "x", Claude: claude, Codex: codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED_BY_CODEX" {
		t.Fatalf("Final = %q, want POLISHED_BY_CODEX", out.Final)
	}
	if out.DraftCodex != "DRAFT_B" {
		t.Fatalf("DraftCodex = %q", out.DraftCodex)
	}
	if out.DraftClaude != "" {
		t.Fatalf("DraftClaude should be empty when Claude failed; got %q", out.DraftClaude)
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "Claude") {
		t.Fatalf("expected warning about Claude failure; got %v", out.Warnings)
	}
	stagesByName := map[string]StageMetric{}
	for _, s := range out.Stages {
		stagesByName[s.Name] = s
	}
	if s := stagesByName["draft-claude"]; s.Err == "" {
		t.Fatalf("draft-claude stage Err should be non-empty")
	}
	if s := stagesByName["merge"]; !s.Skipped {
		t.Fatalf("merge stage should be Skipped when only one draft survives")
	}
}

func TestGenerateCodexDraftFailsThenSingleDraftFlow(t *testing.T) {
	codex := &errEngine{name: "codex", err: errors.New("codex offline")}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"},
		{Text: "POLISHED_BY_CLAUDE"},
	}}

	out, err := Generate(context.Background(), Input{
		UserRequest: "x", Claude: claude, Codex: codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED_BY_CLAUDE" {
		t.Fatalf("Final = %q", out.Final)
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "Codex") {
		t.Fatalf("expected warning about Codex failure; got %v", out.Warnings)
	}
}
