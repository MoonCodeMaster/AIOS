package spec

import "testing"

func TestTopologicalOrder_Valid(t *testing.T) {
	tasks := []*Task{
		{ID: "001-a", Kind: "feature"},
		{ID: "002-b", Kind: "feature", DependsOn: []string{"001-a"}},
		{ID: "003-c", Kind: "feature", DependsOn: []string{"002-b"}},
	}
	ordered, err := TopologicalOrder(tasks)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].ID != "001-a" || ordered[1].ID != "002-b" || ordered[2].ID != "003-c" {
		t.Errorf("order = %v", idsOf(ordered))
	}
}

func TestTopologicalOrder_Cycle(t *testing.T) {
	tasks := []*Task{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	_, err := TopologicalOrder(tasks)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestTopologicalOrder_MissingDep(t *testing.T) {
	tasks := []*Task{
		{ID: "a", DependsOn: []string{"ghost"}},
	}
	_, err := TopologicalOrder(tasks)
	if err == nil {
		t.Fatal("expected missing-dep error")
	}
}

func idsOf(ts []*Task) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}
