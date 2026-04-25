package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func defaultServeConfig() *cli.ServeConfig {
	c := &cli.ServeConfig{}
	cli.ApplyServeDefaultsForTest(c)
	return c
}

func TestServe_Merged_ClosesIssueWithDoneLabel(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := cli.NewServeState()
	runner := &cli.ServeRunner{
		Host: host, State: state, Config: defaultServeConfig(),
		Autopilot: func(_ context.Context, _ string) (cli.AutopilotResult, error) {
			return cli.AutopilotResult{Status: cli.AutopilotMerged, PRNumber: 99, PRURL: "https://example.invalid/pull/99"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	if !host.Closed[42] {
		t.Error("merged issue must be closed")
	}
	labels := labelsAsSet(host.Issues, 42)
	if !labels["aios:done"] {
		t.Errorf("aios:done expected, got %v", labels)
	}
	if labels["aios:do"] || labels["aios:in-progress"] {
		t.Errorf("aios:do and aios:in-progress should be removed, got %v", labels)
	}
}

func TestServe_Abandoned_FilesStuckIssue(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Refactor everything", Body: "vague request", Labels: []string{"aios:do"}},
	}}
	state := cli.NewServeState()
	runner := &cli.ServeRunner{
		Host: host, State: state, Config: defaultServeConfig(),
		Autopilot: func(_ context.Context, _ string) (cli.AutopilotResult, error) {
			return cli.AutopilotResult{Status: cli.AutopilotAbandoned, AuditTrail: "stall_no_progress: ..."}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	if len(host.OpenedIssues) != 1 {
		t.Fatalf("expected 1 stuck issue, got %d", len(host.OpenedIssues))
	}
	stuck := host.OpenedIssues[0]
	if !strings.Contains(stuck.Title, "[aios:stuck]") {
		t.Errorf("stuck title = %q, want [aios:stuck] prefix", stuck.Title)
	}
	if host.Closed[42] {
		t.Error("abandoned issue must NOT be closed (waiting for human triage)")
	}
}

func TestServe_PROpenRed_KeepsIssueOpen(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "", Labels: []string{"aios:do"}},
	}}
	state := cli.NewServeState()
	runner := &cli.ServeRunner{
		Host: host, State: state, Config: defaultServeConfig(),
		Autopilot: func(_ context.Context, _ string) (cli.AutopilotResult, error) {
			return cli.AutopilotResult{Status: cli.AutopilotPRRed, PRNumber: 99, PRURL: "https://example.invalid/pull/99"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	labels := labelsAsSet(host.Issues, 42)
	if !labels["aios:pr-open"] {
		t.Errorf("aios:pr-open expected, got %v", labels)
	}
	if host.Closed[42] {
		t.Error("CI-red issue must not be closed")
	}
	if len(host.Comments[42]) == 0 || !strings.Contains(host.Comments[42][0], "#99") {
		t.Errorf("expected comment referencing PR #99, got %v", host.Comments[42])
	}
}

func labelsAsSet(issues []githost.Issue, num int) map[string]bool {
	m := map[string]bool{}
	for _, i := range issues {
		if i.Number == num {
			for _, l := range i.Labels {
				m[l] = true
			}
		}
	}
	return m
}
