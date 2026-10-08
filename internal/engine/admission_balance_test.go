//go:build linux

// File admission_balance_test.go checks actual mux membership against stale
// cached scores, including burst opens, closes and health/bulk precedence.
package engine

import (
	"io"
	"net"
	"testing"

	"github.com/xtaci/smux"
	"paqet/internal/tnet/kcp"
)

// admissionSlot creates a real mux with a draining peer. No timer/controller
// updates its cached score, making stale population errors deterministic.
func admissionSlot(t *testing.T) *slot {
	t.Helper()
	left, right := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); io.Copy(io.Discard, right) }()
	cfg := smux.DefaultConfig()
	cfg.KeepAliveDisabled = true
	session, err := smux.Client(left, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close(); right.Close(); <-done })
	s := &slot{}
	s.conn.Store(&kcp.Conn{Session: session})
	return s
}

// TestAdmissionBurstUsesLivePopulation keeps old scores unequal during a
// complete burst. Every selected lane must have the current minimum population,
// including changes from local abort; a controller never repairs the scores.
func TestAdmissionBurstUsesLivePopulation(t *testing.T) {
	p := &peer{}
	for i := 0; i < 4; i++ {
		s := admissionSlot(t)
		s.score.Store(uint64(i * 100))
		p.slots = append(p.slots, s)
	}
	for i := 0; i < 128; i++ {
		best := p.bestSlotLocked(nil)
		for _, s := range p.slots {
			if best.conn.Load().Session.NumStreams() > s.conn.Load().Session.NumStreams() {
				t.Fatalf("opening %d chose a busier lane from stale scores", i)
			}
		}
		stream, err := best.conn.Load().Session.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		if i%7 == 0 {
			stream.Abort()
		}
	}
	for _, s := range p.slots {
		if s.conn.Load().Session.NumStreams() != s.conn.Load().Session.NumStreamsSnapshot() {
			t.Fatal("population publication differs from mux ownership")
		}
	}
}

// TestAdmissionLiveCountRetainsHealthAndBulkOrdering prevents the faster gauge
// from overriding suspect-path avoidance, tied bulk pressure or retry exclusions.
func TestAdmissionLiveCountRetainsHealthAndBulkOrdering(t *testing.T) {
	idle, hot := admissionSlot(t), admissionSlot(t)
	hot.score.Store(busyCarrier)
	p := &peer{slots: []*slot{idle, hot}}
	if p.bestSlotLocked(nil) != idle {
		t.Fatal("bulk pressure no longer preferred idle capacity")
	}
	idle.suspect.Store(true)
	if p.bestSlotLocked(nil) != hot {
		t.Fatal("live counts overrode path health")
	}
	if p.bestSlotLocked(map[*slot]bool{hot: true}) != idle {
		t.Fatal("retry exclusion ignored")
	}
	if p.bestSlotLocked(map[*slot]bool{hot: true, idle: true}) != nil {
		t.Fatal("all-excluded selection resurrected a lane")
	}
}

// TestAdmissionBulkHysteresisCannotStrandLanes reproduces a warm verification
// and control-only lane before a burst. Busy bits remain set for the whole test;
// no controller tick can rescue a worker excluded by an absolute hot penalty.
func TestAdmissionBulkHysteresisCannotStrandLanes(t *testing.T) {
	p := &peer{}
	for i := 0; i < 4; i++ {
		s := admissionSlot(t)
		if i < 2 {
			s.score.Store(busyCarrier)
		}
		p.slots = append(p.slots, s)
	}
	for i := 0; i < 64; i++ {
		s := p.bestSlotLocked(nil)
		if _, err := s.conn.Load().Session.OpenStream(); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range p.slots {
		if s.conn.Load().Session.NumStreams() != 16 {
			t.Fatal("cached bulk hysteresis concentrated the burst", s.conn.Load().Session.NumStreams())
		}
	}
}
