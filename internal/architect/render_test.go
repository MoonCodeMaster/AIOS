package architect

import (
	"strings"
	"testing"
)

func sampleBlueprint() Blueprint {
	return Blueprint{
		Title:    "Sample",
		Tagline:  "a sample blueprint for tests",
		Stance:   "balanced",
		MindMap:  "- root: sample\n  - subsystem-a\n    - module-1",
		Sketch:   "two paragraphs of sketch.\nsecond line.",
		DataFlow: "1. step one\n2. step two",
		Tradeoff: "- pro: simple\n- con: limited",
		Roadmap:  "- M1: scaffold\n- M2: feature",
		Risks:    "- risk: scope creep | mitigation: weekly cut",
	}
}

func TestRenderRoundTrip_PreservesAllFields(t *testing.T) {
	in := sampleBlueprint()
	out := ParseBlueprints(Render(in))
	if len(out) != 1 {
		t.Fatalf("expected 1 blueprint after round trip, got %d", len(out))
	}
	got := out[0]
	cases := map[string][2]string{
		"Title":    {got.Title, in.Title},
		"Tagline":  {got.Tagline, in.Tagline},
		"Stance":   {got.Stance, in.Stance},
		"MindMap":  {got.MindMap, in.MindMap},
		"Sketch":   {got.Sketch, in.Sketch},
		"DataFlow": {got.DataFlow, in.DataFlow},
		"Tradeoff": {got.Tradeoff, in.Tradeoff},
		"Roadmap":  {got.Roadmap, in.Roadmap},
		"Risks":    {got.Risks, in.Risks},
	}
	for name, pair := range cases {
		if pair[0] != pair[1] {
			t.Errorf("%s: got %q, want %q", name, pair[0], pair[1])
		}
	}
}

func TestRenderForUser_ContainsKeyHeaders(t *testing.T) {
	out := RenderForUser(2, sampleBlueprint())
	for _, want := range []string{
		"BLUEPRINT 2 — Sample",
		"stance:  balanced",
		"## Mind map",
		"## Architecture sketch",
		"## Data flow",
		"## Tradeoffs",
		"## Roadmap",
		"## Risks",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderForUser output missing %q\nfull:\n%s", want, out)
		}
	}
	if strings.Contains(out, beginMarker) || strings.Contains(out, endMarker) {
		t.Errorf("RenderForUser must strip machine markers; got: %s", out)
	}
}

func TestValid_RequiresTitleStanceMindMap(t *testing.T) {
	full := sampleBlueprint()
	if !full.Valid() {
		t.Fatal("sampleBlueprint should be valid")
	}
	cases := map[string]Blueprint{
		"no title":   {Tagline: "x", Stance: "x", MindMap: "x"},
		"no stance":  {Title: "x", Tagline: "x", MindMap: "x"},
		"no mindmap": {Title: "x", Tagline: "x", Stance: "x"},
	}
	for name, bp := range cases {
		if bp.Valid() {
			t.Errorf("%s: unexpectedly Valid", name)
		}
	}
}
