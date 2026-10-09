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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paqet/internal/protocol"
	"paqet/internal/tnet"
)

// openingTCPPair owns both sides of a real loopback connection for nonblocking
// first-read and directional EOF tests, independently of privileged fixtures.
func openingTCPPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	client, err := net.DialTCP("tcp", nil, l.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	server, err := l.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return client, server
}

// openingCommitCapture copies accepted bytes like a real mux, with a bounded
// short-write seam. Unused stream methods are supplied only by the embedding.
type openingCommitCapture struct {
	tnet.Strm
	data  []byte
	limit int
}

// Write records only the bytes this destination accepts.
func (s *openingCommitCapture) Write(p []byte) (int, error) {
	n := len(p)
	if s.limit >= 0 {
		n = min(n, s.limit)
	}
	s.data = append(s.data, p[:n]...)
	return n, nil
}

// WritePriority exercises the same priority interface as the live stream.
func (s *openingCommitCapture) WritePriority(p []byte) (int, error) {
	if len(p) > 512 {
		return 0, io.ErrShortBuffer
	}
	return s.Write(p)
}

// TestOpeningCommitPreface verifies bounded consumption, the nonblocking
// server-first path, EOF, short-write accounting and rejection after TCP close.
func TestOpeningCommitPreface(t *testing.T) {
	for _, tc := range []struct {
		name        string
		size, limit int
		eof, closed bool
	}{
		{"empty", 0, -1, false, false}, {"data", 20, -1, false, false},
		{"bounded", 8192, -1, false, false}, {"short", 20, 3, false, false},
		{"eof", 0, -1, true, false}, {"closed", 0, -1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, tcp := openingTCPPair(t)
			payload := bytes.Repeat([]byte{0x61}, tc.size)
			if tc.size > 0 {
				if _, err := client.Write(payload); err != nil {
					t.Fatal(err)
				}
			}
			if tc.eof {
				client.CloseWrite()
			}
			if tc.closed {
				tcp.Close()
			}
			stream := &openingCommitCapture{limit: tc.limit}
			var count atomic.Int64
			started := time.Now()
			err := writeOpeningCommit(stream, tcp, &count)
			if time.Since(started) > 100*time.Millisecond {
				t.Fatal("commit waited for application data")
			}
			if tc.closed {
				if err == nil || len(stream.data) != 0 {
					t.Fatal("closed TCP committed", err)
				}
				return
			}
			n := min(tc.size, 511)
			want := append([]byte{0}, payload[:n]...)
			if tc.limit >= 0 {
				want = want[:min(len(want), tc.limit)]
			}
			if !bytes.Equal(stream.data, want) || count.Load() != int64(max(0, len(want)-1)) {
				t.Fatal("commit bytes/accounting changed")
			}
			if tc.limit >= 0 && err != io.ErrShortWrite || tc.limit < 0 && err != nil {
				t.Fatal("commit error", err)
			}
			if tc.size > n {
				tcp.SetReadDeadline(time.Now().Add(time.Second))
				tail := make([]byte, tc.size-n)
				if _, err := io.ReadFull(tcp, tail); err != nil || !bytes.Equal(tail, payload[n:]) {
					t.Fatal("unread TCP tail changed", err)
				}
			}
			if tc.eof {
				tcp.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := tcp.Read(make([]byte, 1)); err != io.EOF {
					t.Fatal("directional EOF consumed", err)
				}
			}
		})
	}
}

