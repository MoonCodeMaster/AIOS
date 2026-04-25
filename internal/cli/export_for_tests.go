package cli

import (
	"context"

	"github.com/MoonCodeMaster/AIOS/internal/run"
)

// FinalizerOptsForTest mirrors finalizerOpts for external test packages.
type FinalizerOptsForTest = finalizerOpts

// FinalizerResultForTest mirrors finalizerResult for external test packages.
type FinalizerResultForTest = finalizerResult

// RunAutopilotFinalizerForTest is a test-only entry point. Production code
// must not depend on it.
func RunAutopilotFinalizerForTest(ctx context.Context, opts FinalizerOptsForTest) (*FinalizerResultForTest, error) {
	return runAutopilotFinalizer(ctx, opts)
}

// WriteAutopilotSummaryForTest is a test-only entry point.
func WriteAutopilotSummaryForTest(rec *run.Recorder, res *FinalizerResultForTest, finalizerErr error) error {
	return writeAutopilotSummary(rec, res, finalizerErr)
}
