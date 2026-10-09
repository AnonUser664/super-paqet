//go:build linux

// File opening_test.go verifies replay ownership and bounded receipt cleanup
// independently of production traffic or privileged namespace setup.
package engine

import (
	"bytes"
	"context"
	"fmt"
	"github.com/xtaci/smux"
	"io"
	"net"
	"paqet/internal/tnet/kcp"
	"sync/atomic"
	"testing"
	"time"

	"paqet/internal/protocol"
	"paqet/internal/tnet"
)

// TestOpeningReceiptReusesDialAndCommitsOnce checks that a delayed copy cannot
// claim the target twice or resurrect it after its ownership reached a relay.
func TestOpeningReceiptReusesDialAndCommitsOnce(t *testing.T) {
	var r openingRegistry
	defer r.close()
	p := protocol.Proto{Type: protocol.PTCP3, RequestID: [16]byte{1}, Addr: &tnet.Addr{Host: "127.0.0.1", Port: 2096}}
	deadline := time.Now().Add(time.Second)
	first, fresh, err := r.reserve(nil, p, 2, deadline)
	if err != nil || !fresh {
		t.Fatal(err)
	}
	replay, fresh, err := r.reserve(nil, p, 2, deadline)
	if err != nil || fresh || replay != first {
		t.Fatal("replay created another dial")
	}
	bad := p
	bad.Addr = &tnet.Addr{Host: "127.0.0.1", Port: 2097}
	if _, _, err := r.reserve(nil, bad, 2, deadline); err == nil {
		t.Fatal("conflicting target accepted")
	}
	left, right := net.Pipe()
	defer right.Close()
	r.finish(first, left, nil)
	if err := r.result(replay); err != nil {
		t.Fatal(err)
	}
	c, err := r.claim(replay)
	if err != nil || c != left {
		t.Fatal("target ownership lost", err)
	}
	defer c.Close()
	if _, err := r.claim(first); err == nil {
		t.Fatal("target claimed twice")
	}
	if _, _, err := r.reserve(nil, p, 2, deadline); err == nil {
		t.Fatal("delayed opening redialed a committed target")
	}
	r.retire(deadline.Add(time.Second), false)
	if len(r.entries) != 0 || len(r.expiry) != 0 {
		t.Fatal("completed receipt leaked")
	}
	// Expiring the tombstone must not close the relay-owned socket.
	go right.Write([]byte{42})
	var got [1]byte
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(c, got[:]); err != nil || got[0] != 42 {
		t.Fatal("receipt expiry closed relay", err)
	}
}