// TestOpeningCommitLargePrefaceUsesRealPriorityLimit guards TLS-sized first
// requests against the actual mux priority cap, rather than an unlimited mock.
func TestOpeningCommitLargePrefaceUsesRealPriorityLimit(t *testing.T) {
	client, tcp := openingTCPPair(t)
	payload := bytes.Repeat([]byte{0x74}, 8192)
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	s, server := openingCarrier(t, 3120)
	local, remote := holdOpeningCarrier(t, s, server)
	local.SetDeadline(time.Now().Add(time.Second))
	remote.SetDeadline(time.Now().Add(time.Second))
	var count atomic.Int64
	if err := writeOpeningCommit(&kcp.Strm{Stream: local}, tcp, &count); err != nil {
		t.Fatal("large first request rejected", err)
	}
	wire := make([]byte, 512)
	if _, err := io.ReadFull(remote, wire); err != nil || wire[0] != 0 || !bytes.Equal(wire[1:], payload[:511]) {
		t.Fatal("commit/preface wire changed", err)
	}
	if count.Load() != 511 {
		t.Fatal("preface byte counter", count.Load())
	}
	tcp.SetReadDeadline(time.Now().Add(time.Second))
	tail := make([]byte, len(payload)-511)
	if _, err := io.ReadFull(tcp, tail); err != nil || !bytes.Equal(tail, payload[511:]) {
		t.Fatal("large first request tail consumed", err)
	}
}

