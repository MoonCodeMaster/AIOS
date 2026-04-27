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
		runCritiqueRefine(ctx, &in, &out, survName)
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
	runCritiqueRefine(ctx, &in, &out, "claude")
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
	if out.Score != nil {
		_ = rec.WriteJSON("specgen/5-score.json", map[string]any{
			"score":  out.Score,
			"issues": out.CritiqueIssues,
		})
	}
	_ = rec.WriteJSON("specgen/stages.json", out.Stages)
}

// runCritiqueRefine runs stages 5 (critique) and optionally 6 (refine) on the
// polished spec. polishEngine is the name of the engine that ran stage 4.
func runCritiqueRefine(ctx context.Context, in *Input, out *Output, polishEngine string) {
	if !in.CritiqueEnabled {
		return
	}
	var critiqueEng engine.Engine
	var critiqueName string
	var refineEng engine.Engine
	var refineName string
	switch polishEngine {
	case "claude":
		critiqueEng, critiqueName = in.Codex, "codex"
		refineEng, refineName = in.Claude, "claude"
	case "codex":
		critiqueEng, critiqueName = in.Claude, "claude"
		refineEng, refineName = in.Codex, "codex"
	default:
		out.Warnings = append(out.Warnings, fmt.Sprintf("critique: unknown polish engine %q; skipping", polishEngine))
		return
	}
	polishedSpec := out.Final
	critiquePrompt, err := prompts.Render("critique.tmpl", map[string]string{"Spec": polishedSpec})
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("critique render: %v", err))
		return
	}
	critiqueText, m5 := runStage(ctx, "critique", critiqueName, critiqueEng, critiquePrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m5)
	if in.Recorder != nil && critiqueText != "" {
		_ = in.Recorder.WriteFile("specgen/5-critique.md", []byte(critiqueText))
	}
	if m5.Err != "" {
		out.Warnings = append(out.Warnings, fmt.Sprintf("critique engine failed: %s", m5.Err))
		return
	}
	score, issues, err := ParseCritiqueOutput(critiqueText)
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("critique parse: %v", err))
		return
	}
	score.Pass = score.Total >= in.CritiqueThreshold
	out.Score = score
	out.CritiqueIssues = issues
	if score.Pass {
		return
	}
	refinePrompt, err := prompts.Render("refine.tmpl", map[string]any{
		"Spec":   polishedSpec,
		"Issues": issues,
	})
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("refine render: %v", err))
		return
	}
	refinedText, m6 := runStage(ctx, "refine", refineName, refineEng, refinePrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m6)
	if in.Recorder != nil && refinedText != "" {
		_ = in.Recorder.WriteFile("specgen/6-refine.md", []byte(refinedText))
	}
	if m6.Err != "" {
		out.Warnings = append(out.Warnings, fmt.Sprintf("refine engine failed: %s; keeping polished spec", m6.Err))
		return
	}
	out.Final = refinedText
	out.Refined = true
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

// Regenerate produces a revised spec from failure feedback. It skips the
// dual-draft stages and instead: (1) calls the non-polish engine with the
// feedback template to produce a feedback-aware draft, (2) merges that with
// the original spec, (3) polishes cross-model, (4) critiques (no refine to
// bound cost). Returns error if the feedback draft or merge fails.
func Regenerate(ctx context.Context, in RegenerateInput) (Output, error) {
	out := Output{}

	// Pick engines: feedback draft runs on the engine NOT used for original polish.
	var feedbackEng engine.Engine
	var feedbackName string
	var polishEng engine.Engine
	var polishName string
	switch in.PolishEngine {
	case "claude":
		feedbackEng, feedbackName = in.Codex, "codex"
		polishEng, polishName = in.Claude, "claude"
	case "codex":
		feedbackEng, feedbackName = in.Claude, "claude"
		polishEng, polishName = in.Codex, "codex"
	default:
		return out, fmt.Errorf("regenerate: unknown polish engine %q", in.PolishEngine)
	}

	// Stage 3': feedback-aware draft.
	fbPrompt, err := prompts.Render("respec-feedback.tmpl", map[string]string{
		"OriginalSpec": in.OriginalSpec,
		"Feedback":     in.Feedback,
	})
	if err != nil {
		return out, fmt.Errorf("render respec-feedback: %w", err)
	}
	fbText, m1 := runStage(ctx, "respec-feedback", feedbackName, feedbackEng, fbPrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m1)
	if m1.Err != "" {
		return out, fmt.Errorf("respec feedback draft: %s", m1.Err)
	}

	// Merge: original spec as draft A, feedback draft as draft B.
	mergePrompt, err := prompts.Render("merge.tmpl", map[string]string{
		"DraftClaude": in.OriginalSpec,
		"DraftCodex":  fbText,
	})
	if err != nil {
		return out, fmt.Errorf("render merge: %w", err)
	}
	mergedText, m2 := runStage(ctx, "respec-merge", feedbackName, feedbackEng, mergePrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m2)
	if m2.Err != "" {
		return out, fmt.Errorf("respec merge: %s", m2.Err)
	}
	out.Merged = mergedText

	// Polish (cross-model relative to feedback engine).
	polishPrompt, err := prompts.Render("polish.tmpl", map[string]string{"Merged": mergedText})
	if err != nil {
		return out, fmt.Errorf("render polish: %w", err)
	}
	polishedText, m3 := runStage(ctx, "respec-polish", polishName, polishEng, polishPrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m3)
	if m3.Err != "" {
		out.Warnings = append(out.Warnings, fmt.Sprintf("respec polish failed: %s; using merged version", m3.Err))
		out.Final = mergedText
	} else {
		out.Final = polishedText
	}

	// Critique (no refine on respec to bound cost).
	if in.CritiqueEnabled {
		// Critique engine = NOT polish engine (same cross-model rule as M3).
		critiquePrompt, err := prompts.Render("critique.tmpl", map[string]string{"Spec": out.Final})
		if err == nil {
			critiqueText, m4 := runStage(ctx, "respec-critique", feedbackName, feedbackEng, critiquePrompt, in.OnStageStart, in.OnStageEnd)
			out.Stages = append(out.Stages, m4)
			if m4.Err == "" {
				score, issues, perr := ParseCritiqueOutput(critiqueText)
				if perr == nil {
					score.Pass = score.Total >= in.CritiqueThreshold
					out.Score = score
					out.CritiqueIssues = issues
					if !score.Pass {
						out.Warnings = append(out.Warnings, fmt.Sprintf("respec critique score %d < threshold %d; skipping refine on respec", score.Total, in.CritiqueThreshold))
					}
				}
			}
		}
	}

	if in.Recorder != nil {
		if out.Final != "" {
			_ = in.Recorder.WriteFile("respec/new-project.md", []byte(out.Final))
		}
	}
	return out, nil
}
