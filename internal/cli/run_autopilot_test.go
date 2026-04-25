package cli

import (
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
)

func TestAutopilotRescues_OnlyStall(t *testing.T) {
	cases := []struct {
		code orchestrator.BlockCode
		want bool
	}{
		{orchestrator.CodeStallNoProgress, true},
		{orchestrator.CodeMaxRoundsExceeded, false},
		{orchestrator.CodeMaxTokensExceeded, false},
		{orchestrator.CodeEngineInvokeFailed, false},
		{orchestrator.CodeRebaseConflict, false},
		{orchestrator.CodeUpstreamBlocked, false},
	}
	for _, c := range cases {
		got := autopilotRescues(c.code)
		if got != c.want {
			t.Errorf("autopilotRescues(%q) = %v, want %v", c.code, got, c.want)
		}
	}
}
