package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputContractsFixturesParse(t *testing.T) {
	contracts, err := OutputContracts()
	if err != nil {
		t.Fatalf("OutputContracts: %v", err)
	}
	for name, contract := range contracts {
		if contract.Version == "" {
			t.Fatalf("%s contract has empty version", name)
		}
		if len(contract.Fixtures) == 0 {
			t.Fatalf("%s contract has no fixtures", name)
		}
		for _, fixture := range contract.Fixtures {
			raw, err := os.ReadFile(filepath.Join("testdata", fixture.Path))
			if err != nil {
				t.Fatalf("%s fixture %s: %v", name, fixture.Path, err)
			}
			switch name {
			case "claude":
				if _, err := parseClaudeOutput(raw); err != nil {
					t.Fatalf("parseClaudeOutput(%s): %v", fixture.Path, err)
				}
			case "codex":
				if _, err := parseCodexOutput(raw); err != nil {
					t.Fatalf("parseCodexOutput(%s): %v", fixture.Path, err)
				}
			default:
				t.Fatalf("unknown output contract %q", name)
			}
		}
	}
}
