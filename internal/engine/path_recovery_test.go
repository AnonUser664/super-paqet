//go:build linux

// File path_recovery_test.go: deterministically verifies outage evidence,
// candidate ownership and configuration races independently of real filtering.
package engine

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"paqet/internal/conf"
	"paqet/internal/tnet/kcp"
)

// recoveryConfig supplies public validated budgets used by the time-controlled tests.
func recoveryConfig(t *testing.T) PathRecoveryConfig {
	t.Helper()
	r := PathRecoveryConfig{Enabled: true}
	if err := r.prepare(false, true); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestPathHealthRequiresFailuresAndNoProgress rejects healthy, idle and brief-loss paths.
func TestPathHealthRequiresFailuresAndNoProgress(t *testing.T) {
	cfg := recoveryConfig(t)
	start := time.Now()
	h := pathHealth{}
	conn := new(kcp.Conn)
	samples := map[*kcp.Conn]carrierProgress{conn: {acked: 100}}
	h.observe(start, 0, time.Time{}, samples)
	if h.eligible(start.Add(time.Minute), 0, cfg) {
		t.Fatal("idle peer was eligible")
	}
	if h.eligible(start.Add(14*time.Second), 3, cfg) {
		t.Fatal("brief outage was eligible")
	}
	if !h.eligible(start.Add(15*time.Second), 3, cfg) {
		t.Fatal("stalled peer was not eligible")
	}
	h.observe(start.Add(31*time.Second), 4, time.Time{}, map[*kcp.Conn]carrierProgress{conn: {acked: 101}})
	if h.eligible(start.Add(62*time.Second), 6, cfg) {
		t.Fatal("old failures survived real progress")
	}
	if !h.eligible(start.Add(62*time.Second), 7, cfg) {
		t.Fatal("new failures did not qualify")
	}
	h.lastAttempt = start.Add(62 * time.Second)
	if h.eligible(start.Add(63*time.Second), 8, cfg) {
		t.Fatal("probe cooldown ignored")
	}
	h.observe(start.Add(64*time.Second), 8, start.Add(63*time.Second), nil)
	if h.eligible(start.Add(100*time.Second), 8, cfg) {
		t.Fatal("successful opening did not reset evidence")
	}
}

// TestPathHealthRetiringCarrierDoesNotResetOutage exercises repeated unanswered
// conversation replacement on one physical tuple, the observed incident pattern.
func TestPathHealthRetiringCarrierDoesNotResetOutage(t *testing.T) {
	cfg := recoveryConfig(t)
	start := time.Now()
	h := pathHealth{}
	h.observe(start, 0, time.Time{}, map[*kcp.Conn]carrierProgress{new(kcp.Conn): {}})
	for i := 1; i <= 4; i++ {
		h.observe(start.Add(time.Duration(i)*10*time.Second), uint64(i), time.Time{}, map[*kcp.Conn]carrierProgress{new(kcp.Conn): {}})
	}
	if !h.eligible(start.Add(40*time.Second), 4, cfg) {
		t.Fatal("local recreation concealed blocked tuple")
	}
}

// blockedRecoveryFixture owns two independent pools and supplies no-progress
// evidence for one. The other pool and forward bind must retain their identity.
func blockedRecoveryFixture(t *testing.T) (*Engine, *liveResource, *liveResource) {
	t.Helper()
	e := reloadFixture(t)
	cfg := fixtureConfig(e.cfg)
	endpoint := Endpoint{Address: "192.0.2.1:29999", SharedSource: true, PathRecovery: recoveryConfig(t)}
	cfg.Peers = map[string]Endpoint{"broken": endpoint, "healthy": {Address: "192.0.2.2:29999"}}
	cfg.Forwards = []Forward{{Listen: "127.0.0.1:9001", Protocol: "tcp", Peer: "broken", Target: "127.0.0.1:80"}}
	if err := e.apply(cfg); err != nil {
		t.Fatal(err)
	}
	old := e.resources["peer/broken"]
	old.peer.recoveryFailures.Store(3)
	old.peer.recoveryHealth.lastProgress = time.Now().Add(-time.Minute)
	old.peer.recoveryHealth.lastAttempt = time.Now() // a claimed probe must be allowed to commit
	return e, old, e.resources["peer/healthy"]
}

// TestPathRecoveryCommitsOnlyVerifiedPeer preserves routes and the public spec
// through a transient effective-source change and an identical config reload.
func TestPathRecoveryCommitsOnlyVerifiedPeer(t *testing.T) {
	e, old, healthy := blockedRecoveryFixture(t)
	bind := e.resources[forwardKey(e.current().Forwards[0])]
	e.pathProbe = func(context.Context, *peer) error { return nil }
	e.recoverPeer("broken", old.peer)
	candidate := e.resources["peer/broken"]
	if candidate == old || !old.released || candidate.released || e.resources["peer/healthy"] != healthy || e.resources[forwardKey(e.current().Forwards[0])] != bind {
		t.Fatal("incorrect scoped commit")
	}
	if e.pathRecoverySucceeded.Load() != 1 || !sameResource(old.spec, candidate.spec) {
		t.Fatal("missing recovery or public contract changed")
	}
	if err := e.apply(fixtureConfig(e.current())); err != nil {
		t.Fatal(err)
	}
	if e.resources["peer/broken"] != candidate {
		t.Fatal("identical reload discarded recovered tuple")
	}
}

// TestPathRecoveryRejectsFailedOrObsoleteProbes covers real path loss, resumed
// traffic, config replacement, removal and cancellation while a probe is pending.
func TestPathRecoveryRejectsFailedOrObsoleteProbes(t *testing.T) {
	for _, scenario := range []string{"failed", "progress", "reload", "live-reliability", "remove", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			e, old, healthy := blockedRecoveryFixture(t)
			var staged *peer
			e.pathProbe = func(ctx context.Context, p *peer) error {
				staged = p
				switch scenario {
				case "failed":
					return errors.New("no PPONG")
				case "progress":
					old.peer.recoverySuccess.Store(time.Now().UnixNano())
				case "reload":
					next := fixtureConfig(e.current())
					ep := next.Peers["broken"]
					ep.Address = "192.0.2.3:29999"
					next.Peers["broken"] = ep
					if err := e.apply(next); err != nil {
						t.Fatal(err)
					}
				case "remove":
					next := fixtureConfig(e.current())
					delete(next.Peers, "broken")
					next.Forwards = nil
					if err := e.apply(next); err != nil {
						t.Fatal(err)
					}
				case "live-reliability":
					next := fixtureConfig(e.current())
					ep := next.Peers["broken"]
					ep.KCP.Interval = 40
					next.Peers["broken"] = ep
					if err := e.apply(next); err != nil {
						t.Fatal(err)
					}
					if e.resources["peer/broken"] != old {
						t.Fatal("fixture failed to exercise live reliability edit")
					}
				case "cancel":
					e.cancel()
				}
				return nil
			}
			e.recoverPeer("broken", old.peer)
			if staged == nil || !staged.closed || e.pathRecoverySucceeded.Load() != 0 || e.resources["peer/healthy"] != healthy {
				t.Fatal("candidate leaked or unrelated path changed")
			}
			if (scenario == "failed" || scenario == "progress") && (e.resources["peer/broken"] != old || old.released) {
				t.Fatal("unverified probe disrupted old peer")
			}
		})
	}
}

