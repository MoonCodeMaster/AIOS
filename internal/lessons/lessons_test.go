package lessons

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReviewerResponse(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMine_AggregatesAcrossRuns(t *testing.T) {
	root := t.TempDir()
	r1 := filepath.Join(root, "2026-04-26T01-00-00", "task-001", "round-1", "reviewer-response.json")
	r2 := filepath.Join(root, "2026-04-26T01-00-00", "task-002", "round-1", "reviewer-response.json")
	r3 := filepath.Join(root, "2026-04-26T02-00-00", "task-001", "round-1", "reviewer-response.json")
	writeReviewerResponse(t, r1, `{"approved":false,"issues":[
		{"severity":"blocking","category":"acceptance","note":"missing test for empty input","file":"foo.go"},
		{"severity":"nit","category":"style","note":"prefer slices.Index over manual loop","file":"foo.go"}
	]}`)
	writeReviewerResponse(t, r2, `{"approved":false,"issues":[
		{"severity":"blocking","category":"acceptance","note":"missing test for empty input","file":"bar.go"}
	]}`)
	writeReviewerResponse(t, r3, `{"approved":true,"issues":[]}`)

	rep, err := Mine(root)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if rep.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", rep.TotalRuns)
	}
	if rep.TotalIssues != 3 {
		t.Errorf("TotalIssues = %d, want 3", rep.TotalIssues)
	}
	if rep.TotalBlocking != 2 {
		t.Errorf("TotalBlocking = %d, want 2", rep.TotalBlocking)
	}
	// "missing test for empty input" appears twice → top recurring note.
	if len(rep.ByNoteShape) == 0 || rep.ByNoteShape[0].Count != 2 {
		t.Errorf("expected top note count 2, got %+v", rep.ByNoteShape)
	}
	if len(rep.ByCategory) == 0 || rep.ByCategory[0].Key != "acceptance" {
		t.Errorf("expected top category 'acceptance', got %+v", rep.ByCategory)
	}
	if len(rep.HotSpotFiles) == 0 || rep.HotSpotFiles[0].Count != 2 {
		t.Errorf("expected hot-spot file count 2, got %+v", rep.HotSpotFiles)
	}
}

func TestNoteShape_NormalisesNumbersAndCase(t *testing.T) {
	a := noteShape("Missing test for empty input on line 42.")
	b := noteShape("missing test for empty input on line 9001!")
	if a != b {
		t.Errorf("note shapes should collapse: a=%q b=%q", a, b)
	}
}

func TestNoteShape_TruncatesLongNotes(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := noteShape(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("long note not truncated: %q", got)
	}
}

func TestRunIDFromPath_ExtractsFirstSegment(t *testing.T) {
	root := ".aios/runs"
	p := ".aios/runs/2026-04-26T01-02-03/task/round-1/reviewer-response.json"
	if got := runIDFromPath(root, p); got != "2026-04-26T01-02-03" {
		t.Errorf("runIDFromPath = %q, want timestamp", got)
	}
}

func TestMine_MissingDirReturnsErr(t *testing.T) {
	_, err := Mine("/this/path/really/does/not/exist")
	if err == nil {
		t.Error("Mine should error on missing root, not silently return empty report (would mislead user with 'no issues found' banner)")
	}
}

func TestRender_NoIssuesShowsExplanation(t *testing.T) {
	var buf bytes.Buffer
	Report{TotalRuns: 3}.Render(&buf)
	if !strings.Contains(buf.String(), "no issues found") {
		t.Errorf("empty report missing explainer:\n%s", buf.String())
	}
}

func TestRender_ProducesAllSections(t *testing.T) {
	rep := Report{
		TotalRuns: 2, TotalIssues: 5, TotalBlocking: 3,
		ByCategory:   []Bucket{{Key: "acceptance", Count: 3}},
		ByNoteShape:  []Bucket{{Key: "missing test", Count: 2}},
		HotSpotFiles: []Bucket{{Key: "foo.go", Count: 4}},
	}
	var buf bytes.Buffer
	rep.Render(&buf)
	out := buf.String()
	for _, want := range []string{
		"lessons learned",
		"top categories:",
		"top recurring notes:",
		"hot-spot files:",
		"interpretation:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, out)
		}
	}
}
