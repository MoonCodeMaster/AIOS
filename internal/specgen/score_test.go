package specgen

import (
	"testing"
)

func TestParseScore(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantScore  SpecScore
		wantIssues int
		wantErr    bool
	}{
		{
			name: "well-formed",
			input: `SCORE
completeness: 3
testability: 2
scope_coherence: 3
constraint_clarity: 2
total: 10

ISSUES
- testability: acceptance criterion 3 lacks a measurable assertion
- constraint_clarity: no error budget specified for the new endpoint`,
			wantScore:  SpecScore{Completeness: 3, Testability: 2, ScopeCoherence: 3, ConstraintClarity: 2, Total: 10},
			wantIssues: 2,
		},
		{
			name: "missing dimension defaults to zero",
			input: `SCORE
completeness: 3
testability: 2
total: 5

ISSUES`,
			wantScore:  SpecScore{Completeness: 3, Testability: 2, ScopeCoherence: 0, ConstraintClarity: 0, Total: 5},
			wantIssues: 0,
		},
		{
			name: "malformed total recomputed",
			input: `SCORE
completeness: 3
testability: 2
scope_coherence: 3
constraint_clarity: 2
total: 99

ISSUES`,
			wantScore:  SpecScore{Completeness: 3, Testability: 2, ScopeCoherence: 3, ConstraintClarity: 2, Total: 10},
			wantIssues: 0,
		},
		{
			name:      "empty input",
			input:     "",
			wantScore: SpecScore{Total: 0},
			wantErr:   true,
		},
		{
			name: "unknown lines ignored",
			input: `SCORE
completeness: 3
testability: 3
scope_coherence: 3
constraint_clarity: 3
total: 12
some_random_field: 99

ISSUES
- completeness: minor gap`,
			wantScore:  SpecScore{Completeness: 3, Testability: 3, ScopeCoherence: 3, ConstraintClarity: 3, Total: 12},
			wantIssues: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, issues, err := ParseCritiqueOutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if score.Completeness != tt.wantScore.Completeness {
				t.Errorf("Completeness = %d, want %d", score.Completeness, tt.wantScore.Completeness)
			}
			if score.Testability != tt.wantScore.Testability {
				t.Errorf("Testability = %d, want %d", score.Testability, tt.wantScore.Testability)
			}
			if score.ScopeCoherence != tt.wantScore.ScopeCoherence {
				t.Errorf("ScopeCoherence = %d, want %d", score.ScopeCoherence, tt.wantScore.ScopeCoherence)
			}
			if score.ConstraintClarity != tt.wantScore.ConstraintClarity {
				t.Errorf("ConstraintClarity = %d, want %d", score.ConstraintClarity, tt.wantScore.ConstraintClarity)
			}
			if score.Total != tt.wantScore.Total {
				t.Errorf("Total = %d, want %d", score.Total, tt.wantScore.Total)
			}
			if len(issues) != tt.wantIssues {
				t.Errorf("issues = %d, want %d", len(issues), tt.wantIssues)
			}
		})
	}
}
