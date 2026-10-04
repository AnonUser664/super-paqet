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

type Listener struct {
	children   []*Listener
	accepted   chan acceptResult
	done       chan struct{}
	closeOnce  sync.Once
	workers    sync.WaitGroup
	PacketConn *socket.PacketConn
	cfg        *conf.KCP
	listener   *kcp.Listener
}

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

func (l *Listener) Addr() net.Addr {
	if len(l.children) > 0 {
		return l.children[0].Addr()
	}
	return l.listener.Addr()
}

func (l *Listener) SetClientTCPF(addr net.Addr, f []conf.TCPF) {
	l.PacketConn.SetClientTCPF(addr, f)
}

func (l *Listener) DeleteClientTCPF(addr net.Addr) {
	l.PacketConn.DeleteClientTCPF(addr)
}

func (l *Listener) RegisterClient(addr net.Addr, owner uint32) {
	l.PacketConn.RegisterClient(addr, owner)
}
func (l *Listener) SetClientTCPFSession(addr net.Addr, owner uint32, f []conf.TCPF) {
	l.PacketConn.SetClientTCPFSession(addr, owner, f)
}
func (l *Listener) DeleteClientSession(addr net.Addr, owner uint32) {
	l.PacketConn.DeleteClientSession(addr, owner)
}

type acceptResult struct {
	conn tnet.Conn
	err  error
}

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
