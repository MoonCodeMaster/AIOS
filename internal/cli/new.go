package cli

import (
	"bufio"
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
	"github.com/spf13/cobra"
)

// NewOpts is the struct form of `aios new` arguments. Extracted so the
// autopilot command can call runNew without going through Cobra flag plumbing.
type NewOpts struct {
	Idea string
	Auto bool // skip the "Confirm and commit?" prompt
}

func newNewCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "new <idea>",
		Short: "Brainstorm an idea into a spec + task list",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			auto, _ := cmd.Flags().GetBool("auto")
			return runNew(NewOpts{Idea: strings.Join(args, " "), Auto: auto})
		},
	}
	c.Flags().Bool("auto", false, "skip the spec/tasks confirmation prompt and commit unconditionally")
	return c
}

func runNew(opts NewOpts) error {
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

	bPrompt, _ := prompts.Render("brainstorm.tmpl", map[string]string{"Idea": opts.Idea})
	bRes, err := claude.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: bPrompt})
	if err != nil {
		return err
	}
	_ = rec.WriteFile("brainstorm.md", []byte(bRes.Text))

	sPrompt, _ := prompts.Render("spec-synth.tmpl", map[string]string{"Transcript": bRes.Text})
	sRes, err := claude.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: sPrompt})
	if err != nil {
		return err
	}
	specPath := filepath.Join(wd, ".aios", "project.md")
	_ = os.MkdirAll(filepath.Dir(specPath), 0o755)
	if err := os.WriteFile(specPath, []byte(sRes.Text), 0o644); err != nil {
		return err
	}

	dPrompt, _ := prompts.Render("decompose.tmpl", map[string]string{"Spec": sRes.Text})
	dRes, err := codex.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: dPrompt})
	if err != nil {
		return err
	}
	tasksDir := filepath.Join(wd, ".aios", "tasks")
	_ = os.MkdirAll(tasksDir, 0o755)
	written, err := writeTaskFiles(tasksDir, dRes.Text)
	if err != nil {
		return err
	}

	fmt.Printf("\nSpec written to %s\n", specPath)
	fmt.Printf("Task files written to %s (%d files)\n\n", tasksDir, written)

	// Auto mode skips the confirmation entirely. Used by `aios autopilot`
	// and by the M4 issue-bot. Legacy interactive `aios new` keeps the gate.
	if !opts.Auto {
		if !confirm("Confirm and commit to aios/staging? [y/N] ") {
			fmt.Println("Left spec + tasks uncommitted. Edit and re-run `aios new --resume` to retry.")
			return nil
		}
	}

	if err := commitNewSpec(wd, cfg.Project.StagingBranch, opts.Idea); err != nil {
		return err
	}
	fmt.Println("Committed to " + cfg.Project.StagingBranch)
	return nil
}

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

func extractTaskID(frontmatter string) string {
	for _, ln := range strings.Split(frontmatter, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "id:") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "id:"))
		}
	}
	return ""
}

func commitNewSpec(wd, staging, idea string) error {
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
	msg := "aios: spec and tasks for " + idea
	return exec.Command("git", "-C", wd, "commit", "-m", msg).Run()
}

// keep bufio import used somewhere
var _ = bufio.NewReader
