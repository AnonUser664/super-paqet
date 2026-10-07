// File migration_test.go verifies ARQ continuity and dispatcher ownership using
// real UDP sockets. A physical outage discards packets rather than returning IO errors.
package kcp

import (
	"bytes"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// migrationSocket simulates tuple loss while preserving the socket lifecycle.
type migrationSocket struct {
	net.PacketConn
	drop atomic.Bool
}

// WriteTo discards selected packets while reporting a successful local send.
func (c *migrationSocket) WriteTo(b []byte, a net.Addr) (int, error) {
	if c.drop.Load() {
		return len(b), nil
	}
	return c.PacketConn.WriteTo(b, a)
}

// migrationListener owns a real socket and its deterministic test dispatcher.
func migrationListener(t *testing.T, block BlockCrypt, outgoing bool) (*Listener, *migrationSocket) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	socket := &migrationSocket{PacketConn: conn}
	l, err := ServeConversationConn(block, 0, 0, socket, outgoing)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close(); socket.Close() })
	return l, socket
}

// migrationPair establishes both sides without an unbounded accept/read wait.
func migrationPair(t *testing.T, client, server *Listener) (*UDPSession, *UDPSession) {
	t.Helper()
	c, err := client.DialConversation(server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetNoDelay(1, 10, 2, 1)
	c.SetWriteDelay(false)
	c.SetDeadline(time.Now().Add(10 * time.Second))
	server.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	s, err := server.AcceptKCP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.SetNoDelay(1, 10, 2, 1)
	s.SetWriteDelay(false)
	s.SetDeadline(time.Now().Add(10 * time.Second))
	var b [1]byte
	if _, err = io.ReadFull(s, b[:]); err != nil {
		t.Fatal(err)
	}
	return c, s
}

// TestMigrationRetainsPendingBytesAndOwnership moves across backend workers,
// then closes old sockets and the candidate without losing the original stream.
func TestMigrationRetainsPendingBytesAndOwnership(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(map[bool]string{false: "null", true: "aes-gcm"}[encrypted], func(t *testing.T) {
			var block BlockCrypt
			if encrypted {
				var err error
				block, err = NewAESGCMCrypt(bytes.Repeat([]byte{7}, 16))
				if err != nil {
					t.Fatal(err)
				}
			}
			client, cs := migrationListener(t, block, true)
			server, ss := migrationListener(t, block, false)
			nextClient, _ := migrationListener(t, block, true)
			nextServer, _ := migrationListener(t, block, false)
			c, s := migrationPair(t, client, server)
			control, via := migrationPair(t, nextClient, nextServer)
			conv := c.GetConv()
			oldRemote := s.RemoteAddr().String()
			cs.drop.Store(true)
			ss.drop.Store(true)
			payload := bytes.Repeat([]byte("integrity-after-move"), 4096)
			writeDone := make(chan error, 1)
			go func() { _, err := c.Write(payload); writeDone <- err }()
			time.Sleep(50 * time.Millisecond)
			if err := s.MigrateVia(via); err != nil {
				t.Fatal(err)
			}
			if err := nextClient.MoveSession(c, nextServer.Addr()); err != nil {
				t.Fatal(err)
			}
			c.RetransmitNow()
			s.RetransmitNow()
			if err := s.MigrateVia(via); err != nil {
				t.Fatalf("idempotent move: %v", err)
			}
			client.Close()
			cs.Close()
			server.Close()
			ss.Close()
			control.Close()
			via.Close()
			if c.isClosed() || s.isClosed() || c.GetConv() != conv || s.GetConv() != conv {
				t.Fatal("logical session lost during physical retirement")
			}
			if s.RemoteAddr().String() == oldRemote {
				t.Fatal("return route unchanged")
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(s, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, got) {
				t.Fatal("pending bytes corrupted")
			}
			if err := <-writeDone; err != nil {
				t.Fatal(err)
			}
			// Continue the same application byte stream in the reverse direction.
			go func() { _, err := s.Write(payload); writeDone <- err }()
			if _, err := io.ReadFull(c, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, got) {
				t.Fatal("reverse stream corrupted")
			}
			if err := <-writeDone; err != nil {
				t.Fatal(err)
			}
			client.sessionLock.RLock()
			oldCount := len(client.sessions)
			client.sessionLock.RUnlock()
			if oldCount != 0 {
				t.Fatal("old dispatcher retained ownership")
			}
		})
	}
}

// TestMigrationRejectsClosedAndCollidingOwners prevents resource loss when
// candidates close concurrently or a target conversation key is occupied.
func TestMigrationRejectsClosedAndCollidingOwners(t *testing.T) {
	client, _ := migrationListener(t, nil, true)
	server, _ := migrationListener(t, nil, false)
	next, _ := migrationListener(t, nil, true)
	c, _ := migrationPair(t, client, server)
	next.sessionLock.Lock()
	key := next.conversationKey(server.Addr(), c.GetConv())
	next.sessions[key] = &UDPSession{}
	next.sessionLock.Unlock()
	if err := next.MoveSession(c, server.Addr()); err == nil {
		t.Fatal("collision accepted")
	}
	next.sessionLock.Lock()
	delete(next.sessions, key)
	next.sessionLock.Unlock()
	next.Close()
	if err := next.MoveSession(c, server.Addr()); err == nil {
		t.Fatal("closed dispatcher accepted")
	}
	if c.isClosed() {
		t.Fatal("failed move closed live session")
	}
}
