package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
)

// respecAttemptCap is the maximum number of spec regenerations per ShipSpec
// invocation. Cap is 1: if respec doesn't recover the run, surface the
// failure rather than burning more engine calls.
const respecAttemptCap = 1

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
func decomposeOnly(ctx context.Context, wd string) error {
	cfg, err := MustConfigFromContext(ctx)
	if err != nil {
		return err
	}
	specPath := filepath.Join(wd, ".aios", "project.md")
	specBody, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read project.md: %w", err)
	}
	codex := &engine.CodexEngine{
		Binary:     cfg.Engines.Codex.Binary,
		ExtraArgs:  cfg.Engines.Codex.ExtraArgs,
		TimeoutSec: cfg.Engines.Codex.TimeoutSec,
		Retry:      retryPolicyFrom(cfg.Engines.Codex),
	}
	dPrompt, err := prompts.Render("decompose.tmpl", map[string]string{"Spec": string(specBody)})
	if err != nil {
		return err
	}
	dRes, err := codex.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: dPrompt})
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
	Status          ShipStatus
	PRURL           string
	PRNumber        int
	AuditTrail      string
	RespecAttempted bool
	RespecOutcome   string
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
// structured result parsed from the run's autopilot-summary.md. When
// sibling tasks abandon with overlapping reviewer-issue fingerprints
// (Jaccard >= cfg.Budget.RespecMinOverlapScore), the spec is regenerated
// from failure feedback and the run retries once.
func ShipSpec(ctx context.Context, wd string) (ShipResult, error) {
	return shipSpecAttempt(ctx, wd, 0)
}

func shipSpecAttempt(ctx context.Context, wd string, attempt int) (ShipResult, error) {
	if err := decomposeOnly(ctx, wd); err != nil {
		return ShipResult{}, fmt.Errorf("decompose: %w", err)
	}

	var (
		mu       sync.Mutex
		captured = make(map[string]orchestrator.Outcome)
	)
	setTaskOutcomeRecorder(func(id string, oc *orchestrator.Outcome) {
		mu.Lock()
		captured[id] = *oc
		mu.Unlock()
	})
	defer setTaskOutcomeRecorder(nil)

	runCmd := newRunCmd()
	runCmd.SetContext(ctx)
	if err := runCmd.Flags().Set("autopilot", "true"); err != nil {
		return ShipResult{}, fmt.Errorf("set --autopilot: %w", err)
	}
	if err := runCmd.Flags().Set("merge", "true"); err != nil {
		return ShipResult{}, fmt.Errorf("set --merge: %w", err)
	}
	runErr := runMain(runCmd, nil)

	res, parseErr := parseLatestShipSummary(wd)

	// Decide whether to respec. Failures here never propagate as runtime
	// errors — the original run result is authoritative; respec is best-effort.
	if attempt < respecAttemptCap {
		cfg, cfgErr := MustConfigFromContext(ctx)
		if cfgErr == nil {
			abandons, ids := collectAbandons(captured, &mu)
			respecCfg := respecConfig{
				Enabled:    cfg.Budget.RespecEnabled(),
				MinOverlap: cfg.Budget.RespecMinOverlapScore,
			}
			alreadyRespecedThisSpec := respecMarkedForCurrentSpec(wd)
			if !alreadyRespecedThisSpec && shouldRespec(abandons, respecCfg, attempt) {
				if applyErr := applyRespec(ctx, wd, cfg, abandons, ids); applyErr == nil {
					res2, err2 := shipSpecAttempt(ctx, wd, attempt+1)
					res2.RespecAttempted = true
					res2.RespecOutcome = describeRespecOutcome(res2.Status)
					return res2, err2
				} else {
					if parseErr == nil {
						res.RespecAttempted = true
						res.RespecOutcome = "respec failed: " + applyErr.Error()
					}
				}
			}
		}
	}

	if runErr != nil {
		if parseErr == nil {
			return res, runErr
		}
		return ShipResult{}, runErr
	}
	return res, parseErr
}

// collectAbandons drains the captured-outcome map for tasks that abandoned
// from autopilot stall recovery — i.e., the reviewer kept rejecting and the
// run gave up after escalation/decompose. Other block codes (budget caps,
// engine errors, git failures, upstream cascades) are filtered out: their
// reviewer-issue fingerprints are empty or noise, and respec'ing the spec
// won't help the underlying mechanical failure.
func collectAbandons(captured map[string]orchestrator.Outcome, mu *sync.Mutex) ([]orchestrator.Outcome, []string) {
	mu.Lock()
	defer mu.Unlock()
	var abandons []orchestrator.Outcome
	var ids []string
	for id, oc := range captured {
		if oc.Final != orchestrator.StateBlocked {
			continue
		}
		if oc.BlockReason == nil || oc.BlockReason.Code != orchestrator.CodeAbandonedAutopilot {
			continue
		}
		abandons = append(abandons, oc)
		ids = append(ids, id)
	}
	return abandons, ids
}

