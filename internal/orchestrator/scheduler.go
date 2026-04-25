package orchestrator

import (
	"fmt"
	"sync"

	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

type TaskID = string

type TaskResult struct {
	ID          TaskID
	Status      string       // "converged" | "blocked"
	Reason      string       // deprecated: mirror of BlockReason.String()
	BlockReason *BlockReason // nil on success; populated on block
}

// Scheduler is the per-run DAG bookkeeper. It owns the ready channel that
// workers pull from, and the done channel that workers push results into.
//
// The Scheduler is safe to call from multiple goroutines.
type Scheduler struct {
	mu         sync.Mutex
	pending    map[TaskID]*spec.Task // not yet ready
	deps       map[TaskID]map[TaskID]struct{}
	dependents map[TaskID]map[TaskID]struct{}
	inflight   int
	settled    int
	total      int
	// blocked records the structured reason for every task that is either
	// directly blocked (by its own worker) or transitively blocked (via the
	// cascade). Cascade entries use CodeUpstreamBlocked with Upstream set to
	// the nearest blocked ancestor.
	blocked  map[TaskID]BlockReason
	ready    chan TaskID
	done     chan struct{} // closed once all tasks are settled (converged or blocked)
	doneOnce sync.Once
}

func NewScheduler(tasks []*spec.Task) (*Scheduler, error) {
	s := &Scheduler{
		pending:    map[TaskID]*spec.Task{},
		deps:       map[TaskID]map[TaskID]struct{}{},
		dependents: map[TaskID]map[TaskID]struct{}{},
		blocked:    map[TaskID]BlockReason{},
		total:      len(tasks),
		ready:      make(chan TaskID, len(tasks)),
		done:       make(chan struct{}),
	}
	for _, t := range tasks {
		s.pending[t.ID] = t
		s.deps[t.ID] = map[TaskID]struct{}{}
		for _, d := range t.DependsOn {
			s.deps[t.ID][d] = struct{}{}
		}
	}
	for id, ds := range s.deps {
		for d := range ds {
			if _, ok := s.pending[d]; !ok {
				return nil, fmt.Errorf("task %s depends on unknown %s", id, d)
			}
			if s.dependents[d] == nil {
				s.dependents[d] = map[TaskID]struct{}{}
			}
			s.dependents[d][id] = struct{}{}
		}
	}
	if cyc := detectCycle(s.deps); cyc != "" {
		return nil, fmt.Errorf("dep cycle involving %s", cyc)
	}
	for id := range s.pending {
		if len(s.deps[id]) == 0 {
			s.enqueueLocked(id)
		}
	}
	// If we started with no work — empty task list, or every task already
	// satisfied — signal done immediately so callers don't hang on Wait().
	// Done() would otherwise be the only place that closes the channel, and
	// it's never called when no worker runs.
	if s.inflight == 0 && len(s.pending) == 0 {
		s.doneOnce.Do(func() { close(s.done) })
	}
	return s, nil
}

// Ready returns the channel workers pull TaskIDs from.
func (s *Scheduler) Ready() <-chan TaskID { return s.ready }

// Done is called by a worker when a task completes (converged or blocked).
// When the task is blocked, r.BlockReason is preserved in the scheduler's
// blocked map; cascaded dependents get a CodeUpstreamBlocked reason that
// names r.ID as the root cause.
func (s *Scheduler) Done(r TaskResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight--
	s.settled++
	if r.Status == "blocked" {
		reason := BlockReason{Code: CodeEngineInvokeFailed, Detail: r.Reason}
		if r.BlockReason != nil {
			reason = *r.BlockReason
		}
		s.blocked[r.ID] = reason
		s.cascadeBlockLocked(r.ID)
	} else {
		s.releaseDependentsLocked(r.ID)
	}
	// Signal the done channel exactly once when all tasks are settled.
	// We do NOT close the ready channel here to avoid breaking consumers
	// that use a non-blocking select; the done channel is the termination signal.
	if s.inflight == 0 && len(s.pending) == 0 {
		s.doneOnce.Do(func() { close(s.done) })
	}
}

// Wait returns a channel that is closed once all tasks have settled
// (either converged or transitively blocked). Callers can use this to know
// when to stop pulling from Ready().
func (s *Scheduler) Wait() <-chan struct{} { return s.done }

func (s *Scheduler) AllSettled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settled == s.total
}

func (s *Scheduler) enqueueLocked(id TaskID) {
	delete(s.pending, id)
	s.inflight++
	s.ready <- id
}

func (s *Scheduler) releaseDependentsLocked(doneID TaskID) {
	for dep := range s.dependents[doneID] {
		delete(s.deps[dep], doneID)
		if len(s.deps[dep]) == 0 {
			if _, stillPending := s.pending[dep]; stillPending {
				s.enqueueLocked(dep)
			}
		}
	}
}

func (s *Scheduler) cascadeBlockLocked(id TaskID) {
	for dep := range s.dependents[id] {
		if _, stillPending := s.pending[dep]; stillPending {
			delete(s.pending, dep)
			s.blocked[dep] = BlockReason{Code: CodeUpstreamBlocked, Upstream: id}
			s.settled++
			s.cascadeBlockLocked(dep)
		}
	}
}

// Blocked returns the set of task IDs that are blocked (directly or
// transitively), along with the structured reason for each. Cascaded entries
// carry Code=CodeUpstreamBlocked with Upstream set to the nearest ancestor
// that blocked directly.
func (s *Scheduler) Blocked() map[TaskID]BlockReason {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[TaskID]BlockReason, len(s.blocked))
	for k, v := range s.blocked {
		out[k] = v
	}
	return out
}

func detectCycle(deps map[TaskID]map[TaskID]struct{}) TaskID {
	white := map[TaskID]struct{}{}
	gray := map[TaskID]struct{}{}
	black := map[TaskID]struct{}{}
	for id := range deps {
		white[id] = struct{}{}
	}
	var visit func(TaskID) TaskID
	visit = func(id TaskID) TaskID {
		delete(white, id)
		gray[id] = struct{}{}
		for dep := range deps[id] {
			if _, ok := black[dep]; ok {
				continue
			}
			if _, ok := gray[dep]; ok {
				return dep
			}
			if c := visit(dep); c != "" {
				return c
			}
		}
		delete(gray, id)
		black[id] = struct{}{}
		return ""
	}
	for {
		var pick TaskID
		found := false
		for id := range white {
			pick = id
			found = true
			break
		}
		if !found {
			break
		}
		if c := visit(pick); c != "" {
			return c
		}
	}
	return ""
}
