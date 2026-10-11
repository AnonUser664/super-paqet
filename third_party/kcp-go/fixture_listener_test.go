package kcp

import (
	"net"
	"sync"
	"testing"
	"time"
)

// fixtureListener owns the sessions accepted by an echo/sink fixture. Closing
// a real Listener stops acceptance but deliberately does not own accepted
// carriers, which can migrate or outlive their listener. Tests must release
// those carriers explicitly so earlier fixtures cannot load later tests.
type fixtureListener struct {
	net.Listener
	mu       sync.Mutex
	accepted []net.Conn
	closed   bool
}

// Accept registers fixture ownership before a handler can start. A concurrent
// Close also releases an acceptance that completes after its cleanup snapshot.
func (l *fixtureListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	closed := l.closed
	if !closed {
		l.accepted = append(l.accepted, c)
	}
	l.mu.Unlock()
	if closed {
		c.Close()
		return nil, net.ErrClosed
	}
	return c, nil
}

// Close stops the acceptor and closes only the sessions this fixture accepted.
// Connection closure stays outside the tracking lock and releases blocked
// handlers, post-processing workers and future timed callbacks.
func (l *fixtureListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	accepted := l.accepted
	l.accepted = nil
	l.mu.Unlock()
	err := l.Listener.Close()
	for _, c := range accepted {
		c.Close()
	}
	return err
}

// TestFixtureListenerOwnsAcceptedSessions verifies that closing an echo
// fixture releases a real accepted KCP carrier, not only its accept socket.
func TestFixtureListenerOwnsAcceptedSessions(t *testing.T) {
	listener, err := ListenWithOptions("127.0.0.1:0", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	owner := &fixtureListener{Listener: listener}
	defer owner.Close()
	listener.SetDeadline(time.Now().Add(2 * time.Second))
	client, err := DialWithOptions(listener.Addr().String(), nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err = client.Write([]byte{42}); err != nil {
		t.Fatal(err)
	}
	accepted, err := owner.Accept()
	if err != nil {
		t.Fatal(err)
	}
	accepted.SetReadDeadline(time.Now().Add(2 * time.Second))
	var b [1]byte
	if _, err = accepted.Read(b[:]); err != nil || b[0] != 42 {
		t.Fatalf("fixture never became active: byte=%d err=%v", b[0], err)
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = accepted.Read(b[:]); err == nil || !accepted.(*UDPSession).isClosed() {
		t.Fatal("fixture teardown left its accepted carrier alive")
	}
}
