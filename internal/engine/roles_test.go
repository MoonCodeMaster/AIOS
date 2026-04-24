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

// TestPickPair_RejectsSameDefaults guards against a misconfigured project
// sneaking through Config.Load and silently self-reviewing (e.g. both
// defaults set to "claude"). Even if the two engine handles in the map are
// distinct objects with the same Name, the pair must be rejected.
func TestPickPair_RejectsSameDefaults(t *testing.T) {
	engines := map[string]Engine{
		"claude": &FakeEngine{Name_: "claude"},
	}
	if _, _, err := PickPair("", map[string]string{}, "claude", "claude", engines); err == nil {
		t.Fatal("expected error when coderDefault == reviewerDefault")
	}
}

// TestPickPair_RejectsRolesByKindMatchingReviewer guards the subtle case
// where roles_by_kind overrides the coder to the same engine as the
// reviewerDefault — which with only two engines resolves cleanly, but the
// failure mode we want to forbid is someone configuring reviewerDefault to
// equal the override.
func TestPickPair_RolesByKindStillCrossModels(t *testing.T) {
	engines := map[string]Engine{
		"claude": &FakeEngine{Name_: "claude"},
		"codex":  &FakeEngine{Name_: "codex"},
	}
	// reviewerDefault = "codex", override coder to "codex" for bugfix.
	// Coder should be codex, reviewer should fall back to coderDefault=claude.
	coder, reviewer, err := PickPair("bugfix",
		map[string]string{"bugfix": "codex"},
		"claude", "codex", engines)
	if err != nil {
		t.Fatal(err)
	}
	if coder.Name() != "codex" || reviewer.Name() != "claude" {
		t.Errorf("got coder=%s reviewer=%s; want codex/claude", coder.Name(), reviewer.Name())
	}
}
