package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "serve",
		Short: "Watch GitHub issues with the aios:do label and run autopilot per issue",
		Long: `Watches a GitHub repo for issues labeled aios:do, runs autopilot per issue,
opens PRs, comments back, files aios:stuck issues with the audit trail on
abandon.

Sequential by default in v0.5.0 — one issue at a time. Concurrency >1 is
configured via .aios/serve.toml [concurrency] max_concurrent_issues but
clamped to 1 internally for now.

Modes:
  aios serve            Long-running daemon. Polls every interval_sec.
  aios serve --once     Single poll cycle, exit. For cron / GitHub Actions.

Requires: gh CLI authenticated (gh auth login). The repo to watch is read
from .aios/serve.toml [repo] owner/name; if absent, the current git repo's
default remote is used.`,
		RunE: runServe,
	}
	c.Flags().Bool("once", false, "single poll cycle, then exit (for cron)")
	c.Flags().String("repo", "", "OWNER/NAME (overrides .aios/serve.toml [repo])")
	return c
}

func runServe(cmd *cobra.Command, _ []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := LoadServeConfig(filepath.Join(wd, ".aios", "serve.toml"))
	if err != nil {
		return fmt.Errorf("load serve config: %w", err)
	}
	if repoFlag, _ := cmd.Flags().GetString("repo"); repoFlag != "" {
		parts := strings.SplitN(repoFlag, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("--repo must be OWNER/NAME, got %q", repoFlag)
		}
		cfg.Repo.Owner, cfg.Repo.Name = parts[0], parts[1]
	}
	if cfg.Concurrency.MaxConcurrentIssues > 1 {
		fmt.Fprintf(os.Stderr, "warn: max_concurrent_issues=%d clamped to 1 in v0.5.0\n", cfg.Concurrency.MaxConcurrentIssues)
		cfg.Concurrency.MaxConcurrentIssues = 1
	}

	statePath := filepath.Join(wd, ".aios", "serve", "state.json")
	state, err := LoadServeState(statePath)
	if err != nil {
		return fmt.Errorf("load serve state: %w", err)
	}
	host := githost.NewCLIHost()

	if err := state.Reconcile(cmd.Context(), host, cfg.Labels.Do, cfg.Labels.InProgress); err != nil {
		fmt.Fprintf(os.Stderr, "warn: reconcile failed: %v\n", err)
	}
	_ = state.Save(statePath)

	once, _ := cmd.Flags().GetBool("once")

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: subprocessAutopilot,
	}

	doOne := func(ctx context.Context) error {
		issues, err := host.ListLabeled(ctx, cfg.Labels.Do)
		if err != nil {
			return fmt.Errorf("list labeled: %w", err)
		}
		for _, iss := range issues {
			if _, present := state.Issues[iss.Number]; present {
				continue
			}
			fmt.Printf("aios serve: claiming issue #%d %q\n", iss.Number, iss.Title)
			if err := runner.RunIssue(ctx, iss); err != nil {
				fmt.Fprintf(os.Stderr, "issue #%d: %v\n", iss.Number, err)
			}
			_ = state.Save(statePath)
			break // sequential — one issue per poll cycle
		}
		return nil
	}

	if once {
		return doOne(cmd.Context())
	}
	interval := time.Duration(cfg.Poll.IntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if err := doOne(cmd.Context()); err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v\n", err)
	}
	for {
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-ticker.C:
			if err := doOne(cmd.Context()); err != nil {
				fmt.Fprintf(os.Stderr, "warn: %v\n", err)
			}
		}
	}
}

// subprocessAutopilot shells out to `aios autopilot "<idea>"` and parses the
// resulting autopilot-summary.md from the latest .aios/runs/<id>/ directory.
func subprocessAutopilot(ctx context.Context, idea string) (AutopilotResult, error) {
	wd, err := os.Getwd()
	if err != nil {
		return AutopilotResult{}, err
	}
	runsDir := filepath.Join(wd, ".aios", "runs")
	beforeIDs := snapshotRunIDs(runsDir)

	cmd := exec.CommandContext(ctx, os.Args[0], "autopilot", idea)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	exitErr := cmd.Run()

	afterIDs := snapshotRunIDs(runsDir)
	newID := newestNew(beforeIDs, afterIDs)
	if newID == "" {
		return AutopilotResult{}, fmt.Errorf("autopilot ran but no new run dir under %s (exit: %v)", runsDir, exitErr)
	}
	summaryPath := filepath.Join(runsDir, newID, "autopilot-summary.md")
	body, err := os.ReadFile(summaryPath)
	if err != nil {
		return AutopilotResult{}, fmt.Errorf("read autopilot-summary.md: %w", err)
	}
	return parseAutopilotSummary(string(body))
}

func snapshotRunIDs(runsDir string) map[string]bool {
	out := map[string]bool{}
	entries, _ := os.ReadDir(runsDir)
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = true
		}
	}
	return out
}

func newestNew(before, after map[string]bool) string {
	var newest string
	for id := range after {
		if before[id] {
			continue
		}
		if id > newest {
			newest = id
		}
	}
	return newest
}

func parseAutopilotSummary(body string) (AutopilotResult, error) {
	res := AutopilotResult{Status: AutopilotUnknown}
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
			res.Status = AutopilotMerged
		case strings.HasPrefix(ln, "Merged: false"):
			res.Status = AutopilotPRRed
		case strings.Contains(ln, "all tasks abandoned") || strings.Contains(ln, "Skipped: no converged tasks"):
			res.Status = AutopilotAbandoned
			res.AuditTrail = body
		}
	}
	if res.Status == AutopilotUnknown {
		return res, fmt.Errorf("autopilot-summary.md did not yield a recognised status:\n%s", body)
	}
	return res, nil
}
