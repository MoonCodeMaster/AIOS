package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/run"
)

func TestAutopilot_AbandonedArtifact_LayoutEndToEnd(t *testing.T) {
	dir := t.TempDir()
	rec, err := run.Open(dir, "run-id")
	if err != nil {
		t.Fatal(err)
	}

	info := run.AbandonedInfo{
		TaskID:      "004-rescue",
		Reason:      "stall_no_progress: 3 consecutive rounds raised identical review issues",
		BlockCode:   "stall_no_progress",
		UsageTokens: 12_345,
		Rounds: []run.AbandonedRound{
			{N: 1, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
			{N: 2, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
			{N: 3, ReviewApproved: false, IssueCount: 3, VerifyGreen: false, Escalated: true},
		},
	}
	if err := run.WriteAbandoned(rec, info); err != nil {
		t.Fatalf("WriteAbandoned: %v", err)
	}

	reportPath := filepath.Join(rec.Root(), "abandoned", "004-rescue", "report.md")
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("report.md missing: %v", err)
	}
	body, _ := os.ReadFile(reportPath)
	for _, want := range []string{"004-rescue", "stall_no_progress", "12345"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("report.md missing %q; got: %s", want, body)
		}
	}

	jsonPath := filepath.Join(rec.Root(), "abandoned", "004-rescue", "full-trail.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("full-trail.json missing: %v", err)
	}
	var roundtrip run.AbandonedInfo
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatalf("full-trail.json invalid JSON: %v", err)
	}
	if roundtrip.TaskID != "004-rescue" {
		t.Errorf("roundtrip TaskID = %q, want %q", roundtrip.TaskID, "004-rescue")
	}
	if len(roundtrip.Rounds) != 3 || !roundtrip.Rounds[2].Escalated {
		t.Errorf("roundtrip rounds = %+v, want 3 with last escalated", roundtrip.Rounds)
	}
}
