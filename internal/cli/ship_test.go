package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestShipSubcommand_Registered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"ship"})
	if err != nil {
		t.Fatalf("ship subcommand not registered: %v", err)
	}
	if cmd.Use != "ship <prompt>" {
		t.Errorf("ship.Use = %q; want %q", cmd.Use, "ship <prompt>")
	}
}

func TestShipSubcommand_RequiresPrompt(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"ship"}) // no prompt
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	err := root.Execute()
	if err == nil {
		t.Fatal("ship without prompt should error")
	}
	if !strings.Contains(err.Error(), "ship needs a prompt") {
		t.Errorf("error %q; want hint about prompt", err.Error())
	}
}

func TestRoot_ShipFlagRemoved(t *testing.T) {
	root := NewRootCmd()
	if root.Flags().Lookup("ship") != nil {
		t.Fatal("--ship flag still registered on root; should be removed in v0.3")
	}
}
