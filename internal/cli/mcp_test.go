package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPPresets_AllParseAsTOMLishHeaders(t *testing.T) {
	for name, body := range mcpPresets {
		header := "[mcp.servers." + name + "]"
		if !strings.Contains(body, header) {
			t.Errorf("preset %q does not contain header %q", name, header)
		}
		if !strings.Contains(body, "# ") {
			t.Errorf("preset %q missing explanatory comment", name)
		}
	}
}

func TestRunMCPScaffold_AppendsAndIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(tmp, ".aios", "config.toml")
	if err := os.WriteFile(cfgPath, []byte("schema_version = 1\n[project]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	if err := runMCPScaffold("github"); err != nil {
		t.Fatalf("first scaffold: %v", err)
	}
	first, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(first), "[mcp.servers.github]") {
		t.Errorf("first scaffold did not append header:\n%s", first)
	}

	// Second call: idempotent — no duplicate appended.
	if err := runMCPScaffold("github"); err != nil {
		t.Fatalf("second scaffold: %v", err)
	}
	second, _ := os.ReadFile(cfgPath)
	if strings.Count(string(second), "[mcp.servers.github]") != 1 {
		t.Errorf("second scaffold duplicated the header; full file:\n%s", second)
	}
}

func TestRunMCPScaffold_UnknownPreset(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".aios", "config.toml"), []byte("schema_version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	err := runMCPScaffold("not-a-preset")
	if err == nil || !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("err = %v, want 'unknown preset'", err)
	}
}

func TestRunMCPScaffold_MissingConfigErrors(t *testing.T) {
	tmp := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	err := runMCPScaffold("github")
	if err == nil || !strings.Contains(err.Error(), "aios init") {
		t.Errorf("err = %v, want hint about `aios init`", err)
	}
}
