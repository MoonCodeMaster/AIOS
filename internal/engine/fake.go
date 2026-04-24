package engine

import (
	"context"
	"errors"
)

// FakeEngine is a scripted Engine used in unit and integration tests.
// It is NOT behind a build tag on purpose: --dry-run may also use it.
type FakeEngine struct {
	Name_    string
	Script   []InvokeResponse
	Received []InvokeRequest
	calls    int
}

func (f *FakeEngine) Name() string { return f.Name_ }

func (f *FakeEngine) Invoke(_ context.Context, req InvokeRequest) (*InvokeResponse, error) {
	f.Received = append(f.Received, req)
	if f.calls >= len(f.Script) {
		return nil, errors.New("FakeEngine: unexpected call (script exhausted)")
	}
	r := f.Script[f.calls]
	f.calls++
	return &r, nil
}
