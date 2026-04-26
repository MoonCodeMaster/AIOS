package architect

import (
	"strings"
	"testing"
)

const threeBlocksFixture = `preamble the model wasn't supposed to write

===BLUEPRINT===
title: Tiny Todo
tagline: a single-binary todo list with sqlite
stance: conservative

## Mind map
- root: tiny-todo
  - cli
    - add
    - list

## Architecture sketch
single Go binary, sqlite file in $XDG_DATA_HOME

## Data flow
1. user runs cli
2. cli writes to sqlite

## Tradeoffs
- pro: zero deps
- con: no sync

## Roadmap
- M1: cli + storage
- M2: tui

## Risks
- risk: schema migrations | mitigation: versioned migrations table
===END===

===BLUEPRINT===
title: Cloud Todo
tagline: shared list with realtime sync
stance: ambitious

## Mind map
- root: cloud-todo
  - api
  - web

## Architecture sketch
postgres + websockets

## Data flow
1. browser opens ws
2. api fans out updates

## Tradeoffs
- pro: multi-device
- con: needs hosting

## Roadmap
- M1: api
- M2: web

## Risks
- risk: dropped sockets | mitigation: heartbeat + replay log
===END===

===BLUEPRINT===
title:
stance: balanced
## Mind map
- root: half-formed
===END===

trailing junk
`

func TestParseBlueprints_DropsInvalidBlocks(t *testing.T) {
	got := ParseBlueprints(threeBlocksFixture)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (third block missing title)", len(got))
	}
	if got[0].Title != "Tiny Todo" {
		t.Errorf("got[0].Title = %q", got[0].Title)
	}
	if got[0].Stance != "conservative" {
		t.Errorf("got[0].Stance = %q", got[0].Stance)
	}
	if !strings.Contains(got[0].MindMap, "tiny-todo") {
		t.Errorf("got[0].MindMap missing root: %q", got[0].MindMap)
	}
	if got[1].Title != "Cloud Todo" {
		t.Errorf("got[1].Title = %q", got[1].Title)
	}
}

func TestParseBlueprints_SectionsFullyPopulated(t *testing.T) {
	got := ParseBlueprints(threeBlocksFixture)
	bp := got[0]
	for name, val := range map[string]string{
		"Sketch":   bp.Sketch,
		"DataFlow": bp.DataFlow,
		"Tradeoff": bp.Tradeoff,
		"Roadmap":  bp.Roadmap,
		"Risks":    bp.Risks,
	} {
		if val == "" {
			t.Errorf("%s empty; got = %+v", name, bp)
		}
	}
}

func TestParseBlueprints_EmptyInput(t *testing.T) {
	if got := ParseBlueprints(""); len(got) != 0 {
		t.Errorf("empty input: got %d blueprints, want 0", len(got))
	}
}

func TestParseBlueprints_NoMarkers(t *testing.T) {
	if got := ParseBlueprints("just some markdown\n## with headings"); len(got) != 0 {
		t.Errorf("no markers: got %d, want 0", len(got))
	}
}
