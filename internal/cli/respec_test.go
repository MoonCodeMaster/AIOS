package cli

import (
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
)

func makeOutcome(issues []orchestrator.ReviewIssue) orchestrator.Outcome {
	return orchestrator.Outcome{
		Final: orchestrator.StateBlocked,
		Rounds: []orchestrator.RoundRecord{
			{N: 1, Review: orchestrator.ReviewResult{Issues: issues}},
		},
	}
}

func TestShouldRespec_TwoOverlapping(t *testing.T) {
	a := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "correctness", File: "handler.go"},
		{Category: "test-coverage", File: "handler.go"},
		{Category: "correctness", File: "db.go"},
	})
	b := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "correctness", File: "handler.go"},
		{Category: "correctness", File: "db.go"},
	})
	cfg := respecConfig{Enabled: true, MinOverlap: 0.5}
	if !shouldRespec([]orchestrator.Outcome{a, b}, cfg, 0) {
		t.Error("expected respec trigger for overlapping abandons")
	}
}

func TestShouldRespec_TwoDisjoint(t *testing.T) {
	a := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "correctness", File: "handler.go"},
	})
	b := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "style", File: "config.go"},
	})
	cfg := respecConfig{Enabled: true, MinOverlap: 0.5}
	if shouldRespec([]orchestrator.Outcome{a, b}, cfg, 0) {
		t.Error("should not trigger for disjoint abandons")
	}
}

func TestShouldRespec_OneAbandon(t *testing.T) {
	a := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "correctness", File: "handler.go"},
	})
	cfg := respecConfig{Enabled: true, MinOverlap: 0.5}
	if shouldRespec([]orchestrator.Outcome{a}, cfg, 0) {
		t.Error("should not trigger for single abandon")
	}
}

func TestShouldRespec_ZeroAbandons(t *testing.T) {
	cfg := respecConfig{Enabled: true, MinOverlap: 0.5}
	if shouldRespec(nil, cfg, 0) {
		t.Error("should not trigger for zero abandons")
	}
}

func TestShouldRespec_RecursionCap(t *testing.T) {
	a := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "correctness", File: "handler.go"},
	})
	b := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "correctness", File: "handler.go"},
	})
	cfg := respecConfig{Enabled: true, MinOverlap: 0.5}
	if shouldRespec([]orchestrator.Outcome{a, b}, cfg, 1) {
		t.Error("should not trigger when respecAttempt=1")
	}
}

func TestShouldRespec_Disabled(t *testing.T) {
	a := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "correctness", File: "handler.go"},
	})
	b := makeOutcome([]orchestrator.ReviewIssue{
		{Category: "correctness", File: "handler.go"},
	})
	cfg := respecConfig{Enabled: false, MinOverlap: 0.5}
	if shouldRespec([]orchestrator.Outcome{a, b}, cfg, 0) {
		t.Error("should not trigger when disabled")
	}
}

func TestAggregateFeedback_Truncation(t *testing.T) {
	var outcomes []orchestrator.Outcome
	for i := 0; i < 50; i++ {
		outcomes = append(outcomes, makeOutcome([]orchestrator.ReviewIssue{
			{Category: "correctness", File: "handler.go", Note: "long issue description that repeats"},
			{Category: "test-coverage", File: "test.go", Note: "another long issue"},
			{Category: "style", File: "util.go", Note: "style issue"},
		}))
	}
	ids := make([]string, len(outcomes))
	for i := range ids {
		ids[i] = "task-" + strings.Repeat("x", 5)
	}
	fb := aggregateFeedback(outcomes, ids)
	lines := strings.Split(fb, "\n")
	if len(lines) > 200 {
		t.Errorf("feedback has %d lines, want <= 200", len(lines))
	}
}
