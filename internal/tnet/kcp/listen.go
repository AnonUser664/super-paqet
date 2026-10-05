// File listen.go: builds incoming KCP/mux listeners and fixed fanout worker groups while
// preserving shared socket ownership.

package kcp

import (
	"fmt"
	"net"
	"sync"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"

	"paqet/internal/conf"
	"paqet/internal/socket"
	"paqet/internal/tnet"
)

// Listener owns incoming packet sockets and optional fixed fanout children; accepted carriers
// do not close those sockets.
type Listener struct {
	// Fixed fanout listeners joined before the first conversation is accepted.
	children []*Listener
	// Child-worker accept results aggregated without transferring packet socket ownership.
	accepted chan acceptResult
	// Broadcast lifecycle signal observed by pending work and shutdown.
	done chan struct{}
	// Makes group/socket shutdown idempotent when multiple paths encounter failure.
	closeOnce sync.Once
	// Wait group ensuring accept aggregators finish before group closure returns.
	workers sync.WaitGroup
	// Listener-owned worker socket or shared encoder leader; accepted connections do not close
	// it.
	PacketConn *socket.PacketConn
	// Prepared KCP/mux settings applied to each accepted conversation.
	cfg *conf.KCP
	// Underlying KCP acceptor; it owns the incoming conversation table.
	listener *kcp.Listener
}

// Listen creates the incoming raw/KCP stack, choosing fixed fanout workers only when
// configured.
func Listen(cfg *conf.KCP, netCfg conf.Network) (tnet.Listener, error) {
	if cfg.PacketWorkers > 1 {
		return listenFanout(cfg, netCfg)
	}
	nCfg := netCfg
	packetConn, err := socket.New(&nCfg)
	if err != nil {
		return nil, fmt.Errorf("kcp: failed to create packetconn: %w", err)
	}

	l, err := kcp.ServeConn(cfg.Block, cfg.Dshard, cfg.Pshard, packetConn)
	if err != nil {
		packetConn.Close()
		return nil, fmt.Errorf("kcp: failed to serve connection: %w", err)
	}
	l.SetMaxSessions(cfg.MaxSessions)

	return &Listener{PacketConn: packetConn, cfg: cfg, listener: l}, nil
}

// Accept wraps an accepted KCP conversation in server mux state without transferring ownership
// of its packet socket.
func (l *Listener) Accept() (tnet.Conn, error) {
	if len(l.children) > 0 {
		select {
		case r := <-l.accepted:
			return r.conn, r.err
		case <-l.done:
			return nil, net.ErrClosed
		}
	}
	conn, err := l.listener.AcceptKCP()
	if err != nil {
		return nil, fmt.Errorf("kcp: failed to accept connection: %w", err)
	}
	if err := aplConf(conn, l.cfg); err != nil {
		conn.Close()
		return nil, err
	}
	sess, err := smux.Server(conn, smuxConf(l.cfg, conn))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("kcp: failed to create smux session: %w", err)
	}
	return &Conn{nil, conn, sess}, nil
}

