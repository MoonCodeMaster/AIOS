package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Solaxis/aios/internal/config"
	"github.com/Solaxis/aios/internal/engine"
	"github.com/Solaxis/aios/internal/engine/prompts"
	"github.com/Solaxis/aios/internal/mcp"
	"github.com/Solaxis/aios/internal/orchestrator"
	"github.com/Solaxis/aios/internal/run"
	"github.com/Solaxis/aios/internal/spec"
	"github.com/Solaxis/aios/internal/verify"
	"github.com/Solaxis/aios/internal/worktree"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run",
		Short: "Iterate over pending tasks in dependency order",
		RunE:  runMain,
	}
	c.Flags().Int("max-rounds", 0, "override max rounds per task")
	c.Flags().Int("max-tokens", 0, "override max tokens per task")
	c.Flags().String("task", "", "run only this task ID")
	c.Flags().Bool("sandbox", false, "run inside sandbox container (stubbed in v0)")
	c.Flags().Int("max-parallel", 0, "override [parallel] max_parallel_tasks (0 = use config)")
	c.Flags().StringSlice("mcp-allow", nil, "run-wide MCP server allowlist (intersected with per-task mcp_allow)")
	c.Flags().Bool("no-mcp", false, "disable all MCP for this run")
	c.Flags().Int("max-tokens-run", 0, "override [parallel] max_tokens_per_run (0 = use config)")
	return c
}

