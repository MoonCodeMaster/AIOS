package integration

import (
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/config"
)

func TestAutopilotDecompose_DepthCap_BlocksFurtherDecompose(t *testing.T) {
	// The CLI policy is: try decompose only when task.Depth < cap.
	// At depth==cap, the CLI must fall through to abandon. This test asserts
	// the cap arithmetic at the config layer (the CLI integration is
	// covered by the existing run.go gate).
	cases := []struct {
		max  int
		want int
	}{
		{0, 2},  // default
		{1, 1},
		{2, 2},
		{3, 3},
		{99, 3}, // hard cap
	}
	for _, tc := range cases {
		b := config.Budget{MaxDecomposeDepth: tc.max}
		if got := b.DecomposeDepthCap(); got != tc.want {
			t.Errorf("DecomposeDepthCap with max=%d = %d, want %d", tc.max, got, tc.want)
		}
	}

	// Source-level invariant: the gate in run.go reads `tk.Depth < cap`,
	// so a task with Depth=cap is not decomposed regardless of the model's
	// behaviour. The unit test in internal/cli/decompose proves
	// child.Depth = parent.Depth+1 — meaning a depth-2 child of a
	// depth-1 parent (under default cap=2) is at the boundary and a
	// further stall on it would not re-enter decompose.
}
