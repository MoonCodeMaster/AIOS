package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/worktree"
	"github.com/spf13/cobra"
)

// duelResult bundles everything one engine produced in a duel — used in
// printDuelVerdict so the caller doesn't have to reconstruct the anonymous
// struct each time.
type duelResult struct {
	tokens   int
	err      error
	duration time.Duration
}

// `aios duel "<task>"` runs both engines on the same task in parallel,
// each in its own throwaway worktree, then asks the reviewer-default
// engine to pick the winning diff. Designed as a competitive demonstrator:
// dual-coder is something neither Claude CLI nor Codex CLI can do alone.
//
// No state is committed to staging. Both worktrees are removed at the end
// regardless of outcome; the audit trail (prompts, raw responses, diffs,
// verdict) is persisted under .aios/runs/<id>/duel/ for review.
func newDuelCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "duel <task description>",
		Short: "Race Claude and Codex on the same task; reviewer picks the winner",
		Long: `aios duel runs both AI engines as coders on the same task in parallel,
in two ephemeral git worktrees. When both have stopped, the project's
reviewer-default engine reads both diffs and picks a winner on three axes:
correctness, minimality, clarity.

Use this to:

  - Stress-test which engine is stronger on a particular kind of change.
  - Get two independent attempts when stakes are high (security fix, data
    migration, performance-critical hot path).
  - Evaluate model upgrades — re-run the same duel after upgrading one CLI
    and see whether the verdict flips.

No commits are made and no branches are merged. Both worktrees are torn
down at the end. Pass --apply to copy the winning diff onto your current
branch as uncommitted changes (review and commit yourself).`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apply, _ := cmd.Flags().GetBool("apply")
			return runDuel(cmd.Context(), strings.Join(args, " "), apply)
		},
	}
	c.Flags().Bool("apply", false, "after the duel, apply the winning diff to the current working tree as uncommitted changes")
	return c
}

func runDuel(ctx context.Context, task string, apply bool) error {
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

	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(wd, ".aios", "runs"), runID)
	if err != nil {
		return err
	}

	claude := &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec}
	codex := &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec}
	var judge engine.Engine = codex
	if cfg.Engines.ReviewerDefault == "claude" {
		judge = claude
	}

	wm := &worktree.Manager{RepoDir: wd, Root: filepath.Join(wd, cfg.Runtime.WorktreeRoot)}

	// Two distinct task IDs so worktree paths and branches do not collide.
	// The "duel-" prefix is meaningless to the worktree manager but lets a
	// human eyeballing `git branch` see what the leftover refs are for.
	idA := "duel-" + runID + "-claude"
	idB := "duel-" + runID + "-codex"
	wtA, err := wm.Create(idA, cfg.Project.StagingBranch)
	if err != nil {
		return fmt.Errorf("create worktree A: %w", err)
	}
	defer wm.Remove(wtA)
	wtB, err := wm.Create(idB, cfg.Project.StagingBranch)
	if err != nil {
		return fmt.Errorf("create worktree B: %w", err)
	}
	defer wm.Remove(wtB)

	fmt.Printf("duel: Claude in %s\n", wtA.Path)
	fmt.Printf("duel: Codex  in %s\n", wtB.Path)
	fmt.Println("duel: both coders running in parallel…")

	promptA, err := prompts.Render("duel-coder.tmpl", map[string]string{"Task": task, "Workdir": wtA.Path})
	if err != nil {
		return fmt.Errorf("render duel-coder for A: %w", err)
	}
	promptB, err := prompts.Render("duel-coder.tmpl", map[string]string{"Task": task, "Workdir": wtB.Path})
	if err != nil {
		return fmt.Errorf("render duel-coder for B: %w", err)
	}

	var (
		resA, resB     duelResult
		rawA, rawB     string
		wg             sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		t0 := time.Now()
		r, err := claude.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: promptA, Workdir: wtA.Path})
		resA.duration = time.Since(t0)
		if err != nil {
			resA.err = err
			return
		}
		rawA = r.Raw
		resA.tokens = r.UsageTokens
	}()
	go func() {
		defer wg.Done()
		t0 := time.Now()
		r, err := codex.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: promptB, Workdir: wtB.Path})
		resB.duration = time.Since(t0)
		if err != nil {
			resB.err = err
			return
		}
		rawB = r.Raw
		resB.tokens = r.UsageTokens
	}()
	wg.Wait()

	_ = rec.WriteFile("duel/coder-A.prompt.txt", []byte(promptA))
	_ = rec.WriteFile("duel/coder-B.prompt.txt", []byte(promptB))
	_ = rec.WriteFile("duel/coder-A.response.raw", []byte(rawA))
	_ = rec.WriteFile("duel/coder-B.response.raw", []byte(rawB))

	if resA.err != nil && resB.err != nil {
		return fmt.Errorf("both coders errored: claude=%v, codex=%v", resA.err, resB.err)
	}

	diffA, dErr := wm.Diff(wtA, cfg.Project.StagingBranch)
	if dErr != nil {
		fmt.Fprintf(os.Stderr, "warn: diff A: %v\n", dErr)
	}
	diffB, dErr := wm.Diff(wtB, cfg.Project.StagingBranch)
	if dErr != nil {
		fmt.Fprintf(os.Stderr, "warn: diff B: %v\n", dErr)
	}
	_ = rec.WriteFile("duel/diff-A.patch", []byte(diffA))
	_ = rec.WriteFile("duel/diff-B.patch", []byte(diffB))

	verdict, judgeRaw, err := runDuelJudge(ctx, judge, task, "claude", "codex", diffA, diffB)
	if err != nil {
		return fmt.Errorf("judge: %w", err)
	}
	_ = rec.WriteFile("duel/verdict.txt", []byte(judgeRaw))

	printDuelVerdict(os.Stdout, "claude", "codex", resA, resB, diffA, diffB, verdict)

	if apply {
		var winningDiff string
		switch verdict.Winner {
		case "A":
			winningDiff = diffA
		case "B":
			winningDiff = diffB
		default:
			fmt.Println("duel: tie; nothing applied. Pick a side and re-run with the diff path printed above.")
			return nil
		}
		if err := applyDiffToWorktree(wd, winningDiff); err != nil {
			return fmt.Errorf("apply winning diff: %w", err)
		}
		fmt.Println("duel: winning diff applied to your working tree as uncommitted changes.")
	}
	return nil
}

