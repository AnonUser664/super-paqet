//go:build linux

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

type peer struct {
	mu          sync.RWMutex
	growMu      sync.Mutex
	closed      bool
	growRetry   time.Time
	growBackoff time.Duration
	createSlot  func(context.Context) (*slot, error)
	engine      *Engine
	endpoint    Endpoint
	slots       []*slot
	next        atomic.Uint64
}
type slot struct {
	score   atomic.Uint64
	guard   io.Closer
	mu      sync.Mutex
	conn    atomic.Pointer[kcp.Conn]
	network conf.Network
	retry   time.Time
	backoff time.Duration
}

func (p *peer) connection(ctx context.Context, s *slot) (*kcp.Conn, error) {
	if c := s.conn.Load(); c != nil && !c.Session.IsClosed() {
		return c, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	a, err := net.ResolveUDPAddr("udp", p.endpoint.Address)
	if err != nil {
		return nil, err
	}
	conn, err := kcp.Dial(a, &p.endpoint.KCP, s.network)
	if err != nil {
		if s.backoff == 0 {
			s.backoff = 100 * time.Millisecond
		} else {
			s.backoff = min(5*time.Second, s.backoff*2)
		}
		s.retry = time.Now().Add(s.backoff)
		p.engine.log().Debug("session.connect_failed", "remote", p.endpoint.Address, "retry_ms", s.backoff.Milliseconds(), "error", err)
		return nil, err
	}
	c := conn.(*kcp.Conn)
	strm, err := c.OpenStrm()
	if err == nil {
		strm.SetDeadline(time.Now().Add(p.engine.cfg.Limits.OpenDuration))
		err = (&protocol.Proto{Type: protocol.PTCPF, TCPF: p.endpoint.Network.TCP.RF}).Write(strm)
		strm.Close()
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	s.conn.Store(c)
	p.engine.log().Debug("session.connected", "conv", c.UDPSession.GetConv(), "remote", p.endpoint.Address, "local", c.LocalAddr().String())
	if p.endpoint.Adaptive == nil || *p.endpoint.Adaptive {
		p.engine.addTuner(c, p.endpoint.KCP.Sndwnd, p.endpoint.KCP.Rcvwnd, s)
	} else {
		p.engine.addPassive(c)
	}
	s.backoff = 0
	s.retry = time.Time{}
	return c, nil
}

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

func (p *peer) invalidate(s *slot, c *kcp.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn.Load() == c {
		p.engine.log().Debug("session.invalidated", "conv", c.UDPSession.GetConv(), "remote", p.endpoint.Address)
		c.Close()
		s.conn.Store(nil)
		s.score.Store(0)
	}
}

func (p *peer) invalidateIdle(s *slot, c *kcp.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn.Load() == c && c.Session.CloseIfIdle() {
		p.engine.log().Debug("session.idle_invalidated", "conv", c.UDPSession.GetConv(), "remote", p.endpoint.Address)
		c.Close()
		s.conn.Store(nil)
		s.score.Store(0)
	}
}

func (p *peer) close() {
	p.mu.Lock()
	p.closed = true
	slots := p.slots
	p.mu.Unlock()
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
	grow := best.score.Load() >= busyCarrier && len(p.slots) < p.endpoint.MaxSessions
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
	grow = best.score.Load() >= busyCarrier && len(p.slots) < p.endpoint.MaxSessions
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
		p.engine.log().Debug("peer.pool_growth_failed", "remote", p.endpoint.Address, "error", err)
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
	p.engine.log().Debug("peer.pool_grew", "remote", p.endpoint.Address, "carriers", count, "maximum", p.endpoint.MaxSessions)
	return s, nil
}

func (p *peer) allocateSlot(ctx context.Context) (*slot, error) {
	guard, n, err := reserve(p.endpoint.Network)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		guard.Close()
		return nil, err
	}
	if p.engine.cfg.Firewall == nil || *p.engine.cfg.Firewall {
		if err := p.engine.fw.add(&n); err != nil {
			guard.Close()
			return nil, err
		}
	}
	return &slot{network: n, guard: guard}, nil
}
