package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func TestAutopilot_CIRed_LeavesPROpenAndDoesNotMerge(t *testing.T) {
	host := &githost.FakeHost{ChecksByPR: map[int]githost.ChecksState{1: githost.ChecksRed}}

	res, err := cli.RunAutopilotFinalizerForTest(context.Background(), cli.FinalizerOptsForTest{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		Title:          "aios: 001-a",
		Body:           "test body",
		ConvergedCount: 1,
		ChecksTimeout:  time.Second,
	})
	if err == nil {
		t.Fatal("expected error on red checks")
	}
	if !strings.Contains(err.Error(), "red") {
		t.Errorf("error %q should mention 'red'", err)
	}
	if res == nil || res.PR == nil {
		t.Fatal("PR should still be reported on red so user has the URL")
	}
	if host.Merged[res.PR.Number] {
		t.Errorf("must not merge red PR #%d", res.PR.Number)
	}
	if len(host.OpenedPRs) != 1 {
		t.Errorf("expected exactly 1 PR opened, got %d", len(host.OpenedPRs))
	}
}