func runMain(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	sandbox, _ := cmd.Flags().GetBool("sandbox")
	if sandbox {
		return errors.New("--sandbox is not implemented in v0")
	}

	// --- parallel / budget flags ---
	workers := cfg.Parallel.Workers()
	if mp, _ := cmd.Flags().GetInt("max-parallel"); mp > 0 {
		workers = mp
	}
	runTokenCap := cfg.Parallel.RunTokenCap()
	if mtr, _ := cmd.Flags().GetInt("max-tokens-run"); mtr > 0 {
		runTokenCap = mtr
	}

	// --- MCP flags ---
	mcpServers := cfg.MCP.Servers
	if noMcp, _ := cmd.Flags().GetBool("no-mcp"); noMcp {
		mcpServers = nil
	}
	if allow, _ := cmd.Flags().GetStringSlice("mcp-allow"); len(allow) > 0 {
		filtered := map[string]config.MCPServer{}
		for _, name := range allow {
			if s, ok := mcpServers[name]; ok {
				filtered[name] = s
			}
		}
		mcpServers = filtered
	}

	// Build MCP manager (nil when --no-mcp or no servers configured).
	var mcpMgr *mcp.Manager
	if len(mcpServers) > 0 {
		mcpMgr = mcp.NewManager(mcpServers)
		defer mcpMgr.Shutdown(context.Background())
	}

	if err := preflight(wd, cfg); err != nil {
		return err
	}
	tasks, err := spec.LoadTasks(filepath.Join(wd, ".aios", "tasks"))
	if err != nil {
		return err
	}

	onlyID, _ := cmd.Flags().GetString("task")
	mr, _ := cmd.Flags().GetInt("max-rounds")
	mt, _ := cmd.Flags().GetInt("max-tokens")

	// Filter tasks to pending (and optionally a single ID).
	var pending []*spec.Task
	for _, t := range tasks {
		if t.Status != "pending" {
			continue
		}
		if onlyID != "" && t.ID != onlyID {
			continue
		}
		pending = append(pending, t)
	}

	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(wd, ".aios", "runs"), runID)
	if err != nil {
		return err
	}

	// Build a run-level token budget with its own cancel so it can wind down
	// workers gracefully when the cap is hit.
	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	rb := orchestrator.NewRunBudget(runCtx, runCancel, runTokenCap)

	// Build a fast lookup map from task ID to *spec.Task.
	taskByID := make(map[string]*spec.Task, len(pending))
	for _, t := range pending {
		taskByID[t.ID] = t
	}

	// Build common engine instances (shared across tasks; engines are stateless).
	claudeEng := &engine.ClaudeEngine{
		Binary:     cfg.Engines.Claude.Binary,
		ExtraArgs:  cfg.Engines.Claude.ExtraArgs,
		TimeoutSec: cfg.Engines.Claude.TimeoutSec,
	}
	codexEng := &engine.CodexEngine{
		Binary:     cfg.Engines.Codex.Binary,
		ExtraArgs:  cfg.Engines.Codex.ExtraArgs,
		TimeoutSec: cfg.Engines.Codex.TimeoutSec,
	}
	engMap := map[string]engine.Engine{"claude": claudeEng, "codex": codexEng}

	// Reviewer engine for the MergeQueue re-review path.
	reviewerForMerge, _, err := engine.PickPair("", cfg.Engines.RolesByKind,
		cfg.Engines.CoderDefault, cfg.Engines.ReviewerDefault, engMap)
	if err != nil {
		// Fall back gracefully; MergeQueue reviewer is only used on rebase+diff-changed.
		reviewerForMerge = codexEng
	}

	wm := &worktree.Manager{RepoDir: wd, Root: filepath.Join(wd, cfg.Runtime.WorktreeRoot)}

	// stagingSHA shells out to get the current HEAD of the staging branch.
	stagingSHA := func() (string, error) {
		out, err := exec.Command("git", "-C", wd, "rev-parse", cfg.Project.StagingBranch).Output()
		if err != nil {
			return "", fmt.Errorf("rev-parse staging: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}

	// Per-task closure for RunAll.
	taskFn := func(ctx context.Context, id orchestrator.TaskID) (orchestrator.TaskResult, *orchestrator.MergeRequest) {
		tk, ok := taskByID[id]
		if !ok {
			return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: "task-not-found"}, nil
		}

		// Capture staging HEAD before creating the worktree so the MergeQueue
		// can detect whether a rebase is needed.
		parentSHA, err := stagingSHA()
		if err != nil {
			return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: "rev-parse-failed"}, nil
		}

		wt, err := wm.Create(tk.ID, cfg.Project.StagingBranch)
		if err != nil {
			return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: "worktree-add-failed: " + err.Error()}, nil
		}
		defer wm.Remove(wt) // best-effort; the branch is retained for inspection

		// Get MCP scope for this task (nil when task has no mcp_allow or mcpMgr is nil).
		var mcpScope *engine.McpScope
		if mcpMgr != nil {
			scope, err := mcpMgr.ScopeFor(tk, filepath.Join(rec.Root(), "task-"+tk.ID))
			if err != nil {
				return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: "mcp-scope-failed: " + err.Error()}, nil
			}
			mcpScope = scope
		}

		// Pick coder/reviewer engines for this task kind.
		coderEng, reviewerEng, err := engine.PickPair(
			tk.Kind, cfg.Engines.RolesByKind,
			cfg.Engines.CoderDefault, cfg.Engines.ReviewerDefault, engMap,
		)
		if err != nil {
			return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: "engine-pick-failed: " + err.Error()}, nil
		}

		// Wrap engines to auto-attach MCP scope to every Invoke call.
		if mcpScope != nil {
			coderEng = withMcpScope(coderEng, mcpScope)
			reviewerEng = withMcpScope(reviewerEng, mcpScope)
		}

		// Wrap engines so every Invoke contributes to the run-level token budget.
		coderEng = withRunBudget(coderEng, rb)
		reviewerEng = withRunBudget(reviewerEng, rb)

		checks := []verify.Check{
			{Name: "test_cmd", Cmd: cfg.Verify.TestCmd, Skipped: cfg.Verify.Skipped["test_cmd"]},
			{Name: "lint_cmd", Cmd: cfg.Verify.LintCmd, Skipped: cfg.Verify.Skipped["lint_cmd"]},
			{Name: "typecheck_cmd", Cmd: cfg.Verify.TypecheckCmd, Skipped: cfg.Verify.Skipped["typecheck_cmd"]},
			{Name: "build_cmd", Cmd: cfg.Verify.BuildCmd, Skipped: cfg.Verify.Skipped["build_cmd"]},
		}

		maxRounds := cfg.Budget.MaxRoundsPerTask
		if mr > 0 {
			maxRounds = mr
		}
		maxTokens := cfg.Budget.MaxTokensPerTask
		if mt > 0 {
			maxTokens = mt
		}

		dep := &orchestrator.Deps{
			Coder:    coderEng,
			Reviewer: reviewerEng,
			Verifier: liveVerifier{workdir: wt.Path, checks: checks, timeout: 5 * time.Minute},
			Diff:     func() (string, error) { return wm.Diff(wt, cfg.Project.StagingBranch) },
			MaxRounds: maxRounds,
			MaxTokens: maxTokens,
			MaxWall:   time.Duration(cfg.Budget.MaxWallMinutesPerTask) * time.Minute,
		}

		fmt.Printf("→ task %s (%s)\n", tk.ID, tk.Kind)
		outcome, err := orchestrator.Run(ctx, tk, dep)
		if err != nil {
			return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: err.Error()}, nil
		}

		// Record every round's artefacts.
		for i, r := range outcome.Rounds {
			_ = rec.WriteRoundFile(tk.ID, i+1, "coder-text.txt", []byte(r.CoderText))
			jb, _ := json.MarshalIndent(r.Checks, "", "  ")
			_ = rec.WriteRoundFile(tk.ID, i+1, "verify.json", jb)
			jb2, _ := json.MarshalIndent(r.Review, "", "  ")
			_ = rec.WriteRoundFile(tk.ID, i+1, "reviewer-response.json", jb2)
		}

		if outcome.Final != orchestrator.StateConverged {
			tk.Status = "blocked"
			rpt := buildReport(tk, outcome)
			_ = rec.WriteTaskFile(tk.ID, "report.md", []byte(run.RenderReport(rpt)))
			_ = updateTaskFile(tk)
			fmt.Printf("✗ task %s BLOCKED: %s\n", tk.ID, outcome.Reason)
			return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: outcome.Reason}, nil
		}

		// Compute the diff for the MergeRequest BEFORE committing (the commit
		// changes what wm.Diff returns against the branch).
		diff, _ := wm.Diff(wt, cfg.Project.StagingBranch)

		// Commit on the task branch.
		g := &worktree.Git{Dir: wt.Path}
		if _, err := g.Run("add", "-A"); err != nil {
			return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: "git-add-failed: " + err.Error()}, nil
		}
		if _, err := g.Run("commit", "--allow-empty", "-m", "aios: converged task "+tk.ID); err != nil {
			return orchestrator.TaskResult{ID: id, Status: "blocked", Reason: "commit-failed: " + err.Error()}, nil
		}

		// Recompute diff post-commit for the MergeRequest (HEAD..staging).
		postDiff, _ := wm.Diff(wt, cfg.Project.StagingBranch)
		_ = diff // pre-commit diff kept for reference; post-commit is authoritative

		tk.Status = "converged"
		_ = updateTaskFile(tk)
		fmt.Printf("✓ task %s converged in %d rounds\n", tk.ID, len(outcome.Rounds))

		// reReview is called by MergeQueue when a rebase changes the diff.
		// It re-invokes the reviewer engine on the rebased diff so that a
		// diff the reviewer has never seen is never silently merged.
		reReview := func(newDiff []byte) (bool, error) {
			promptText, err := prompts.Render("reviewer.tmpl", struct {
				Task   *spec.Task
				Diff   string
				Checks []verify.CheckResult
			}{
				Task:   tk,
				Diff:   string(newDiff),
				Checks: []verify.CheckResult{},
			})
			if err != nil {
				return false, fmt.Errorf("rebase re-review render: %w", err)
			}
			resp, err := reviewerEng.Invoke(ctx, engine.InvokeRequest{
				Role:    engine.RoleReviewer,
				Prompt:  promptText,
				Workdir: wt.Path,
			})
			if err != nil {
				return false, fmt.Errorf("rebase re-review invoke: %w", err)
			}
			var rev orchestrator.ReviewResult
			if err := json.Unmarshal([]byte(resp.Text), &rev); err != nil {
				return false, fmt.Errorf("rebase re-review parse: %w", err)
			}
			// Inline allSatisfied (orchestrator.allSatisfied is unexported).
			for _, c := range rev.Criteria {
				if c.Status != "satisfied" {
					return false, nil
				}
			}
			return rev.Approved, nil
		}

		return orchestrator.TaskResult{ID: id, Status: "converged"},
			&orchestrator.MergeRequest{
				TaskID:    id,
				Branch:    wt.Branch,
				ParentSHA: parentSHA,
				Diff:      []byte(postDiff),
				ReReview:  reReview,
			}
	}

	rep, err := orchestrator.RunAll(runCtx, orchestrator.RunAllOpts{
		RepoDir:       wd,
		StagingBranch: cfg.Project.StagingBranch,
		Tasks:         pending,
		Workers:       workers,
		Reviewer:      reviewerForMerge,
		Task:          taskFn,
	})
	if err != nil && err != context.Canceled {
		return fmt.Errorf("RunAll: %w", err)
	}

	if len(rep.Blocked) > 0 {
		os.Exit(2)
	}
	return nil
}

