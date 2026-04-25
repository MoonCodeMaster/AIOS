package githost

import (
	"context"
	"testing"
	"time"
)

func TestFakeHost_OpenPRReturnsIncrementingNumbers(t *testing.T) {
	f := &FakeHost{}
	pr1, err := f.OpenPR(context.Background(), "main", "feat/a", "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	pr2, err := f.OpenPR(context.Background(), "main", "feat/b", "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	if pr1.Number == pr2.Number {
		t.Errorf("PR numbers collided: %d == %d", pr1.Number, pr2.Number)
	}
}

func TestFakeHost_WaitForChecksReturnsConfiguredState(t *testing.T) {
	f := &FakeHost{ChecksByPR: map[int]ChecksState{1: ChecksGreen}}
	pr, _ := f.OpenPR(context.Background(), "main", "h", "t", "b")
	if pr.Number != 1 {
		t.Fatalf("expected PR #1, got %d", pr.Number)
	}
	state, err := f.WaitForChecks(context.Background(), pr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if state != ChecksGreen {
		t.Errorf("state = %q, want green", state)
	}
}

func TestFakeHost_MergePRRefusesIfChecksNotGreen(t *testing.T) {
	f := &FakeHost{ChecksByPR: map[int]ChecksState{1: ChecksRed}}
	pr, _ := f.OpenPR(context.Background(), "main", "h", "t", "b")
	err := f.MergePR(context.Background(), pr, MergeSquash)
	if err == nil {
		t.Error("MergePR should refuse a red PR")
	}
}

func TestFakeHost_MergePRMarksMerged(t *testing.T) {
	f := &FakeHost{ChecksByPR: map[int]ChecksState{1: ChecksGreen}}
	pr, _ := f.OpenPR(context.Background(), "main", "h", "t", "b")
	if err := f.MergePR(context.Background(), pr, MergeSquash); err != nil {
		t.Fatal(err)
	}
	if !f.Merged[pr.Number] {
		t.Errorf("PR %d should be marked merged", pr.Number)
	}
}
