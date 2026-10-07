//go:build linux

// File migration.go authenticates physical carrier moves using bounded inner
// controls. Tokens are bearer capabilities, not encryption or on-path protection.
package engine

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"time"

	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"paqet/internal/tnet/kcp"
)

// migrationRegistry retains one capability per accepted live carrier. No state
// is allocated per TCP customer, per packet, or for an unrecognized token.
type migrationRegistry struct {
	mu      sync.Mutex
	entries map[[32]byte]*migrationRecord
}

// migrationRecord scopes a capability to its listener generation and original
// peer IP. Epochs prevent delayed controls from reverting a completed move.
type migrationRecord struct {
	conn     *kcp.Conn
	listener tnet.Listener
	ip       string
	epoch    uint64
}

// peerIP compares actual transport endpoints rather than caller-supplied data.
func peerIP(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	return host
}

// forgetMigration removes dead session capabilities before accepted ownership
// is released. A new backend process deliberately cannot resurrect old sockets.
func (e *Engine) forgetMigration(c *kcp.Conn) {
	e.migrations.mu.Lock()
	defer e.migrations.mu.Unlock()
	if cap := c.Migration.Load(); cap != nil {
		delete(e.migrations.entries, cap.Token)
	}
}

// migrationControl serializes only infrequent control transactions. Ordinary
// KCP input still uses the existing per-worker conversation lookup.
func (e *Engine) migrationControl(listener tnet.Listener, via *kcp.Conn, request protocol.Proto) protocol.Proto {
	reply := protocol.Proto{Type: protocol.PMREPLY, Capability: request.Capability, Epoch: request.Epoch, Status: 1}
	if via == nil || via.Session.IsClosed() {
		return reply
	}
	e.migrations.mu.Lock()
	defer e.migrations.mu.Unlock()
	if via.Session.IsClosed() {
		return reply
	}
	if request.Type == protocol.PMTOKEN {
		// A self move is a harmless capability check on conversation listeners.
		if !via.UDPSession.CanMigrate(via.UDPSession) {
			return reply
		}
		cap := via.Migration.Load()
		if cap == nil {
			cap = &kcp.MigrationCapability{}
			if _, err := rand.Read(cap.Token[:]); err != nil {
				return reply
			}
			if e.migrations.entries == nil {
				e.migrations.entries = make(map[[32]byte]*migrationRecord)
			}
			// Never overwrite another carrier, even in the improbable random collision case.
			if e.migrations.entries[cap.Token] != nil {
				return reply
			}
			e.migrations.entries[cap.Token] = &migrationRecord{conn: via, listener: listener, ip: peerIP(via.RemoteAddr())}
			via.Migration.Store(cap)
		}
		reply.Capability, reply.Epoch, reply.Status = cap.Token, cap.Epoch, 0
		return reply
	}
	r := e.migrations.entries[request.Capability]
	if r == nil || r.listener != listener || r.ip == "" || peerIP(via.RemoteAddr()) != r.ip || r.conn == via || r.conn.Session.IsClosed() || request.Status != 0 || request.Epoch == 0 || !r.conn.UDPSession.CanMigrate(via.UDPSession) {
		return reply
	}
	// Repeated commits are safe only on the already committed tuple. An old
	// candidate cannot use the same epoch to move the session somewhere else.
	if request.Epoch == r.epoch {
		if r.conn.RemoteAddr().String() == via.RemoteAddr().String() {
			reply.Status = 0
		}
		return reply
	}
	// A later generation can supersede an earlier commit whose reply was lost.
	// Strict monotonicity still prevents delayed controls from undoing that move.
	if request.Epoch < r.epoch {
		return reply
	}
	if request.Type == protocol.PMCHECK {
		reply.Status = 0
		return reply
	}
	if request.Type != protocol.PMMOVE {
		return reply
	}
	oldAddr := r.conn.RemoteAddr()
	if err := r.conn.UDPSession.MigrateVia(via.UDPSession); err != nil {
		return reply
	}
	// The candidate has already negotiated return flags. Add original ownership
	// before retiring its old tuple; fixed PA remains PA after candidate closure.
	owner := r.conn.UDPSession.GetConv()
	listener.RegisterClient(via.RemoteAddr(), owner)
	listener.DeleteClientSession(oldAddr, owner)
	r.epoch = request.Epoch
	r.conn.Migration.Store(&kcp.MigrationCapability{Token: request.Capability, Epoch: r.epoch})
	r.conn.UDPSession.RetransmitNow()
	reply.Status = 0
	e.log().Debug("path.migration_accepted", "conv", owner, "epoch", r.epoch, "old_remote", oldAddr.String(), "new_remote", r.conn.RemoteAddr().String())
	return reply
}

// migrationRPC bounds a control stream without changing the live carrier's
// shared deadlines. A lost reply can be retried with the same token and epoch.
func migrationRPC(ctx context.Context, c *kcp.Conn, request protocol.Proto) (protocol.Proto, error) {
	var reply protocol.Proto
	stream, err := c.OpenStrmContext(ctx)
	if err != nil {
		return reply, err
	}
	defer abortOpening(stream)
	stop := context.AfterFunc(ctx, func() { abortOpening(stream) })
	defer stop()
	deadline, ok := ctx.Deadline()
	if !ok {
		return reply, fmt.Errorf("migration control requires a deadline")
	}
	stream.SetDeadline(deadline)
	if err = request.Write(stream); err != nil {
		return reply, err
	}
	if err = reply.Read(stream); err != nil {
		return reply, err
	}
	if reply.Type != protocol.PMREPLY || (request.Type != protocol.PMTOKEN && (reply.Capability != request.Capability || reply.Epoch != request.Epoch)) {
		return reply, fmt.Errorf("unexpected migration reply")
	}
	if reply.Status != 0 {
		return reply, fmt.Errorf("peer declined session migration")
	}
	return reply, ctx.Err()
}