// mcpScopedEngine wraps an Engine, auto-attaching an McpScope to every Invoke
// call where the request doesn't already have one.
type mcpScopedEngine struct {
	inner engine.Engine
	scope *engine.McpScope
}

func (m *mcpScopedEngine) Name() string { return m.inner.Name() }
func (m *mcpScopedEngine) Invoke(ctx context.Context, req engine.InvokeRequest) (*engine.InvokeResponse, error) {
	if req.Mcp == nil {
		req.Mcp = m.scope
	}
	return m.inner.Invoke(ctx, req)
}

func withMcpScope(eng engine.Engine, scope *engine.McpScope) engine.Engine {
	if scope == nil {
		return eng
	}
	return &mcpScopedEngine{inner: eng, scope: scope}
}

// runBudgetEngine wraps an Engine and charges every Invoke's token usage
// against the run-level RunBudget.
type runBudgetEngine struct {
	inner  engine.Engine
	budget *orchestrator.RunBudget
}

func (r *runBudgetEngine) Name() string { return r.inner.Name() }
func (r *runBudgetEngine) Invoke(ctx context.Context, req engine.InvokeRequest) (*engine.InvokeResponse, error) {
	resp, err := r.inner.Invoke(ctx, req)
	if err != nil {
		return resp, err
	}
	if resp != nil && resp.UsageTokens > 0 {
		if budgetErr := r.budget.Add(resp.UsageTokens); budgetErr != nil {
			// Cancel has already been fired; return the response so the
			// per-task budget also sees tokens, but signal budget exceeded.
			return resp, budgetErr
		}
	}
	return resp, nil
}

