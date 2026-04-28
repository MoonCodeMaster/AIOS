package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MoonCodeMaster/AIOS/internal/lessons"
	"github.com/spf13/cobra"
)

// `aios lessons` mines every reviewer-response.json under .aios/runs/ and
// reports the top recurring issue categories, notes, and file hot-spots.
// A 30-second read that tells the user where to spend coder-prompt edits,
// spec rewrites, or refactors to pay back the most review-loop time.
func newLessonsCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "lessons",
		Short:         "Mine .aios/runs/ for recurring reviewer issues; report the top patterns",
		Annotations:   map[string]string{gateAnnotation: gateLevelAIOS},
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `aios lessons walks every reviewer-response.json under .aios/runs/ and
emits a small report:

  - top categories of reviewer issue (acceptance, correctness, style, …)
  - top recurring note shapes (numbers and case normalised, so two notes
    differing only in line numbers cluster together)
  - hot-spot files that show up in many issues
  - noisiest runs by issue count

The intent is signal, not data: 10–15 lines of "here is what AIOS keeps
catching for you, and here is where it lives." Use the output to update
coder.tmpl, the spec, or the codebase itself.

Reads on-disk artifacts only; no model call.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			runsDir := filepath.Join(wd, ".aios", "runs")
			if _, err := os.Stat(runsDir); err != nil {
				return fmt.Errorf("no runs directory at %s — run `aios run` first", runsDir)
			}
			rep, err := lessons.Mine(runsDir)
			if err != nil {
				return err
			}
			rep.Render(cmd.OutOrStdout())
			return nil
		},
	}
}
