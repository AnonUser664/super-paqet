//go:build linux

// File peer.go: owns outgoing carrier pools and bounded opening recovery while retaining
// healthy established stream ownership.

package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"paqet/internal/conf"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"paqet/internal/tnet/kcp"
	"sync"
	"sync/atomic"
	"time"
)

// peer owns the outgoing carrier pool; pool membership and growth are serialized separately
// from each carrier reconnect.
type peer struct {
	// Protects pool membership and closed state; expensive creation also uses growMu.
	mu sync.RWMutex
	// Serializes expensive pool expansion separately from read-only carrier selection.
	growMu sync.Mutex
	// Rejects future work after lifecycle shutdown has begun.
	closed bool
	// Next permitted pool growth attempt after an allocation failure.
	growRetry time.Time
	// Bounded growth retry delay to prevent repeated failure from consuming resources.
	growBackoff time.Duration
	// Optional test seam for allocation/growth races; normal runtime uses allocateSlot.
	createSlot func(context.Context) (*slot, error)
	// Owning runtime for limits, cancellation, firewall and diagnostics.
	engine *Engine
	// Pool generation cancellation also releases half-closed application relays.
	ctx context.Context
	// Cancels generation-owned opening and relay work on replacement/removal.
	cancel context.CancelFunc
	// Endpoint-owned firewall journal; growth and close serialize their rule mutation.
	fw firewall
	// Firewall policy belongs to this generation, even during a global policy reload.
	manageFirewall bool
	// Serializes real slot allocation with final firewall teardown.
	allocationMu sync.Mutex
	// Nonshared carriers own individual journals so one tuple can be retired
	// without removing sibling rules. Allocation and transfer hold allocationMu.
	slotRules []*firewall
	// Prepared peer configuration inherited by newly allocated slots.
	endpoint Endpoint
	// Resource-owned live reliability template; nil permits small unit fixtures.
	settings *atomic.Pointer[Endpoint]
	// Carrier reservations owned by this pool, bounded by max_sessions.
	slots []*slot
	// Optional generation-owned demultiplexer for a single verified source tuple.
	shared *kcp.SharedDialer
	// One kernel reservation belongs to the pool rather than any individual lane.
	sharedGuard io.Closer
	// Prepared shared source tuple retained across lane reconnects and growth.
	sharedNetwork conf.Network
	// Atomic selection cursor used to distribute equal-pressure choices.
	next atomic.Uint64
	// Recovery only responds to transport failures, never target rejection or an idle peer.
	recoveryFailures atomic.Uint64
	recoverySuccess  atomic.Int64
	recoveryPending  atomic.Bool
	// Serializes low-frequency progress accounting with a staged probe's final check.
	recoveryMu     sync.Mutex
	recoveryHealth pathHealth
}

// slot retains a source reservation and an atomically published carrier generation, plus
// pressure score/backoff.
type slot struct {
	// Cached bulk/traffic pressure. Live stream populations are sampled through
	// the mux's atomic gauge rather than waiting for the next controller tick.
	score atomic.Uint64
	// Kernel port reservation owned by this slot until pool teardown.
	guard io.Closer
	// Per-source dispatcher permits preserving a conversation when the socket moves.
	transport *kcp.SharedDialer
	// Scoped rule ownership transferred with this slot during verified recovery.
	fw *firewall
	// Serializes cached-generation replacement and reconnect backoff.
	mu sync.Mutex
	// Atomically published owned outgoing carrier generation; readers validate it before
	// invalidation.
	conn atomic.Pointer[kcp.Conn]
	// Prepared source reservation for this carrier; replacement reuses this endpoint contract.
	network conf.Network
	// Earliest reconnect after a failed carrier creation.
	retry time.Time
	// Bounded reconnect delay; successful creation resets it.
	backoff time.Duration
	// Low-frequency health accounting is independent for each physical tuple.
	recoveryFailures atomic.Uint64
	recoverySuccess  atomic.Int64
	recoveryPending  atomic.Bool
	suspect          atomic.Bool
	// A selected old slot may outlive publication briefly; it must never reopen
	// a released source reservation after a successful recovery transaction.
	retired        atomic.Bool
	recoveryMu     sync.Mutex
	recoveryHealth carrierHealth
	// One bounded post-move warning is sampled with ordinary carrier health.
	migrationWatch migrationObservation
}

