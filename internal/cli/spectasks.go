package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
)

// writeTaskFiles parses a decompose-prompt response (===TASK=== separated)
// and writes one .md file per task under tasksDir. Returns the count written.
func writeTaskFiles(dir, raw string) (int, error) {
	parts := strings.Split(raw, "\n===TASK===\n")
	count := 0
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id := extractTaskID(p)
		if id == "" {
			continue
		}
		path := filepath.Join(dir, id+".md")
		if err := os.WriteFile(path, []byte(p+"\n"), 0o644); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// extractTaskID pulls the `id:` field from a task frontmatter block.
func extractTaskID(frontmatter string) string {
	for _, ln := range strings.Split(frontmatter, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "id:") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "id:"))
		}
	}
	return ""
}

// commitSpec stashes any uncommitted edits, switches to the staging branch,
// stages .aios/, and commits with a one-line message describing the source.
// (Renamed from commitNewSpec — no longer "new"-specific.)
func commitSpec(wd, staging, source string) error {
	stash := exec.Command("git", "-C", wd, "stash", "-u")
	_ = stash.Run()
	chk := exec.Command("git", "-C", wd, "checkout", staging)
	if err := chk.Run(); err != nil {
		return err
	}
	add := exec.Command("git", "-C", wd, "add", ".aios")
	if err := add.Run(); err != nil {
		return err
	}
	msg := "aios: spec and tasks for " + source
	return exec.Command("git", "-C", wd, "commit", "-m", msg).Run()
}

// decomposeOnly turns the existing .aios/project.md into task files via
// codex's decompose prompt, writes them under .aios/tasks/, and commits
// the result to the staging branch. Used by both ShipSpec and the REPL.
func decomposeOnly(wd string) error {
	specPath := filepath.Join(wd, ".aios", "project.md")
	specBody, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read project.md: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return err
	}
	codex := &engine.CodexEngine{
		Binary:     cfg.Engines.Codex.Binary,
		ExtraArgs:  cfg.Engines.Codex.ExtraArgs,
		TimeoutSec: cfg.Engines.Codex.TimeoutSec,
	}
	dPrompt, err := prompts.Render("decompose.tmpl", map[string]string{"Spec": string(specBody)})
	if err != nil {
		return err
	}
	dRes, err := codex.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: dPrompt})
	if err != nil {
		return err
	}
	tasksDir := filepath.Join(wd, ".aios", "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return err
	}
	if _, err := writeTaskFiles(tasksDir, dRes.Text); err != nil {
		return err
	}
	return commitSpec(wd, cfg.Project.StagingBranch, "interactive session")
}

// ShipStatus is the outcome of a ship pipeline run.
type ShipStatus int

const (
	ShipUnknown ShipStatus = iota
	ShipMerged
	ShipPRRed
	ShipAbandoned
)

// ShipResult is the structured outcome of one ShipSpec or ShipPrompt run.
type ShipResult struct {
	Status     ShipStatus
	PRURL      string
	PRNumber   int
	AuditTrail string
}

// ShipPromptInput bundles the inputs to ShipPrompt. Engines and the
// (optional) ShipSpecFn override are injectable for tests.
type ShipPromptInput struct {
	Wd                string
	Prompt            string
	Claude            engine.Engine
	Codex             engine.Engine
	ShipSpecFn        func(ctx context.Context, wd string) (ShipResult, error) // nil = use real ShipSpec
	OnStage           func(name string)                                        // optional progress callback for specgen stages
	CritiqueEnabled   bool
	CritiqueThreshold int
}

// ShipPrompt runs specgen.Generate on the prompt, writes the polished
// spec to .aios/project.md, then calls ShipSpec to decompose+execute.
// On specgen failure, project.md is NOT written and the error is returned.
func ShipPrompt(ctx context.Context, in ShipPromptInput) (ShipResult, error) {
	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(in.Wd, ".aios", "runs"), runID)
	if err != nil {
		return ShipResult{}, fmt.Errorf("open run dir: %w", err)
	}
	out, err := specgen.Generate(ctx, specgen.Input{
		UserRequest:       in.Prompt,
		Claude:            in.Claude,
		Codex:             in.Codex,
		Recorder:          rec,
		CritiqueEnabled:   in.CritiqueEnabled,
		CritiqueThreshold: in.CritiqueThreshold,
		OnStageStart:      in.OnStage,
	})
	if err != nil {
		return ShipResult{}, fmt.Errorf("specgen: %w", err)
	}
	specPath := filepath.Join(in.Wd, ".aios", "project.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		return ShipResult{}, err
	}
	if err := os.WriteFile(specPath, []byte(out.Final), 0o644); err != nil {
		return ShipResult{}, err
	}
	if in.ShipSpecFn != nil {
		return in.ShipSpecFn(ctx, in.Wd)
	}
	return ShipSpec(ctx, in.Wd)
}

// ShipSpec decomposes the existing .aios/project.md into task files,
// then runs `aios run --autopilot --merge` in-process. Returns a
// structured result parsed from the run's autopilot-summary.md.
func ShipSpec(ctx context.Context, wd string) (ShipResult, error) {
	if err := decomposeOnly(wd); err != nil {
		return ShipResult{}, fmt.Errorf("decompose: %w", err)
	}
	runCmd := newRunCmd()
	if err := runCmd.Flags().Set("autopilot", "true"); err != nil {
		return ShipResult{}, fmt.Errorf("set --autopilot: %w", err)
	}
	if err := runCmd.Flags().Set("merge", "true"); err != nil {
		return ShipResult{}, fmt.Errorf("set --merge: %w", err)
	}
	if err := runMain(runCmd, nil); err != nil {
		// Even on error, parse the summary if it landed.
		if res, perr := parseLatestShipSummary(wd); perr == nil {
			return res, err
		}
		return ShipResult{}, err
	}
	return parseLatestShipSummary(wd)
}

// parseLatestShipSummary reads the autopilot-summary.md from the most
// recently created .aios/runs/<id>/ directory and parses it into a
// ShipResult. Same parser shape as the previous serve-side parser.
func parseLatestShipSummary(wd string) (ShipResult, error) {
	runsDir := filepath.Join(wd, ".aios", "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return ShipResult{}, fmt.Errorf("read runs dir: %w", err)
	}
	var newest string
	for _, e := range entries {
		if e.IsDir() && e.Name() > newest {
			newest = e.Name()
		}
	}
	if newest == "" {
		return ShipResult{}, fmt.Errorf("no runs found under %s", runsDir)
	}
	body, err := os.ReadFile(filepath.Join(runsDir, newest, "autopilot-summary.md"))
	if err != nil {
		return ShipResult{}, fmt.Errorf("read autopilot-summary.md: %w", err)
	}
	return parseShipSummary(string(body))
}

func parseShipSummary(body string) (ShipResult, error) {
	res := ShipResult{Status: ShipUnknown}
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "PR: "):
			res.PRURL = strings.TrimPrefix(ln, "PR: ")
			parts := strings.Split(res.PRURL, "/")
			if len(parts) > 0 {
				_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &res.PRNumber)
			}
		case strings.HasPrefix(ln, "Merged: true"):
			res.Status = ShipMerged
		case strings.HasPrefix(ln, "Merged: false"):
			res.Status = ShipPRRed
		case strings.Contains(ln, "all tasks abandoned") || strings.Contains(ln, "Skipped: no converged tasks"):
			res.Status = ShipAbandoned
			res.AuditTrail = body
		}
	}
	if res.Status == ShipUnknown {
		return res, fmt.Errorf("autopilot-summary.md did not yield a recognised status:\n%s", body)
	}
	return res, nil
}
