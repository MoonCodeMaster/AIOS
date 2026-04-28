package cli

import "testing"

func TestUnblockCmd_Registered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"unblock"})
	if err != nil {
		t.Fatalf("unblock subcommand not registered: %v", err)
	}
	if cmd.Use != "unblock <task-id>" {
		t.Errorf("unblock.Use = %q; want %q", cmd.Use, "unblock <task-id>")
	}
}

func TestResumeCmd_Removed(t *testing.T) {
	root := NewRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "resume" {
			t.Fatal("`resume` command still registered; should be removed in v0.3")
		}
	}
}
