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

// FailOnCallEngine returns scripted responses; the call numbered FailOnCall
// (1-based) returns an error instead. Used by REPL integration tests to
// exercise mid-pipeline failure paths.
type FailOnCallEngine struct {
	Name_      string
	Script     []InvokeResponse
	Received   []InvokeRequest
	FailOnCall int // 1-based; 0 = never fail
	calls      int
}

func (f *FailOnCallEngine) Name() string { return f.Name_ }

func (f *FailOnCallEngine) Invoke(_ context.Context, req InvokeRequest) (*InvokeResponse, error) {
	f.Received = append(f.Received, req)
	f.calls++
	if f.calls == f.FailOnCall {
		return nil, errors.New("FailOnCallEngine: scripted failure")
	}
	if f.calls-1 >= len(f.Script) {
		return &InvokeResponse{Text: ""}, nil
	}
	r := f.Script[f.calls-1]
	return &r, nil
}
