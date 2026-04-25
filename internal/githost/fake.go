package githost

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Compile-time guard: FakeHost satisfies Host.
var _ Host = (*FakeHost)(nil)

// FakeHost is an in-memory Host for tests. It is goroutine-safe so concurrent
// integration tests don't race on the merged map.
type FakeHost struct {
	mu     sync.Mutex
	nextID int
	prs    map[int]*PR

	// ChecksByPR lets tests configure WaitForChecks return values. Missing
	// entries are treated as ChecksGreen for ergonomics.
	ChecksByPR map[int]ChecksState
	// Merged records which PRs MergePR was called on. Read by tests.
	Merged map[int]bool
	// OpenedPRs records every PR opened, in order, so tests can assert on
	// title/body/branches without poking internal state.
	OpenedPRs []*PR
}

func (f *FakeHost) OpenPR(_ context.Context, base, head, title, body string) (*PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prs == nil {
		f.prs = map[int]*PR{}
	}
	if f.Merged == nil {
		f.Merged = map[int]bool{}
	}
	f.nextID++
	pr := &PR{
		Number: f.nextID,
		URL:    fmt.Sprintf("https://example.invalid/pull/%d", f.nextID),
		Head:   head,
		Base:   base,
	}
	f.prs[pr.Number] = pr
	f.OpenedPRs = append(f.OpenedPRs, pr)
	return pr, nil
}

func (f *FakeHost) WaitForChecks(_ context.Context, pr *PR, _ time.Duration) (ChecksState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if state, ok := f.ChecksByPR[pr.Number]; ok {
		return state, nil
	}
	return ChecksGreen, nil
}

func (f *FakeHost) MergePR(_ context.Context, pr *PR, _ MergeMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if state, ok := f.ChecksByPR[pr.Number]; ok && state != ChecksGreen {
		return errors.New("FakeHost: refusing to merge a non-green PR")
	}
	if f.Merged == nil {
		f.Merged = map[int]bool{}
	}
	f.Merged[pr.Number] = true
	return nil
}
