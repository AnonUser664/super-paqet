package smux

import "testing"

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
