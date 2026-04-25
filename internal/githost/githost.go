// Package githost wraps the GitHub host (currently the `gh` CLI) for AIOS's
// autopilot finalizer. Two implementations: CLIHost (real, shells out to gh)
// and FakeHost (deterministic in-memory state machine for tests).
package githost

import (
	"context"
	"errors"
	"time"
)

// MergeMode is how a PR should be merged. Only Squash is implemented in M1
// because the spec mandates one tidy commit per autopilot run on main.
type MergeMode string

const (
	MergeSquash MergeMode = "squash"
	MergeMerge  MergeMode = "merge"
	MergeRebase MergeMode = "rebase"
)

// PR identifies a pull request opened against the host.
type PR struct {
	Number int    // PR number, e.g. 42
	URL    string // full URL, e.g. https://github.com/owner/repo/pull/42
	Head   string // head branch, e.g. aios/staging
	Base   string // base branch, e.g. main
}

// ChecksState summarises the aggregate result of all required checks on a PR
// at a single polling moment. It is a value type so callers can compare it.
type ChecksState string

const (
	ChecksPending ChecksState = "pending"
	ChecksGreen   ChecksState = "green"
	ChecksRed     ChecksState = "red"
)

// ErrChecksTimeout is returned by WaitForChecks when the deadline elapses
// before any required check transitions away from pending.
var ErrChecksTimeout = errors.New("checks did not complete before timeout")

// Host is the GitHub adapter the autopilot finalizer talks to. Two impls:
// CLIHost (real `gh`) and FakeHost (test double).
type Host interface {
	// OpenPR opens a PR from head→base with the given title and body, and
	// returns the resulting PR. Idempotency is the caller's responsibility:
	// re-opening when one already exists is the host's behaviour to define.
	OpenPR(ctx context.Context, base, head, title, body string) (*PR, error)

	// WaitForChecks polls all required checks on the PR until the aggregate
	// state is green or red, or until timeout fires. Returns ErrChecksTimeout
	// on deadline. Polling cadence is host-defined (default 10s for CLI).
	WaitForChecks(ctx context.Context, pr *PR, timeout time.Duration) (ChecksState, error)

	// MergePR merges the PR with the requested mode. Returns nil on success.
	// Implementations must refuse to merge when checks are not green; that is
	// a safety invariant of the autopilot — the caller should have verified
	// state with WaitForChecks first, but MergePR is the last line of defence.
	MergePR(ctx context.Context, pr *PR, mode MergeMode) error
}

// CLIHost and FakeHost are declared in cli.go and fake.go respectively.
// Empty placeholder structs here so the interface assertions in tests
// compile before those files are written.
type CLIHost struct{ _ struct{} }
type FakeHost struct{ _ struct{} }

func (*CLIHost) OpenPR(context.Context, string, string, string, string) (*PR, error) {
	return nil, errors.New("not implemented")
}
func (*CLIHost) WaitForChecks(context.Context, *PR, time.Duration) (ChecksState, error) {
	return "", errors.New("not implemented")
}
func (*CLIHost) MergePR(context.Context, *PR, MergeMode) error {
	return errors.New("not implemented")
}

func (*FakeHost) OpenPR(context.Context, string, string, string, string) (*PR, error) {
	return nil, errors.New("not implemented")
}
func (*FakeHost) WaitForChecks(context.Context, *PR, time.Duration) (ChecksState, error) {
	return "", errors.New("not implemented")
}
func (*FakeHost) MergePR(context.Context, *PR, MergeMode) error {
	return errors.New("not implemented")
}
