//go:build linux

// File path_recovery.go: verifies a new raw source tuple before replacing a
// peer whose carriers have stopped progressing. A lost whole IP path cannot be
// repaired this way; failed probes leave the existing peer and routes intact.
package engine

import (
	"context"
	"fmt"
	"net"
	"time"

	"paqet/internal/protocol"
	"paqet/internal/tnet/kcp"
)

// PathRecoveryConfig is opt-in because some deployments require fixed source
// ports. Timings bound outage detection and probe traffic, not KCP retransmission.
type PathRecoveryConfig struct {
	// PreserveConnections opts into negotiated physical migration of live sessions.
	PreserveConnections                       bool   `yaml:"preserve_connections"`
	Enabled                                   bool   `yaml:"enabled"`
	StalledAfter                              string `yaml:"stalled_after"`
	RetryInterval                             string `yaml:"retry_interval"`
	ProbeTimeout                              string `yaml:"probe_timeout"`
	stalledAfter, retryInterval, probeTimeout time.Duration
}

// prepare validates explicit budgets even when disabled, catching latent typos.
func (r *PathRecoveryConfig) prepare(listener, shared bool) error {
	if r.PreserveConnections && (!r.Enabled || shared || listener) {
		return fmt.Errorf("path_recovery.preserve_connections requires enabled recovery and independent outgoing source ports")
	}
	if r.Enabled && listener {
		return fmt.Errorf("path_recovery requires an outgoing peer")
	}
	fields := []struct {
		name     string
		value    *string
		duration *time.Duration
		fallback string
		min, max time.Duration
	}{
		{"stalled_after", &r.StalledAfter, &r.stalledAfter, "15s", 15 * time.Second, 10 * time.Minute},
		{"retry_interval", &r.RetryInterval, &r.retryInterval, "15s", 10 * time.Second, 10 * time.Minute},
		{"probe_timeout", &r.ProbeTimeout, &r.probeTimeout, "5s", time.Second, time.Minute},
	}
	for _, f := range fields {
		if *f.value == "" {
			*f.value = f.fallback
		}
		d, err := time.ParseDuration(*f.value)
		if err != nil || d < f.min || d > f.max {
			return fmt.Errorf("path_recovery.%s must be %s..%s", f.name, f.min, f.max)
		}
		*f.duration = d
	}
	return nil
}

// carrierProgress distinguishes delivered bytes from outgoing ACK attempts.
// Keys are generation pointers, so retiring a carrier cannot manufacture a
// negative counter delta or reset the peer's no-progress clock.
type carrierProgress struct{ acked, received uint64 }
type pathHealth struct {
	lastProgress, lastAttempt time.Time
	failuresAtProgress        uint64
	carriers                  map[*kcp.Conn]carrierProgress
}

// observe resets failure evidence on any received data, acknowledged data or
// successful opening. Keepalive ACKs also protect genuinely healthy idle paths.
func (h *pathHealth) observe(now time.Time, failures uint64, success time.Time, current map[*kcp.Conn]carrierProgress) {
	progressing := success.After(h.lastProgress)
	for conn, v := range current {
		old := h.carriers[conn]
		if v.acked > old.acked || v.received > old.received {
			progressing = true
		}
	}
	if h.lastProgress.IsZero() || progressing {
		h.lastProgress = now
		h.failuresAtProgress = failures
	}
	h.carriers = current
}

// eligible excludes idle, progressing and recently probed peers. Three failed
// transport openings are required in addition to the no-progress duration.
func (h *pathHealth) eligible(now time.Time, failures uint64, cfg PathRecoveryConfig) bool {
	return cfg.Enabled && failures >= h.failuresAtProgress+3 &&
		now.Sub(h.lastProgress) >= cfg.stalledAfter &&
		(h.lastAttempt.IsZero() || now.Sub(h.lastAttempt) >= cfg.retryInterval)
}

// recoveryCheck samples only the bounded live carrier pool; application stream
// count is irrelevant and no per-stream scan or packet-path callback is added.
func (p *peer) recoveryCheck(now time.Time, claim bool) bool {
	return p.recoveryAllowed(now, claim, true)
}

// recoveryAllowed can ignore the cooldown only when committing the probe that
// already claimed it. Progress/failure evidence remains mandatory at commit.
func (p *peer) recoveryAllowed(now time.Time, claim, cooldown bool) bool {
	p.recoveryMu.Lock()
	defer p.recoveryMu.Unlock()
	p.mu.RLock()
	current := make(map[*kcp.Conn]carrierProgress, len(p.slots))
	for _, s := range p.slots {
		if c := s.conn.Load(); c != nil && !c.Session.IsClosed() {
			stats := c.UDPSession.TransportStats()
			current[c] = carrierProgress{stats.AckedBytes, stats.ReceivedBytes}
		}
	}
	closed := p.closed
	p.mu.RUnlock()
	failures := p.recoveryFailures.Load()
	p.recoveryHealth.observe(now, failures, time.Unix(0, p.recoverySuccess.Load()), current)
	cfg := p.configuration().PathRecovery
	if !cooldown {
		cfg.retryInterval = 0
	}
	ready := !closed && p.recoveryHealth.eligible(now, failures, cfg)
	if ready && claim {
		p.recoveryHealth.lastAttempt = now
	}
	return ready
}

