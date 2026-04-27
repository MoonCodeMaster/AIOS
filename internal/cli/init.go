package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/verify/detect"
	"github.com/spf13/cobra"
)

const v01Examples = `
# --- v0.1 features (uncomment to enable) ---

# [engines.claude]
# retry_max_attempts = 3         # total attempts per invocation (default 3)
# retry_base_ms = 1000           # base backoff in ms before jitter (default 1000)
# retry_enabled = true           # set false to disable retries (default true)

# [engines.codex]
# retry_max_attempts = 3
# retry_base_ms = 1000
# retry_enabled = true

# [specgen]
# critique_enabled = true        # cross-model critique after polish (default true)
# critique_threshold = 9         # score 0-12; refine fires below this (default 9)

# [parallel]
# max_parallel_tasks = 4         # number of concurrent task workers (default 4)
# max_tokens_per_run = 1000000   # run-wide token cap; 0 disables (default 1,000,000)

# [budget]
# compress_history = false       # compress older rounds in coder prompt (default false)
# compress_after_rounds = 2      # keep last N rounds verbatim (default 2)
# compress_target_tokens = 50000 # token budget for compressed brief (default 50000)

# [mcp.servers.github]
# binary = "github-mcp-server"
# args = ["stdio"]
# env = { GITHUB_TOKEN = "${env:GITHUB_TOKEN}" }
# allowed_tools = ["search_code", "get_pr"]

# [mcp.servers.fs-readonly]
# binary = "mcp-fs"
# args = ["--read-only", "--root", "."]
# allowed_tools = ["read_file", "list_dir"]

# Per-task opt-in (in .aios/tasks/<id>.md frontmatter):
#   mcp_allow: [github, fs-readonly]
#   mcp_allow_tools:
#     github: [search_code]
`

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Bootstrap AIOS in the current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			if _, err := os.Stat(filepath.Join(wd, ".git")); err != nil {
				return fmt.Errorf("not a git repo: .git missing")
			}
			cfgPath := filepath.Join(wd, ".aios", "config.toml")
			if _, err := os.Stat(cfgPath); err == nil {
				fmt.Println("AIOS already initialized (.aios/config.toml exists). Re-running will overwrite it.")
				if !confirm("Continue? [y/N] ") {
					return nil
				}
			}

			s := detect.All(wd)
			reader := bufio.NewReader(os.Stdin)
			cfg := config.Config{
				SchemaVersion: config.CurrentSchemaVersion,
				Project:       config.Project{Name: filepath.Base(wd), BaseBranch: "main", StagingBranch: "aios/staging"},
				Engines: config.Engines{
					CoderDefault:    "claude",
					ReviewerDefault: "codex",
					RolesByKind: map[string]string{
						"feature": "claude", "scaffold": "claude", "greenfield": "claude",
						"bugfix": "codex", "refactor": "codex", "test-writing": "codex",
					},
					Claude: config.EngineBinary{Binary: "claude", TimeoutSec: 600},
					Codex:  config.EngineBinary{Binary: "codex", TimeoutSec: 600},
				},
				Budget: config.Budget{MaxRoundsPerTask: 5, MaxTokensPerTask: 200000, MaxWallMinutesPerTask: 30},
				Verify: config.Verify{
					TestCmd:      promptFor(reader, "test_cmd", s["test_cmd"]),
					LintCmd:      promptFor(reader, "lint_cmd", s["lint_cmd"]),
					TypecheckCmd: promptFor(reader, "typecheck_cmd", s["typecheck_cmd"]),
					BuildCmd:     promptFor(reader, "build_cmd", s["build_cmd"]),
				},
				Runtime: config.Runtime{SandboxImage: "aios/sandbox:latest", WorktreeRoot: ".aios/worktrees"},
			}

			if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
				return err
			}
			f, err := os.Create(cfgPath)
			if err != nil {
				return err
			}
			defer f.Close()
			if err := toml.NewEncoder(f).Encode(cfg); err != nil {
				return err
			}
			if _, err := fmt.Fprint(f, v01Examples); err != nil {
				return err
			}
			appendGitignore(wd)
			ensureStagingBranch(wd, cfg.Project.BaseBranch, cfg.Project.StagingBranch)
			fmt.Printf("Wrote %s\n", cfgPath)
			fmt.Printf("Ensured branch %s exists.\n", cfg.Project.StagingBranch)
			return nil
		},
	}
}

func promptFor(r *bufio.Reader, name, suggested string) string {
	if suggested != "" {
		fmt.Printf("%s [%s]: ", name, suggested)
	} else {
		fmt.Printf("%s (empty = not configured): ", name)
	}
	s, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return suggested
	}
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return suggested
	}
	return s
}

func confirm(msg string) bool {
	fmt.Print(msg)
	r := bufio.NewReader(os.Stdin)
	s, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	return strings.TrimSpace(strings.ToLower(s)) == "y"
}

func appendGitignore(wd string) {
	p := filepath.Join(wd, ".gitignore")
	existing, _ := os.ReadFile(p)
	marker := "# AIOS"
	if strings.Contains(string(existing), marker) {
		return
	}
	extra := "\n" + marker + "\n.aios/worktrees/\n.aios/runs/*/*/round-*/verify.json.tmp\n"
	_ = os.WriteFile(p, append(existing, []byte(extra)...), 0o644)
}

func ensureStagingBranch(wd, base, staging string) {
	// idempotent: ignore if branch already exists
	_ = exec.Command("git", "-C", wd, "branch", staging, base).Run()
}
