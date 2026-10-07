//go:build linux

// File carrier_recovery_test.go: time-controlled delivery/failure tests and
// generation-race checks. Real sockets and rule ownership are checked in netns.
package engine

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"paqet/internal/conf"
	"paqet/internal/tnet/kcp"
)

// TestCarrierHealthDirectionalStall checks an established sender independently
// of sibling/inbound progress, idle traffic and legitimate receiver backpressure.
func TestCarrierHealthDirectionalStall(t *testing.T) {
	cfg, now := recoveryConfig(t), time.Now()
	c := new(kcp.Conn)
	h := carrierHealth{}
	h.observeCarrier(now, 0, time.Time{}, c, carrierProgress{acked: 100}, 1024, 1, 100)
	h.observeCarrier(now.Add(14*time.Second), 0, time.Time{}, c, carrierProgress{acked: 100, received: 200}, 1024, 1, 100)
	if h.ready(now.Add(14*time.Second), 0, cfg, true) {
		t.Fatal("brief delivery gap recovered")
	}
	if !h.ready(now.Add(15*time.Second), 0, cfg, true) {
		t.Fatal("inbound traffic hid outbound stall")
	}
	h.observeCarrier(now.Add(16*time.Second), 0, time.Time{}, c, carrierProgress{acked: 101}, 1024, 1, 100)
	if h.ready(now.Add(17*time.Second), 0, cfg, true) {
		t.Fatal("ACK progress did not clear stall")
	}
	for _, scenario := range []string{"idle", "no-streams", "zero-window"} {
		t.Run(scenario, func(t *testing.T) {
			h := carrierHealth{}
			pending, streams, window := uint64(1024), 1, 100
			switch scenario {
			case "idle":
				pending = 0
			case "no-streams":
				streams = 0
			case "zero-window":
				window = 0
			}
			h.observeCarrier(now, 0, time.Time{}, c, carrierProgress{}, pending, streams, window)
			if h.ready(now.Add(time.Minute), 0, cfg, true) {
				t.Fatal("non-outage became eligible")
			}
		})
	}
	h.lastAttempt = now.Add(16 * time.Second)
	if h.ready(now.Add(30*time.Second), 0, cfg, true) {
		t.Fatal("15-second cooldown ignored")
	}
	if !h.ready(now.Add(31*time.Second), 0, cfg, true) {
		t.Fatal("cooldown did not expire")
	}
}

// carrierRecoveryFixture supplies independent slots without raw socket creation.
// Every candidate still exercises the real ownership/commit implementation.
func carrierRecoveryFixture(t *testing.T) (*Engine, *peer, *slot) {
	t.Helper()
	e, old, _ := blockedRecoveryFixture(t)
	next := fixtureConfig(e.current())
	ep := next.Peers["broken"]
	ep.SharedSource, ep.Sessions, ep.MaxSessions = false, 4, 4
	next.Peers["broken"] = ep
	original := e.resourceFactory
	e.resourceFactory = func(spec resourceSpec) (*liveResource, error) {
		r, err := original(spec)
		if err == nil && spec.kind == "peer" {
			for i := 0; i < spec.endpoint.Sessions; i++ {
				r.peer.slots = append(r.peer.slots, &slot{network: conf.Network{Port: 40000 + i, IPv4: conf.Addr{Addr: &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000 + i}}}})
			}
		}
		return r, err
	}
	if err := e.apply(next); err != nil {
		t.Fatal(err)
	}
	if e.resources["peer/broken"] == old {
		t.Fatal("fixture did not replace source contract")
	}
	p := e.resources["peer/broken"].peer
	s := p.slots[0]
	s.recoveryFailures.Store(3)
	s.recoveryHealth.lastProgress = time.Now().Add(-time.Minute)
	s.recoveryHealth.lastAttempt = time.Now()
	return e, p, s
}

// TestCarrierRecoveryPreservesSiblingsAndReload verifies only the failed slot
// changes; effective source reservations survive an identical config reload.
func TestCarrierRecoveryPreservesSiblingsAndReload(t *testing.T) {
	e, p, old := carrierRecoveryFixture(t)
	siblings := append([]*slot(nil), p.slots[1:]...)
	bind := e.resources[forwardKey(e.current().Forwards[0])]
	e.pathProbe = func(context.Context, *peer) error { return nil }
	e.recoverCarrier("broken", p, 0, old)
	if p.slots[0] == old || !old.retired.Load() || e.resources["peer/broken"].peer != p || e.resources[forwardKey(e.current().Forwards[0])] != bind {
		t.Fatal("incorrect carrier-scoped commit")
	}
	for i, s := range siblings {
		if p.slots[i+1] != s {
			t.Fatal("sibling replaced")
		}
	}
	if e.pathRecoverySucceeded.Load() != 1 || len(p.slots) != 4 {
		t.Fatal("incorrect recovery count or pool bound")
	}
	if _, err := p.connection(context.Background(), old); !errors.Is(err, net.ErrClosed) {
		t.Fatal("retired tuple could reopen", err)
	}
	newSlot := p.slots[0]
	if err := e.apply(fixtureConfig(e.current())); err != nil {
		t.Fatal(err)
	}
	if p.slots[0] != newSlot {
		t.Fatal("reload discarded effective tuple")
	}
}

// TestCarrierRecoveryRejectsObsoleteCandidate checks whole-path loss, resumed
// progress and both live and replacing configuration edits during a probe.
func TestCarrierRecoveryRejectsObsoleteCandidate(t *testing.T) {
	for _, scenario := range []string{"failed", "progress", "live-reliability", "reload", "remove", "cancel", "slot-changed"} {
		t.Run(scenario, func(t *testing.T) {
			e, p, old := carrierRecoveryFixture(t)
			var candidate *peer
			e.pathProbe = func(ctx context.Context, staged *peer) error {
				candidate = staged
				switch scenario {
				case "failed":
					return errors.New("no round-trip")
				case "progress":
					old.recoverySuccess.Store(time.Now().UnixNano())
				case "cancel":
					e.cancel()
				case "slot-changed":
					p.mu.Lock()
					p.slots[0] = &slot{}
					p.mu.Unlock()
				default:
					next := fixtureConfig(e.current())
					ep := next.Peers["broken"]
					switch scenario {
					case "live-reliability":
						ep.KCP.Interval = 40
					case "reload":
						ep.Address = "192.0.2.3:29999"
					case "remove":
						delete(next.Peers, "broken")
						next.Forwards = nil
					}
					if scenario != "remove" {
						next.Peers["broken"] = ep
					}
					if err := e.apply(next); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}
			e.recoverCarrier("broken", p, 0, old)
			if candidate == nil || !candidate.closed || e.pathRecoverySucceeded.Load() != 0 {
				t.Fatal("stale candidate committed or leaked")
			}
			if (scenario == "failed" || scenario == "progress") && p.slots[0] != old {
				t.Fatal("old tuple was disrupted")
			}
		})
	}
}

// TestCarrierAdmissionPrefersHealthy retains a fallback after a whole-peer
// outage, while avoiding suspect carriers whenever a healthy sibling exists.
func TestCarrierAdmissionPrefersHealthy(t *testing.T) {
	bad, good := &slot{}, &slot{}
	bad.suspect.Store(true)
	good.score.Store(busyCarrier + 10)
	p := &peer{slots: []*slot{bad, good}}
	for range 20 {
		if p.bestSlotLocked(nil) != good {
			t.Fatal("suspect tuple selected ahead of healthy sibling")
		}
	}
	good.suspect.Store(true)
	if p.bestSlotLocked(nil) == nil {
		t.Fatal("all-suspect fallback unavailable")
	}
}
