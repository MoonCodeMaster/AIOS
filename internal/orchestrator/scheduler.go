package orchestrator

import (
	"fmt"
	"sort"
	"sync"

	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

type TaskID = string

type TaskResult struct {
	ID          TaskID
	Status      string       // "converged" | "blocked" | "decomposed" | "abandoned"
	Reason      string       // deprecated: mirror of BlockReason.String()
	BlockReason *BlockReason // nil on success; populated on block
	// Children is populated when Status == "decomposed" and lists the sub-tasks
	// produced by the auto-decompose handler. Scheduler.Done splices them into
	// pending and rewires dependents to wait on the children rather than the
	// (now-decomposed) parent.
	Children []*spec.Task
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
	order      map[TaskID]int
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
		order:      map[TaskID]int{},
		total:      len(tasks),
		ready:      make(chan TaskID, len(tasks)),
		done:       make(chan struct{}),
	}
	for i, t := range tasks {
		s.pending[t.ID] = t
		s.deps[t.ID] = map[TaskID]struct{}{}
		s.order[t.ID] = i
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
	for _, t := range tasks {
		id := t.ID
		if _, stillPending := s.pending[id]; !stillPending {
			continue
		}
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
	if r.Status == "decomposed" {
		s.spliceDecomposedLocked(r.ID, r.Children)
		if s.inflight == 0 && len(s.pending) == 0 {
			s.doneOnce.Do(func() { close(s.done) })
		}
		return
	}
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

// spliceDecomposedLocked atomically inserts the children of a decomposed
// parent into the scheduler's pending set, rewires every dependent of the
// parent to depend on ALL children (a dependent of a decomposed parent must
// wait for the entire split to converge), and enqueues any children that
// have no remaining deps. The parent itself is silently retired — it neither
// converges nor blocks. Caller must hold s.mu.
func (s *Scheduler) spliceDecomposedLocked(parentID TaskID, children []*spec.Task) {
	if len(children) == 0 {
		// Defensive: a decomposed result with no children is equivalent to
		// abandoning the parent. Cascade-block dependents.
		s.blocked[parentID] = BlockReason{Code: CodeStallNoProgress, Detail: "decomposed with empty children"}
		s.cascadeBlockLocked(parentID)
		return
	}
	parentDependents := s.dependents[parentID]
	childIDSet := map[TaskID]struct{}{}
	for _, c := range children {
		childIDSet[c.ID] = struct{}{}
	}
	for _, c := range children {
		s.pending[c.ID] = c
		if _, ok := s.order[c.ID]; !ok {
			s.order[c.ID] = len(s.order)
		}
		s.deps[c.ID] = map[TaskID]struct{}{}
		for _, d := range c.DependsOn {
			s.deps[c.ID][d] = struct{}{}
		}
		if s.dependents[c.ID] == nil {
			s.dependents[c.ID] = map[TaskID]struct{}{}
		}
		s.total++
		for d := range s.deps[c.ID] {
			if s.dependents[d] == nil {
				s.dependents[d] = map[TaskID]struct{}{}
			}
			s.dependents[d][c.ID] = struct{}{}
		}
	}
	// Rewire every dependent-of-parent: drop the dep on parent, add a dep on
	// every child. Each dependent now waits for the full split.
	for dep := range parentDependents {
		delete(s.deps[dep], parentID)
		for cid := range childIDSet {
			s.deps[dep][cid] = struct{}{}
			if s.dependents[cid] == nil {
				s.dependents[cid] = map[TaskID]struct{}{}
			}
			s.dependents[cid][dep] = struct{}{}
		}
	}
	// Children with no remaining deps are immediately ready.
	for _, c := range children {
		if len(s.deps[c.ID]) == 0 {
			s.enqueueLocked(c.ID)
		}
	}
}

func (s *Scheduler) releaseDependentsLocked(doneID TaskID) {
	var ready []TaskID
	for dep := range s.dependents[doneID] {
		delete(s.deps[dep], doneID)
		if len(s.deps[dep]) == 0 {
			if _, stillPending := s.pending[dep]; stillPending {
				ready = append(ready, dep)
			}
		}
	}
	s.sortByOrderLocked(ready)
	for _, dep := range ready {
		s.enqueueLocked(dep)
	}
}

func (s *Scheduler) sortByOrderLocked(ids []TaskID) {
	sort.Slice(ids, func(i, j int) bool {
		oi, iok := s.order[ids[i]]
		oj, jok := s.order[ids[j]]
		switch {
		case iok && jok && oi != oj:
			return oi < oj
		case iok != jok:
			return iok
		default:
			return ids[i] < ids[j]
		}
	})
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
