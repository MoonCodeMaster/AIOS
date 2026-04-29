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

func TestResumeCmd_Registered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"resume"})
	if err != nil {
		t.Fatalf("resume subcommand not registered: %v", err)
	}
	if cmd.Name() != "resume" {
		t.Errorf("resume.Name() = %q; want %q", cmd.Name(), "resume")
	}
}
