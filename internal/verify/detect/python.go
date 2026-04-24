package detect

import (
	"os"
	"path/filepath"
)

func DetectPython(workdir string) Suggestion {
	for _, f := range []string{"pyproject.toml", "setup.py", "requirements.txt"} {
		if _, err := os.Stat(filepath.Join(workdir, f)); err == nil {
			return Suggestion{"test_cmd": "pytest -q"}
		}
	}
	return nil
}
