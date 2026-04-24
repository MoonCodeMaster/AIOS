package orchestrator

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

func tk(id string, deps ...string) *spec.Task {
	return &spec.Task{ID: id, DependsOn: deps, Status: "pending", Acceptance: []string{"x"}}
}

func TestSchedulerInitialReadySet(t *testing.T) {
	tasks := []*spec.Task{tk("a"), tk("b"), tk("c", "a"), tk("d", "a", "b")}
	s, err := NewScheduler(tasks)
	if err != nil {
		t.Fatal(err)
	}
	got := drainReady(s, 2)
	sort.Strings(got)
	if !equal(got, []string{"a", "b"}) {
		t.Errorf("initial ready = %v, want [a b]", got)
	}
}

func TestSchedulerCompletionAdvances(t *testing.T) {
	tasks := []*spec.Task{tk("a"), tk("b", "a"), tk("c", "b")}
	s, err := NewScheduler(tasks)
	if err != nil {
		t.Fatal(err)
	}
	first := drainReady(s, 1)
	if len(first) != 1 || first[0] != "a" {
		t.Fatalf("first ready = %v, want [a]", first)
	}
	s.Done(TaskResult{ID: "a", Status: "converged"})
	second := drainReady(s, 1)
	if len(second) != 1 || second[0] != "b" {
		t.Fatalf("after a converged, ready = %v, want [b]", second)
	}
}

func TestSchedulerBlockedTransitiveBlock(t *testing.T) {
	tasks := []*spec.Task{tk("a"), tk("b", "a"), tk("c", "b")}
	s, err := NewScheduler(tasks)
	if err != nil {
		t.Fatal(err)
	}
	first := drainReady(s, 1)
	if first[0] != "a" {
		t.Fatal("expected a first")
	}
	s.Done(TaskResult{ID: "a", Status: "blocked", Reason: "x",
		BlockReason: NewBlock(CodeStallNoProgress, "x")})
	// Remaining tasks should never become ready.
	more := drainReadyNonBlocking(s)
	if len(more) != 0 {
		t.Errorf("after a blocked, ready = %v, want empty (transitive block)", more)
	}
	if !s.AllSettled() {
		t.Errorf("AllSettled() should be true after transitive block")
	}

	// The blocked map must record the real cause for 'a' and
	// upstream_blocked pointers for every cascaded descendant.
	blocked := s.Blocked()
	if len(blocked) != 3 {
		t.Fatalf("blocked = %v, want all three tasks recorded", blocked)
	}
	if got := blocked["a"]; got.Code != CodeStallNoProgress {
		t.Errorf("a.Code = %s, want %s", got.Code, CodeStallNoProgress)
	}
	for _, id := range []string{"b", "c"} {
		got := blocked[TaskID(id)]
		if got.Code != CodeUpstreamBlocked {
			t.Errorf("%s.Code = %s, want upstream_blocked", id, got.Code)
		}
		// b's upstream is a; c's upstream is b (nearest ancestor).
	}
	if blocked["b"].Upstream != "a" {
		t.Errorf("b.Upstream = %q, want a", blocked["b"].Upstream)
	}
	if blocked["c"].Upstream != "b" {
		t.Errorf("c.Upstream = %q, want b", blocked["c"].Upstream)
	}
}

func TestSchedulerCycleRejected(t *testing.T) {
	tasks := []*spec.Task{tk("a", "b"), tk("b", "a")}
	if _, err := NewScheduler(tasks); err == nil {
		t.Fatal("expected cycle error")
	}
}

// helpers
func drainReady(s *Scheduler, n int) []string {
	out := []string{}
	timeout := time.After(2 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case id := <-s.Ready():
			out = append(out, string(id))
		case <-timeout:
			return out
		}
	}
	return out
}

func drainReadyNonBlocking(s *Scheduler) []string {
	out := []string{}
	for {
		select {
		case id := <-s.Ready():
			out = append(out, string(id))
		default:
			return out
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ = context.Background // keep import