// applyRespec persists the respec audit trail under the most recent run dir,
// stashes stale task files, regenerates the spec from failure feedback, and
// overwrites .aios/project.md. The follow-up shipSpecAttempt re-decomposes
// and re-runs — old tasks must be cleared so the new decomposition starts
// from a clean slate.
func applyRespec(ctx context.Context, wd string, cfg *config.Config, abandons []orchestrator.Outcome, ids []string) error {
	runDir, err := latestRunDir(wd)
	if err != nil {
		return fmt.Errorf("latest run dir: %w", err)
	}
	respecDir := filepath.Join(runDir, "respec")
	if err := os.MkdirAll(respecDir, 0o755); err != nil {
		return fmt.Errorf("mkdir respec: %w", err)
	}

	feedback := aggregateFeedback(abandons, ids)
	if err := os.WriteFile(filepath.Join(respecDir, "feedback.md"), []byte(feedback), 0o644); err != nil {
		return fmt.Errorf("write feedback.md: %w", err)
	}

	tasksDir := filepath.Join(wd, ".aios", "tasks")
	oldTasksDir := filepath.Join(respecDir, "old-tasks")
	if err := stashTasks(tasksDir, oldTasksDir); err != nil {
		return fmt.Errorf("stash old tasks: %w", err)
	}

	specPath := filepath.Join(wd, ".aios", "project.md")
	origSpec, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read project.md: %w", err)
	}

	claude := &engine.ClaudeEngine{
		Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs,
		TimeoutSec: cfg.Engines.Claude.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Claude),
	}
	codex := &engine.CodexEngine{
		Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs,
		TimeoutSec: cfg.Engines.Codex.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Codex),
	}

	rec, err := run.Open(filepath.Dir(runDir), filepath.Base(runDir))
	if err != nil {
		return fmt.Errorf("open run dir: %w", err)
	}

	out, err := specgen.Regenerate(ctx, specgen.RegenerateInput{
		OriginalSpec:      string(origSpec),
		Feedback:          feedback,
		Claude:            claude,
		Codex:             codex,
		Recorder:          rec,
		PolishEngine:      "claude",
		CritiqueEnabled:   cfg.Specgen.CritiqueOn(),
		CritiqueThreshold: cfg.Specgen.Threshold(),
	})
	if err != nil {
		return fmt.Errorf("regenerate: %w", err)
	}
	if out.Final == "" {
		return fmt.Errorf("regenerate produced an empty spec")
	}
	if err := os.WriteFile(specPath, []byte(out.Final), 0o644); err != nil {
		return fmt.Errorf("write new project.md: %w", err)
	}
	// Mark the regenerated spec as already-respeced so a process crash or
	// daemon retry that re-enters ShipSpec on this same spec body cannot
	// trigger a second respec. The marker is keyed by spec content hash:
	// a user editing project.md changes the hash and respec becomes
	// available again, which is the intended behavior.
	if markErr := markRespecAttempted(wd); markErr != nil {
		// Best-effort; surface in audit but don't fail the respec.
		_ = os.WriteFile(filepath.Join(respecDir, "marker-error.txt"),
			[]byte(markErr.Error()), 0o644)
	}
	return nil
}

// respecStateDir is the project-level location for cross-run respec state.
func respecStateDir(wd string) string {
	return filepath.Join(wd, ".aios", "state", "respec")
}

// specHashHex computes the sha256 hex of the current .aios/project.md, or
// the empty string when the spec file is missing or unreadable.
func specHashHex(wd string) string {
	body, err := os.ReadFile(filepath.Join(wd, ".aios", "project.md"))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// markRespecAttempted records that the current spec has already been
// regenerated once. The marker file's name is the spec's sha256 hex so the
// gate is per-spec-content rather than per-project.
func markRespecAttempted(wd string) error {
	hash := specHashHex(wd)
	if hash == "" {
		return fmt.Errorf("hash project.md: empty")
	}
	dir := respecStateDir(wd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir respec state: %w", err)
	}
	path := filepath.Join(dir, hash+".attempted")
	body := fmt.Sprintf("respec applied at %s\n", time.Now().UTC().Format(time.RFC3339))
	return os.WriteFile(path, []byte(body), 0o644)
}

// respecMarkedForCurrentSpec returns true when the current project.md has
// already been respeced once. Defends against the daemon-restart scenario
// where a fresh ShipSpec call would otherwise re-arm respec on the
// regenerated spec.
func respecMarkedForCurrentSpec(wd string) bool {
	hash := specHashHex(wd)
	if hash == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(respecStateDir(wd), hash+".attempted"))
	return err == nil
}

// stashTasks moves every .md file from tasksDir into oldTasksDir, leaving
// tasksDir empty for the post-respec decompose. Missing source dir is not
// an error — it just means there are no tasks to stash.
func stashTasks(tasksDir, oldTasksDir string) error {
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(oldTasksDir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		src := filepath.Join(tasksDir, e.Name())
		dst := filepath.Join(oldTasksDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s: %w", e.Name(), err)
		}
	}
	return nil
}

// latestRunDir returns the most recent .aios/runs/<id>/ directory by name
// (run IDs are UTC timestamps so lexicographic == chronological).
func latestRunDir(wd string) (string, error) {
	runsDir := filepath.Join(wd, ".aios", "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return "", fmt.Errorf("read runs dir: %w", err)
	}
	var newest string
	for _, e := range entries {
		if e.IsDir() && e.Name() > newest {
			newest = e.Name()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no runs found under %s", runsDir)
	}
	return filepath.Join(runsDir, newest), nil
}

func describeRespecOutcome(s ShipStatus) string {
	switch s {
	case ShipMerged:
		return "merged after respec"
	case ShipPRRed:
		return "PR red after respec"
	case ShipAbandoned:
		return "abandoned after respec"
	default:
		return "unknown after respec"
	}
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