// recoverPaths limits concurrency globally and per peer. Probes run outside the
// configuration transaction lock so a slow remote never blocks config reload.
func (e *Engine) recoverPaths() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case now := <-ticker.C:
			view := e.view.Load()
			if view == nil {
				continue
			}
			for name, p := range view.peers {
				if p.configuration().PathRecovery.Enabled && !p.configuration().SharedSource {
					e.checkCarrierRecovery(now, name, p)
					continue
				}
				if !p.configuration().PathRecovery.Enabled || !p.recoveryCheck(now, false) {
					continue
				}
				select {
				case e.recoverySlots <- struct{}{}:
				default:
					continue
				}
				if !p.recoveryPending.CompareAndSwap(false, true) {
					<-e.recoverySlots
					continue
				}
				if !p.recoveryCheck(now, true) {
					p.recoveryPending.Store(false)
					<-e.recoverySlots
					continue
				}
				e.launch(func() {
					defer func() { p.recoveryPending.Store(false); <-e.recoverySlots }()
					e.recoverPeer(name, p)
				})
			}
		}
	}
}

// freshSourceSpec requests a reserved ephemeral source while preserving flags,
// address families, target listener, encryption and all reliability settings.
func freshSourceSpec(spec resourceSpec) resourceSpec {
	// Explicit initial ports are a startup contract, not a limit on verified
	// recovery. A candidate must reserve a port different from every live lane.
	spec.endpoint.SourcePorts = nil
	spec.endpoint.Network.Port = 0
	for _, a := range []*net.UDPAddr{spec.endpoint.Network.IPv4.Addr, spec.endpoint.Network.IPv6.Addr} {
		if a == nil {
			continue
		}
		copy := *a
		copy.Port = 0
		if a == spec.endpoint.Network.IPv4.Addr {
			spec.endpoint.Network.IPv4.Addr = &copy
			spec.endpoint.Network.IPv4.Addr_ = copy.String()
		} else {
			spec.endpoint.Network.IPv6.Addr = &copy
			spec.endpoint.Network.IPv6.Addr_ = copy.String()
		}
	}
	return spec
}

// probePeer proves received end-to-end protocol progress on an unpublished
// carrier. Local setup success is insufficient. Cancellation may close this
// carrier because it has never carried customer streams.
func probePeer(ctx context.Context, p *peer) error {
	c, err := p.connection(ctx, p.slots[0])
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	stream, err := c.OpenStrmContext(ctx)
	if err != nil {
		return err
	}
	defer abortOpening(stream)
	deadline, _ := ctx.Deadline()
	stream.SetDeadline(deadline)
	proto := protocol.Proto{Type: protocol.PPING}
	if err = proto.Write(stream); err != nil {
		return err
	}
	if err = proto.Read(stream); err != nil {
		return err
	}
	if proto.Type != protocol.PPONG {
		return fmt.Errorf("path probe received unexpected response")
	}
	return ctx.Err()
}

// recoverPeer stages a fresh tuple, verifies it, rechecks the old peer, then
// atomically replaces just its route bindings. Failed or stale candidates are
// fully closed; a progressing old carrier always wins over the candidate.
func (e *Engine) recoverPeer(name string, p *peer) {
	key := "peer/" + name
	e.reloadMu.Lock()
	old := e.resources[key]
	if e.ctx.Err() != nil || old == nil || old.peer != p {
		e.reloadMu.Unlock()
		return
	}
	candidate, err := e.buildResource(freshSourceSpec(old.spec))
	if err != nil {
		e.reloadMu.Unlock()
		e.log().Warn("path.recovery_prepare_failed", "peer", name, "error", err)
		return
	}
	// Keep the public contract for later identical reloads. Only the socket's
	// effective source is transient; restart still begins at the configured port.
	candidate.spec = old.spec
	candidate.settings.Store(&candidate.spec.endpoint)
	e.reloadMu.Unlock()
	e.pathRecoveryAttempts.Add(1)
	e.log().Warn("path.recovery_probe", "peer", name, "remote", p.configuration().Address)
	ctx, cancel := context.WithTimeout(p.lifecycle(), p.configuration().PathRecovery.probeTimeout)
	probe := e.pathProbe
	if probe == nil {
		probe = probePeer
	}
	err = probe(ctx, candidate.peer)
	cancel()
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	keep := func() {
		if cleanupErr := candidate.close(); cleanupErr != nil {
			e.retired = append(e.retired, candidate)
			e.log().Error("path.recovery_cleanup_failed", "peer", name, "error", cleanupErr)
		}
	}
	if err != nil {
		keep()
		e.pathRecoveryRejected.Add(1)
		e.log().Warn("path.recovery_probe_failed", "peer", name, "error", err)
		return
	}
	if e.ctx.Err() != nil || e.resources[key] != old || !sameResource(old.spec, candidate.spec) || !p.recoveryAllowed(time.Now(), false, false) {
		keep()
		e.pathRecoveryRejected.Add(1)
		e.log().Debug("path.recovery_discarded", "peer", name, "reason", "old peer progressed or configuration changed")
		return
	}
	e.resources[key] = candidate
	e.publish(e.current())
	candidate.start()
	if cleanupErr := old.close(); cleanupErr != nil {
		e.retired = append(e.retired, old)
		e.log().Error("path.recovery_cleanup_failed", "peer", name, "error", cleanupErr)
	}
	e.pathRecoverySucceeded.Add(1)
	local := ""
	if candidate.peer.shared != nil {
		if candidate.peer.sharedNetwork.IPv4.Addr != nil {
			local = candidate.peer.sharedNetwork.IPv4.Addr.String()
		} else {
			local = candidate.peer.sharedNetwork.IPv6.Addr.String()
		}
	}
	e.log().Warn("path.recovered", "peer", name, "remote", p.configuration().Address, "local", local, "reason", "fresh tuple verified after transport stalled")
}