// TestOpeningReceiptBoundsAndExpiry closes abandoned targets and wakes joining
// handlers, including a dial completing concurrently with engine shutdown.
func TestOpeningReceiptBoundsAndExpiry(t *testing.T) {
	var r openingRegistry
	p := protocol.Proto{Type: protocol.PTCP3, RequestID: [16]byte{1}, Addr: &tnet.Addr{Host: "127.0.0.1", Port: 2096}}
	deadline := time.Now().Add(time.Second)
	first, _, _ := r.reserve(nil, p, 1, deadline)
	other := p
	other.RequestID = [16]byte{2}
	if _, _, err := r.reserve(nil, other, 1, deadline); err == nil {
		t.Fatal("receipt storage exceeded bound")
	}
	r.retire(deadline.Add(time.Second), false)
	select {
	case <-first.ready:
	default:
		t.Fatal("expired dial did not wake waiter")
	}
	if err := r.result(first); err == nil {
		t.Fatal("expired result accepted")
	}
	left, right := net.Pipe()
	defer right.Close()
	r.finish(first, left, nil)
	right.SetReadDeadline(time.Now().Add(time.Second))
	var got [1]byte
	if _, err := right.Read(got[:]); err != io.EOF {
		t.Fatal("late dial socket leaked", err)
	}
	second, _, err := r.reserve(nil, other, 1, deadline.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	l2, r2 := net.Pipe()
	defer r2.Close()
	r.finish(second, l2, nil)
	r.close()
	r2.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := r2.Read(got[:]); err != io.EOF {
		t.Fatal("shutdown leaked pending target", err)
	}
	if _, _, err := r.reserve(nil, p, 1, deadline); err == nil {
		t.Fatal("closed registry reopened")
	}
}

// TestOpeningReceiptRetainsFailedDial prevents retries from dialing a rejected
// or unreachable target again, while propagating its original failure.
func TestOpeningReceiptRetainsFailedDial(t *testing.T) {
	var r openingRegistry
	defer r.close()
	p := protocol.Proto{Type: protocol.PTCP3, RequestID: [16]byte{9}, Addr: &tnet.Addr{Host: "127.0.0.1", Port: 2096}}
	ticket, _, _ := r.reserve(nil, p, 1, time.Now().Add(time.Second))
	r.finish(ticket, nil, context.DeadlineExceeded)
	replay, fresh, err := r.reserve(nil, p, 1, time.Now().Add(time.Second))
	if err != nil || fresh || replay != ticket || r.result(replay) != context.DeadlineExceeded {
		t.Fatal("failed dial was repeated")
	}
	if _, err := r.claim(replay); err == nil {
		t.Fatal("failed target committed")
	}
}

// TestAcknowledgedOpeningEscapesLateReceiveBlock fills the selected lane only
// after ACK2. A sibling must finish the same target dial while original blocked
// streams retain their payloads and ownership. This is the missed production
// case: a cached admission hint cannot repair an already assigned opening.
func TestAcknowledgedOpeningEscapesLateReceiveBlock(t *testing.T) {
	e := reloadFixture(t)
	cfg := smux.DefaultConfig()
	cfg.Version, cfg.HalfClose = 2, true
	cfg.MaxReceiveBuffer, cfg.MaxStreamBuffer, cfg.MaxFrameSize = 4096, 4096, 1024
	blocked, first := openingCarrierConn(t, 3101, nil, cfg)
	slow, remoteSlow := holdOpeningCarrier(t, blocked, first)
	extra, remoteExtra := holdOpeningCarrier(t, blocked, first)
	healthy, second := openingCarrierConn(t, 3102, nil, cfg)
	healthy.score.Store(20)
	t.Cleanup(func() { blocked.conn.Load().Close(); first.Close(); healthy.conn.Load().Close(); second.Close() })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	target, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	remote, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	var dials atomic.Int32
	writer := make(chan error, 1)
	held := bytes.Repeat([]byte("held"), 1024)
	tail := bytes.Repeat([]byte{0x65}, 1024)
	e.openingDial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dials.Add(1)
		if _, err := remoteSlow.Write(held); err != nil {
			return nil, err
		}
		go func() { _, err := remoteExtra.Write(tail); writer <- err }()
		deadline := time.Now().Add(time.Second)
		for {
			_, _, full := blocked.conn.Load().Session.ReceiveBufferStats()
			if full {
				break
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("fixture never filled receiver")
			}
			time.Sleep(time.Millisecond)
		}
		return target, nil
	}
	handlers := make(chan error, 2)
	for _, server := range []*smux.Session{first, second} {
		go func(server *smux.Session) {
			stream, err := server.AcceptStream()
			if err != nil {
				handlers <- err
				return
			}
			defer stream.Abort()
			e.handle(e.ctx, nil, &kcp.Strm{Stream: stream}, 0)
			handlers <- nil
		}(server)
	}
	echo := make(chan error, 1)
	go func() {
		var data [5]byte
		_, err := io.ReadFull(remote, data[:])
		if err == nil && string(data[:]) != "hello" {
			err = io.ErrUnexpectedEOF
		}
		if err == nil {
			_, err = remote.Write([]byte("reply"))
		}
		echo <- err
	}()
	p := &peer{engine: e, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{blocked, healthy}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	opened, err := p.open(ctx, protocol.PTCP3, listener.Addr().String())
	if err != nil {
		t.Fatal("acknowledged opening failed behind full receive lane", err)
	}
	defer opened.Close()
	if dials.Load() != 1 || e.stats.OpenRetries.Load() != 1 {
		t.Fatalf("dial/retry count: %d/%d", dials.Load(), e.stats.OpenRetries.Load())
	}
	if blocked.suspect.Load() || p.recoveryFailures.Load() != 0 {
		t.Fatal("capacity was misclassified as failed transport")
	}
	opened.SetDeadline(time.Now().Add(time.Second))
	if _, err := opened.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	var response [5]byte
	if _, err := io.ReadFull(opened, response[:]); err != nil || string(response[:]) != "reply" {
		t.Fatal("winning relay changed bytes", err)
	}
	if err := <-echo; err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		stream *smux.Stream
		want   []byte
	}{{slow, held}, {extra, tail}} {
		x.stream.SetReadDeadline(time.Now().Add(time.Second))
		got := make([]byte, len(x.want))
		if _, err := io.ReadFull(x.stream, got); err != nil || !bytes.Equal(got, x.want) {
			t.Fatal("original stream payload damaged", err)
		}
	}
	if err := <-writer; err != nil {
		t.Fatal(err)
	}
	if blocked.conn.Load().Session.IsClosed() {
		t.Fatal("old established carrier retired")
	}
	// Closing both sides releases both handler relays before fixture cleanup.
	opened.Close()
	remote.Close()
	blocked.conn.Load().Close()
	first.Close()
	healthy.conn.Load().Close()
	second.Close()
	for range 2 {
		select {
		case err := <-handlers:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("handler cleanup did not finish")
		}
	}
}
