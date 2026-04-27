package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/spf13/cobra"
)

// `aios review <PR>` runs both engines as reviewers on the same pull
// request in parallel, then merges their reviews into a single
// consolidated comment via the synthesizer engine. Optionally posts the
// merged comment back to the PR via `gh pr comment`.
//
// The killer feature: any developer who reviews PRs can demonstrate the
// dual-model value to their team in 30 seconds with no setup beyond
// `aios init`.
func newReviewCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "review <pr-number-or-url>",
		Short: "Cross-model PR review: both engines review, synthesizer merges, you decide whether to post",
		Long: `aios review fetches a PR via the gh CLI, runs both Claude and Codex as
reviewers in parallel, then asks the project's reviewer-default engine to
merge their feedback into a single consolidated comment.

By default the merged review is printed to stdout. Pass --post to also
publish it as a PR comment via gh.

Verdict policy: the merged verdict is the more conservative of the two
(request-changes wins over comment-only wins over approve). Disagreements
are surfaced rather than hidden.

Requires the gh CLI authenticated and a PR identifier. The argument may be
a number ("42") or a full URL ("https://github.com/owner/repo/pull/42").`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			post, _ := cmd.Flags().GetBool("post")
			return runReview(cmd.Context(), args[0], post)
		},
	}
	c.Flags().Bool("post", false, "after the merge, post the consolidated review as a PR comment via `gh pr comment`")
	return c
}

func runReview(ctx context.Context, prArg string, post bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("aios review requires the gh CLI on PATH (https://cli.github.com)")
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("run `aios init` first: %w", err)
	}

	prRef, err := parsePRRef(prArg)
	if err != nil {
		return err
	}

	meta, err := fetchPRMeta(ctx, prRef)
	if err != nil {
		return fmt.Errorf("gh pr view %s: %w", prRef, err)
	}
	diff, err := fetchPRDiff(ctx, prRef)
	if err != nil {
		return fmt.Errorf("gh pr diff %s: %w", prRef, err)
	}

	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(wd, ".aios", "runs"), runID)
	if err != nil {
		return err
	}
	_ = rec.WriteFile("review/diff.patch", []byte(diff))
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	_ = rec.WriteFile("review/pr-meta.json", metaJSON)

	claude := &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Claude)}
	codex := &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Codex)}
	var synth engine.Engine = codex
	synthName := "codex"
	if cfg.Engines.ReviewerDefault == "claude" {
		synth = claude
		synthName = "claude"
	}
	_ = synthName // retained for future per-merge attribution

	prompt, err := prompts.Render("pr-review.tmpl", map[string]string{
		"Repo":    meta.Repo,
		"Title":   meta.Title,
		"HeadRef": meta.HeadRefName,
		"BaseRef": meta.BaseRefName,
		"Body":    meta.Body,
		"Diff":    diff,
	})
	if err != nil {
		return fmt.Errorf("render pr-review: %w", err)
	}

	fmt.Printf("review: PR %s — running Claude and Codex in parallel…\n", prRef)
	var (
		reviewA, reviewB string
		errA, errB       error
		wg               sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		r, err := claude.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: prompt})
		if err != nil {
			errA = err
			return
		}
		reviewA = r.Text
	}()
	go func() {
		defer wg.Done()
		r, err := codex.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: prompt})
		if err != nil {
			errB = err
			return
		}
		reviewB = r.Text
	}()
	wg.Wait()
	_ = rec.WriteFile("review/claude.txt", []byte(reviewA))
	_ = rec.WriteFile("review/codex.txt", []byte(reviewB))

	if errA != nil && errB != nil {
		return fmt.Errorf("both reviewers errored: claude=%v, codex=%v", errA, errB)
	}
	// One-side fallback: post that side's raw review.
	if errA != nil || errB != nil {
		surviving := reviewA
		if errA != nil {
			surviving = reviewB
		}
		fmt.Println("review: one engine errored; using the surviving review without merge.")
		fmt.Println(surviving)
		if post {
			return postPRComment(ctx, prRef, surviving)
		}
		return nil
	}

	mergePrompt, err := prompts.Render("pr-review-merge.tmpl", map[string]string{
		"Title":   meta.Title,
		"AuthorA": "claude",
		"AuthorB": "codex",
		"ReviewA": reviewA,
		"ReviewB": reviewB,
	})
	if err != nil {
		return fmt.Errorf("render pr-review-merge: %w", err)
	}
	merged, err := synth.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: mergePrompt})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: synthesizer errored (%v); printing both raw reviews instead.\n", err)
		fmt.Println("\n## Claude review\n" + reviewA)
		fmt.Println("\n## Codex review\n" + reviewB)
		return nil
	}
	_ = rec.WriteFile("review/merged.md", []byte(merged.Text))
	fmt.Println(merged.Text)
	if post {
		return postPRComment(ctx, prRef, merged.Text)
	}
	return nil
}

// prRef is the canonical "owner/repo#number" or just "<number>" string we
// pass into gh sub-commands. gh accepts either; we keep both shapes
// supported via parsePRRef.
type prRef = string

func parsePRRef(s string) (prRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty PR identifier")
	}
	// Bare number — gh resolves this against the current repo.
	if _, err := strconv.Atoi(s); err == nil {
		return s, nil
	}
	// URL form: gh accepts the URL directly for `pr view` and `pr diff`.
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s, nil
	}
	// owner/repo#NNN — gh accepts this too.
	if strings.Contains(s, "#") {
		return s, nil
	}
	return "", fmt.Errorf("PR identifier must be a number, URL, or owner/repo#NNN; got %q", s)
}

// prMeta is the minimum the review prompt needs.
type prMeta struct {
	Repo        string `json:"-"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	URL         string `json:"url"`
}

func fetchPRMeta(ctx context.Context, ref prRef) (prMeta, error) {
	args := []string{"pr", "view", ref, "--json", "title,body,headRefName,baseRefName,url"}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		// Surface gh's stderr — that's where "no such PR", "auth missing",
		// "not in a repo" actually live; .Output() drops it.
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return prMeta{}, fmt.Errorf("%w: %s", err, stderr)
	}
	var m prMeta
	if err := json.Unmarshal(out, &m); err != nil {
		return prMeta{}, fmt.Errorf("parse gh pr view output: %w", err)
	}
	// Derive repo from the URL: https://github.com/<owner>/<repo>/pull/N
	m.Repo = repoFromURL(m.URL)
	return m, nil
}

func repoFromURL(url string) string {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(url, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func fetchPRDiff(ctx context.Context, ref prRef) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "diff", ref)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("%w: %s", err, stderr)
	}
	return string(out), nil
}

func postPRComment(ctx context.Context, ref prRef, body string) error {
	tmp, err := os.CreateTemp("", "aios-review-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	cmd := exec.CommandContext(ctx, "gh", "pr", "comment", ref, "--body-file", tmp.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh pr comment: %w\n%s", err, string(out))
	}
	fmt.Printf("review: posted to %s\n", strings.TrimSpace(string(out)))
	return nil
}
