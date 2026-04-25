package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func TestServeRunner_Merged_LabelsAndCloses(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := NewServeState()
	cfg := &ServeConfig{}
	applyServeDefaults(cfg)

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: func(_ context.Context, _ string) (AutopilotResult, error) {
			return AutopilotResult{Status: AutopilotMerged, PRNumber: 99, PRURL: "https://example.invalid/pull/99"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	if !host.Closed[42] {
		t.Error("merged issue must be closed")
	}
	got := labelSetOf(host.Issues, 42)
	if got["aios:do"] {
		t.Error("aios:do should be removed after merge")
	}
	if !got["aios:done"] {
		t.Errorf("aios:done expected, labels = %v", got)
	}
	comments := host.Comments[42]
	if len(comments) == 0 || !strings.Contains(comments[0], "#99") {
		t.Errorf("expected merge comment referencing #99, got %v", comments)
	}
}

func TestServeRunner_Abandoned_OpensStuckIssue(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := NewServeState()
	cfg := &ServeConfig{}
	applyServeDefaults(cfg)

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: func(_ context.Context, _ string) (AutopilotResult, error) {
			return AutopilotResult{Status: AutopilotAbandoned, AuditTrail: "trail content"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	if len(host.OpenedIssues) != 1 {
		t.Fatalf("expected 1 stuck issue opened, got %d", len(host.OpenedIssues))
	}
	stuck := host.OpenedIssues[0]
	if !strings.HasPrefix(stuck.Title, "[aios:stuck]") {
		t.Errorf("stuck issue title = %q, want [aios:stuck] prefix", stuck.Title)
	}
	got := labelSetOf(host.Issues, 42)
	if !got["aios:stuck"] {
		t.Errorf("aios:stuck expected on original issue, labels = %v", got)
	}
	if host.Closed[42] {
		t.Error("abandoned issue should NOT be closed (waiting for human triage)")
	}
}

func TestServeRunner_PROpenRed_KeepsOpen(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := NewServeState()
	cfg := &ServeConfig{}
	applyServeDefaults(cfg)

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: func(_ context.Context, _ string) (AutopilotResult, error) {
			return AutopilotResult{Status: AutopilotPRRed, PRNumber: 99, PRURL: "https://example.invalid/pull/99"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	got := labelSetOf(host.Issues, 42)
	if !got["aios:pr-open"] {
		t.Errorf("aios:pr-open expected, labels = %v", got)
	}
	if host.Closed[42] {
		t.Error("PR-red issue should NOT be closed")
	}
}

func TestServeRunner_AutopilotError_SurfacesAndReleases(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := NewServeState()
	cfg := &ServeConfig{}
	applyServeDefaults(cfg)

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: func(_ context.Context, _ string) (AutopilotResult, error) {
			return AutopilotResult{}, errors.New("autopilot binary not found")
		},
	}
	err := runner.RunIssue(context.Background(), host.Issues[0])
	if err == nil {
		t.Fatal("expected error from autopilot")
	}
	got := labelSetOf(host.Issues, 42)
	if got["aios:in-progress"] {
		t.Error("aios:in-progress should be removed on error")
	}
	if !got["aios:do"] {
		t.Error("aios:do should be re-added on error so the issue can be retried")
	}
}

func labelSetOf(issues []githost.Issue, num int) map[string]bool {
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
