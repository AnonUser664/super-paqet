//go:build linux

// File session_writer_linux_test.go exercises optional output ownership using
// real UDP dispatchers, including close isolation and live physical movement.
package kcp

import (
	"bytes"
	"golang.org/x/net/ipv4"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// writerSocket records created output objects; input remains the shared socket.
type writerSocket struct {
	net.PacketConn
	mu      sync.Mutex
	writers []*ownedTestWriter
}

func (p *writerSocket) NewSessionWriter() (SessionWriter, error) {
	w := &ownedTestWriter{parent: p, done: make(chan struct{})}
	p.mu.Lock()
	p.writers = append(p.writers, w)
	p.mu.Unlock()
	return w, nil
}
func (p *writerSocket) newest() *ownedTestWriter {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writers[len(p.writers)-1]
}

// ownedTestWriter closes only its own lifecycle, never the shared UDP receiver.
type ownedTestWriter struct {
	parent *writerSocket
	done   chan struct{}
	once   sync.Once
}

func (w *ownedTestWriter) WriteBatch(ms []ipv4.Message, flags int) (int, error) {
	n, _, err := w.WriteBatchWithDrops(ms, flags)
	return n, err
}
func (w *ownedTestWriter) WriteBatchWithDrops(ms []ipv4.Message, _ int) (int, int, error) {
	select {
	case <-w.done:
		return 0, 0, net.ErrClosed
	default:
	}
	for i, m := range ms {
		if _, err := w.parent.WriteTo(m.Buffers[0], m.Addr); err != nil {
			return i, 0, err
		}
	}
	return len(ms), 0, nil
}
func (w *ownedTestWriter) SetWriteDeadline(time.Time) error { return nil }
func (w *ownedTestWriter) Close() error                     { w.once.Do(func() { close(w.done) }); return nil }

// ownedWriterListener retains a real packet receiver independently of carriers.
func ownedWriterListener(t *testing.T) (*Listener, *writerSocket) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &writerSocket{PacketConn: pc}
	l, err := ServeConversationConn(nil, 0, 0, p, false)
	if err != nil {
		pc.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close(); pc.Close() })
	return l, p
}

// TestSessionWriterClosePreservesListener verifies final output retirement does
// not close a receiver needed by another carrier.
func TestSessionWriterClosePreservesListener(t *testing.T) {
	client, _ := migrationListener(t, nil, true)
	server, p := ownedWriterListener(t)
	c, s := migrationPair(t, client, server)
	if !s.HasSessionWriter() {
		t.Fatal("optional writer was not installed")
	}
	w := p.newest()
	s.Close()
	c.Close()
	select {
	case <-w.done:
	case <-time.After(2 * time.Second):
		t.Fatal("output writer leaked after carrier close")
	}
	c2, s2 := migrationPair(t, client, server)
	defer c2.Close()
	defer s2.Close()
	if !s2.HasSessionWriter() {
		t.Fatal("shared listener failed after carrier close")
	}
}

// TestSessionWriterMigrationRetiresOnlyOldGeneration retains ARQ data while
// exchanging output objects, then closes the retired receiver independently.
func TestSessionWriterMigrationRetiresOnlyOldGeneration(t *testing.T) {
	client, _ := migrationListener(t, nil, true)
	old, oldPacket := ownedWriterListener(t)
	next, nextPacket := ownedWriterListener(t)
	c, s := migrationPair(t, client, old)
	defer c.Close()
	defer s.Close()
	previous := oldPacket.newest()
	if err := client.MoveSession(c, next.Addr()); err != nil {
		t.Fatal(err)
	}
	if err := next.MoveSession(s, client.Addr()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-previous.done:
	default:
		t.Fatal("retired output descriptor remained open")
	}
	current := nextPacket.newest()
	select {
	case <-current.done:
		t.Fatal("new output descriptor was closed")
	default:
	}
	old.Close()
	oldPacket.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	s.SetDeadline(time.Now().Add(3 * time.Second))
	payload := []byte("after physical movement")
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len(payload))
	if _, err := io.ReadFull(s, data); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("input after move: %q/%v", data, err)
	}
	if _, err := s.Write(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, data); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("output after move: %q/%v", data, err)
	}
	s.Close()
	select {
	case <-current.done:
	case <-time.After(2 * time.Second):
		t.Fatal("moved output descriptor leaked")
	}
}

// TestSessionWriterDialOnlyKeepsOriginalOutput protects independent client
// source slots from receiving redundant output descriptors during migration.
func TestSessionWriterDialOnlyKeepsOriginalOutput(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	provider := &writerSocket{PacketConn: pc}
	p := makeListenerPlatform(provider, false)
	if p.hasSessionWriter() || len(provider.writers) != 0 {
		t.Fatal("outgoing route allocated an incoming-only writer")
	}
}
