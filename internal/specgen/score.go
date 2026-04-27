package specgen

import (
	"errors"
	"strconv"
	"strings"
)

// ErrEmptyCritique is returned when the critique output is empty or blank.
var ErrEmptyCritique = errors.New("specgen: empty critique output")

// ParseCritiqueOutput parses the structured critique output into a score and
// issue list. Missing dimensions default to 0, malformed totals are recomputed,
// unknown lines are ignored, and empty input returns ErrEmptyCritique.
func ParseCritiqueOutput(raw string) (*SpecScore, []CritiqueIssue, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &SpecScore{}, nil, ErrEmptyCritique
	}

	score := &SpecScore{}
	var issues []CritiqueIssue
	inScore := false
	inIssues := false
	gotTotal := false
	declaredTotal := 0

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if upper == "SCORE" {
			inScore = true
			inIssues = false
			continue
		}
		if upper == "ISSUES" {
			inIssues = true
			inScore = false
			continue
		}
		if inScore {
			if k, v, ok := parseScoreKV(line); ok {
				switch k {
				case "completeness":
					score.Completeness = clampDim(v)
				case "testability":
					score.Testability = clampDim(v)
				case "scope_coherence":
					score.ScopeCoherence = clampDim(v)
				case "constraint_clarity":
					score.ConstraintClarity = clampDim(v)
				case "total":
					declaredTotal = v
					gotTotal = true
				}
			}
		}
		if inIssues && strings.HasPrefix(line, "- ") {
			rest := strings.TrimPrefix(line, "- ")
			dim, note := splitIssue(rest)
			issues = append(issues, CritiqueIssue{Dimension: dim, Note: note})
		}
	}

	computed := score.Completeness + score.Testability + score.ScopeCoherence + score.ConstraintClarity
	if gotTotal && declaredTotal == computed {
		score.Total = declaredTotal
	} else {
		score.Total = computed
	}
	return score, issues, nil
}

func parseScoreKV(line string) (string, int, bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", 0, false
	}
	k := strings.TrimSpace(line[:idx])
	v, err := strconv.Atoi(strings.TrimSpace(line[idx+1:]))
	if err != nil {
		return k, 0, false
	}
	return k, v, true
}

func clampDim(v int) int {
	if v < 0 {
		return 0
	}
	if v > 3 {
		return 3
	}
	return v
}

func splitIssue(s string) (string, string) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return "", s
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:])
}