// connection lazily creates or reuses a slot carrier, sends its flag setup and preserves
// reconnect backoff.
func (p *peer) connection(ctx context.Context, s *slot) (*kcp.Conn, error) {
	if s.retired.Load() {
		return nil, net.ErrClosed
	}
	if c := s.conn.Load(); c != nil && !c.Session.IsClosed() {
		return c, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired.Load() {
		return nil, net.ErrClosed
	}
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return nil, net.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c := s.conn.Load(); c != nil {
		if !c.Session.IsClosed() {
			return c, nil
		}
		c.Close()
		s.conn.Store(nil)
		s.score.Store(0)
	}
	if time.Now().Before(s.retry) {
		return nil, fmt.Errorf("peer reconnect backoff")
	}
	a, err := net.ResolveUDPAddr("udp", p.configuration().Address)
	if err != nil {
		return nil, err
	}
	endpoint := p.configuration()
	var conn tnet.Conn
	if p.shared != nil {
		conn, err = p.shared.Dial(a, &endpoint.KCP)
	} else if endpoint.PathRecovery.PreserveConnections {
		if s.transport == nil {
			s.transport, err = kcp.NewSharedDialer(&endpoint.KCP, s.network, 2)
		}
		if err == nil {
			conn, err = s.transport.Dial(a, &endpoint.KCP)
		}
	} else {
		conn, err = kcp.Dial(a, &endpoint.KCP, s.network)
	}
	if err != nil {
		if s.backoff == 0 {
			s.backoff = 100 * time.Millisecond
		} else {
			s.backoff = min(5*time.Second, s.backoff*2)
		}
		s.retry = time.Now().Add(s.backoff)
		p.engine.log().Debug("session.connect_failed", "remote", p.configuration().Address, "retry_ms", s.backoff.Milliseconds(), "error", err)
		return nil, err
	}
	c := conn.(*kcp.Conn)
	setupCtx, cancelSetup := context.WithTimeout(ctx, p.engine.current().Limits.OpenDuration)
	stopSetup := context.AfterFunc(setupCtx, func() { c.Close() })
	strm, err := c.OpenStrmContext(setupCtx)
	if err == nil {
		setupDeadline, _ := setupCtx.Deadline()
		strm.SetDeadline(setupDeadline)
		err = (&protocol.Proto{Type: protocol.PTCPF, TCPF: p.configuration().Network.TCP.RF}).Write(strm)
		// Setup owns an unpublished carrier: its context can close the carrier
		// if this ordinary close stalls, without affecting other forwards.
		strm.Close()
	}
	stopped := stopSetup()
	if err == nil && !stopped {
		err = setupCtx.Err()
		if err == nil {
			err = context.Canceled
		}
	}
	cancelSetup()
	if err != nil {
		c.Close()
		return nil, err
	}
	// Publish only after control setup succeeded; other opens may now reuse this complete carrier generation.
	s.conn.Store(c)
	if endpoint.PathRecovery.PreserveConnections {
		p.engine.launch(func() { p.engine.negotiateMigration(p, c) })
	}
	p.engine.log().Debug("session.connected", "conv", c.UDPSession.GetConv(), "remote", p.configuration().Address, "local", c.LocalAddr().String(), "phase", "local_setup", "peer_verified", false)
	if p.settings != nil {
		p.engine.addEndpoint(c, p.settings, s)
	} else if p.endpoint.Adaptive == nil || *p.endpoint.Adaptive {
		p.engine.addTuner(c, p.endpoint.KCP.Sndwnd, p.endpoint.KCP.Rcvwnd, s)
	} else {
		p.engine.addPassive(c)
	}
	s.backoff = 0
	s.retry = time.Time{}
	return c, nil
}

// open opens one target stream, separates transport receipt from target failure and bounds
// carrier retry selection.
func (p *peer) open(ctx context.Context, kind byte, target string) (tnet.Strm, error) {
	ctx, cancel := context.WithTimeout(ctx, p.engine.current().Limits.OpenDuration)
	defer cancel()
	a, err := tnet.NewAddr(target)
	if err != nil {
		return nil, err
	}
	var last error
	var excluded map[*slot]bool
	transportRetry := false
	p.mu.RLock()
	attemptLimit := len(p.slots) + 1
	p.mu.RUnlock()
	for attempts := 0; attempts < attemptLimit; attempts++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s, err := p.selectSlot(ctx, excluded)
		if err != nil {
			return nil, err
		}
		if transportRetry {
			p.engine.stats.OpenRetries.Add(1)
			transportRetry = false
		}
		c, err := p.connection(ctx, s)
		if err != nil {
			last = err
			if excluded == nil {
				excluded = make(map[*slot]bool)
			}
			excluded[s] = true
			continue
		}
		deadline, _ := ctx.Deadline()
		firstReply := p.receiptDeadline(time.Now(), deadline, s, excluded, c.UDPSession.GetSRTT())
		openingCtx, cancelOpening := context.WithDeadline(ctx, firstReply)
		strm, err := c.OpenStrmContext(openingCtx)
		cancelOpening()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				p.invalidateIdle(s, c)
				return nil, err
			}
			var timeout interface{ Timeout() bool }
			if errors.As(err, &timeout) && timeout.Timeout() && p.engine.ctx.Err() == nil {
				p.recoveryFailures.Add(1)
				s.recordTransportFailure()
				p.invalidateIdle(s, c)
				transportRetry = true
				p.engine.log().Debug("opening.syn_timeout", "remote", p.configuration().Address, "conv", c.UDPSession.GetConv(), "attempt", attempts+1, "remaining_ms", time.Until(deadline).Milliseconds(), "streams", c.Session.NumStreams(), "error", err)
			} else {
				p.invalidate(s, c)
			}
			if excluded == nil {
				excluded = make(map[*slot]bool)
			}
			excluded[s] = true
			last = err
			continue
		}
		strm.SetDeadline(firstReply)
		stop := context.AfterFunc(ctx, func() { abortOpening(strm) })
		err = (&protocol.Proto{Type: kind, Addr: a}).Write(strm)
		var ack [1]byte
		if err == nil {
			_, err = io.ReadFull(strm, ack[:])
			if err == nil && ack[0] == 2 {
				strm.SetDeadline(deadline)
				_, err = io.ReadFull(strm, ack[:])
			}
			if err == nil && ack[0] != 0 {
				err = fmt.Errorf("remote target connection rejected")
			}
		}
		stopped := stop()
		if err == nil && !stopped {
			err = ctx.Err()
			if err == nil {
				err = context.Canceled
			}
		}
		if err != nil {
			var timeout interface{ Timeout() bool }
			transportTimedOut := ack[0] == 0 && errors.As(err, &timeout) && timeout.Timeout()
			if transportTimedOut && p.engine.ctx.Err() == nil {
				p.recoveryFailures.Add(1)
				s.recordTransportFailure()
				abortOpening(strm)
				// Only this new stream failed. Existing forwards retain their carrier.
				transportRetry = true
				p.engine.log().Debug("opening.transport_timeout", "remote", p.configuration().Address, "target", target, "conv", c.UDPSession.GetConv(), "attempt", attempts+1, "remaining_ms", time.Until(deadline).Milliseconds(), "streams", c.Session.NumStreams(), "error", err)
				p.invalidateIdle(s, c)
				if excluded == nil {
					excluded = make(map[*slot]bool)
				}
				excluded[s] = true
				last = err
				continue
			}
			abortOpening(strm)
			return nil, err
		}
		strm.SetDeadline(time.Time{})
		p.recoverySuccess.Store(time.Now().UnixNano())
		s.recoverySuccess.Store(time.Now().UnixNano())
		s.suspect.Store(false)
		return strm, nil
	}
	return nil, fmt.Errorf("peer unavailable: %w", last)
}