// TestFreshSourceSpecPreservesEnvelope verifies dual-stack clone ownership,
// flags and reliability survive requesting a different reserved source port.
func TestFreshSourceSpecPreservesEnvelope(t *testing.T) {
	ipv4 := &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 29997}
	ipv6 := &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 29997}
	spec := resourceSpec{kind: "peer", endpoint: Endpoint{Address: "192.0.2.20:29999", SharedSource: true, Network: conf.Network{Port: 29997, IPv4: conf.Addr{Addr: ipv4}, IPv6: conf.Addr{Addr: ipv6}}}}
	fresh := freshSourceSpec(spec)
	if fresh.endpoint.Network.Port != 0 || fresh.endpoint.Network.IPv4.Addr.Port != 0 || fresh.endpoint.Network.IPv6.Addr.Port != 0 || ipv4.Port != 29997 || ipv6.Port != 29997 {
		t.Fatal("source clone corrupted configured endpoint")
	}
	fresh.endpoint.Network = spec.endpoint.Network
	if !reflect.DeepEqual(fresh, spec) {
		t.Fatal("fresh source altered non-network contract")
	}
}

// TestPathRecoveryConfigValidation prevents unsupported listener/nonshared
// recovery and rejects unreasonable traffic or detection budgets.
func TestPathRecoveryConfigValidation(t *testing.T) {
	for _, r := range []PathRecoveryConfig{{Enabled: true, StalledAfter: "1s"}, {Enabled: true, RetryInterval: "0s"}, {Enabled: true, ProbeTimeout: "2m"}} {
		if r.prepare(false, true) == nil {
			t.Fatal("bad recovery budget accepted")
		}
	}
	r := PathRecoveryConfig{Enabled: true}
	if r.prepare(true, true) == nil {
		t.Fatal("unsupported source contract accepted")
	}
	if err := r.prepare(false, false); err != nil {
		t.Fatal(err)
	}
}
