package detect

import (
	"os"
	"path/filepath"
)

func DetectGo(workdir string) Suggestion {
	if _, err := os.Stat(filepath.Join(workdir, "go.mod")); err != nil {
		return nil
	}
	return Suggestion{
		"test_cmd":  "go test ./...",
		"build_cmd": "go build ./...",
	}
}