// abortOpening releases failed new-stream ownership without spending another
// control-write timeout. KCP streams support a bounded best-effort reset; the
// fallback preserves the generic stream contract for other implementations.
func abortOpening(stream tnet.Strm) {
	if abort, ok := stream.(interface{ Abort() error }); ok {
		abort.Abort()
	} else {
		stream.Close()
	}
}

// receiptDeadline reserves time for untried carriers when one ordered lane
// stalls. A busy carrier is retained by invalidateIdle; its existing streams do
// not justify spending the whole new-opening deadline. ACK2 subsequently grants
// the full target-dial budget, so slow target connections are never retried.
func (p *peer) receiptDeadline(now, deadline time.Time, selected *slot, excluded map[*slot]bool, rtt int32) time.Time {
	p.mu.RLock()
	remaining := 1
	for _, s := range p.slots {
		if s != selected && !excluded[s] {
			remaining++
		}
	}
	p.mu.RUnlock()
	if remaining == 1 {
		return deadline
	}
	budget := max(2*time.Second, time.Duration(rtt)*8*time.Millisecond)
	budget = min(budget, deadline.Sub(now)/time.Duration(remaining))
	return now.Add(budget)
}

// invalidate retires only the cached generation that failed so an old operation cannot close
// its replacement.
func (p *peer) invalidate(s *slot, c *kcp.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn.Load() == c {
		p.engine.log().Debug("session.invalidated", "conv", c.UDPSession.GetConv(), "remote", p.configuration().Address)
		c.Close()
		s.conn.Store(nil)
		s.score.Store(0)
	}
}

