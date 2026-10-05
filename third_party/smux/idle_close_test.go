// File idle_close_test.go: exercises idle close regressions; fixtures must preserve cleanup
// and expose byte/lifecycle failures explicitly.

package smux

import "testing"

// TestIdleClosePreservesActiveStream checks Idle Close Preserves Active Stream so a change
// cannot silently weaken the recorded regression contract.
func TestIdleClosePreservesActiveStream(t *testing.T) {
	client, _, c, s := resetPair(t)
	if client.CloseIfIdle() {
		t.Fatal("idle close killed active stream")
	}
	c.Close()
	s.Close()
	if !client.CloseIfIdle() || !client.IsClosed() {
		t.Fatal("empty session did not close")
	}
	if _, err := client.OpenStream(); err == nil {
		t.Fatal("closed session accepted a new stream")
	}
}
