package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newAutopilotCmd is the user-facing single command:
//
//	aios autopilot "<idea>"
//
// It runs `aios new --auto` then `aios run --autopilot --merge` end-to-end
// with no human prompts. Equivalent to invoking those two commands by hand,
// minus the confirm gate and minus the manual `git merge`.
func newAutopilotCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "autopilot <idea>",
		Short: "Run new+run end-to-end with no prompts; open PR, wait for CI, squash-merge",
		Long: `Drives the full AIOS lifecycle for one idea with no human input:

  1. brainstorm + spec-synth + decompose (no confirmation prompt)
  2. coder↔reviewer loop per task with verify+escalation
  3. open PR aios/staging→main, poll GitHub Actions, squash-merge on green

Stalled tasks are abandoned (audit trail under .aios/runs/<id>/abandoned/<task>/)
so a single bad task does not block the rest of the run. CI red or timeout
leaves the PR open without merging — the URL is printed and the run exits 2.

Requires: gh CLI on PATH, an authenticated gh session, and a configured git remote.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idea := strings.Join(args, " ")
			if err := runNew(NewOpts{Idea: idea, Auto: true}); err != nil {
				return fmt.Errorf("aios new (auto): %w", err)
			}
			runCmd := newRunCmd()
			_ = runCmd.Flags().Set("autopilot", "true")
			_ = runCmd.Flags().Set("merge", "true")
			return runMain(runCmd, nil)
		},
	}
	return c
}
