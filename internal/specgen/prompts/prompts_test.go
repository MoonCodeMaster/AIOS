package prompts

import (
	"strings"
	"testing"
)

func TestRenderDraft(t *testing.T) {
	out, err := Render("draft.tmpl", map[string]any{
		"UserRequest":    "build a todo app",
		"CurrentSpec":    "",
		"PriorTurns":     []map[string]string{},
		"ProjectContext": "",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "build a todo app") {
		t.Fatalf("draft template did not interpolate UserRequest; got: %s", out)
	}
}

func TestRenderMerge(t *testing.T) {
	out, err := Render("merge.tmpl", map[string]string{
		"DraftClaude": "DRAFT_A_BODY",
		"DraftCodex":  "DRAFT_B_BODY",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "DRAFT_A_BODY") || !strings.Contains(out, "DRAFT_B_BODY") {
		t.Fatalf("merge template did not include both drafts; got: %s", out)
	}
}

func TestRenderPolish(t *testing.T) {
	out, err := Render("polish.tmpl", map[string]string{"Merged": "MERGED_BODY"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "MERGED_BODY") {
		t.Fatalf("polish template did not interpolate Merged; got: %s", out)
	}
}
