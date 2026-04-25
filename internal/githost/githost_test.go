package githost

import "testing"

// TestHostInterfaceShape locks the public surface of the Host interface so
// downstream callers (autopilot finalizer, fake) compile against a stable shape.
func TestHostInterfaceShape(t *testing.T) {
	var _ Host = (*CLIHost)(nil)
	var _ Host = (*FakeHost)(nil)
}
