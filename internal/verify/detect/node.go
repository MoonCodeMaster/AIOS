package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

func DetectNode(workdir string) Suggestion {
	p := filepath.Join(workdir, "package.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var pj packageJSON
	if err := json.Unmarshal(raw, &pj); err != nil {
		return nil
	}
	s := Suggestion{}
	if _, ok := pj.Scripts["test"]; ok {
		s["test_cmd"] = "npm test"
	}
	if _, ok := pj.Scripts["lint"]; ok {
		s["lint_cmd"] = "npm run lint"
	}
	if _, ok := pj.Scripts["typecheck"]; ok {
		s["typecheck_cmd"] = "npm run typecheck"
	}
	if _, ok := pj.Scripts["build"]; ok {
		s["build_cmd"] = "npm run build"
	}
	return s
}
