package specgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/specgen/prompts"
)

func Generate(ctx context.Context, in Input) (Output, error) {
	if in.Claude == nil || in.Codex == nil {
		return Output{}, errors.New("specgen: Claude and Codex engines are required")
	}
	out := Output{}

	priorForTmpl := make([]map[string]string, len(in.PriorTurns))
	for i, t := range in.PriorTurns {
		priorForTmpl[i] = map[string]string{"UserMessage": t.UserMessage}
	}
	draftPrompt, err := prompts.Render("draft.tmpl", map[string]any{
		"UserRequest":    in.UserRequest,
		"CurrentSpec":    in.CurrentSpec,
		"PriorTurns":     priorForTmpl,
		"ProjectContext": in.ProjectContext,
	})
	if err != nil {
		return out, fmt.Errorf("render draft prompt: %w", err)
	}

	// Stages 1 and 2 in parallel.
	type draftResult struct {
		text   string
		metric StageMetric
	}
	claudeCh := make(chan draftResult, 1)
	codexCh := make(chan draftResult, 1)
	go func() {
		text, m := runStage(ctx, "draft-claude", "claude", in.Claude, draftPrompt, in.OnStageStart, in.OnStageEnd)
		claudeCh <- draftResult{text, m}
	}()
	go func() {
		text, m := runStage(ctx, "draft-codex", "codex", in.Codex, draftPrompt, in.OnStageStart, in.OnStageEnd)
		codexCh <- draftResult{text, m}
	}()
	c := <-claudeCh
	x := <-codexCh
	out.Stages = append(out.Stages, c.metric, x.metric)
	if c.metric.Err != "" {
		return out, fmt.Errorf("stage draft-claude: %s", c.metric.Err)
	}
	if x.metric.Err != "" {
		return out, fmt.Errorf("stage draft-codex: %s", x.metric.Err)
	}
	out.DraftClaude = c.text
	out.DraftCodex = x.text

	// Stage 3: Codex merge
	mergePrompt, err := prompts.Render("merge.tmpl", map[string]string{
		"DraftClaude": out.DraftClaude,
		"DraftCodex":  out.DraftCodex,
	})
	if err != nil {
		return out, fmt.Errorf("render merge prompt: %w", err)
	}
	mergedText, m3 := runStage(ctx, "merge", "codex", in.Codex, mergePrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m3)
	if m3.Err != "" {
		return out, fmt.Errorf("stage merge: %s", m3.Err)
	}
	out.Merged = mergedText

	// Stage 4: Claude polish
	polishPrompt, err := prompts.Render("polish.tmpl", map[string]string{"Merged": out.Merged})
	if err != nil {
		return out, fmt.Errorf("render polish prompt: %w", err)
	}
	polishedText, m4 := runStage(ctx, "polish", "claude", in.Claude, polishPrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m4)
	if m4.Err != "" {
		return out, fmt.Errorf("stage polish: %s", m4.Err)
	}
	out.Final = polishedText

	if in.Recorder != nil {
		_ = in.Recorder.WriteFile("specgen/draft-claude.md", []byte(out.DraftClaude))
		_ = in.Recorder.WriteFile("specgen/draft-codex.md", []byte(out.DraftCodex))
		_ = in.Recorder.WriteFile("specgen/merged.md", []byte(out.Merged))
		_ = in.Recorder.WriteFile("specgen/final.md", []byte(out.Final))
		if data, err := json.MarshalIndent(out.Stages, "", "  "); err == nil {
			_ = in.Recorder.WriteFile("specgen/stages.json", data)
		}
	}

	return out, nil
}

func runStage(ctx context.Context, name, engineName string, eng engine.Engine, prompt string,
	onStart func(string), onEnd func(string, error)) (string, StageMetric) {
	if onStart != nil {
		onStart(name)
	}
	t0 := time.Now()
	resp, err := eng.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: prompt})
	if onEnd != nil {
		onEnd(name, err)
	}
	m := StageMetric{Name: name, Engine: engineName, DurationMs: int(time.Since(t0).Milliseconds())}
	if err != nil {
		m.Err = err.Error()
		return "", m
	}
	m.TokensUsed = resp.UsageTokens
	return resp.Text, m
}
