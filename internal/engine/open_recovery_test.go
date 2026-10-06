//go:build linux

// File open_recovery_test.go reproduces ordered-carrier stalls without root or
// production traffic. Existing streams must survive retries of a new opening.
package engine

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	kcplib "github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
	"paqet/internal/protocol"
	"paqet/internal/tnet/kcp"
)

// openingCarrier pairs real mux sessions over a deterministic in-memory link;
// a UDP KCP handle supplies the same RTT/identity API used by pool recovery.
func openingCarrier(t *testing.T, conv uint32) (*slot, *smux.Session) {
	t.Helper()
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sink, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		packet.Close()
		t.Fatal(err)
	}
	udp, err := kcplib.NewConn3(conv, sink.LocalAddr(), nil, 0, 0, packet)
	if err != nil {
		packet.Close()
		sink.Close()
		t.Fatal(err)
	}
	left, right := net.Pipe()
	cfg := smux.DefaultConfig()
	cfg.Version = 2
	client, err := smux.Client(left, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server, err := smux.Server(right, cfg)
	if err != nil {
		t.Fatal(err)
	}
	conn := &kcp.Conn{UDPSession: udp, Session: client}
	s := &slot{}
	s.conn.Store(conn)
	t.Cleanup(func() { conn.Close(); server.Close(); packet.Close(); sink.Close() })
	return s, server
}

// holdOpeningCarrier creates an established stream so recovery cannot solve a
// new opening timeout by discarding the carrier and existing application data.
func holdOpeningCarrier(t *testing.T, s *slot, server *smux.Session) (*smux.Stream, *smux.Stream) {
	t.Helper()
	local, err := s.conn.Load().Session.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := server.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { local.Close(); remote.Close() })
	return local, remote
}

// TestBusyOpeningRetriesHealthyCarrier proves that a silent busy lane cannot
// spend the entire opening deadline while a healthy sibling is available.
func TestBusyOpeningRetriesHealthyCarrier(t *testing.T) {
	e := reloadFixture(t)
	stalled, first := openingCarrier(t, 1101)
	healthy, second := openingCarrier(t, 1102)
	oldLocal, oldRemote := holdOpeningCarrier(t, stalled, first)
	// Force the stalled lane to be the first choice independently of scheduling.
	healthy.score.Store(1)
	p := &peer{engine: e, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{stalled, healthy}}
	failures := make(chan error, 2)
	go func() {
		s, err := first.AcceptStream()
		if err != nil {
			failures <- err
			return
		}
		defer s.Close()
		var proto protocol.Proto
		err = proto.Read(s)
		if err == nil {
			b := make([]byte, 1)
			n, readErr := s.Read(b)
			err = fmt.Errorf("stalled read n=%d byte=%d error=%v", n, b[0], readErr)
		} else {
			err = fmt.Errorf("stalled control: %w", err)
		}
		failures <- err
	}()
	go func() {
		s, err := second.AcceptStream()
		if err != nil {
			failures <- err
			return
		}
		var proto protocol.Proto
		if err = proto.Read(s); err == nil {
			err = writeOpeningAck(&kcp.Strm{Stream: s}, 0)
		}
		failures <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := p.open(ctx, protocol.PTCP2, "127.0.0.1:2096")
	if err != nil {
		select {
		case cause := <-failures:
			t.Logf("server cause: %v", cause)
		default:
		}
		t.Fatalf("healthy sibling was not tried within the opening deadline: %v", err)
	}
	defer stream.Close()
	if e.stats.OpenRetries.Load() != 1 {
		t.Fatalf("retry counter: %d", e.stats.OpenRetries.Load())
	}
	if stalled.conn.Load() == nil || stalled.conn.Load().Session.IsClosed() {
		t.Fatal("retry destroyed a busy carrier")
	}
	oldLocal.SetDeadline(time.Now().Add(time.Second))
	oldRemote.SetDeadline(time.Now().Add(time.Second))
	write := make(chan error, 1)
	go func() { _, err := oldLocal.Write([]byte("alive")); write <- err }()
	var data [5]byte
	if _, err := io.ReadFull(oldRemote, data[:]); err != nil || string(data[:]) != "alive" {
		t.Fatalf("established stream did not survive: %q %v", data, err)
	}
	if err := <-write; err != nil {
		t.Fatal(err)
	}
}

// TestTransportReceiptRetainsTargetDialBudget distinguishes a transport stall
// from an acknowledged target dial; receipt ACK2 must prevent duplicate retries.
func TestTransportReceiptRetainsTargetDialBudget(t *testing.T) {
	e := reloadFixture(t)
	s, server := openingCarrier(t, 1201)
	holdOpeningCarrier(t, s, server)
	spare, _ := openingCarrier(t, 1202)
	spare.score.Store(1)
	p := &peer{engine: e, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{s, spare}}
	done := make(chan error, 1)
	go func() {
		stream, err := server.AcceptStream()
		if err != nil {
			done <- err
			return
		}
		var proto protocol.Proto
		if err = proto.Read(stream); err == nil {
			err = writeOpeningAck(&kcp.Strm{Stream: stream}, 2)
		}
		if err == nil {
			time.Sleep(650 * time.Millisecond)
			err = writeOpeningAck(&kcp.Strm{Stream: stream}, 0)
		}
		done <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := p.open(ctx, protocol.PTCP2, "127.0.0.1:2096")
	if err != nil {
		t.Fatalf("receipt did not retain the target dial deadline: %v", err)
	}
	defer stream.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
