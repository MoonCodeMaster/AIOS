package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/verify"
)

// CompressConfig controls round-history compression behavior.
type CompressConfig struct {
	Enabled      bool
	AfterRounds  int // last K rounds stay verbatim
	TargetTokens int
	UseLLM       bool
}

// CompressRounds returns a compressed brief for rounds older than the last K,
// or empty string when compression is disabled or unnecessary. The second
// return value is the LLM token cost (0 for algorithmic).
func CompressRounds(ctx context.Context, cfg CompressConfig, rounds []RoundRecord, reviewer engine.Engine) (string, int, error) {
	if !cfg.Enabled {
		return "", 0, nil
	}
	if len(rounds) <= cfg.AfterRounds {
		return "", 0, nil
	}
	toCompress := rounds[:len(rounds)-cfg.AfterRounds]
	if len(toCompress) == 0 {
		return "", 0, nil
	}
	if cfg.UseLLM && reviewer != nil {
		brief, tokens, err := LLMBrief(ctx, reviewer, toCompress, cfg.TargetTokens)
		return brief, tokens, err
	}
	return AlgorithmicBrief(toCompress, cfg.TargetTokens), 0, nil
}

// AlgorithmicBrief produces a deterministic structured summary of the given
// rounds without any LLM call. Each round becomes one paragraph. Returns
// empty string when the input slice is empty.
func AlgorithmicBrief(rounds []RoundRecord, targetTokens int) string {
	if len(rounds) == 0 {
		return ""
	}
	maxWordsPerRound := 100
	if targetTokens > 0 && len(rounds) > 0 {
		perRound := targetTokens / 5 / len(rounds)
		if perRound > 0 && perRound < maxWordsPerRound {
			maxWordsPerRound = perRound
		}
	}

	var b strings.Builder
	b.WriteString("Prior rounds (compressed):")
	for _, r := range rounds {
		b.WriteString("\n  Round ")
		fmt.Fprintf(&b, "%d: ", r.N)
		writeRoundSummary(&b, r, maxWordsPerRound)
	}
	return b.String()
}

func writeRoundSummary(b *strings.Builder, r RoundRecord, maxWords int) {
	issueCount := len(r.Review.Issues)
	files := issueFiles(r.Review.Issues)
	var unmet, met []string
	for _, c := range r.Review.Criteria {
		if c.Status == "satisfied" {
			met = append(met, c.ID)
		} else {
			unmet = append(unmet, c.ID)
		}
	}

	// Issue count + files
	fmt.Fprintf(b, "reviewer raised %d issues", issueCount)
	if len(files) > 0 {
		maxFiles := 10
		if maxWords > 0 {
			if budget := (maxWords / 2) / 5; budget < maxFiles {
				if budget < 1 {
					budget = 1
				}
				maxFiles = budget
			}
		}
		if len(files) > maxFiles {
			shown := files[:maxFiles]
			fmt.Fprintf(b, " in %s... (+%d more)", strings.Join(shown, ", "), len(files)-maxFiles)
		} else {
			fmt.Fprintf(b, " in %s", strings.Join(files, ", "))
		}
	}
	b.WriteString("; ")

	// Criteria
	if len(unmet) > 0 {
		fmt.Fprintf(b, "criteria %s unmet", strings.Join(unmet, ","))
	} else {
		b.WriteString("all criteria met")
	}
	b.WriteString("; ")

	// Addressed count
	addressed := issueCount
	if len(unmet) > 0 {
		// Rough heuristic: if criteria are still unmet, not all issues were addressed.
		addressed = issueCount - len(unmet)
		if addressed < 0 {
			addressed = 0
		}
	}
	if addressed == issueCount {
		b.WriteString("coder addressed all")
	} else {
		fmt.Fprintf(b, "coder partially addressed (%d of %d)", addressed, issueCount)
	}
	b.WriteString("; ")

	// Blocking-issue notes — without these the coder forgets *what* the
	// reviewer rejected and tends to repeat the same mistake. Budget a
	// quarter of the per-round word cap to a few of the worst notes; rely
	// on the deterministic ordering of r.Review.Issues for stability.
	noteBudget := maxWords / 4
	if noteBudget < 8 {
		noteBudget = 8
	}
	const maxNotes = 3
	noteCount := 0
	for _, iss := range r.Review.Issues {
		if iss.Severity != "blocking" || strings.TrimSpace(iss.Note) == "" {
			continue
		}
		if noteCount == 0 {
			b.WriteString("blocking notes: ")
		} else {
			b.WriteString(" | ")
		}
		b.WriteString(truncateWords(iss.Note, noteBudget))
		noteCount++
		if noteCount >= maxNotes {
			break
		}
	}
	if noteCount > 0 {
		b.WriteString("; ")
	}

	// Verify status
	if verify.AllGreen(r.Checks) {
		b.WriteString("verify green.")
	} else {
		var failed []string
		for _, c := range r.Checks {
			if c.Status == verify.StatusFailed || c.Status == verify.StatusTimedOut {
				failed = append(failed, c.Name)
			}
		}
		if len(failed) > 0 {
			fmt.Fprintf(b, "verify red (%s).", strings.Join(failed, ", "))
		} else {
			b.WriteString("verify red.")
		}
	}
}

// truncateWords returns the first n whitespace-separated words of s, with an
// ellipsis appended when truncation occurred. n <= 0 returns s unchanged.
func truncateWords(s string, n int) string {
	if n <= 0 {
		return s
	}
	fields := strings.Fields(s)
	if len(fields) <= n {
		return strings.Join(fields, " ")
	}
	return strings.Join(fields[:n], " ") + "…"
}

// issueFiles extracts unique file paths from review issues, sorted for
// determinism.
func issueFiles(issues []ReviewIssue) []string {
	seen := map[string]bool{}
	for _, i := range issues {
		if i.File != "" {
			seen[i.File] = true
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// LLMBrief calls the reviewer engine to produce a compressed summary. On any
// error it falls back to AlgorithmicBrief. Returns the brief, token cost, and
// nil error (fallback is silent).
func LLMBrief(ctx context.Context, reviewer engine.Engine, rounds []RoundRecord, targetTokens int) (string, int, error) {
	prompt := buildLLMCompressPrompt(rounds)
	resp, err := reviewer.Invoke(ctx, engine.InvokeRequest{
		Role:   engine.RoleReviewer,
		Prompt: prompt,
	})
	if err != nil {
		// Fall back to algorithmic on any engine error.
		return AlgorithmicBrief(rounds, targetTokens), 0, nil
	}
	if resp.Text == "" {
		return AlgorithmicBrief(rounds, targetTokens), 0, nil
	}
	return resp.Text, resp.UsageTokens, nil
}

func buildLLMCompressPrompt(rounds []RoundRecord) string {
	var b strings.Builder
	b.WriteString("Summarize the following coder/reviewer round history into a 200-word brief.\n")
	b.WriteString("Focus on: what issues were raised, which were resolved, which persist.\n\n")
	for _, r := range rounds {
		fmt.Fprintf(&b, "Round %d:\n", r.N)
		fmt.Fprintf(&b, "  Issues: %d\n", len(r.Review.Issues))
		for _, iss := range r.Review.Issues {
			fmt.Fprintf(&b, "  - [%s] %s", iss.Severity, iss.Note)
			if iss.File != "" {
				fmt.Fprintf(&b, " (%s)", iss.File)
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "  Approved: %v\n", r.Review.Approved)
		for _, c := range r.Review.Criteria {
			fmt.Fprintf(&b, "  Criterion %s: %s\n", c.ID, c.Status)
		}
		b.WriteString("\n")
	}
	return b.String()
}
