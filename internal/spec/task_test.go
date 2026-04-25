package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTask_Valid(t *testing.T) {
	data, _ := os.ReadFile(filepath.Join("testdata", "tasks", "001-a.md"))
	task, err := ParseTask(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "001-a" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Kind != "feature" {
		t.Errorf("Kind = %q", task.Kind)
	}
	if task.Status != "pending" {
		t.Errorf("Status = %q", task.Status)
	}
	if len(task.Acceptance) == 0 {
		t.Error("Acceptance empty")
	}
}

func TestParseTaskMcpAllow(t *testing.T) {
	const src = `---
id: 004-mcp
kind: feature
acceptance:
  - works
mcp_allow: [github, fs-readonly]
mcp_allow_tools:
  github: [search_code]
---
body
`
	tk, err := ParseTask(src)
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if got := tk.MCPAllow; len(got) != 2 || got[0] != "github" || got[1] != "fs-readonly" {
		t.Errorf("MCPAllow = %v, want [github fs-readonly]", got)
	}
	if got := tk.MCPAllowTools["github"]; len(got) != 1 || got[0] != "search_code" {
		t.Errorf("MCPAllowTools[github] = %v, want [search_code]", got)
	}
}

func TestParseTaskMcpAllowDefaults(t *testing.T) {
	const src = `---
id: 005-no-mcp
kind: feature
acceptance:
  - works
---
body
`
	tk, err := ParseTask(src)
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if len(tk.MCPAllow) != 0 {
		t.Errorf("MCPAllow = %v, want empty (default deny)", tk.MCPAllow)
	}
	if tk.MCPAllowTools != nil && len(tk.MCPAllowTools) != 0 {
		t.Errorf("MCPAllowTools = %v, want nil/empty", tk.MCPAllowTools)
	}
}

func TestParseTask_PreservesDecomposeFields(t *testing.T) {
	src := `---
id: 005.1
kind: feature
parent_id: "005"
depth: 1
status: pending
acceptance:
  - c1
---
sub-task body`
	task, err := ParseTask(src)
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if task.ParentID != "005" {
		t.Errorf("ParentID = %q, want %q", task.ParentID, "005")
	}
	if task.Depth != 1 {
		t.Errorf("Depth = %d, want 1", task.Depth)
	}
}

func TestParseTask_AcceptsDecomposedAndAbandonedStatus(t *testing.T) {
	for _, status := range []string{"decomposed", "abandoned"} {
		src := "---\nid: x\nkind: feature\nstatus: " + status + "\nacceptance:\n  - c1\n---\nbody"
		task, err := ParseTask(src)
		if err != nil {
			t.Fatalf("ParseTask(%q): %v", status, err)
		}
		if task.Status != status {
			t.Errorf("Status = %q, want %q", task.Status, status)
		}
	}
}

func TestParseTask_DecomposedIntoRoundtrips(t *testing.T) {
	src := `---
id: "005"
kind: feature
status: decomposed
decomposed_into: ["005.1", "005.2"]
acceptance:
  - c1
---
parent body`
	task, err := ParseTask(src)
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if len(task.DecomposedInto) != 2 || task.DecomposedInto[0] != "005.1" || task.DecomposedInto[1] != "005.2" {
		t.Errorf("DecomposedInto = %v, want [005.1 005.2]", task.DecomposedInto)
	}
}
