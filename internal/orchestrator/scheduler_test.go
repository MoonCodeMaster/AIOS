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

func TestSchedulerInitialReadyOrderFollowsInput(t *testing.T) {
	tasks := []*spec.Task{tk("b"), tk("a"), tk("c")}
	s, err := NewScheduler(tasks)
	if err != nil {
		t.Fatal(err)
	}
	got := drainReady(s, 3)
	if !equal(got, []string{"b", "a", "c"}) {
		t.Errorf("initial ready order = %v, want input order [b a c]", got)
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

func TestSchedulerDependentReadyOrderFollowsInput(t *testing.T) {
	tasks := []*spec.Task{tk("root"), tk("b", "root"), tk("a", "root"), tk("c", "root")}
	s, err := NewScheduler(tasks)
	if err != nil {
		t.Fatal(err)
	}
	first := drainReady(s, 1)
	if len(first) != 1 || first[0] != "root" {
		t.Fatalf("first ready = %v, want [root]", first)
	}
	s.Done(TaskResult{ID: "root", Status: "converged"})
	got := drainReady(s, 3)
	if !equal(got, []string{"b", "a", "c"}) {
		t.Errorf("dependent ready order = %v, want input order [b a c]", got)
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

// TestNewScheduler_EmptyTasksClosesDoneImmediately is the regression test for
// the "empty task list hangs the run" bug: with zero tasks, NewScheduler must
// close its done channel before returning so callers waiting on Wait() don't
// block indefinitely.
func TestNewScheduler_EmptyTasksClosesDoneImmediately(t *testing.T) {
	s, err := NewScheduler(nil)
	if err != nil {
		t.Fatalf("NewScheduler(nil): %v", err)
	}
	select {
	case <-s.Wait():
		// expected: done is already closed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait() did not unblock for an empty task list — scheduler hangs forever")
	}
}

func TestScheduler_Done_DecomposedSplicesChildren(t *testing.T) {
	parent := &spec.Task{ID: "005", Acceptance: []string{"c1"}}
	dependent := &spec.Task{ID: "006", DependsOn: []string{"005"}, Acceptance: []string{"c1"}}
	s, err := NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}

	got := <-s.Ready()
	if got != "005" {
		t.Fatalf("first ready = %q, want %q", got, "005")
	}

	c1 := &spec.Task{ID: "005.1", Acceptance: []string{"c1"}, ParentID: "005", Depth: 1}
	c2 := &spec.Task{ID: "005.2", Acceptance: []string{"c1"}, ParentID: "005", Depth: 1}
	s.Done(TaskResult{ID: "005", Status: "decomposed", Children: []*spec.Task{c1, c2}})

	enqueued := map[TaskID]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-s.Ready():
			enqueued[id] = true
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("child %d not enqueued (got %v)", i+1, enqueued)
		}
	}
	if !enqueued["005.1"] || !enqueued["005.2"] {
		t.Errorf("expected both 005.1 and 005.2 enqueued, got %v", enqueued)
	}
}

func TestScheduler_Done_DecomposedRewiresDependents(t *testing.T) {
	parent := &spec.Task{ID: "005", Acceptance: []string{"c1"}}
	dependent := &spec.Task{ID: "006", DependsOn: []string{"005"}, Acceptance: []string{"c1"}}
	s, err := NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Ready()

	c1 := &spec.Task{ID: "005.1", Acceptance: []string{"c1"}}
	c2 := &spec.Task{ID: "005.2", Acceptance: []string{"c1"}}
	s.Done(TaskResult{ID: "005", Status: "decomposed", Children: []*spec.Task{c1, c2}})

	<-s.Ready()
	<-s.Ready()

	s.Done(TaskResult{ID: "005.1", Status: "converged"})
	select {
	case got := <-s.Ready():
		t.Errorf("006 enqueued prematurely after only 005.1 converged: got %q", got)
	case <-time.After(50 * time.Millisecond):
		// expected
	}

	s.Done(TaskResult{ID: "005.2", Status: "converged"})
	select {
	case got := <-s.Ready():
		if got != "006" {
			t.Errorf("expected 006 to enqueue after both children converged, got %q", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("006 did not enqueue after both 005.1 and 005.2 converged")
	}
}

func TestScheduler_Done_DecomposedNoCascadeOnParent(t *testing.T) {
	parent := &spec.Task{ID: "005", Acceptance: []string{"c1"}}
	dependent := &spec.Task{ID: "006", DependsOn: []string{"005"}, Acceptance: []string{"c1"}}
	s, err := NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Ready()
	c1 := &spec.Task{ID: "005.1", Acceptance: []string{"c1"}}
	s.Done(TaskResult{ID: "005", Status: "decomposed", Children: []*spec.Task{c1}})

	if _, blocked := s.Blocked()["006"]; blocked {
		t.Error("006 must not be blocked when its parent decomposed")
	}
}