// duelVerdict is the parsed shape of the judge's response. Free-text fields
// are rendered as-is; only Winner is consumed by --apply.
type duelVerdict struct {
	Winner      string // "A" | "B" | "tie"
	Correctness string
	Minimality  string
	Clarity     string
	Reason      string
}

func runDuelJudge(ctx context.Context, judge engine.Engine, task, authorA, authorB, diffA, diffB string) (duelVerdict, string, error) {
	prompt, err := prompts.Render("duel-judge.tmpl", map[string]string{
		"Task":    task,
		"AuthorA": authorA,
		"AuthorB": authorB,
		"DiffA":   diffA,
		"DiffB":   diffB,
	})
	if err != nil {
		return duelVerdict{}, "", fmt.Errorf("render duel-judge: %w", err)
	}
	resp, err := judge.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: prompt})
	if err != nil {
		return duelVerdict{}, "", err
	}
	v := parseVerdict(resp.Text)
	return v, resp.Text, nil
}

// parseVerdict reads the ===VERDICT=== block. Tolerant: if the block is
// missing, returns Winner="tie" so the caller does not crash on a
// malformed judge response. Free-text fields are concatenated lines until
// the next field key or the end marker.
func parseVerdict(raw string) duelVerdict {
	v := duelVerdict{Winner: "tie"}
	lines := strings.Split(raw, "\n")
	inside := false
	currentKey := ""
	currentBody := []string{}
	flush := func() {
		body := strings.TrimSpace(strings.Join(currentBody, "\n"))
		switch currentKey {
		case "winner":
			w := strings.ToLower(strings.TrimSpace(body))
			switch w {
			case "a":
				v.Winner = "A"
			case "b":
				v.Winner = "B"
			default:
				v.Winner = "tie"
			}
		case "correctness":
			v.Correctness = body
		case "minimality":
			v.Minimality = body
		case "clarity":
			v.Clarity = body
		case "reason":
			v.Reason = body
		}
		currentBody = currentBody[:0]
	}
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "===VERDICT===" {
			inside = true
			continue
		}
		if trim == "===END===" {
			if currentKey != "" {
				flush()
			}
			inside = false
			continue
		}
		if !inside {
			continue
		}
		// Field detection: a line "key: ..." starts a new field iff key is
		// one we recognise. Otherwise it's a continuation of the previous
		// field's body.
		if k, val, ok := splitKVLine(trim); ok && isVerdictKey(k) {
			if currentKey != "" {
				flush()
			}
			currentKey = strings.ToLower(k)
			currentBody = []string{val}
			continue
		}
		currentBody = append(currentBody, ln)
	}
	if inside && currentKey != "" {
		flush()
	}
	return v
}

func splitKVLine(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

func isVerdictKey(k string) bool {
	switch strings.ToLower(k) {
	case "winner", "correctness", "minimality", "clarity", "reason":
		return true
	}
	return false
}

func printDuelVerdict(w io.Writer, authorA, authorB string, resA, resB duelResult, diffA, diffB string, v duelVerdict) {
	bar := strings.Repeat("─", 78)
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "DUEL VERDICT: %s\n", winnerLabel(v.Winner, authorA, authorB))
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "%-7s  tokens=%-6d  duration=%s  diff=%d B\n", authorA, resA.tokens, resA.duration.Round(time.Second), len(diffA))
	fmt.Fprintf(w, "%-7s  tokens=%-6d  duration=%s  diff=%d B\n", authorB, resB.tokens, resB.duration.Round(time.Second), len(diffB))
	fmt.Fprintln(w, bar)
	if v.Reason != "" {
		fmt.Fprintf(w, "reason:       %s\n", v.Reason)
	}
	if v.Correctness != "" {
		fmt.Fprintf(w, "correctness:  %s\n", v.Correctness)
	}
	if v.Minimality != "" {
		fmt.Fprintf(w, "minimality:   %s\n", v.Minimality)
	}
	if v.Clarity != "" {
		fmt.Fprintf(w, "clarity:      %s\n", v.Clarity)
	}
}

func winnerLabel(winner, authorA, authorB string) string {
	switch winner {
	case "A":
		return authorA + " wins"
	case "B":
		return authorB + " wins"
	default:
		return "tie"
	}
}

// applyDiffToWorktree pipes the patch into `git apply` against the current
// working tree. Patches that don't apply cleanly fail loudly — the user can
// still inspect the saved diff under .aios/runs/<id>/duel/.
func applyDiffToWorktree(wd, diff string) error {
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("winning diff is empty; nothing to apply")
	}
	patchPath := filepath.Join(wd, ".aios", "runs", "duel.patch.tmp")
	if err := os.MkdirAll(filepath.Dir(patchPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(patchPath, []byte(diff), 0o644); err != nil {
		return err
	}
	defer os.Remove(patchPath)
	c := exec.Command("git", "-C", wd, "apply", "--whitespace=fix", patchPath)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply failed: %w\n%s", err, string(out))
	}
	return nil
}
