package detect

import (
	"os"
	"path/filepath"
)

func DetectRust(workdir string) Suggestion {
	if _, err := os.Stat(filepath.Join(workdir, "Cargo.toml")); err != nil {
		return nil
	}
	return Suggestion{
		"test_cmd":  "cargo test",
		"build_cmd": "cargo build",
	}
}
