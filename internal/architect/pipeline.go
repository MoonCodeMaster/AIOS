package architect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
)

// ErrSynthesisShort means the synthesis stage returned fewer than three
// parseable blueprints. Caller decides whether to fall back, retry, or
// abort.
var ErrSynthesisShort = errors.New("architect: synthesis produced fewer than 3 finalists")

// Input is everything Run needs to drive a 4-round mind-map session.
//
// Claude is asked for two of the three independent proposals (it has been
// the better divergent generator in our tests); Codex contributes the third.
// Synthesizer decides the final three. The CLI wires Synthesizer to the
// project's reviewer-default engine so the synthesis call is always handled
// by the model that did NOT write the majority of the proposals — a small
// extra cross-model check on top of the explicit critique round.
type Input struct {
	Idea        string
	Claude      engine.Engine
	Codex       engine.Engine
	Synthesizer engine.Engine
}

// Output carries the three finalists plus every raw model response keyed by
// "<round>-<author>". The CLI persists RawArtifacts under
// .aios/runs/<id>/architect/ so the user can audit every step.
type Output struct {
	Finalists    []Blueprint
	RawArtifacts map[string]string
	UsedFallback bool // true when synthesis errored and we returned the refined pool directly
}

// Run executes the four-round pipeline. Errors from any individual model
// call are surfaced (caller may retry); the only "soft" path is synthesis
// returning a short list, which is signalled via ErrSynthesisShort so the
// CLI can fall back to the refined pool.
func Run(ctx context.Context, in Input) (Output, error) {
	if in.Idea == "" || in.Claude == nil || in.Codex == nil || in.Synthesizer == nil {
		return Output{}, errors.New("architect.Run: nil engine or empty idea")
	}
	out := Output{RawArtifacts: map[string]string{}}

	// ---------- Round 1: independent proposals (parallel) ----------
	var (
		claudeProp, codexProp string
		claudeErr, codexErr   error
		wg                    sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		p, err := renderPropose(in.Idea, "claude", 2)
		if err != nil {
			claudeErr = err
			return
		}
		r, err := in.Claude.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: p})
		if err != nil {
			claudeErr = err
			return
		}
		claudeProp = r.Text
	}()
	go func() {
		defer wg.Done()
		p, err := renderPropose(in.Idea, "codex", 1)
		if err != nil {
			codexErr = err
			return
		}
		r, err := in.Codex.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: p})
		if err != nil {
			codexErr = err
			return
		}
		codexProp = r.Text
	}()
	wg.Wait()
	if claudeErr != nil && codexErr != nil {
		return out, fmt.Errorf("architect: both proposers errored: claude=%v, codex=%v", claudeErr, codexErr)
	}
	out.RawArtifacts["1-claude.txt"] = claudeProp
	out.RawArtifacts["1-codex.txt"] = codexProp

	// ---------- Round 2: cross-critique (parallel) ----------
	// Codex critiques Claude's blueprints; Claude critiques Codex's.
	var critOnClaude, critOnCodex string
	var critOnClaudeErr, critOnCodexErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		if claudeProp == "" {
			return
		}
		p, err := renderCritique(in.Idea, claudeProp)
		if err != nil {
			critOnClaudeErr = err
			return
		}
		r, err := in.Codex.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: p})
		if err != nil {
			critOnClaudeErr = err
			return
		}
		critOnClaude = r.Text
	}()
	go func() {
		defer wg.Done()
		if codexProp == "" {
			return
		}
		p, err := renderCritique(in.Idea, codexProp)
		if err != nil {
			critOnCodexErr = err
			return
		}
		r, err := in.Claude.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: p})
		if err != nil {
			critOnCodexErr = err
			return
		}
		critOnCodex = r.Text
	}()
	wg.Wait()
	// Critique errors are non-fatal — refinement just runs without notes.
	if critOnClaudeErr != nil {
		out.RawArtifacts["2-codex-on-claude.err"] = critOnClaudeErr.Error()
	}
	if critOnCodexErr != nil {
		out.RawArtifacts["2-claude-on-codex.err"] = critOnCodexErr.Error()
	}
	out.RawArtifacts["2-codex-on-claude.txt"] = critOnClaude
	out.RawArtifacts["2-claude-on-codex.txt"] = critOnCodex

	// ---------- Round 3: refinement (parallel per author) ----------
	var refinedClaude, refinedCodex string
	wg.Add(2)
	go func() {
		defer wg.Done()
		if claudeProp == "" {
			return
		}
		p, err := renderRefine(in.Idea, "claude", claudeProp, critOnClaude)
		if err != nil {
			refinedClaude = claudeProp // fall back to round-1 output
			return
		}
		r, err := in.Claude.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: p})
		if err != nil {
			refinedClaude = claudeProp
			return
		}
		refinedClaude = r.Text
	}()
	go func() {
		defer wg.Done()
		if codexProp == "" {
			return
		}
		p, err := renderRefine(in.Idea, "codex", codexProp, critOnCodex)
		if err != nil {
			refinedCodex = codexProp
			return
		}
		r, err := in.Codex.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: p})
		if err != nil {
			refinedCodex = codexProp
			return
		}
		refinedCodex = r.Text
	}()
	wg.Wait()
	out.RawArtifacts["3-claude.txt"] = refinedClaude
	out.RawArtifacts["3-codex.txt"] = refinedCodex

	pool := joinNonEmpty(refinedClaude, refinedCodex)

	// ---------- Round 4: synthesis (single call) ----------
	synthPrompt, err := renderSynthesize(in.Idea, pool)
	if err != nil {
		return out, fmt.Errorf("render synthesis prompt: %w", err)
	}
	synthResp, synthErr := in.Synthesizer.Invoke(ctx, engine.InvokeRequest{
		Role:   engine.RoleReviewer,
		Prompt: synthPrompt,
	})
	if synthErr != nil {
		// Fall back to whatever the refined pool already contains.
		fallback := ParseBlueprints(pool)
		if len(fallback) >= 3 {
			out.Finalists = fallback[:3]
			out.UsedFallback = true
			out.RawArtifacts["4-synthesis.err"] = synthErr.Error()
			return out, nil
		}
		return out, fmt.Errorf("synthesis failed and refined pool has %d (<3) blueprints: %w", len(fallback), synthErr)
	}
	out.RawArtifacts["4-synthesis.txt"] = synthResp.Text
	finalists := ParseBlueprints(synthResp.Text)
	if len(finalists) < 3 {
		// Same fallback policy: prefer pool over a partial synthesis.
		fallback := ParseBlueprints(pool)
		if len(fallback) >= 3 {
			out.Finalists = fallback[:3]
			out.UsedFallback = true
			return out, nil
		}
		return out, ErrSynthesisShort
	}
	out.Finalists = finalists[:3]
	return out, nil
}

func renderPropose(idea, authorTag string, count int) (string, error) {
	return prompts.Render("bp-propose.tmpl", map[string]any{
		"Idea":      idea,
		"AuthorTag": authorTag,
		"Count":     count,
	})
}

func renderCritique(idea, blueprints string) (string, error) {
	return prompts.Render("bp-critique.tmpl", map[string]any{
		"Idea":       idea,
		"Blueprints": blueprints,
	})
}

func renderRefine(idea, authorTag, blueprints, critiques string) (string, error) {
	return prompts.Render("bp-refine.tmpl", map[string]any{
		"Idea":       idea,
		"AuthorTag":  authorTag,
		"Blueprints": blueprints,
		"Critiques":  critiques,
	})
}

func renderSynthesize(idea, pool string) (string, error) {
	return prompts.Render("bp-synthesize.tmpl", map[string]any{
		"Idea": idea,
		"Pool": pool,
	})
}

func joinNonEmpty(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "\n\n")
}
