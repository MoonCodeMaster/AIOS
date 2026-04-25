package cli

import (
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/architect"
)

func bp(title, stance string) architect.Blueprint {
	return architect.Blueprint{
		Title: title, Tagline: title + " tagline", Stance: stance,
		MindMap: "- root: " + title, Sketch: "sketch.", DataFlow: "1. step",
		Tradeoff: "- pro: x\n- con: y", Roadmap: "- M1: ship", Risks: "- risk: r | mitigation: m",
	}
}

func threeFinalists() []architect.Blueprint {
	return []architect.Blueprint{
		bp("Conservative one", "conservative"),
		bp("Balanced one", "balanced"),
		bp("Ambitious one", "ambitious"),
	}
}

func TestChooseBlueprint_PickHonoursFlag(t *testing.T) {
	got, err := chooseBlueprint(threeFinalists(), 2, strings.NewReader(""))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Index != 2 {
		t.Errorf("Index = %d, want 2", got.Index)
	}
	if got.Blueprint.Title != "Balanced one" {
		t.Errorf("Title = %q", got.Blueprint.Title)
	}
}

func TestChooseBlueprint_StdinSelection(t *testing.T) {
	got, err := chooseBlueprint(threeFinalists(), 0, strings.NewReader("3\n"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Index != 3 || got.Blueprint.Title != "Ambitious one" {
		t.Errorf("got = %+v", got)
	}
}

func TestChooseBlueprint_RejectsOutOfRange(t *testing.T) {
	_, err := chooseBlueprint(threeFinalists(), 0, strings.NewReader("9\n"))
	if err == nil {
		t.Fatal("expected error for out-of-range pick")
	}
}

func TestChooseBlueprint_RejectsNonNumeric(t *testing.T) {
	_, err := chooseBlueprint(threeFinalists(), 0, strings.NewReader("foo\n"))
	if err == nil {
		t.Fatal("expected error for non-numeric input")
	}
}

func TestChooseBlueprint_WrongFinalistCount(t *testing.T) {
	_, err := chooseBlueprint(threeFinalists()[:2], 1, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error when fewer than 3 finalists supplied")
	}
}
