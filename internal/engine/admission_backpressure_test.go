//go:build linux

// File admission_backpressure_test.go reproduces a slow reader filling one
// mux lane while established streams and healthy sibling openings remain live.
package engine

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	kcplib "github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
	"paqet/internal/protocol"
	"paqet/internal/tnet/kcp"
)

// TestAdmissionAvoidsFullReceiveLane uses real mux flow control rather than a
// fabricated blocked bit. Drain afterward to prove the original streams and
// payloads survive; prefer normal scoring again when backpressure clears.
func TestAdmissionAvoidsFullReceiveLane(t *testing.T) {
	cfg := smux.DefaultConfig()
	cfg.Version, cfg.MaxReceiveBuffer, cfg.MaxStreamBuffer, cfg.MaxFrameSize = 2, 4096, 4096, 1024
	blocked, server := openingCarrierConn(t, 2101, nil, cfg)
	first, remoteFirst := holdOpeningCarrier(t, blocked, server)
	second, remoteSecond := holdOpeningCarrier(t, blocked, server)
	// An assertion may intentionally fail while recvLoop is paused. Release
	// the carrier first so per-stream cleanup cannot wait behind that pause.
	t.Cleanup(func() { blocked.conn.Load().Close(); server.Close() })
	payload := bytes.Repeat([]byte{0x61}, 4096)
	if _, err := remoteFirst.Write(payload); err != nil {
		t.Fatal(err)
	}
	extra := bytes.Repeat([]byte{0x72}, 1024)
	done := make(chan error, 1)
	go func() { _, err := remoteSecond.Write(extra); done <- err }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, _, full := blocked.conn.Load().Session.ReceiveBufferStats()
		if full {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not fill the shared receive budget")
		}
		time.Sleep(time.Millisecond)
	}
	c := &controller{slot: blocked}
	s := kcplib.TransportStats{RemoteWindow: 4096, MSS: 1326}
	c.cachePressure(blocked.conn.Load(), s, time.Now())
	if blocked.score.Load()&blockedCarrier == 0 {
		t.Fatal("sample did not publish real receive backpressure")
	}
	healthy, healthyServer := openingCarrier(t, 2103)
	healthy.score.Store(busyCarrier + 20)
	e := reloadFixture(t)
	p := &peer{engine: e, slots: []*slot{blocked, healthy}}
	for range 20 {
		if got := p.bestSlotLocked(nil); got != healthy {
			t.Fatal("new opening chose a full receive lane over a healthy busy sibling")
		}
	}
	accepted := make(chan error, 1)
	go func() {
		stream, err := healthyServer.AcceptStream()
		if err != nil {
			accepted <- err
			return
		}
		defer stream.Close()
		var request protocol.Proto
		if err = request.Read(stream); err == nil {
			err = writeOpeningAck(&kcp.Strm{Stream: stream}, 0)
		}
		accepted <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	opened, err := p.open(ctx, protocol.PTCP2, "127.0.0.1:2096")
	if err != nil {
		t.Fatalf("healthy sibling could not open beside a full receive lane: %v", err)
	}
	opened.Close()
	if err := <-accepted; err != nil || e.stats.OpenRetries.Load() != 0 {
		t.Fatalf("opening required a retry or failed its receipt: %v", err)
	}
	first.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(first, got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("held payload changed: %v", err)
	}
	second.SetReadDeadline(time.Now().Add(2 * time.Second))
	got = make([]byte, len(extra))
	if _, err := io.ReadFull(second, got); err != nil || !bytes.Equal(got, extra) {
		t.Fatalf("second held payload changed: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("held writer did not resume")
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		_, _, full := blocked.conn.Load().Session.ReceiveBufferStats()
		if !full {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("receive backpressure did not clear after drain")
		}
		time.Sleep(time.Millisecond)
	}
	c.cachePressure(blocked.conn.Load(), s, time.Now())
	if blocked.score.Load()&blockedCarrier != 0 || p.bestSlotLocked(nil) != blocked {
		t.Fatal("cleared lane did not return to ordinary pressure scoring")
	}
	if blocked.conn.Load().Session.IsClosed() {
		t.Fatal("capacity pressure closed the established carrier")
	}
}

// TestAdmissionBackpressureFallback retains health precedence, exclusions and a
// usable choice when every lane is blocked. Remote zero-window pause is sampled
// independently of local receiver fullness and is not a migration request.
func TestAdmissionBackpressureFallback(t *testing.T) {
	paused, _ := openingCarrier(t, 2102)
	c := &controller{slot: paused}
	c.cachePressure(paused.conn.Load(), kcplib.TransportStats{MSS: 1326}, time.Now())
	if paused.score.Load()&blockedCarrier == 0 || paused.score.Load() < busyCarrier {
		t.Fatal("remote receive-window pause was not published as capacity pressure")
	}
	other := &slot{}
	other.score.Store(busyCarrier | blockedCarrier | 10)
	p := &peer{slots: []*slot{paused, other}}
	if p.bestSlotLocked(nil) != paused {
		t.Fatal("all-blocked fallback lost original pressure ordering")
	}
	if p.bestSlotLocked(map[*slot]bool{paused: true}) != other {
		t.Fatal("opening exclusions were ignored")
	}
	other.score.Store(busyCarrier + 10)
	other.suspect.Store(true)
	if p.bestSlotLocked(nil) != paused {
		t.Fatal("capacity hint overrode transport health precedence")
	}
	other.suspect.Store(false)
	if p.bestSlotLocked(nil) != other {
		t.Fatal("unblocked healthy sibling did not receive the opening")
	}
	if _, err := (&peer{slots: []*slot{paused}, closed: true}).selectSlot(context.Background()); err == nil {
		t.Fatal("capacity fallback reopened a closed pool")
	}
}
