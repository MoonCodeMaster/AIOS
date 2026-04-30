package engine

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed testdata/output-contracts.json
var outputContractFS embed.FS

// OutputFixture records one CLI output fixture covered by the parser
// regression guard. Paths are relative to internal/engine/testdata.
type OutputFixture struct {
	Path string `json:"path"`
}

// OutputContract records the CLI version whose output fixtures were captured.
type OutputContract struct {
	Version        string          `json:"version"`
	VersionCommand string          `json:"version_command"`
	Fixtures       []OutputFixture `json:"fixtures"`
}

type outputContractFile struct {
	Schema    int                       `json:"schema"`
	Contracts map[string]OutputContract `json:"contracts"`
}

// OutputContracts returns parser fixture metadata embedded in the AIOS binary.
func OutputContracts() (map[string]OutputContract, error) {
	raw, err := outputContractFS.ReadFile("testdata/output-contracts.json")
	if err != nil {
		return nil, fmt.Errorf("read output contracts: %w", err)
	}
	var doc outputContractFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse output contracts: %w", err)
	}
	if doc.Schema != 1 {
		return nil, fmt.Errorf("unsupported output contract schema %d", doc.Schema)
	}
	return doc.Contracts, nil
}

// OutputContractFor returns the parser contract metadata for one engine.
func OutputContractFor(name string) (OutputContract, bool, error) {
	contracts, err := OutputContracts()
	if err != nil {
		return OutputContract{}, false, err
	}
	c, ok := contracts[name]
	return c, ok, nil
}
