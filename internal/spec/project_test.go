package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProject_Valid(t *testing.T) {
	data, _ := os.ReadFile(filepath.Join("testdata", "project-valid.md"))
	p, err := ParseProject(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if p.Goal == "" {
		t.Error("Goal empty")
	}
	if len(p.AcceptanceBar) < 1 {
		t.Error("AcceptanceBar empty")
	}
}

func TestLoadProject_NoFrontmatter(t *testing.T) {
	data, _ := os.ReadFile(filepath.Join("testdata", "project-no-frontmatter.md"))
	_, err := ParseProject(string(data))
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}
