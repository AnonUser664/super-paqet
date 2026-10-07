//go:build linux

// File carrier_recovery.go: verifies and replaces one outgoing source tuple at
// a time. Sibling carriers, route bindings and listener generations stay alive.
package engine

import (
	"context"
	"time"

	"paqet/internal/protocol"
	"paqet/internal/tnet/kcp"
)

// carrierHealth extends opening-failure evidence with a directional delivery
// clock. Inbound traffic cannot hide outbound data whose ACKs have stopped.
type carrierHealth struct {
	pathHealth
	pendingSince  time.Time
	lastConn      *kcp.Conn
	lastAcked     uint64
	remoteBlocked bool
}

// observeCarrier counts actual delivery, not attempted sends. A closed/recreated
// conversation retains opening-failure evidence on the same physical tuple.
// Remote zero-window flow control is never classified as a path outage.
func (h *carrierHealth) observeCarrier(now time.Time, failures uint64, success time.Time, conn *kcp.Conn, sample carrierProgress, pending uint64, streams, remoteWindow int) {
	current := map[*kcp.Conn]carrierProgress{}
	if conn != nil {
		// An inbound-only half-path can still deliver keepalives while every
		// request sent by this client disappears. Only ACKed outbound data or
		// a completed opening clears its failed-opening evidence.
		current[conn] = carrierProgress{acked: sample.acked}
	}
	h.observe(now, failures, success, current)
	// A full receive window can also expire new stream openings. Preserve
	// their evidence, but never hop while the live receiver requests a pause.
	h.remoteBlocked = conn != nil && remoteWindow == 0
	if conn == nil || conn != h.lastConn || sample.acked > h.lastAcked || pending == 0 || streams == 0 || remoteWindow == 0 {
		h.pendingSince = time.Time{}
	}
	if conn != nil && pending > 0 && streams > 0 && remoteWindow > 0 && h.pendingSince.IsZero() {
		h.pendingSince = now
	}
	h.lastConn, h.lastAcked = conn, sample.acked
}

// ready allows either repeated failed openings or sustained unacknowledged
// established traffic. The cooldown applies independently to each source port.
func (h *carrierHealth) ready(now time.Time, failures uint64, cfg PathRecoveryConfig, cooldown bool) bool {
	if !cfg.Enabled || h.remoteBlocked || (cooldown && !h.lastAttempt.IsZero() && now.Sub(h.lastAttempt) < cfg.retryInterval) {
		return false
	}
	return (failures >= h.failuresAtProgress+3 && now.Sub(h.lastProgress) >= cfg.stalledAfter) ||
		(!h.pendingSince.IsZero() && now.Sub(h.pendingSince) >= cfg.stalledAfter)
}

// recordTransportFailure is constant-time on the failed-opening path. Actual
// ACK/data progress clears suspicion at the next bounded health sample.
func (s *slot) recordTransportFailure() {
	s.recoveryFailures.Add(1)
}

// carrierRecoveryAllowed samples only this slot; a healthy sibling cannot mask
// its stalled tuple. No per-customer scan or per-packet callback is required.
func (s *slot) carrierRecoveryAllowed(now time.Time, cfg PathRecoveryConfig, claim, cooldown bool) bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	c := s.conn.Load()
	var sample carrierProgress
	var pending uint64
	streams, remoteWindow := 0, 0
	if c != nil && !c.Session.IsClosed() {
		stats := c.UDPSession.TransportStats()
		sample = carrierProgress{stats.AckedBytes, stats.ReceivedBytes}
		pending, streams, remoteWindow = stats.PendingBytes, c.Session.NumStreams(), stats.RemoteWindow
	} else {
		c = nil
	}
	failures := s.recoveryFailures.Load()
	h := &s.recoveryHealth
	h.observeCarrier(now, failures, time.Unix(0, s.recoverySuccess.Load()), c, sample, pending, streams, remoteWindow)
	// Avoid repeatedly assigning new opens to a proven failing tuple, while
	// keeping all-suspect pools usable if the original path resumes.
	s.suspect.Store(!h.remoteBlocked && (failures >= h.failuresAtProgress+3 || (!h.pendingSince.IsZero() && now.Sub(h.pendingSince) >= cfg.stalledAfter)))
	ready := h.ready(now, failures, cfg, cooldown)
	if ready && claim {
		h.lastAttempt = now
	}
	return ready
}

