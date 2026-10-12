//go:build linux && !spq_private_writers

// File private_output_disabled_linux_test.go prevents a normal build from
// silently enabling the rejected private-output experiment.
package socket

import "testing"

// TestDefaultBuildKeepsSharedOutput uses an already-closed factory fixture so
// accidental factory invocation is visible without requiring raw privileges.
func TestDefaultBuildKeepsSharedOutput(t *testing.T) {
	p := &PacketConn{raw: &rawPacket{writersClosed: true}}
	writer, err := p.NewSessionWriter()
	if writer != nil || err != nil {
		t.Fatalf("ordinary build invoked private writer factory: %v/%v", writer, err)
	}
}
