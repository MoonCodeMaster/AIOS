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

	Issues          []Issue
	NextIssueNumber int
	OpenedIssues    []Issue
	Comments        map[int][]string
	Closed          map[int]bool
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

func (f *FakeHost) ListLabeled(_ context.Context, label string) ([]Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Issue
	for _, i := range f.Issues {
		for _, l := range i.Labels {
			if l == label {
				out = append(out, i)
				break
			}
		}
	}
	return out, nil
}

func (f *FakeHost) AddLabel(_ context.Context, issueNum int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, iss := range f.Issues {
		if iss.Number != issueNum {
			continue
		}
		for _, existing := range iss.Labels {
			if existing == label {
				return nil
			}
		}
		f.Issues[i].Labels = append(iss.Labels, label)
		return nil
	}
	return fmt.Errorf("FakeHost: issue %d not found for AddLabel", issueNum)
}

func (f *FakeHost) RemoveLabel(_ context.Context, issueNum int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, iss := range f.Issues {
		if iss.Number != issueNum {
			continue
		}
		out := iss.Labels[:0]
		for _, existing := range iss.Labels {
			if existing == label {
				continue
			}
			out = append(out, existing)
		}
		f.Issues[i].Labels = out
		return nil
	}
	return fmt.Errorf("FakeHost: issue %d not found for RemoveLabel", issueNum)
}

func (f *FakeHost) AddComment(_ context.Context, issueNum int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Comments == nil {
		f.Comments = map[int][]string{}
	}
	f.Comments[issueNum] = append(f.Comments[issueNum], body)
	return nil
}

func (f *FakeHost) OpenIssue(_ context.Context, title, body string, labels []string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NextIssueNumber == 0 {
		f.NextIssueNumber = 1000
	}
	n := f.NextIssueNumber
	f.NextIssueNumber++
	iss := Issue{Number: n, Title: title, Body: body, Labels: labels, URL: fmt.Sprintf("https://example.invalid/issues/%d", n)}
	f.Issues = append(f.Issues, iss)
	f.OpenedIssues = append(f.OpenedIssues, iss)
	return n, nil
}

func (f *FakeHost) CloseIssue(_ context.Context, issueNum int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Closed == nil {
		f.Closed = map[int]bool{}
	}
	f.Closed[issueNum] = true
	return nil
}
