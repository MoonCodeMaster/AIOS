package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Solaxis/aios/internal/spec"
)

func TestIsTestFile(t *testing.T) {
	cases := map[string]bool{
		"foo_test.go":      true,
		"foo.test.ts":      true,
		"Foo.test.tsx":     true,
		"foo.spec.js":      true,
		"foo_test.py":      true,
		"test_foo.py":      true,
		"main.go":          false,
		"README.md":        false,
		"foo_tests.go":     false, // not the canonical _test.go suffix
		"test_foo.txt":     false, // python prefix only counts on .py
	}
	for name, want := range cases {
		if got := isTestFile(name); got != want {
			t.Errorf("isTestFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFindTestFiles_SkipsIgnoredDirs(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("a_test.go", "package x\n")
	mustWrite("internal/util/util_test.go", "package util\n")
	mustWrite("node_modules/lib/x.test.js", "ignored\n")
	mustWrite("vendor/foo/y_test.go", "ignored\n")
	mustWrite(".git/objects/something_test.go", "ignored\n")
	mustWrite("README.md", "not a test file\n")

	got := findTestFiles(dir)

	wantIncludes := []string{"a_test.go", filepath.Join("internal", "util", "util_test.go")}
	for _, w := range wantIncludes {
		var found bool
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("findTestFiles missing %q; got %v", w, got)
		}
	}
	for _, g := range got {
		if strings.HasPrefix(g, "node_modules") ||
			strings.HasPrefix(g, "vendor") ||
			strings.HasPrefix(g, ".git") {
			t.Errorf("findTestFiles returned %q from an ignored dir", g)
		}
	}
}

func TestSimilarTasks_FiltersByKindAndStatus(t *testing.T) {
	all := []*spec.Task{
		{ID: "001", Kind: "feature", Status: "converged", Acceptance: []string{"a"}},
		{ID: "002", Kind: "feature", Status: "pending", Acceptance: []string{"b"}},
		{ID: "003", Kind: "bugfix", Status: "converged", Acceptance: []string{"c"}},
		{ID: "004", Kind: "feature", Status: "converged", Acceptance: []string{"d"}},
	}
	target := &spec.Task{ID: "999", Kind: "feature"}
	got := similarTasks(all, target)
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2", len(got))
	}
	for _, g := range got {
		if g.Kind != "feature" {
			t.Errorf("kind = %q, want feature", g.Kind)
		}
	}
	// Target itself must never appear.
	for _, g := range got {
		if g.ID == target.ID {
			t.Errorf("similarTasks included the target task")
		}
	}
}

func TestSimilarTasks_ExcludesSelf(t *testing.T) {
	target := &spec.Task{ID: "001", Kind: "feature"}
	all := []*spec.Task{
		{ID: "001", Kind: "feature", Status: "converged"},
		{ID: "002", Kind: "feature", Status: "converged", Acceptance: []string{"x"}},
	}
	got := similarTasks(all, target)
	if len(got) != 1 || got[0].ID != "002" {
		t.Errorf("got %+v, want only 002", got)
	}
}

func TestReadReadmeExcerpt_TruncatesLongFile(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 0; i < readmeMaxLines+50; i++ {
		lines = append(lines, "line")
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readReadmeExcerpt(dir)
	if !strings.Contains(got, "README truncated") {
		t.Errorf("expected truncation marker; got tail: %q", got[len(got)-200:])
	}
	if strings.Count(got, "\n") > readmeMaxLines+1 {
		t.Errorf("excerpt has more than readmeMaxLines lines")
	}
}

func TestReadReadmeExcerpt_Missing(t *testing.T) {
	if got := readReadmeExcerpt(t.TempDir()); got != "" {
		t.Errorf("expected empty for missing README, got %q", got)
	}
}

func TestLoadProject_MissingIsNotError(t *testing.T) {
	dir := t.TempDir()
	got, err := loadProject(dir)
	if err != nil {
		t.Fatalf("missing project.md should not error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil project, got %+v", got)
	}
}

func TestLoadProject_Parses(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `---
name: ToyApp
goal: reverse argv
non_goals:
  - parse JSON
constraints:
  - Go 1.22+
acceptance_bar:
  - prints reversed args
---
## Architecture
A single binary.
`
	if err := os.WriteFile(filepath.Join(dir, ".aios", "project.md"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "ToyApp" || got.Goal != "reverse argv" {
		t.Errorf("parsed project = %+v", got)
	}
}
