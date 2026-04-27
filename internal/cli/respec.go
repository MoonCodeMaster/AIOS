package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
)

type respecConfig struct {
	Enabled    bool
	MinOverlap float64
}

// shouldRespec returns true when the respec trigger conditions are all met:
// enabled, first attempt, >=2 abandons, and sufficient fingerprint overlap.
func shouldRespec(abandons []orchestrator.Outcome, cfg respecConfig, attempt int) bool {
	if !cfg.Enabled || attempt > 0 || len(abandons) < 2 {
		return false
	}
	return avgPairwiseJaccard(abandons) >= cfg.MinOverlap
}

type fingerprint = map[string]bool

func taskFingerprint(o orchestrator.Outcome) fingerprint {
	fp := fingerprint{}
	for _, r := range o.Rounds {
		for _, iss := range r.Review.Issues {
			key := iss.File + ":" + iss.Category
			fp[key] = true
		}
	}
	return fp
}

func jaccard(a, b fingerprint) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a)
	for k := range b {
		if !a[k] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func avgPairwiseJaccard(outcomes []orchestrator.Outcome) float64 {
	fps := make([]fingerprint, len(outcomes))
	for i, o := range outcomes {
		fps[i] = taskFingerprint(o)
	}
	var sum float64
	pairs := 0
	for i := 0; i < len(fps); i++ {
		for j := i + 1; j < len(fps); j++ {
			sum += jaccard(fps[i], fps[j])
			pairs++
		}
	}
	if pairs == 0 {
		return 0
	}
	return sum / float64(pairs)
}

// aggregateFeedback builds a bullet list summarizing abandoned tasks for the
// respec feedback template. Capped at 200 lines; oldest tasks truncated first.
func aggregateFeedback(outcomes []orchestrator.Outcome, taskIDs []string) string {
	const maxLines = 200
	var sections []string
	for i, o := range outcomes {
		id := ""
		if i < len(taskIDs) {
			id = taskIDs[i]
		}
		sections = append(sections, formatTaskFeedback(id, o))
	}

	// Join and check line count; truncate oldest first if over budget.
	for len(sections) > 1 {
		joined := strings.Join(sections, "\n")
		if len(strings.Split(joined, "\n")) <= maxLines {
			return joined
		}
		sections = sections[1:]
	}
	return strings.Join(sections, "\n")
}

func formatTaskFeedback(id string, o orchestrator.Outcome) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s:", id)

	// Unmet criteria from last round.
	if len(o.Rounds) > 0 {
		last := o.Rounds[len(o.Rounds)-1]
		var unmet []string
		for _, c := range last.Review.Criteria {
			if c.Status != "satisfied" {
				unmet = append(unmet, c.ID)
			}
		}
		if len(unmet) > 0 {
			fmt.Fprintf(&b, " unmet criteria: %s;", strings.Join(unmet, ", "))
		}
	}

	// Category counts across all rounds.
	cats := map[string]int{}
	files := map[string]bool{}
	for _, r := range o.Rounds {
		for _, iss := range r.Review.Issues {
			if iss.Category != "" {
				cats[iss.Category]++
			}
			if iss.File != "" {
				files[iss.File] = true
			}
		}
	}
	if len(cats) > 0 {
		var parts []string
		for k, v := range cats {
			parts = append(parts, fmt.Sprintf("%s(%d)", k, v))
		}
		sort.Strings(parts)
		fmt.Fprintf(&b, " recurring issues: %s;", strings.Join(parts, ", "))
	}
	if len(files) > 0 {
		sorted := make([]string, 0, len(files))
		for f := range files {
			sorted = append(sorted, f)
		}
		sort.Strings(sorted)
		fmt.Fprintf(&b, " touched files: %s", strings.Join(sorted, ", "))
	}
	return b.String()
}
