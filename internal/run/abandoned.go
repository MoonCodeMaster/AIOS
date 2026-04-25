package run

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// AbandonedRound is the per-round summary captured for an abandoned task.
// Smaller than orchestrator.RoundRecord — full prompts/responses are already
// persisted under round-N/, so this is a flat index.
type AbandonedRound struct {
	N              int  `json:"n"`
	ReviewApproved bool `json:"review_approved"`
	IssueCount     int  `json:"issue_count"`
	VerifyGreen    bool `json:"verify_green"`
	Escalated      bool `json:"escalated,omitempty"`
}

// AbandonedInfo is the audit-trail summary for a task that was abandoned in
// autopilot mode. Round-level prompts/responses live alongside under
// round-N/; this struct is the index a future reader hits first.
type AbandonedInfo struct {
	TaskID      string           `json:"task_id"`
	Reason      string           `json:"reason"`     // BlockReason.String()
	BlockCode   string           `json:"block_code"` // orchestrator.BlockCode value
	UsageTokens int              `json:"usage_tokens"`
	Rounds      []AbandonedRound `json:"rounds"`
}

// WriteAbandoned persists report.md + full-trail.json under
// .aios/runs/<id>/abandoned/<task>/. Overwrites are intentional — re-running
// against the same task ID should refresh the artefact, not error.
func WriteAbandoned(rec *Recorder, info AbandonedInfo) error {
	if info.TaskID == "" {
		return fmt.Errorf("WriteAbandoned: empty TaskID")
	}
	rel := filepath.Join("abandoned", info.TaskID)
	if err := rec.WriteFile(filepath.Join(rel, "report.md"), []byte(renderAbandonedReport(info))); err != nil {
		return fmt.Errorf("write report.md: %w", err)
	}
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal full-trail: %w", err)
	}
	if err := rec.WriteFile(filepath.Join(rel, "full-trail.json"), raw); err != nil {
		return fmt.Errorf("write full-trail.json: %w", err)
	}
	return nil
}

func renderAbandonedReport(info AbandonedInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Abandoned: %s\n\n", info.TaskID)
	fmt.Fprintf(&b, "**Block code:** `%s`\n\n", info.BlockCode)
	if info.Reason != "" {
		fmt.Fprintf(&b, "**Reason:** %s\n\n", info.Reason)
	}
	fmt.Fprintf(&b, "**Tokens used:** %d\n\n", info.UsageTokens)
	if len(info.Rounds) > 0 {
		b.WriteString("## Rounds\n\n")
		b.WriteString("| Round | Approved | Issues | Verify | Escalated |\n")
		b.WriteString("|---:|:---:|---:|:---:|:---:|\n")
		for _, r := range info.Rounds {
			fmt.Fprintf(&b, "| %d | %v | %d | %v | %v |\n",
				r.N, r.ReviewApproved, r.IssueCount, r.VerifyGreen, r.Escalated)
		}
	}
	b.WriteString("\nFull per-round prompts and responses live alongside this file under `round-N/`.\n")
	return b.String()
}
