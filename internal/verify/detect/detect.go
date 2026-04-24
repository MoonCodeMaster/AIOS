package detect

// Suggestion is the raw output of autodetect: a map from verify key name
// (test_cmd, lint_cmd, typecheck_cmd, build_cmd) to a suggested shell cmdline.
type Suggestion map[string]string

type Detector func(workdir string) Suggestion

var detectors = []Detector{
	DetectGo,
	DetectNode,
	DetectPython,
	DetectRust,
}

// All runs every detector; later results overwrite earlier ones on conflict.
// (Projects with multiple ecosystems: user will confirm during aios init.)
func All(workdir string) Suggestion {
	out := Suggestion{}
	for _, d := range detectors {
		for k, v := range d(workdir) {
			if v != "" {
				out[k] = v
			}
		}
	}
	return out
}
