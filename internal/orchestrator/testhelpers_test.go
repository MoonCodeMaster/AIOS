package orchestrator

import "os"

func writeFileHelper(path, s string) error {
	return os.WriteFile(path, []byte(s), 0o644)
}
