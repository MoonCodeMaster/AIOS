package integration

import (
	"context"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func TestServe_Recovery_GitHubOrphanReleased(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "in-flight before kill", Labels: []string{"aios:in-progress"}},
	}}
	state := cli.NewServeState()
	if err := state.Reconcile(context.Background(), host, "aios:do", "aios:in-progress"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	labels := map[string]bool{}
	for _, l := range host.Issues[0].Labels {
		labels[l] = true
	}
	if labels["aios:in-progress"] {
		t.Error("aios:in-progress should be removed by reconcile")
	}
	if !labels["aios:do"] {
		t.Errorf("aios:do should be re-added by reconcile, got %v", labels)
	}
}

func TestServe_Recovery_StateOrphanDropped(t *testing.T) {
	host := &githost.FakeHost{}
	state := cli.NewServeState()
	state.Add(99, "stale-run")
	if err := state.Reconcile(context.Background(), host, "aios:do", "aios:in-progress"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, present := state.Issues[99]; present {
		t.Errorf("state-only orphan should be dropped, state = %+v", state.Issues)
	}
}
