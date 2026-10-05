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
	// Prepared peer configuration inherited by newly allocated slots.
	endpoint Endpoint
	// Resource-owned live reliability template; nil permits small unit fixtures.
	settings *atomic.Pointer[Endpoint]
	// Carrier reservations owned by this pool, bounded by max_sessions.
	slots []*slot
	// Atomic selection cursor used to distribute equal-pressure choices.
	next atomic.Uint64
}

// slot retains a source reservation and an atomically published carrier generation, plus
// pressure score/backoff.
type slot struct {
	// Cached stream/traffic pressure; selection need not scan every stream or packet counter.
	score atomic.Uint64
	// Kernel port reservation owned by this slot until pool teardown.
	guard io.Closer
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
}

// connection lazily creates or reuses a slot carrier, sends its flag setup and preserves
// reconnect backoff.
func (p *peer) connection(ctx context.Context, s *slot) (*kcp.Conn, error) {
	if c := s.conn.Load(); c != nil && !c.Session.IsClosed() {
		return c, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	conn, err := kcp.Dial(a, &endpoint.KCP, s.network)
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
	strm, err := c.OpenStrm()
	if err == nil {
		strm.SetDeadline(time.Now().Add(p.engine.current().Limits.OpenDuration))
		err = (&protocol.Proto{Type: protocol.PTCPF, TCPF: p.configuration().Network.TCP.RF}).Write(strm)
		strm.Close()
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	// Publish only after control setup succeeded; other opens may now reuse this complete carrier generation.
	s.conn.Store(c)
	p.engine.log().Debug("session.connected", "conv", c.UDPSession.GetConv(), "remote", p.configuration().Address, "local", c.LocalAddr().String())
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
	a, err := tnet.NewAddr(target)
	if err != nil {
		return nil, err
	}
	var last error
	var excluded map[*slot]bool
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
		c, err := p.connection(ctx, s)
		if err != nil {
			last = err
			if excluded == nil {
				excluded = make(map[*slot]bool)
			}
			excluded[s] = true
			continue
		}
		strm, err := c.OpenStrm()
		if err != nil {
			p.invalidate(s, c)
			if excluded == nil {
				excluded = make(map[*slot]bool)
			}
			excluded[s] = true
			last = err
			continue
		}
		deadline, _ := ctx.Deadline()
		firstReply := time.Now().Add(max(2*time.Second, time.Duration(c.UDPSession.GetSRTT())*8*time.Millisecond))
		// A temporary outage must not kill established forwards just because a
		// new request missed a short opening deadline. Busy sessions get the
		// configured opening grace; idle stale sessions retain quick replacement.
		if c.Session.NumStreams() > 1 {
			firstReply = deadline
		}
		if deadline.Before(firstReply) {
			firstReply = deadline
		}
		strm.SetDeadline(firstReply)
		stop := context.AfterFunc(ctx, func() { strm.Close() })
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
				strm.Close()
				p.invalidateIdle(s, c)
				if excluded == nil {
					excluded = make(map[*slot]bool)
				}
				excluded[s] = true
				last = err
				continue
			}
			strm.Close()
			return nil, err
		}
		strm.SetDeadline(time.Time{})
		return strm, nil
	}
	return nil, fmt.Errorf("peer unavailable: %w", last)
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
		if s.guard != nil {
			s.guard.Close()
		}
		s.mu.Unlock()
	}
}

// busyCarrier adds a cached pressure penalty so new short streams prefer capacity away from
// sustained bulk work.
const busyCarrier = uint64(1) << 63

// Admission reads cached pressure, avoiding KCP locks and snapshot allocations
// on the path that establishes hundreds of thousands of forwards.
func (p *peer) bestSlotLocked(excluded map[*slot]bool) *slot {
	start := int((p.next.Add(1) - 1) % uint64(len(p.slots)))
	var best *slot
	for i := 0; i < len(p.slots); i++ {
		candidate := p.slots[(start+i)%len(p.slots)]
		if excluded[candidate] {
			continue
		}
		if best == nil || candidate.score.Load() < best.score.Load() {
			best = candidate
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
	p.mu.RUnlock()
	if closed {
		return nil, net.ErrClosed
	}
	guard, n, err := reserve(p.configuration().Network)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		guard.Close()
		return nil, err
	}
	if p.manageFirewall {
		if err := p.fw.add(&n); err != nil {
			guard.Close()
			return nil, err
		}
	}
	return &slot{network: n, guard: guard}, nil
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
