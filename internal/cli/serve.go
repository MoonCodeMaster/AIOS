package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
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
		Autopilot: inProcessShip,
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

// inProcessShip runs the new ship pipeline for one issue body. Replaces
// the prior subprocess-out-to-`aios autopilot` path.
//
// Note: ShipPrompt internally creates its own run dir and parseLatestShipSummary
// picks the newest by lex sort. Safe here because serve processes one issue at
// a time; concurrent ShipPrompt calls in the same wd would race and need a
// (beforeIDs, afterIDs) snapshot pattern inside ShipSpec.
func inProcessShip(ctx context.Context, idea string) (AutopilotResult, error) {
	wd, err := os.Getwd()
	if err != nil {
		return AutopilotResult{}, err
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return AutopilotResult{}, fmt.Errorf("load config: %w", err)
	}
	claude := &engine.ClaudeEngine{
		Binary:     cfg.Engines.Claude.Binary,
		ExtraArgs:  cfg.Engines.Claude.ExtraArgs,
		TimeoutSec: cfg.Engines.Claude.TimeoutSec,
	}
	codex := &engine.CodexEngine{
		Binary:     cfg.Engines.Codex.Binary,
		ExtraArgs:  cfg.Engines.Codex.ExtraArgs,
		TimeoutSec: cfg.Engines.Codex.TimeoutSec,
	}
	res, err := ShipPrompt(ctx, ShipPromptInput{
		Wd: wd, Prompt: idea, Claude: claude, Codex: codex,
	})
	return shipResultToAutopilot(res), err
}

// shipResultToAutopilot bridges the new ShipResult type to the existing
// AutopilotResult/AutopilotStatus types still used by serve_runner.go.
func shipResultToAutopilot(s ShipResult) AutopilotResult {
	out := AutopilotResult{PRURL: s.PRURL, PRNumber: s.PRNumber, AuditTrail: s.AuditTrail}
	switch s.Status {
	case ShipMerged:
		out.Status = AutopilotMerged
	case ShipPRRed:
		out.Status = AutopilotPRRed
	case ShipAbandoned:
		out.Status = AutopilotAbandoned
	default:
		out.Status = AutopilotUnknown
	}
	return out
}
