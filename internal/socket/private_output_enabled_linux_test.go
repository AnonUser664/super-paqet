//go:build linux && spq_private_writers

// File private_output_enabled_linux_test.go verifies explicit experimental
// builds still select the factory and respect closed-listener ownership.
package socket

import (
	"errors"
	"net"
	"testing"
)

// TestExperimentalBuildSelectsPrivateOutput rejects creation on a retired
// listener without needing to open a privileged socket in the test process.
func TestExperimentalBuildSelectsPrivateOutput(t *testing.T) {
	p := &PacketConn{raw: &rawPacket{writersClosed: true}}
	writer, err := p.NewSessionWriter()
	if writer != nil || !errors.Is(err, net.ErrClosed) {
		t.Fatalf("experimental build did not select retired factory: %v/%v", writer, err)
	}
}
