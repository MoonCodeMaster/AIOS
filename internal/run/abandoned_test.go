package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAbandoned_WritesReportAndJSON(t *testing.T) {
	dir := t.TempDir()
	rec, err := Open(dir, "run-id")
	if err != nil {
		t.Fatal(err)
	}
	rounds := []AbandonedRound{
		{N: 1, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
		{N: 2, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
		{N: 3, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
	}
	info := AbandonedInfo{
		TaskID:      "004",
		Reason:      "stall_no_progress: 3 consecutive rounds raised identical review issues",
		BlockCode:   "stall_no_progress",
		UsageTokens: 12_345,
		Rounds:      rounds,
	}
	if err := WriteAbandoned(rec, info); err != nil {
		t.Fatalf("WriteAbandoned: %v", err)
	}

	reportPath := filepath.Join(rec.Root(), "abandoned", "004", "report.md")
	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report.md: %v", err)
	}
	if !strings.Contains(string(body), "004") {
		t.Errorf("report.md missing task ID: %q", body)
	}
	if !strings.Contains(string(body), "stall_no_progress") {
		t.Errorf("report.md missing block code: %q", body)
	}

	jsonPath := filepath.Join(rec.Root(), "abandoned", "004", "full-trail.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read full-trail.json: %v", err)
	}
	var roundtrip AbandonedInfo
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatalf("full-trail.json is not valid JSON: %v", err)
	}
	if roundtrip.TaskID != "004" || roundtrip.UsageTokens != 12_345 {
		t.Errorf("roundtrip mismatch: %+v", roundtrip)
	}
}

func TestWriteAbandoned_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rec, _ := Open(dir, "run-id")
	info := AbandonedInfo{TaskID: "004", BlockCode: "x"}
	if err := WriteAbandoned(rec, info); err != nil {
		t.Fatal(err)
	}
	if err := WriteAbandoned(rec, info); err != nil {
		t.Errorf("second WriteAbandoned should overwrite cleanly, got: %v", err)
	}
}
