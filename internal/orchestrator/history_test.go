package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/verify"
)

func TestAlgorithmicBrief_FiveRoundsThresholdTwo(t *testing.T) {
	rounds := makeTestRounds(5)
	brief := AlgorithmicBrief(rounds[:3], 50000)
	if brief == "" {
		t.Fatal("brief should not be empty for 3 compressible rounds")
	}
	if !strings.Contains(brief, "Prior rounds (compressed):") {
		t.Error("brief should start with header")
	}
	if !strings.Contains(brief, "Round 1:") {
		t.Error("brief should contain Round 1")
	}
	if !strings.Contains(brief, "Round 3:") {
		t.Error("brief should contain Round 3")
	}
	// Rounds 4 and 5 are verbatim (last K=2), so they should NOT appear in the brief.
	if strings.Contains(brief, "Round 4:") {
		t.Error("brief should not contain Round 4 (verbatim)")
	}
	if strings.Contains(brief, "Round 5:") {
		t.Error("brief should not contain Round 5 (verbatim)")
	}
}

func TestAlgorithmicBrief_OneRound_NoCompression(t *testing.T) {
	// CompressRounds should return empty when nothing to compress.
	rounds := makeTestRounds(1)
	brief := AlgorithmicBrief(rounds[:0], 50000)
	if brief != "" {
		t.Errorf("brief should be empty for 0 compressible rounds, got %q", brief)
	}
}

func TestAlgorithmicBrief_TokenBudget(t *testing.T) {
	rounds := makeTestRounds(5)
	brief := AlgorithmicBrief(rounds[:3], 50000)
	// ~100 tokens per round target. Simple word count as proxy.
	words := len(strings.Fields(brief))
	// 3 rounds * 100 tokens ≈ 300 words max (generous). Header adds a few.
	if words > 400 {
		t.Errorf("brief has %d words, expected under 400 for 3 rounds", words)
	}
}

func TestAlgorithmicBrief_Deterministic(t *testing.T) {
	rounds := makeTestRounds(5)
	brief1 := AlgorithmicBrief(rounds[:3], 50000)
	brief2 := AlgorithmicBrief(rounds[:3], 50000)
	if brief1 != brief2 {
		t.Error("algorithmic brief must be deterministic; two calls produced different output")
	}
}

func TestAlgorithmicBrief_FileListTruncation(t *testing.T) {
	// Round with >10 files should truncate the file list.
	r := RoundRecord{
		N: 1,
		Review: ReviewResult{
			Criteria: []CriterionStatus{{ID: "c1", Status: "unmet"}},
			Issues:   make([]ReviewIssue, 0, 12),
		},
		Checks: []verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}},
	}
	for i := 0; i < 12; i++ {
		r.Review.Issues = append(r.Review.Issues, ReviewIssue{
			Severity: "blocking",
			File:     fmt.Sprintf("file%d.go", i),
		})
	}
	brief := AlgorithmicBrief([]RoundRecord{r}, 50000)
	if !strings.Contains(brief, "...") || !strings.Contains(brief, "more") {
		t.Error("file list should be truncated when >10 files")
	}
}

func TestLLMBrief_Success(t *testing.T) {
	fakeReviewer := &engine.FakeEngine{
		Name_: "codex",
		Script: []engine.InvokeResponse{
			{Text: "Rounds 1-3 summary: coder fixed auth bugs across 3 rounds."},
		},
	}
	rounds := makeTestRounds(3)
	brief, tokens, err := LLMBrief(context.Background(), fakeReviewer, rounds, 50000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if brief == "" {
		t.Error("brief should not be empty")
	}
	_ = tokens // CompressionTokens tracked but value depends on fake
}

func TestLLMBrief_FallbackOnError(t *testing.T) {
	fakeReviewer := &engine.FakeEngine{
		Name_:  "codex",
		Script: []engine.InvokeResponse{}, // exhausted → error
	}
	rounds := makeTestRounds(3)
	brief, _, err := LLMBrief(context.Background(), fakeReviewer, rounds, 50000)
	if err != nil {
		t.Fatalf("should fall back to algorithmic, not error: %v", err)
	}
	if !strings.Contains(brief, "Prior rounds (compressed):") {
		t.Error("fallback should produce algorithmic brief")
	}
}

func TestCompressRounds_Disabled(t *testing.T) {
	rounds := makeTestRounds(5)
	brief, _, err := CompressRounds(context.Background(), CompressConfig{
		Enabled:      false,
		AfterRounds:  2,
		TargetTokens: 50000,
	}, rounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if brief != "" {
		t.Errorf("compression disabled should return empty brief, got %q", brief)
	}
}

func TestCompressRounds_BelowThreshold(t *testing.T) {
	rounds := makeTestRounds(2)
	brief, _, err := CompressRounds(context.Background(), CompressConfig{
		Enabled:      true,
		AfterRounds:  2,
		TargetTokens: 50000,
	}, rounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if brief != "" {
		t.Errorf("below threshold should return empty brief, got %q", brief)
	}
}

// makeTestRounds builds N rounds with realistic review data for testing.
func makeTestRounds(n int) []RoundRecord {
	rounds := make([]RoundRecord, n)
	for i := range rounds {
		rounds[i] = RoundRecord{
			N: i + 1,
			Review: ReviewResult{
				Approved: i == n-1,
				Criteria: []CriterionStatus{
					{ID: "c1", Status: "satisfied"},
					{ID: "c2", Status: "unmet"},
				},
				Issues: []ReviewIssue{
					{Severity: "blocking", Category: "correctness", Note: "missing nil check", File: "handler.go", Line: 42},
					{Severity: "nit", Category: "style", Note: "rename var", File: "db.go"},
				},
			},
			Checks: []verify.CheckResult{
				{Name: "test_cmd", Status: verify.StatusPassed},
				{Name: "lint_cmd", Status: verify.StatusFailed, ExitCode: 1},
			},
		}
	}
	return rounds
}

var _ = errors.New // keep import stable
