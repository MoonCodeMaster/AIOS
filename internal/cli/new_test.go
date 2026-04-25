package cli

import "testing"

// TestNewOptsHasAutoField is a compile-time assertion that NewOpts has the
// Auto field. The behaviour test that --auto actually skips the prompt is
// covered end-to-end in test/integration once the autopilot finalizer is
// wired up — runNew talks to real engines and a real git repo, which a unit
// test cannot economically stub.
func TestNewOptsHasAutoField(t *testing.T) {
	var o NewOpts
	o.Auto = true
	if !o.Auto {
		t.Error("NewOpts.Auto roundtrip failed")
	}
}
