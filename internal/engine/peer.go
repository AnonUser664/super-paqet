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
	engine   *Engine
	endpoint Endpoint
	slots    []*slot
	next     atomic.Uint64
}
type slot struct {
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
		p.engine.addTuner(c, p.endpoint.KCP.Sndwnd, p.endpoint.KCP.Rcvwnd)
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
	for attempts := 0; attempts < len(p.slots); attempts++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s := p.slots[(p.next.Add(1)-1)%uint64(len(p.slots))]
		c, err := p.connection(ctx, s)
		if err != nil {
			last = err
			continue
		}
		strm, err := c.OpenStrm()
		if err != nil {
			p.invalidate(s, c)
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
	}
}

func (p *peer) invalidateIdle(s *slot, c *kcp.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn.Load() == c && c.Session.CloseIfIdle() {
		p.engine.log().Debug("session.idle_invalidated", "conv", c.UDPSession.GetConv(), "remote", p.endpoint.Address)
		c.Close()
		s.conn.Store(nil)
	}
}

func (p *peer) close() {
	for _, s := range p.slots {
		s.mu.Lock()
		if c := s.conn.Swap(nil); c != nil {
			c.Close()
		}
		s.mu.Unlock()
	}
}