func withRunBudget(eng engine.Engine, rb *orchestrator.RunBudget) engine.Engine {
	if rb == nil {
		return eng
	}
	return &runBudgetEngine{inner: eng, budget: rb}
}

func preflight(wd string, cfg *config.Config) error {
	// 1. git status clean (ignoring .aios/worktrees/)
	out, err := exec.Command("git", "-C", wd, "status", "--porcelain").Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		path := strings.TrimSpace(line[2:])
		if strings.HasPrefix(path, ".aios/worktrees/") {
			continue
		}
		return fmt.Errorf("working tree not clean: %q", line)
	}
	// 2. staging ancestor-or-equal to base
	_, err = exec.Command("git", "-C", wd, "merge-base", "--is-ancestor",
		cfg.Project.StagingBranch, cfg.Project.BaseBranch).CombinedOutput()
	if err != nil {
		// not an ancestor is fine *if* staging is identical; detect with rev-parse
		b1, _ := exec.Command("git", "-C", wd, "rev-parse", cfg.Project.StagingBranch).Output()
		b2, _ := exec.Command("git", "-C", wd, "rev-parse", cfg.Project.BaseBranch).Output()
		if strings.TrimSpace(string(b1)) != strings.TrimSpace(string(b2)) {
			return fmt.Errorf("aios/staging has drifted from %s; reconcile before running", cfg.Project.BaseBranch)
		}
	}
	// 3. engine binaries
	for _, b := range []string{cfg.Engines.Claude.Binary, cfg.Engines.Codex.Binary} {
		if _, err := exec.LookPath(b); err != nil {
			return fmt.Errorf("engine binary %q not on PATH", b)
		}
	}
	return nil
}

type liveVerifier struct {
	workdir string
	checks  []verify.Check
	timeout time.Duration
}

func (v liveVerifier) Run() []verify.CheckResult {
	return verify.Run(context.Background(), v.workdir, v.checks, v.timeout)
}

func buildReport(task *spec.Task, o *orchestrator.Outcome) run.Report {
	rpt := run.Report{TaskID: task.ID, Status: "blocked", Reason: o.Reason, UsageTokens: o.UsageTokens}
	for _, r := range o.Rounds {
		var unmet []string
		for _, c := range r.Review.Criteria {
			if c.Status != "satisfied" {
				unmet = append(unmet, c.ID)
			}
		}
		rpt.Rounds = append(rpt.Rounds, run.Round{
			N: r.N, ReviewApproved: r.Review.Approved,
			UnmetCriteria: unmet, IssueCount: len(r.Review.Issues),
			VerifyGreen: verify.AllGreen(r.Checks),
		})
	}
	return rpt
}

func updateTaskFile(t *spec.Task) error {
	raw, err := os.ReadFile(t.Path)
	if err != nil {
		return err
	}
	updated := replaceStatusInFrontmatter(string(raw), t.Status)
	return os.WriteFile(t.Path, []byte(updated), 0o644)
}

func replaceStatusInFrontmatter(src, newStatus string) string {
	lines := strings.Split(src, "\n")
	inFM := false
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "---" {
			if !inFM {
				inFM = true
				continue
			}
			break
		}
		if inFM && strings.HasPrefix(trim, "status:") {
			indent := ln[:len(ln)-len(strings.TrimLeft(ln, " "))]
			lines[i] = indent + "status: " + newStatus
			return strings.Join(lines, "\n")
		}
	}
	return src
}
