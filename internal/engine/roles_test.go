package engine

import "testing"

func TestPickCoder_FromKind(t *testing.T) {
	roles := map[string]string{
		"feature":  "claude",
		"bugfix":   "codex",
		"refactor": "codex",
	}
	engines := map[string]Engine{
		"claude": &FakeEngine{Name_: "claude"},
		"codex":  &FakeEngine{Name_: "codex"},
	}
	cases := []struct {
		kind, wantCoder, wantReviewer string
	}{
		{"feature", "claude", "codex"},
		{"bugfix", "codex", "claude"},
		{"refactor", "codex", "claude"},
	}
	for _, c := range cases {
		coder, reviewer, err := PickPair(c.kind, roles, "claude", "codex", engines)
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if coder.Name() != c.wantCoder || reviewer.Name() != c.wantReviewer {
			t.Errorf("%s: got %s/%s, want %s/%s", c.kind,
				coder.Name(), reviewer.Name(), c.wantCoder, c.wantReviewer)
		}
	}
}

func TestPickCoder_FallbackToDefault(t *testing.T) {
	engines := map[string]Engine{
		"claude": &FakeEngine{Name_: "claude"},
		"codex":  &FakeEngine{Name_: "codex"},
	}
	coder, reviewer, err := PickPair("unknown-kind", map[string]string{}, "claude", "codex", engines)
	if err != nil {
		t.Fatal(err)
	}
	if coder.Name() != "claude" || reviewer.Name() != "codex" {
		t.Errorf("got %s/%s", coder.Name(), reviewer.Name())
	}
}
