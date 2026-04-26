package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/architect"
	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/spf13/cobra"
)

// newArchitectCmd is the user-facing front door:
//
//	aios architect "<idea>"
//
// It runs Claude+Codex through the propose→critique→refine→synthesize
// pipeline, prints three distinct mind-map blueprints, takes a single
// selection from the user, then chains straight into `aios autopilot` with
// the chosen blueprint as the spec. Zero further prompts.
func newArchitectCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "architect <idea>",
		Short: "Multi-round mind-map planner: 3 blueprints, you pick one, AIOS ships it",
		Long: `aios architect drives the full mind-map → ship lifecycle for one idea
with at most one human keystroke in the middle (the blueprint pick).

Pipeline:

  1. Round 1 — Claude and Codex independently propose blueprints.
  2. Round 2 — each model critiques the OTHER's blueprints.
  3. Round 3 — each author refines its own blueprints from the critique.
  4. Round 4 — synthesizer (= reviewer-default engine) emits exactly three
     finalists labelled conservative / balanced / ambitious.

You pick 1, 2, or 3. AIOS then converts the chosen blueprint into the
project spec, decomposes it into tasks, and runs the standard autopilot
loop (open PR → wait for CI → squash-merge on green) without any further
prompts.

Flags:

  --pick N   skip the prompt and select blueprint N (1..3)
  --auto     equivalent to --pick 1; intended for fully unattended use

Requires: gh CLI on PATH, an authenticated gh session, a configured git remote.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pick, _ := cmd.Flags().GetInt("pick")
			auto, _ := cmd.Flags().GetBool("auto")
			if auto && pick == 0 {
				pick = 1
			}
			if pick < 0 || pick > 3 {
				return fmt.Errorf("--pick must be 1, 2, or 3 (got %d)", pick)
			}
			idea := strings.Join(args, " ")
			return runArchitect(cmd.Context(), idea, pick)
		},
	}
	c.Flags().Int("pick", 0, "skip the selection prompt; pick blueprint 1, 2, or 3")
	c.Flags().Bool("auto", false, "fully unattended; equivalent to --pick 1")
	return c
}

func runArchitect(ctx context.Context, idea string, pick int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("run `aios init` first: %w", err)
	}
	// Same preflight as autopilot — the chain ends in a PR + merge.
	if err := newAutopilotPreflight(wd).Check(); err != nil {
		return err
	}

	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(wd, ".aios", "runs"), runID)
	if err != nil {
		return err
	}

	claude := &engine.ClaudeEngine{
		Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec,
	}
	codex := &engine.CodexEngine{
		Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec,
	}
	// Synthesizer = the project's reviewer-default engine. This keeps the
	// "writer != reviewer" invariant intact: whichever engine wrote the
	// majority of proposals is NOT the one consolidating them.
	var synth engine.Engine = codex
	if cfg.Engines.ReviewerDefault == "claude" {
		synth = claude
	}

	fmt.Printf("architect: running 4-round pipeline for %q\n", idea)
	fmt.Println("architect: round 1 — Claude + Codex propose blueprints in parallel…")

	out, err := architect.Run(ctx, architect.Input{
		Idea: idea, Claude: claude, Codex: codex, Synthesizer: synth,
	})
	if persistErr := persistArchitectArtifacts(rec, out.RawArtifacts); persistErr != nil {
		fmt.Fprintf(os.Stderr, "warn: persist architect artifacts: %v\n", persistErr)
	}
	if err != nil {
		return fmt.Errorf("architect pipeline: %w", err)
	}
	if out.UsedFallback {
		fmt.Println("architect: synthesis fell back to refined pool (synthesizer errored or returned <3); finalists may be less distinct than usual.")
	}

	chosen, err := chooseBlueprint(out.Finalists, pick, os.Stdin)
	if err != nil {
		return err
	}
	fmt.Printf("\narchitect: blueprint %d selected — %q (%s)\n\n", chosen.Index, chosen.Blueprint.Title, chosen.Blueprint.Stance)

	// Persist the chosen blueprint for audit/restart.
	_ = rec.WriteFile("architect/chosen.txt", []byte(architect.Render(chosen.Blueprint)))

	if err := writeBlueprintAsSpec(ctx, wd, idea, chosen.Blueprint, claude, codex); err != nil {
		return err
	}
	fmt.Printf("architect: spec + tasks committed to %s\n", cfg.Project.StagingBranch)

	// Chain into autopilot+merge. We construct a fresh runCmd with both
	// flags set so the rest of the lifecycle is identical to
	// `aios autopilot`. No subprocess — direct call into runMain.
	runCmd := newRunCmd()
	if err := runCmd.Flags().Set("autopilot", "true"); err != nil {
		return fmt.Errorf("internal: set --autopilot: %w", err)
	}
	if err := runCmd.Flags().Set("merge", "true"); err != nil {
		return fmt.Errorf("internal: set --merge: %w", err)
	}
	return runMain(runCmd, nil)
}

// pickedBlueprint pairs the chosen blueprint with its 1-based index, so the
// caller can log which slot won without recomputing it.
type pickedBlueprint struct {
	Index     int
	Blueprint architect.Blueprint
}

// chooseBlueprint prints the three finalists, then either honours `pick` or
// reads a number from r. r is parameterised so tests can drive it.
func chooseBlueprint(finalists []architect.Blueprint, pick int, r io.Reader) (pickedBlueprint, error) {
	if len(finalists) != 3 {
		return pickedBlueprint{}, fmt.Errorf("expected 3 finalists, got %d", len(finalists))
	}
	for i, bp := range finalists {
		fmt.Println(architect.RenderForUser(i+1, bp))
	}
	if pick > 0 {
		return pickedBlueprint{Index: pick, Blueprint: finalists[pick-1]}, nil
	}
	fmt.Print("\nPick blueprint [1/2/3]: ")
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return pickedBlueprint{}, fmt.Errorf("read selection: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > 3 {
		return pickedBlueprint{}, fmt.Errorf("selection must be 1, 2, or 3 (got %q)", strings.TrimSpace(line))
	}
	return pickedBlueprint{Index: n, Blueprint: finalists[n-1]}, nil
}

// writeBlueprintAsSpec converts the chosen blueprint into .aios/project.md
// (via bp-to-spec) and into .aios/tasks/*.md (via the existing
// decompose.tmpl pipeline). It then commits the result on the staging
// branch using the same helpers `aios new` uses, so downstream tools see
// identical state regardless of whether the user came in via `new` or
// `architect`.
func writeBlueprintAsSpec(ctx context.Context, wd, idea string, bp architect.Blueprint, claude, codex engine.Engine) error {
	specPrompt, err := prompts.Render("bp-to-spec.tmpl", map[string]string{
		"Idea":      idea,
		"Blueprint": architect.Render(bp),
	})
	if err != nil {
		return fmt.Errorf("render bp-to-spec: %w", err)
	}
	specResp, err := claude.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: specPrompt})
	if err != nil {
		return fmt.Errorf("bp-to-spec invoke: %w", err)
	}
	specPath := filepath.Join(wd, ".aios", "project.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(specPath, []byte(specResp.Text), 0o644); err != nil {
		return err
	}

	dPrompt, err := prompts.Render("decompose.tmpl", map[string]string{"Spec": specResp.Text})
	if err != nil {
		return fmt.Errorf("render decompose: %w", err)
	}
	dResp, err := codex.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: dPrompt})
	if err != nil {
		return fmt.Errorf("decompose invoke: %w", err)
	}
	tasksDir := filepath.Join(wd, ".aios", "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return err
	}
	if _, err := writeTaskFiles(tasksDir, dResp.Text); err != nil {
		return err
	}

	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return err
	}
	return commitNewSpec(wd, cfg.Project.StagingBranch, idea+" (architect)")
}

func persistArchitectArtifacts(rec *run.Recorder, artifacts map[string]string) error {
	for name, body := range artifacts {
		if err := rec.WriteFile(filepath.Join("architect", name), []byte(body)); err != nil {
			return err
		}
	}
	return nil
}
