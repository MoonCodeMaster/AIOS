package prompts

import (
	"strings"
	"testing"
)

func TestRender_Coder(t *testing.T) {
	out, err := Render("coder.tmpl", map[string]any{
		"Project": map[string]string{"Goal": "reverse argv"},
		"Task": map[string]any{
			"ID":         "001-a",
			"Body":       "Do the thing.",
			"Acceptance": []string{"criterion one", "criterion two"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"001-a", "reverse argv", "criterion one", "criterion two"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRender_Reviewer_EmitsJSONBlock(t *testing.T) {
	out, err := Render("reviewer.tmpl", map[string]any{
		"Task": map[string]any{
			"ID":         "001-a",
			"Acceptance": []string{"c1"},
		},
		"Diff":   "diff --git a/x b/x",
		"Checks": []map[string]any{{"Name": "test_cmd", "Status": "passed", "ExitCode": 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"approved"`) {
		t.Errorf("reviewer template should show JSON schema: %s", out)
	}
}

func TestRender_Unknown(t *testing.T) {
	_, err := Render("nope.tmpl", nil)
	if err == nil {
		t.Error("expected error")
	}
}
