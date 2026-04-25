package githost

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
)

func TestCLIHost_ListLabeled_ParsesGhJSON(t *testing.T) {
	host := &CLIHost{
		exec: fakeExec(`[{"number":42,"title":"Add /health","body":"endpoint","labels":[{"name":"aios:do"},{"name":"good first issue"}],"url":"https://example.invalid/issues/42"}]`, 0),
	}
	issues, err := host.ListLabeled(context.Background(), "aios:do")
	if err != nil {
		t.Fatalf("ListLabeled: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 42 || issues[0].Title != "Add /health" {
		t.Errorf("ListLabeled = %+v, want one issue #42 'Add /health'", issues)
	}
	if !reflect.DeepEqual(issues[0].Labels, []string{"aios:do", "good first issue"}) {
		t.Errorf("Labels = %v, want [aios:do good first issue]", issues[0].Labels)
	}
}

func TestCLIHost_AddLabel_InvocationShape(t *testing.T) {
	var captured []string
	host := &CLIHost{exec: func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExec("", 0)(name, args...)
	}}
	if err := host.AddLabel(context.Background(), 42, "aios:in-progress"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	want := []string{"gh", "issue", "edit", "42", "--add-label", "aios:in-progress"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}

func TestCLIHost_RemoveLabel_InvocationShape(t *testing.T) {
	var captured []string
	host := &CLIHost{exec: func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExec("", 0)(name, args...)
	}}
	if err := host.RemoveLabel(context.Background(), 42, "aios:do"); err != nil {
		t.Fatalf("RemoveLabel: %v", err)
	}
	want := []string{"gh", "issue", "edit", "42", "--remove-label", "aios:do"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}

func TestCLIHost_AddComment_InvocationShape(t *testing.T) {
	var captured []string
	host := &CLIHost{exec: func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExec("", 0)(name, args...)
	}}
	if err := host.AddComment(context.Background(), 42, "merged in #99"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	want := []string{"gh", "issue", "comment", "42", "--body", "merged in #99"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}

func TestCLIHost_OpenIssue_ParsesNewIssueURL(t *testing.T) {
	host := &CLIHost{exec: fakeExec("https://github.com/owner/repo/issues/77\n", 0)}
	num, err := host.OpenIssue(context.Background(), "title", "body", []string{"aios:stuck"})
	if err != nil {
		t.Fatalf("OpenIssue: %v", err)
	}
	if num != 77 {
		t.Errorf("OpenIssue number = %d, want 77", num)
	}
}

func TestCLIHost_CloseIssue_InvocationShape(t *testing.T) {
	var captured []string
	host := &CLIHost{exec: func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExec("", 0)(name, args...)
	}}
	if err := host.CloseIssue(context.Background(), 42); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	want := []string{"gh", "issue", "close", "42"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}