// negotiateMigration runs after ordinary setup publication, avoiding an extra
// round trip on customer openings. Older backends simply retain replacement.
func (e *Engine) negotiateMigration(p *peer, c *kcp.Conn) {
	ctx, cancel := context.WithTimeout(p.lifecycle(), p.configuration().PathRecovery.probeTimeout)
	defer cancel()
	reply, err := migrationRPC(ctx, c, protocol.Proto{Type: protocol.PMTOKEN})
	if err != nil {
		e.log().Debug("path.migration_unavailable", "conv", c.UDPSession.GetConv(), "error", err)
		return
	}
	if reply.Capability == ([32]byte{}) || c.Session.IsClosed() {
		return
	}
	c.Migration.Store(&kcp.MigrationCapability{Token: reply.Capability, Epoch: reply.Epoch})
	e.log().Debug("path.migration_ready", "conv", c.UDPSession.GetConv())
}

// finishMigration retries an ambiguous commit without reverting or closing
// established streams. The normal health detector remains the bounded fallback
// if the new physical path also stops delivering or the backend lost state.
func (e *Engine) finishMigration(p *peer, name string, c, control *kcp.Conn, request protocol.Proto) {
	defer control.Close()
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(p.lifecycle(), p.configuration().PathRecovery.probeTimeout)
		_, err := migrationRPC(ctx, control, request)
		cancel()
		if err == nil {
			c.UDPSession.RetransmitNow()
			e.log().Debug("path.migration_confirmed", "peer", name, "conv", c.UDPSession.GetConv(), "epoch", request.Epoch)
			return
		}
		if p.lifecycle().Err() != nil || c.Session.IsClosed() {
			return
		}
		e.log().Debug("path.migration_commit_retry", "peer", name, "conv", c.UDPSession.GetConv(), "epoch", request.Epoch, "attempt", attempt, "error", err)
	}
	e.log().Warn("path.migration_unconfirmed", "peer", name, "conv", c.UDPSession.GetConv(), "epoch", request.Epoch, "reason", "commit response unavailable; live session retained for transport health checks")
}

// migrationObservation adds one warning at most for an early failure of a newly
// adopted tuple. ACK/receive deltas are evidence, never a filtering diagnosis.
type migrationObservation struct {
	at              time.Time
	acked, received uint64
	oldPort         int
	warned          bool
	progressLogged  bool
	conn            *kcp.Conn
	streams         int
}

// sample selects bounded diagnostic events from existing delivery counters.
// The caller owns recoveryMu; warning eligibility comes from transport health.
func (w *migrationObservation) sample(now time.Time, acked uint64, failed bool) (progress, warning bool) {
	if w.at.IsZero() || now.Sub(w.at) > time.Minute {
		return false, false
	}
	progress = !w.progressLogged && acked > 0
	if progress {
		w.progressLogged = true
	}
	warning = failed && !w.warned
	if warning {
		w.warned = true
	}
	return
}

// observeMigration emits correlation evidence for a recently moved carrier.
// Flow control, idle sessions and short delivery gaps do not qualify as stalls.
func (e *Engine) observeMigration(now time.Time, name string, index int, s *slot, cfg PathRecoveryConfig) {
	if e.ctx.Err() != nil {
		return
	}
	// Most slots have no active watch: avoid another transport snapshot then.
	s.recoveryMu.Lock()
	active := !s.migrationWatch.at.IsZero() && now.Sub(s.migrationWatch.at) <= time.Minute
	if !active {
		s.migrationWatch = migrationObservation{}
	}
	s.recoveryMu.Unlock()
	if !active {
		return
	}
	eligible := s.carrierRecoveryAllowed(now, cfg, false, false)
	s.recoveryMu.Lock()
	w := &s.migrationWatch
	if w.at.IsZero() || now.Sub(w.at) > time.Minute {
		*w = migrationObservation{}
		s.recoveryMu.Unlock()
		return
	}
	c := w.conn
	if c == nil {
		s.recoveryMu.Unlock()
		return
	}
	stats := c.UDPSession.TransportStats()
	acked, received := stats.AckedBytes-w.acked, stats.ReceivedBytes-w.received
	closed := c.Session.IsClosed()
	progress, warning := w.sample(now, acked, eligible || (closed && w.streams > 0))
	age, oldPort := now.Sub(w.at), w.oldPort
	// A dead mux may still contain stream buffers while relays unwind. The log
	// window must not retain those customer buffers after this observation.
	if closed {
		w.conn = nil
	}
	s.recoveryMu.Unlock()
	if progress && !closed {
		e.log().Debug("path.migration_progress", "peer", name, "session", index, "conv", c.UDPSession.GetConv(), "source_port", s.network.Port, "elapsed_ms", age.Milliseconds(), "acked_bytes", acked, "received_bytes", received)
	}
	if warning {
		reason := "transport stopped delivering shortly after migration; cause unconfirmed"
		if closed {
			reason = "logical carrier closed shortly after migration; cause unconfirmed"
		}
		e.log().Warn("path.migration_early_stall", "peer", name, "session", index, "conv", c.UDPSession.GetConv(), "old_source_port", oldPort, "new_source_port", s.network.Port, "elapsed_ms", age.Milliseconds(), "acked_bytes", acked, "received_bytes", received, "pending_bytes", stats.PendingBytes, "remote_window", stats.RemoteWindow, "streams", c.Session.NumStreams(), "carrier_closed", closed, "reason", reason)
	}
}