// checkCarrierRecovery snapshots pool membership before bounded probes. Each
// slot has one candidate at most, and the engine permits four globally.
func (e *Engine) checkCarrierRecovery(now time.Time, name string, p *peer) {
	p.mu.RLock()
	slots := append([]*slot(nil), p.slots...)
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return
	}
	for index, s := range slots {
		cfg := p.configuration().PathRecovery
		e.observeMigration(now, name, index, s, cfg)
		if !s.carrierRecoveryAllowed(now, cfg, false, true) {
			continue
		}
		select {
		case e.recoverySlots <- struct{}{}:
		default:
			continue
		}
		if !s.recoveryPending.CompareAndSwap(false, true) {
			<-e.recoverySlots
			continue
		}
		if !s.carrierRecoveryAllowed(now, cfg, true, true) {
			s.recoveryPending.Store(false)
			<-e.recoverySlots
			continue
		}
		e.launch(func() {
			defer func() { s.recoveryPending.Store(false); <-e.recoverySlots }()
			e.recoverCarrier(name, p, index, s)
		})
	}
}

// recoverCarrier stages a single unpublished lane using ordinary resource
// ownership, then transfers it only after a protocol round trip and final
// generation/configuration checks. Failed probes never tear down old streams.
func (e *Engine) recoverCarrier(name string, p *peer, index int, oldSlot *slot) {
	key := "peer/" + name
	e.reloadMu.Lock()
	old := e.resources[key]
	p.mu.RLock()
	valid := !p.closed && index < len(p.slots) && p.slots[index] == oldSlot
	p.mu.RUnlock()
	if e.ctx.Err() != nil || old == nil || old.peer != p || !valid {
		e.reloadMu.Unlock()
		return
	}
	contract := old.spec
	spec := freshSourceSpec(contract)
	spec.endpoint.Sessions, spec.endpoint.MaxSessions = 1, 1
	candidate, err := e.buildResource(spec)
	if err != nil {
		e.reloadMu.Unlock()
		e.log().Warn("path.recovery_prepare_failed", "peer", name, "session", index, "error", err)
		return
	}
	e.reloadMu.Unlock()
	e.pathRecoveryAttempts.Add(1)
	e.log().Warn("path.recovery_probe", "peer", name, "session", index, "old_source_port", oldSlot.network.Port, "remote", contract.endpoint.Address)
	ctx, cancel := context.WithTimeout(p.lifecycle(), contract.endpoint.PathRecovery.probeTimeout)
	probe := e.pathProbe
	if probe == nil {
		probe = probePeer
	}
	err = probe(ctx, candidate.peer)
	// Check remote support before moving anything. Declined/older/dead sessions
	// keep the existing verified replacement behavior.
	oldConn := oldSlot.conn.Load()
	var migration *kcp.MigrationCapability
	var control *kcp.Conn
	if err == nil && contract.endpoint.PathRecovery.PreserveConnections && oldConn != nil && !oldConn.Session.IsClosed() {
		cap := oldConn.Migration.Load()
		control = candidate.peer.slots[0].conn.Load()
		if cap != nil && control != nil && cap.Epoch != ^uint64(0) {
			request := protocol.Proto{Type: protocol.PMCHECK, Capability: cap.Token, Epoch: cap.Epoch + 1}
			if _, checkErr := migrationRPC(ctx, control, request); checkErr == nil {
				migration = &kcp.MigrationCapability{Token: cap.Token, Epoch: cap.Epoch + 1}
			} else {
				e.log().Debug("path.migration_fallback", "peer", name, "session", index, "error", checkErr)
			}
		}
	}
	cancel()
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	discard := func() {
		if cleanupErr := candidate.close(); cleanupErr != nil {
			e.retired = append(e.retired, candidate)
			e.log().Error("path.recovery_cleanup_failed", "peer", name, "session", index, "error", cleanupErr)
		}
		e.pathRecoveryRejected.Add(1)
	}
	if err != nil {
		discard()
		e.log().Warn("path.recovery_probe_failed", "peer", name, "session", index, "error", err)
		return
	}
	p.mu.RLock()
	valid = !p.closed && index < len(p.slots) && p.slots[index] == oldSlot
	p.mu.RUnlock()
	if e.ctx.Err() != nil || e.resources[key] != old || !sameResource(old.spec, contract) || !valid || !oldSlot.carrierRecoveryAllowed(time.Now(), contract.endpoint.PathRecovery, false, false) {
		discard()
		e.log().Debug("path.recovery_discarded", "peer", name, "session", index, "reason", "old carrier progressed or configuration changed")
		return
	}
	// The candidate is unpublished, with exactly one slot. Transfer its journal
	// and guard into the original pool without canceling that pool's relays.
	newSlot := candidate.peer.slots[0]
	preserved := false
	oldSlot.mu.Lock()
	if migration != nil && oldSlot.conn.Load() == oldConn && !oldConn.Session.IsClosed() && newSlot.transport != nil {
		if moveErr := newSlot.transport.Adopt(oldConn); moveErr == nil {
			oldSlot.retired.Store(true)
			oldSlot.conn.Store(nil)
			newSlot.conn.Store(oldConn)
			oldConn.Migration.Store(migration)
			stats := oldConn.UDPSession.TransportStats()
			newSlot.migrationWatch = migrationObservation{at: time.Now(), acked: stats.AckedBytes, received: stats.ReceivedBytes, oldPort: oldSlot.network.Port, conn: oldConn, streams: oldConn.Session.NumStreams()}
			preserved = true
		} else {
			e.log().Warn("path.migration_fallback", "peer", name, "session", index, "error", moveErr)
		}
	}
	oldSlot.mu.Unlock()
	p.allocationMu.Lock()
	candidate.peer.allocationMu.Lock()
	for _, rules := range candidate.peer.slotRules {
		p.slotRules = append(p.slotRules, rules)
	}
	candidate.peer.slotRules = nil
	for i, rules := range p.slotRules {
		if rules == oldSlot.fw {
			p.slotRules = append(p.slotRules[:i], p.slotRules[i+1:]...)
			break
		}
	}
	candidate.peer.allocationMu.Unlock()
	oldSlot.mu.Lock()
	p.mu.Lock()
	oldSlot.retired.Store(true)
	p.slots[index] = newSlot
	p.mu.Unlock()
	oldSlot.mu.Unlock()
	p.allocationMu.Unlock()
	candidate.peer.mu.Lock()
	candidate.peer.slots = nil
	candidate.peer.mu.Unlock()
	// Retarget the candidate's adaptive/live reliability registration to the
	// lasting peer template rather than its one-slot staging configuration.
	if c := newSlot.conn.Load(); c != nil {
		if preserved {
			// Retain learned RTT/window/pacing state instead of rebuilding a controller.
			e.tuneMu.Lock()
			if tuner := e.tuners[c]; tuner != nil {
				tuner.slot = newSlot
				tuner.endpoint = p.settings
			}
			e.tuneMu.Unlock()
			request := protocol.Proto{Type: protocol.PMMOVE, Capability: migration.Token, Epoch: migration.Epoch}
			e.launch(func() { e.finishMigration(p, name, c, control, request) })
		} else {
			e.addEndpoint(c, p.settings, newSlot)
		}
	}
	candidate.close()
	retired := &liveResource{fw: oldSlot.fw, release: func() {
		oldSlot.mu.Lock()
		defer oldSlot.mu.Unlock()
		if c := oldSlot.conn.Swap(nil); c != nil {
			c.Close()
		}
		if oldSlot.transport != nil {
			oldSlot.transport.Close()
		}
	}, finalize: func() {
		if oldSlot.guard != nil {
			oldSlot.guard.Close()
		}
	}}
	if cleanupErr := retired.close(); cleanupErr != nil {
		e.retired = append(e.retired, retired)
		e.log().Error("path.recovery_cleanup_failed", "peer", name, "session", index, "error", cleanupErr)
	}
	e.pathRecoverySucceeded.Add(1)
	e.log().Warn("path.recovered", "peer", name, "session", index, "old_source_port", oldSlot.network.Port, "new_source_port", newSlot.network.Port, "local", newSlot.network.IPv4.Addr, "connections_preserved", preserved, "reason", "fresh carrier tuple verified after transport stalled")
}