// TestOpeningReceiptReusesDialAndCommitsOnce checks that a delayed copy cannot
// claim the target twice or resurrect it after its ownership reached a relay.
func TestOpeningReceiptReusesDialAndCommitsOnce(t *testing.T) {
	var r openingRegistry
	defer r.close()
	p := protocol.Proto{Type: protocol.PTCP3, RequestID: [16]byte{1}, Addr: &tnet.Addr{Host: "127.0.0.1", Port: 2096}}
	deadline := time.Now().Add(time.Second)
	first, fresh, err := r.reserve(context.Background(), nil, p, 2, deadline)
	if err != nil || !fresh {
		t.Fatal(err)
	}
	replay, fresh, err := r.reserve(context.Background(), nil, p, 2, deadline)
	if err != nil || fresh || replay != first {
		t.Fatal("replay created another dial")
	}
	bad := p
	bad.Addr = &tnet.Addr{Host: "127.0.0.1", Port: 2097}
	if _, _, err := r.reserve(context.Background(), nil, bad, 2, deadline); err == nil {
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
	if _, _, err := r.reserve(context.Background(), nil, p, 2, deadline); err == nil {
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
	first, _, _ := r.reserve(context.Background(), nil, p, 1, deadline)
	other := p
	other.RequestID = [16]byte{2}
	if _, _, err := r.reserve(context.Background(), nil, other, 1, deadline); err == nil {
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
	second, _, err := r.reserve(context.Background(), nil, other, 1, deadline.Add(time.Second))
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
	if _, _, err := r.reserve(context.Background(), nil, p, 1, deadline); err == nil {
		t.Fatal("closed registry reopened")
	}
}

// TestOpeningReceiptRetainsFailedDial prevents retries from dialing a rejected
// or unreachable target again, while propagating its original failure.
func TestOpeningReceiptRetainsFailedDial(t *testing.T) {
	var r openingRegistry
	defer r.close()
	p := protocol.Proto{Type: protocol.PTCP3, RequestID: [16]byte{9}, Addr: &tnet.Addr{Host: "127.0.0.1", Port: 2096}}
	ticket, _, _ := r.reserve(context.Background(), nil, p, 1, time.Now().Add(time.Second))
	r.finish(ticket, nil, context.DeadlineExceeded)
	replay, fresh, err := r.reserve(context.Background(), nil, p, 1, time.Now().Add(time.Second))
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
	for _, preface := range []bool{false, true} {
		t.Run(fmt.Sprintf("preface-%t", preface), func(t *testing.T) { acknowledgedOpeningEscapesLateReceiveBlock(t, preface) })
	}
}

// acknowledgedOpeningEscapesLateReceiveBlock shares the real mux regression
// between ordinary commit and commit/application coalescing.
func acknowledgedOpeningEscapesLateReceiveBlock(t *testing.T, preface bool) {
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
		if _, err := remote.Write([]byte("banner")); err != nil {
			echo <- err
			return
		}
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
	var initial []*net.TCPConn
	if preface {
		client, accepted := openingTCPPair(t)
		if _, err := client.Write([]byte("hello")); err != nil {
			t.Fatal(err)
		}
		initial = append(initial, accepted)
	}
	opened, err := p.open(ctx, protocol.PTCP3, listener.Addr().String(), initial...)
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
	var banner [6]byte
	if _, err := io.ReadFull(opened, banner[:]); err != nil || string(banner[:]) != "banner" {
		t.Fatal("discarded attempt consumed target banner", err)
	}
	if !preface {
		if _, err := opened.Write([]byte("hello")); err != nil {
			t.Fatal(err)
		}
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

// TestOpeningReceiptConcurrentClaim proves replay streams cannot simultaneously
// take the one dial result, even when commits and publication race.
func TestOpeningReceiptConcurrentClaim(t *testing.T) {
	var r openingRegistry
	defer r.close()
	p := protocol.Proto{Type: protocol.PTCP3, RequestID: [16]byte{3}, Addr: &tnet.Addr{Host: "127.0.0.1", Port: 2096}}
	ticket, _, err := r.reserve(context.Background(), nil, p, 10, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	r.finish(ticket, left, nil)
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c, err := r.claim(ticket); err == nil {
				if c != left {
					t.Error("different target claimed")
				}
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("committed %d times", winners.Load())
	}
}

// TestOpeningReceiptListenerRetirement releases abandoned targets during reload
// without waiting for expiry or disturbing another listener generation.
func TestOpeningReceiptListenerRetirement(t *testing.T) {
	var r openingRegistry
	defer r.close()
	first, second := &kcp.Listener{}, &kcp.Listener{}
	ctx, cancel := context.WithCancel(context.Background())
	p := protocol.Proto{Type: protocol.PTCP3, RequestID: [16]byte{4}, Addr: &tnet.Addr{Host: "127.0.0.1", Port: 2096}}
	a, _, err := r.reserve(ctx, first, p, 10, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := r.reserve(context.Background(), second, p, 10, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer right.Close()
	r.finish(a, left, nil)
	cancel()
	r.retireListener(first)
	if count, pending := r.counts(); count != 1 || pending != 1 {
		t.Fatalf("counts=%d/%d", count, pending)
	}
	if _, _, err := r.reserve(ctx, first, p, 10, time.Now().Add(time.Second)); err != context.Canceled {
		t.Fatal("cancelled generation accepted", err)
	}
	right.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := right.Read(make([]byte, 1)); err != io.EOF {
		t.Fatal("retired target leaked", err)
	}
	if r.entries[b.key] != b || len(r.expiry) != 1 || r.expiry[0] != b {
		t.Fatal("unrelated receipt retired")
	}
}

// TestAcknowledgedSlowTargetKeepsBudget verifies read checkpoints never mistake
// a healthy slow target for carrier failure or spend another target dial.
func TestAcknowledgedSlowTargetKeepsBudget(t *testing.T) {
	e := reloadFixture(t)
	s, remoteMux := openingCarrier(t, 3110)
	sibling, _ := openingCarrier(t, 3111)
	sibling.score.Store(20)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var dials atomic.Int32
	e.openingDial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		select {
		case <-time.After(600 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		strm, err := remoteMux.AcceptStream()
		if err != nil {
			return
		}
		defer strm.Abort()
		e.handle(e.ctx, nil, &kcp.Strm{Stream: strm}, 0)
	}()
	p := &peer{engine: e, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{s, sibling}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opened, err := p.open(ctx, protocol.PTCP3, listener.Addr().String())
	if err != nil {
		t.Fatal("healthy delayed target failed", err)
	}
	remote, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	opened.Close()
	remote.Close()
	s.conn.Load().Close()
	remoteMux.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not release")
	}
	if dials.Load() != 1 || e.stats.OpenRetries.Load() != 0 || s.suspect.Load() {
		t.Fatal("healthy slow target retried or suspected")
	}
}
