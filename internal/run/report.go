package run

import (
	"fmt"
	"strings"
)

type Report struct {
	TaskID      string
	Status      string // "converged" | "blocked"
	Reason      string // machine-readable reason code
	Rounds      []Round
	UsageTokens int
}

type Round struct {
	N              int
	DiffLines      int
	VerifyGreen    bool
	ReviewApproved bool
	UnmetCriteria  []string
	IssueCount     int
}

func RenderReport(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task %s — %s\n\n", r.TaskID, r.Status)
	if r.Reason != "" {
		fmt.Fprintf(&b, "**Reason:** `%s`\n\n", r.Reason)
	}
	fmt.Fprintf(&b, "**Total token usage:** %d\n\n", r.UsageTokens)
	fmt.Fprintln(&b, "## Rounds")
	for _, round := range r.Rounds {
		fmt.Fprintf(&b, "\n### Round %d\n", round.N)
		fmt.Fprintf(&b, "- diff lines: %d\n", round.DiffLines)
		fmt.Fprintf(&b, "- verify green: %v\n", round.VerifyGreen)
		fmt.Fprintf(&b, "- reviewer approved: %v\n", round.ReviewApproved)
		fmt.Fprintf(&b, "- unmet criteria (%d):\n", len(round.UnmetCriteria))
		for _, c := range round.UnmetCriteria {
			fmt.Fprintf(&b, "  - %s\n", c)
		}
		fmt.Fprintf(&b, "- reviewer issue count: %d\n", round.IssueCount)
	}
	return b.String()
}
