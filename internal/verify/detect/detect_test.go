package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect_Node(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"x","scripts":{"test":"jest","lint":"eslint ."}}`), 0o644)
	s := All(dir)
	if s["test_cmd"] == "" {
		t.Error("expected test_cmd")
	}
	if s["lint_cmd"] == "" {
		t.Error("expected lint_cmd")
	}
}

func TestDetect_Go(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	s := All(dir)
	if s["test_cmd"] != "go test ./..." {
		t.Errorf("test_cmd = %q", s["test_cmd"])
	}
	if s["build_cmd"] != "go build ./..." {
		t.Errorf("build_cmd = %q", s["build_cmd"])
	}
}

func TestDetect_Python(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\nname='x'\n"), 0o644)
	s := All(dir)
	if s["test_cmd"] == "" {
		t.Error("expected test_cmd")
	}
}

func TestDetect_Rust(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname=\"x\"\n"), 0o644)
	s := All(dir)
	if s["test_cmd"] != "cargo test" {
		t.Errorf("test_cmd = %q", s["test_cmd"])
	}
	if s["build_cmd"] != "cargo build" {
		t.Errorf("build_cmd = %q", s["build_cmd"])
	}
}

func TestDetect_Empty(t *testing.T) {
	s := All(t.TempDir())
	if len(s) != 0 {
		t.Errorf("expected empty, got %v", s)
	}
}