// invalidateIdle retires stale carriers only when they have no established streams to disrupt.
func (p *peer) invalidateIdle(s *slot, c *kcp.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn.Load() == c && c.Session.CloseIfIdle() {
		p.engine.log().Debug("session.idle_invalidated", "conv", c.UDPSession.GetConv(), "remote", p.configuration().Address)
		c.Close()
		s.conn.Store(nil)
		s.score.Store(0)
	}
}

// close marks the pool closed and releases every published carrier/guard while excluding
// concurrent growth.
func (p *peer) close() {
	p.mu.Lock()
	p.closed = true
	slots := p.slots
	p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
	for _, s := range slots {
		s.mu.Lock()
		if c := s.conn.Swap(nil); c != nil {
			c.Close()
		}
		if s.transport != nil {
			s.transport.Close()
		}
		if s.guard != nil {
			s.guard.Close()
		}
		s.mu.Unlock()
	}
	// Allocation and final journal teardown already serialize on this lock.
	// Taking it here also excludes a first shared socket being staged during close.
	p.allocationMu.Lock()
	if p.shared != nil {
		p.shared.Close()
	}
	if p.sharedGuard != nil {
		p.sharedGuard.Close()
	}
	p.allocationMu.Unlock()
}

// busyCarrier adds a cached pressure penalty so new short streams prefer capacity away from
// sustained bulk work.
const busyCarrier = uint64(1) << 63

// admissionScore combines cached bulk pressure with the current mux population.
// A 250 ms old stream count can send an entire opening burst to a formerly idle
// lane. This uses only atomic reads: no KCP/receive-map locks, per-stream scans,
// allocations or extra controller ticks are needed on the admission path.
func (s *slot) admissionScore() uint64 {
	score := s.score.Load()
	if conn := s.conn.Load(); conn != nil {
		score = score&busyCarrier | uint64(conn.Session.NumStreamsSnapshot())
	}
	return score
}

// Admission retains health precedence and cached bulk hysteresis while using
// live stream population to break ties between equally busy carriers.
func (p *peer) bestSlotLocked(excluded map[*slot]bool) *slot {
	start := int((p.next.Add(1) - 1) % uint64(len(p.slots)))
	var best *slot
	var bestScore uint64
	for i := 0; i < len(p.slots); i++ {
		candidate := p.slots[(start+i)%len(p.slots)]
		if excluded[candidate] {
			continue
		}
		score := candidate.admissionScore()
		// Prefer a progressing tuple over one with repeated transport failures;
		// retain a fallback when every tuple is suspect so restored paths can
		// still prove progress before a replacement is ready.
		if best == nil || (best.suspect.Load() && !candidate.suspect.Load()) ||
			(best.suspect.Load() == candidate.suspect.Load() && score < bestScore) {
			best = candidate
			bestScore = score
		}
	}
	return best
}

// selectSlot chooses low-pressure carriers and serializes bounded pool growth so concurrent
// opens do not create duplicate spares.
func (p *peer) selectSlot(ctx context.Context, exclusions ...map[*slot]bool) (*slot, error) {
	var excluded map[*slot]bool
	if len(exclusions) > 0 {
		excluded = exclusions[0]
	}
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, net.ErrClosed
	}
	best := p.bestSlotLocked(excluded)
	if best == nil {
		p.mu.RUnlock()
		return nil, fmt.Errorf("all peer carriers failed this opening attempt")
	}
	grow := best.score.Load() >= busyCarrier && len(p.slots) < p.configuration().MaxSessions
	p.mu.RUnlock()
	if !grow {
		return best, nil
	}
	p.growMu.Lock()
	defer p.growMu.Unlock()
	if time.Now().Before(p.growRetry) {
		return best, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, net.ErrClosed
	}
	best = p.bestSlotLocked(excluded)
	if best == nil {
		p.mu.RUnlock()
		return nil, fmt.Errorf("all peer carriers failed this opening attempt")
	}
	grow = best.score.Load() >= busyCarrier && len(p.slots) < p.configuration().MaxSessions
	p.mu.RUnlock()
	if !grow {
		return best, nil
	}
	factory := p.allocateSlot
	if p.createSlot != nil {
		factory = p.createSlot
	}
	s, err := factory(ctx)
	if err != nil {
		if p.growBackoff == 0 {
			p.growBackoff = 100 * time.Millisecond
		} else {
			p.growBackoff = min(5*time.Second, p.growBackoff*2)
		}
		p.growRetry = time.Now().Add(p.growBackoff)
		p.engine.log().Debug("peer.pool_growth_failed", "remote", p.configuration().Address, "error", err)
		return best, nil
	}
	p.mu.Lock()
	if p.closed || ctx.Err() != nil {
		p.mu.Unlock()
		if s.guard != nil {
			s.guard.Close()
		}
		return nil, net.ErrClosed
	}
	p.slots = append(p.slots, s)
	p.growBackoff = 0
	p.growRetry = time.Time{}
	count := len(p.slots)
	p.mu.Unlock()
	p.engine.log().Debug("peer.pool_grew", "remote", p.configuration().Address, "carriers", count, "maximum", p.configuration().MaxSessions)
	return s, nil
}

