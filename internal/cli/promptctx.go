package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
	"github.com/MoonCodeMaster/AIOS/internal/verify"
)

// projectContext is the static, per-task context handed to the coder/reviewer
// prompts. It is gathered once at task start and reused across rounds —
// nothing in here changes round-over-round.
type projectContext struct {
	Project       *spec.Project
	Workdir       string
	ReadmeExcerpt string
	TestFiles     []string
	SimilarTasks  []similarTask
}

type similarTask struct {
	ID         string
	Kind       string
	Acceptance []string
}

// coderData is the data shape consumed by coder.tmpl on the first round.
type coderData struct {
	Project       *spec.Project
	Task          *spec.Task
	Workdir       string
	ReadmeExcerpt string
	TestFiles     []string
	SimilarTasks  []similarTask
}

// coderReviseData is the shape consumed by coder-revise.tmpl. It carries the
// previous round's diff, verify results, and reviewer issues alongside the
// static project context. Escalated=true switches the template into its
// hard-constraint mode: the prompt prepends a standout banner telling the
// coder this is a last-chance retry triggered by stall detection.
type coderReviseData struct {
	Project       *spec.Project
	Task          *spec.Task
	Workdir       string
	ReadmeExcerpt string
	Round         int
	PrevDiff      string
	PrevChecks    []verify.CheckResult
	Issues        []orchestrator.ReviewIssue
	Escalated     bool
}

// reviewerData is the shape consumed by reviewer.tmpl.
type reviewerData struct {
	Project     *spec.Project
	Task        *spec.Task
	Diff        string
	Checks      []verify.CheckResult
	MCPFailures []engine.McpCall
}

// loadProject parses .aios/project.md if present. A missing file is not fatal
// — early projects may not have a spec yet — so this returns (nil, nil) in
// that case and the prompts render with empty project fields.
func loadProject(repoDir string) (*spec.Project, error) {
	path := filepath.Join(repoDir, ".aios", "project.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return spec.ParseProject(string(raw))
}

// buildProjectContext gathers the static prompt context for a task. workdir
// is the per-task worktree; allTasks is the full task list (used to find
// already-converged tasks of the same kind to reference).
func buildProjectContext(repoDir, workdir string, project *spec.Project, task *spec.Task, allTasks []*spec.Task) projectContext {
	return projectContext{
		Project:       project,
		Workdir:       workdir,
		ReadmeExcerpt: readReadmeExcerpt(repoDir),
		TestFiles:     findTestFiles(workdir),
		SimilarTasks:  similarTasks(allTasks, task),
	}
}

// readmeMaxLines caps the README excerpt so a 5,000-line README does not
// blow the prompt budget. The model can read the rest via tools if it wants.
const readmeMaxLines = 200

func readReadmeExcerpt(repoDir string) string {
	for _, name := range []string{"README.md", "README.MD", "README", "readme.md"} {
		raw, err := os.ReadFile(filepath.Join(repoDir, name))
		if err != nil {
			continue
		}
		lines := strings.Split(string(raw), "\n")
		if len(lines) <= readmeMaxLines {
			return strings.TrimRight(string(raw), "\n")
		}
		head := strings.Join(lines[:readmeMaxLines], "\n")
		return head + "\n... (README truncated at " + strconv.Itoa(readmeMaxLines) + " lines)"
	}
	return ""
}

// testFileCap limits the test-file index so the prompt stays compact.
const testFileCap = 40

// testFileExts is the set of suffixes that identify a test file across the
// stacks AIOS already supports (Go, Node/TS, Python, Rust).
var testFileExts = []string{
	"_test.go",
	".test.ts",
	".test.tsx",
	".test.js",
	".test.jsx",
	".spec.ts",
	".spec.tsx",
	".spec.js",
	".spec.jsx",
	"_test.py",
	"test_", // python: test_foo.py prefix; matched by HasPrefix below
}

func findTestFiles(workdir string) []string {
	if workdir == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(workdir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Skip vendored, build, and harness state directories.
		if d.IsDir() {
			name := d.Name()
			if name == "node_modules" || name == "vendor" || name == "target" ||
				name == ".git" || name == ".aios" || name == "bin" || name == "dist" {
				return fs.SkipDir
			}
			return nil
		}
		base := d.Name()
		if isTestFile(base) {
			rel, err := filepath.Rel(workdir, path)
			if err == nil {
				out = append(out, rel)
			}
		}
		if len(out) >= testFileCap {
			return fs.SkipAll
		}
		return nil
	})
	return out
}

func isTestFile(base string) bool {
	for _, ext := range testFileExts {
		if ext == "test_" {
			if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
				return true
			}
			continue
		}
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	// Rust convention: tests in tests/ dir or `mod tests` blocks. We only
	// catch the directory layout above (caller can read tests/*.rs).
	return false
}

// similarTasks returns up to 5 already-converged tasks of the same kind. They
// give the coder a concrete reference for "what does a finished one look like
// in this codebase". Excludes the target task itself.
func similarTasks(all []*spec.Task, target *spec.Task) []similarTask {
	const cap = 5
	var out []similarTask
	for _, t := range all {
		if t.ID == target.ID || t.Kind != target.Kind || t.Status != "converged" {
			continue
		}
		out = append(out, similarTask{ID: t.ID, Kind: t.Kind, Acceptance: t.Acceptance})
		if len(out) >= cap {
			break
		}
	}
	return out
}