// Close stops accept workers and shared listener sockets; individual accepted carriers are
// closed separately.
func (l *Listener) Close() error {
	if len(l.children) > 0 {
		l.closeOnce.Do(func() {
			close(l.done)
			for _, c := range l.children {
				c.Close()
			}
			l.workers.Wait()
		})
		return nil
	}
	var err error
	if l.listener != nil {
		if e := l.listener.Close(); e != nil {
			err = e
		}
	}
	if l.PacketConn != nil {
		if e := l.PacketConn.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// Addr reports the listener endpoint, using the first fixed worker for a fanout group.
func (l *Listener) Addr() net.Addr {
	if len(l.children) > 0 {
		return l.children[0].Addr()
	}
	return l.listener.Addr()
}

// SetClientTCPF delegates a peer flag-cycle update to the listener's shared packet encoder.
func (l *Listener) SetClientTCPF(addr net.Addr, f []conf.TCPF) {
	l.PacketConn.SetClientTCPF(addr, f)
}

// DeleteClientTCPF removes a compatibility peer flag override without closing the listener
// socket.
func (l *Listener) DeleteClientTCPF(addr net.Addr) {
	l.PacketConn.DeleteClientTCPF(addr)
}

// RegisterClient records conversation ownership for peer flag state shared by listener
// workers.
func (l *Listener) RegisterClient(addr net.Addr, owner uint32) {
	l.PacketConn.RegisterClient(addr, owner)
}

// SetClientTCPFSession delegates a generation-checked update so delayed old setup cannot
// overwrite a replacement.
func (l *Listener) SetClientTCPFSession(addr net.Addr, owner uint32, f []conf.TCPF) {
	l.PacketConn.SetClientTCPFSession(addr, owner, f)
}

// DeleteClientSession clears shared peer state only when this conversation still owns it.
func (l *Listener) DeleteClientSession(addr net.Addr, owner uint32) {
	l.PacketConn.DeleteClientSession(addr, owner)
}

// acceptResult passes a child listener's accepted carrier or error to the fixed fanout group.
type acceptResult struct {
	// Accepted child carrier passed to the group; ownership is released if shutdown wins
	// publication.
	conn tnet.Conn
	// Failure propagated with the result rather than silently dropping lifecycle/output errors.
	err error
}

// listenFanout joins all sockets before accepting traffic so worker membership cannot move a
// live conversation away from its state.
func listenFanout(cfg *conf.KCP, netCfg conf.Network) (tnet.Listener, error) {
	// Ask the kernel for a unique group ID, avoiding collisions between
	// concurrent processes/listeners in the same network namespace.
	netCfg.FanoutUnique = true
	netCfg.FanoutEnabled = true
	group := &Listener{cfg: cfg, accepted: make(chan acceptResult), done: make(chan struct{})}
	// Join every socket to the hash group before starting any KCP monitor,
	// so membership changes cannot move an already accepted flow to a worker
	// without its session state.
	for i := 0; i < cfg.PacketWorkers; i++ {
		n := netCfg
		packet, err := socket.New(&n)
		if err != nil {
			for _, c := range group.children {
				c.PacketConn.Close()
			}
			return nil, err
		}
		if group.PacketConn == nil {
			group.PacketConn = packet
			netCfg.FanoutID = n.FanoutID
			netCfg.FanoutUnique = false
		} else {
			packet.SharePacketEncoder(group.PacketConn)
		}
		group.children = append(group.children, &Listener{PacketConn: packet, cfg: cfg})
	}
	for _, child := range group.children {
		l, err := kcp.ServeConn(cfg.Block, cfg.Dshard, cfg.Pshard, child.PacketConn)
		if err != nil {
			for _, c := range group.children {
				if c.listener != nil {
					c.listener.Close()
				}
				c.PacketConn.Close()
			}
			return nil, err
		}
		l.SetMaxSessions(cfg.MaxSessions)
		child.listener = l
	}
	for _, child := range group.children {
		group.workers.Add(1)
		go func() {
			defer group.workers.Done()
			for {
				conn, err := child.Accept()
				if err != nil {
					select {
					case group.accepted <- acceptResult{nil, err}:
					case <-group.done:
					}
					return
				}
				select {
				case group.accepted <- acceptResult{conn, nil}:
				case <-group.done:
					conn.Close()
					return
				}
			}
		}()
	}
	return group, nil
}

// PacketConnections exposes listener-owned worker sockets for telemetry, not ownership
// transfer.
func (l *Listener) PacketConnections() []*socket.PacketConn {
	if len(l.children) == 0 {
		return []*socket.PacketConn{l.PacketConn}
	}
	out := make([]*socket.PacketConn, 0, len(l.children))
	for _, c := range l.children {
		out = append(out, c.PacketConn)
	}
	return out
}
