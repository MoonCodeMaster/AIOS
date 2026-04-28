package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/MoonCodeMaster/AIOS/internal/cost"
	"github.com/spf13/cobra"
)

// `aios cost` reports a USD estimate for one run, the most recent run,
// or all runs in .aios/runs/. The summary is computed entirely from the
// already-persisted on-disk artifacts — no model call, no token counter
// running in the background. You can run `aios cost` against a year-old
// run directory and get the same answer the original session would have
// shown at exit.
func newCostCmd() *cobra.Command {
	c := &cobra.Command{
		Use:         "cost [run-id]",
		Short:       "Estimate the USD cost of a run from its on-disk audit trail",
		Annotations: map[string]string{gateAnnotation: gateLevelAIOS},
		Long: `aios cost walks .aios/runs/<id>/**/coder.response.raw and
reviewer.response.raw, sums tokens by engine, and applies the pricing table
in internal/cost/pricing.go.

Without arguments, costs the most recent run. Pass --all to cost every run
in .aios/runs/ and print a per-run table. Pass an explicit run ID to cost
that one run.

Pricing is hardcoded; treat the result as an estimate, not an invoice.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, _ := cmd.Flags().GetBool("all")
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			runsDir := filepath.Join(wd, ".aios", "runs")
			if all {
				return costAllRuns(runsDir, cmd.OutOrStdout())
			}
			runID := ""
			if len(args) > 0 {
				runID = args[0]
			}
			if runID == "" {
				latest, err := latestRunID(runsDir)
				if err != nil {
					return err
				}
				if latest == "" {
					return fmt.Errorf("no runs in %s", runsDir)
				}
				runID = latest
			}
			return costOneRun(filepath.Join(runsDir, runID), runID, cmd.OutOrStdout())
		},
	}
	c.Flags().Bool("all", false, "cost every run in .aios/runs/ and print a per-run summary")
	return c
}

func costOneRun(runDir, runID string, out io.Writer) error {
	tally, err := cost.FromRunDir(runDir)
	if err != nil {
		return fmt.Errorf("walk %s: %w", runDir, err)
	}
	fmt.Fprintf(out, "run: %s\n", runID)
	tally.Render(out)
	return nil
}

func costAllRuns(runsDir string, out io.Writer) error {
	ids, err := listRunIDs(runsDir)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("no runs in %s", runsDir)
	}
	var grandTotal float64
	for _, id := range ids {
		tally, err := cost.FromRunDir(filepath.Join(runsDir, id))
		if err != nil {
			fmt.Fprintf(out, "%s  error: %v\n", id, err)
			continue
		}
		usd := tally.EstimateUSD()
		grandTotal += usd
		fmt.Fprintf(out, "%s  $%6.2f\n", id, usd)
	}
	fmt.Fprintf(out, "─────────────────────────────────────\n")
	fmt.Fprintf(out, "total across %d runs:  $%6.2f\n", len(ids), grandTotal)
	return nil
}

func listRunIDs(runsDir string) ([]string, error) {
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func latestRunID(runsDir string) (string, error) {
	ids, err := listRunIDs(runsDir)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", nil
	}
	// Run IDs are timestamp-formatted (2006-01-02T15-04-05) so lexicographic
	// sort = chronological sort.
	return ids[len(ids)-1], nil
}
