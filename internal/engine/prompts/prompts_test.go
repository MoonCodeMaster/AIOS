package prompts

import (
	"strings"
	"testing"
)

func mustContain(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(out, p) {
			t.Errorf("output missing %q\nfull output:\n%s", p, out)
		}
	}
}

func TestRender_Coder_IncludesAllContext(t *testing.T) {
	out, err := Render("coder.tmpl", map[string]any{
		"Project": map[string]any{
			"Name":     "ToyApp",
			"Goal":     "reverse argv",
			"NonGoals": []string{"do not parse JSON"},
		},
		"Task": map[string]any{
			"ID":         "001-a",
			"Kind":       "feature",
			"Body":       "Implement reverseArgv.",
			"Acceptance": []string{"prints reversed args", "exits 0 on empty argv"},
		},
		"Workdir":       "/tmp/work/001-a",
		"ReadmeExcerpt": "## ToyApp\nReverses arguments.",
		"TestFiles":     []string{"main_test.go", "internal/util/util_test.go"},
		"SimilarTasks": []map[string]any{
			{"ID": "000-scaffold", "Kind": "feature", "Acceptance": []string{"main exists"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		"001-a", "feature", "reverse argv",
		"do not parse JSON",
		"## ToyApp",
		"main_test.go",
		"000-scaffold",
		"prints reversed args",
		"Do not commit",
	)
}

func TestRender_Coder_DegradesGracefullyWithoutContext(t *testing.T) {
	// No Project, no README, no test files, no similar tasks. Should still render.
	out, err := Render("coder.tmpl", map[string]any{
		"Task": map[string]any{
			"ID":         "001",
			"Kind":       "scaffold",
			"Body":       "scaffold the package",
			"Acceptance": []string{"package builds"},
		},
		"Workdir": "/tmp/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "001", "scaffold", "package builds")
	if strings.Contains(out, "README excerpt:") {
		t.Errorf("README section should be omitted when ReadmeExcerpt is empty")
	}
}

func TestRender_CoderRevise_CarriesPriorRound(t *testing.T) {
	out, err := Render("coder-revise.tmpl", map[string]any{
		"Project": map[string]any{"Name": "ToyApp", "Goal": "reverse argv"},
		"Task": map[string]any{
			"ID":         "001-a",
			"Kind":       "feature",
			"Acceptance": []string{"prints reversed args"},
		},
		"Workdir":  "/tmp/work/001-a",
		"Round":    2,
		"PrevDiff": "diff --git a/main.go b/main.go\n+ // wrong",
		"PrevChecks": []map[string]any{
			{"Name": "test_cmd", "Status": "failed", "ExitCode": 1},
		},
		"Issues": []map[string]any{
			{"Severity": "blocking", "Category": "correctness", "Note": "off-by-one in loop", "File": "main.go", "Line": 12},
			{"Severity": "nit", "Category": "style", "Note": "unused var", "File": "", "Line": 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		"001-a", "round 2",
		"diff --git",          // prior diff
		"test_cmd: failed",    // prior verify result
		"Blocking",            // section header
		"correctness",
		"off-by-one in loop",
		"main.go:12",
		"Nits",
		"unused var",
	)
}

// TestRender_CoderRevise_Escalated verifies the escalation banner is
// rendered above the normal revise content when Escalated=true, and omitted
// when Escalated is false/unset, so non-escalated rounds do not carry the
// "last chance" framing.
func TestRender_CoderRevise_Escalated(t *testing.T) {
	base := map[string]any{
		"Project": map[string]any{"Name": "ToyApp", "Goal": "reverse argv"},
		"Task": map[string]any{
			"ID":         "001-a",
			"Kind":       "feature",
			"Acceptance": []string{"prints reversed args"},
		},
		"Workdir":  "/tmp/work/001-a",
		"Round":    4,
		"PrevDiff": "diff --git a/main.go b/main.go",
		"Issues": []map[string]any{
			{"Severity": "blocking", "Category": "correctness", "Note": "still broken"},
		},
	}
	// Escalated = true renders the banner.
	escBase := map[string]any{}
	for k, v := range base {
		escBase[k] = v
	}
	escBase["Escalated"] = true
	outEsc, err := Render("coder-revise.tmpl", escBase)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, outEsc,
		"ESCALATED RETRY",
		"LAST CHANCE BEFORE BLOCK",
		"stall_no_progress",
		"(escalated)",
	)
	// Escalated = false (or unset) must NOT render the banner.
	outNormal, err := Render("coder-revise.tmpl", base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(outNormal, "ESCALATED RETRY") {
		t.Errorf("normal revise prompt should not carry the escalation banner")
	}
}

func TestRender_Reviewer_StructuredSchema(t *testing.T) {
	out, err := Render("reviewer.tmpl", map[string]any{
		"Project": map[string]any{"Name": "ToyApp", "Goal": "reverse argv"},
		"Task": map[string]any{
			"ID":         "001-a",
			"Kind":       "feature",
			"Acceptance": []string{"prints reversed args"},
		},
		"Diff":   "diff --git a/x b/x",
		"Checks": []map[string]any{{"Name": "test_cmd", "Status": "passed", "ExitCode": 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Schema must include all five issue fields and the category enum.
	mustContain(t, out,
		`"approved"`, `"criteria"`, `"issues"`,
		`"category"`, `"file"`, `"line"`,
		"correctness", "acceptance", "regression",
		"test-coverage", "style", "security", "performance",
		"approved` MUST be `false",
	)
}

func TestRender_Brainstorm_HasMarker(t *testing.T) {
	out, err := Render("brainstorm.tmpl", map[string]string{"Idea": "build a todo CLI"})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "build a todo CLI", "ONE AT A TIME", "[[BRAINSTORM DONE]]")
}

func TestRender_SpecSynth_FieldList(t *testing.T) {
	out, err := Render("spec-synth.tmpl", map[string]string{"Transcript": "Q: ... A: ..."})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		"Q: ... A: ...",
		"name", "goal", "non_goals", "constraints", "acceptance_bar",
		"Architecture sketch", "Open questions",
	)
}

func TestRender_Decompose_HasSeparatorRule(t *testing.T) {
	out, err := Render("decompose.tmpl", map[string]string{"Spec": "spec body"})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		"spec body",
		"===TASK===",
		"id", "kind", "depends_on", "status", "acceptance",
		"scaffold", "feature", "bugfix", "refactor", "test-writing",
	)
}

func TestRender_DecomposeStuck(t *testing.T) {
	out, err := Render("decompose-stuck.tmpl", map[string]any{
		"ParentID":   "005",
		"ParentBody": "Add a /health endpoint with a unit test.",
		"Issues":     []string{"missing test for 500 case", "handler signature wrong"},
		"LastDiff":   "diff --git a/handler.go b/handler.go\n+func Health() {}",
		"Acceptance": []string{"endpoint returns 200", "test covers 500"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"005", "/health", "missing test for 500", "===TASK===", "depends_on", "depth"} {
		if !strings.Contains(out, want) {
			t.Errorf("decompose-stuck.tmpl output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRender_DecomposeMerge(t *testing.T) {
	out, err := Render("decompose-merge.tmpl", map[string]any{
		"ParentID":   "005",
		"ParentBody": "Add /health.",
		"ProposalA":  "---\nid: 005.1\n---\nClaude's split A",
		"ProposalB":  "---\nid: 005.1\n---\nCodex's split B",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"005", "Claude", "Codex", "Proposal A", "Proposal B", "===TASK===", "merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("decompose-merge.tmpl output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRender_Unknown(t *testing.T) {
	_, err := Render("nope.tmpl", nil)
	if err == nil {
		t.Error("expected error")
	}
}