// allocateSlot reserves source-port/firewall resources without opening the remote carrier
// until traffic needs it.
func (p *peer) allocateSlot(ctx context.Context) (*slot, error) {
	p.allocationMu.Lock()
	defer p.allocationMu.Unlock()
	p.mu.RLock()
	closed := p.closed
	index := len(p.slots)
	p.mu.RUnlock()
	if closed {
		return nil, net.ErrClosed
	}
	endpoint := p.configuration()
	if endpoint.SharedSource && p.shared != nil {
		return newSlot(p.sharedNetwork, nil, nil), nil
	}
	network := endpoint.Network
	if len(endpoint.SourcePorts) > 0 {
		if index >= len(endpoint.SourcePorts) {
			return nil, fmt.Errorf("peer source port list exhausted")
		}
		network.Port = endpoint.SourcePorts[index]
	}
	guard, n, err := reserve(network)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		guard.Close()
		return nil, err
	}
	var rules *firewall
	if p.manageFirewall {
		rules = &p.fw
		if !endpoint.SharedSource {
			rules = &firewall{}
			p.slotRules = append(p.slotRules, rules)
		}
		if err := rules.add(&n); err != nil {
			guard.Close()
			if !endpoint.SharedSource {
				p.cleanSlotRules(rules)
			}
			return nil, err
		}
	}
	if endpoint.SharedSource {
		shared, err := kcp.NewSharedDialer(&endpoint.KCP, n, endpoint.MaxSessions)
		if err != nil {
			guard.Close()
			return nil, err
		}
		p.shared, p.sharedGuard, p.sharedNetwork = shared, guard, n
		return newSlot(n, nil, nil), nil
	}
	return newSlot(n, guard, rules), nil
}

// newSlot starts the evidence clock when the tuple is reserved. Very short
// opening deadlines must not lose failures that precede the first health tick.
func newSlot(network conf.Network, guard io.Closer, rules *firewall) *slot {
	s := &slot{network: network, guard: guard, fw: rules}
	s.recoveryHealth.lastProgress = time.Now()
	return s
}

// cleanSlotRules removes one journal only after successful scoped cleanup.
// The caller holds allocationMu; a failed cleanup remains owned for shutdown.
func (p *peer) cleanSlotRules(rules *firewall) error {
	if err := rules.close(); err != nil {
		return err
	}
	for i, owned := range p.slotRules {
		if owned == rules {
			p.slotRules = append(p.slotRules[:i], p.slotRules[i+1:]...)
			break
		}
	}
	return nil
}

// closeSlotRules retries every nonshared journal after sockets have stopped.
func (p *peer) closeSlotRules() error {
	p.allocationMu.Lock()
	defer p.allocationMu.Unlock()
	var errs []error
	for _, rules := range append([]*firewall(nil), p.slotRules...) {
		errs = append(errs, p.cleanSlotRules(rules))
	}
	return errors.Join(errs...)
}

// configuration supplies a coherent immutable endpoint template. Live reload
// replaces the pointer rather than mutating a value used by concurrent opens.
func (p *peer) configuration() *Endpoint {
	if p.settings != nil {
		if endpoint := p.settings.Load(); endpoint != nil {
			return endpoint
		}
	}
	return &p.endpoint
}

// lifecycle is the pool generation's context, with a fallback for unit fixtures.
func (p *peer) lifecycle() context.Context {
	if p.ctx != nil {
		return p.ctx
	}
	return p.engine.ctx
}
