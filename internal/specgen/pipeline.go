package specgen

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen/prompts"
)

// Generate runs the 4-stage dual-AI pipeline. Three execution paths:
// both drafts fail (returns the wrapped error and a partial Output);
// one drafter survives (the survivor's draft is polished by the same
// engine, no merge); both drafts succeed (Codex merges, Claude polishes).
// Stage-3 and stage-4 each have their own fallback (longer draft / merged
// version). On success Output.Final is the polished spec. On error
// Output.Final is undefined and callers must not persist it.
func Generate(ctx context.Context, in Input) (Output, error) {
	if in.Claude == nil || in.Codex == nil {
		return Output{}, errors.New("specgen: Claude and Codex engines are required")
	}
	out := Output{}

	priorForTmpl := buildPriorContext(in.PriorTurns)
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
	out.DraftClaude = c.text
	out.DraftCodex = x.text

	claudeOK := c.metric.Err == ""
	codexOK := x.metric.Err == ""

	switch {
	case !claudeOK && !codexOK:
		out.Stages = append(out.Stages,
			StageMetric{Name: "merge", Engine: "codex", Skipped: true},
			StageMetric{Name: "polish", Engine: "claude", Skipped: true},
		)
		persist(in.Recorder, out)
		return out, fmt.Errorf("both drafters failed: claude=%q codex=%q", c.metric.Err, x.metric.Err)

	case !claudeOK || !codexOK:
		var surviving, survName string
		var survEngine engine.Engine
		var failedName string
		if claudeOK {
			surviving, survName, survEngine = c.text, "claude", in.Claude
			failedName = "Codex"
		} else {
			surviving, survName, survEngine = x.text, "codex", in.Codex
			failedName = "Claude"
		}
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("%s draft failed; spec built from %s alone — consider rerunning.", failedName, survName))
		out.Stages = append(out.Stages, StageMetric{
			Name: "merge", Engine: "codex", Skipped: true, Fallback: "single-draft",
		})
		polishPrompt, err := prompts.Render("polish.tmpl", map[string]string{"Merged": surviving})
		if err != nil {
			return out, fmt.Errorf("render polish prompt: %w", err)
		}
		polishedText, m4 := runStage(ctx, "polish", survName, survEngine, polishPrompt, in.OnStageStart, in.OnStageEnd)
		out.Stages = append(out.Stages, m4)
		if m4.Err != "" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("Polish step failed; spec is the surviving draft. (%s)", m4.Err))
			out.Final = surviving
		} else {
			out.Final = polishedText
		}
		persist(in.Recorder, out)
		return out, nil
	}

	// Both drafts succeeded — normal merge + polish path.
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
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("Merge step failed; using longer draft as fallback. (%s)", m3.Err))
		// On exact tie, prefer Codex — it'd have been the merger anyway.
		if len(out.DraftCodex) >= len(out.DraftClaude) {
			out.Merged = out.DraftCodex
		} else {
			out.Merged = out.DraftClaude
		}
		// Mark stage as fallback for the audit trail.
		out.Stages[len(out.Stages)-1].Fallback = "longer-draft"
	} else {
		out.Merged = mergedText
	}

	polishPrompt, err := prompts.Render("polish.tmpl", map[string]string{"Merged": out.Merged})
	if err != nil {
		return out, fmt.Errorf("render polish prompt: %w", err)
	}
	polishedText, m4 := runStage(ctx, "polish", "claude", in.Claude, polishPrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m4)
	if m4.Err != "" {
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("Polish step failed; spec is the merged version. (%s)", m4.Err))
		out.Final = out.Merged
	} else {
		out.Final = polishedText
	}
	persist(in.Recorder, out)
	return out, nil
}

// persist writes intermediate drafts and stage metrics to the recorder.
// Best-effort — never fail the pipeline if a debug write errors.
func persist(rec *run.Recorder, out Output) {
	if rec == nil {
		return
	}
	if out.DraftClaude != "" {
		_ = rec.WriteFile("specgen/draft-claude.md", []byte(out.DraftClaude))
	}
	if out.DraftCodex != "" {
		_ = rec.WriteFile("specgen/draft-codex.md", []byte(out.DraftCodex))
	}
	if out.Merged != "" {
		_ = rec.WriteFile("specgen/merged.md", []byte(out.Merged))
	}
	if out.Final != "" {
		_ = rec.WriteFile("specgen/final.md", []byte(out.Final))
	}
	_ = rec.WriteJSON("specgen/stages.json", out.Stages)
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

// priorContextThreshold is the byte cap for prior-turn material before
// summarization kicks in. Conservative starting value; tunable in code.
const priorContextThreshold = 200 * 1024

// buildPriorContext flattens prior turns for the draft template. If the
// total accumulated size exceeds priorContextThreshold, older turns are
// collapsed into one "summary" entry.
func buildPriorContext(turns []Turn) []map[string]string {
	total := 0
	for _, t := range turns {
		total += len(t.UserMessage) + len(t.FinalSpec)
	}
	if total <= priorContextThreshold {
		out := make([]map[string]string, len(turns))
		for i, t := range turns {
			out[i] = map[string]string{"UserMessage": t.UserMessage}
		}
		return out
	}
	if len(turns) == 0 {
		return nil
	}
	last := turns[len(turns)-1]
	older := turns[:len(turns)-1]
	collapsed := fmt.Sprintf("[prior context summarized: %d earlier turns over %d bytes — see .aios/sessions/<id>/session.json for full history]",
		len(older), total-len(last.UserMessage)-len(last.FinalSpec))
	return []map[string]string{
		{"UserMessage": collapsed},
		{"UserMessage": last.UserMessage},
	}
}
