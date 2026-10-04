//go:build linux

package engine

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"paqet/internal/conf"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"paqet/internal/tnet/kcp"
)

type Stats struct{ Active, Accepted, Rejected, Errors, Aborted, Sent, Received, Sessions atomic.Int64 }
type Engine struct {
	tuneMu      sync.Mutex
	tuners      map[*kcp.Conn]*controller
	cfg         *Config
	ctx         context.Context
	cancel      context.CancelFunc
	stats       Stats
	peers       map[string]*peer
	closers     []io.Closer
	fw          firewall
	wg          sync.WaitGroup
	diagnostics *diagnostics
	flowIDs     atomic.Uint64
}

func Run(ctx context.Context, cfg *Config) (err error) {
	d := newDiagnostics(cfg.Log, nil)
	defer d.close()
	if cfg.Firewall == nil || *cfg.Firewall {
		if err := RecoverFirewall(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	e := &Engine{cfg: cfg, ctx: ctx, cancel: cancel, peers: make(map[string]*peer), tuners: make(map[*kcp.Conn]*controller)}
	e.diagnostics = d
	e.log().Info("engine.start", "cpus", runtime.GOMAXPROCS(0), "connection_limit", cfg.Limits.Connections, "session_limit", cfg.Limits.Sessions, "log_flow_sample", cfg.Log.FlowSample)
	defer func() {
		cancel()
		e.close()
		e.wg.Wait()
		cleanupErr := e.fw.close()
		err = errors.Join(err, cleanupErr)
		e.log().Info("engine.stopped", "active", e.stats.Active.Load(), "errors", e.stats.Errors.Load(), "aborted", e.stats.Aborted.Load(), "firewall_cleanup_error", cleanupErr, "log_dropped", d.dropped.Load())
	}()
	e.launch(e.tune)
	e.launch(e.observe)
	if cfg.Limits.MemoryMiB > 0 {
		old := debug.SetMemoryLimit(cfg.Limits.MemoryMiB << 20)
		defer debug.SetMemoryLimit(old)
	}
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}
	required := uint64(cfg.Limits.Connections*2 + 4096)
	if limit.Cur < required {
		limit.Cur = min(required, limit.Max)
		if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
			return fmt.Errorf("raise file limit: %w", err)
		}
		if limit.Cur < required {
			e.log().Warn("file limit below configured connection capacity", "files", limit.Cur, "required", required)
		}
	}
	for _, endpoint := range cfg.Listeners {
		endpoint.KCP.MaxSessions = int(cfg.Limits.Sessions)
		guard, n, err := reserve(endpoint.Network)
		if err != nil {
			return err
		}
		e.closers = append(e.closers, guard)
		if cfg.Firewall == nil || *cfg.Firewall {
			if err := e.fw.add(&n); err != nil {
				return err
			}
		}
		listener, err := kcp.Listen(&endpoint.KCP, n)
		if err != nil {
			return err
		}
		e.closers = append(e.closers, listener)
		e.launch(func() { e.serve(listener, endpoint) })
		e.log().Info("listener.ready", "address", endpoint.Address, "interface", n.Interface.Name)
	}
	for name, endpoint := range cfg.Peers {
		p := &peer{engine: e, endpoint: endpoint}
		for i := 0; i < endpoint.Sessions; i++ {
			guard, n, err := reserve(endpoint.Network)
			if err != nil {
				return err
			}
			e.closers = append(e.closers, guard)
			if cfg.Firewall == nil || *cfg.Firewall {
				if err := e.fw.add(&n); err != nil {
					return err
				}
			}
			p.slots = append(p.slots, &slot{network: n})
		}
		e.peers[name] = p
	}
	for _, f := range cfg.Forwards {
		if f.Protocol == "udp" {
			if err := e.startUDP(f); err != nil {
				return err
			}
			continue
		}
		addr, err := net.ResolveTCPAddr("tcp", f.Listen)
		if err != nil {
			return err
		}
		listener, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return err
		}
		e.closers = append(e.closers, listener)
		e.launch(func() { e.forward(listener, f) })
		e.log().Info("forward.ready", "listen", f.Listen, "peer", f.Peer, "target", f.Target)
	}
	if cfg.Metrics != "" {
		if err := e.startMetrics(); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func (e *Engine) launch(fn func()) { e.wg.Add(1); go func() { defer e.wg.Done(); fn() }() }
func (e *Engine) close() {
	for _, p := range e.peers {
		p.close()
	}
	for i := len(e.closers) - 1; i >= 0; i-- {
		e.closers[i].Close()
	}
}

func (e *Engine) acquire() bool {
	for {
		n := e.stats.Active.Load()
		if n >= e.cfg.Limits.Connections {
			e.stats.Rejected.Add(1)
			return false
		}
		if e.stats.Active.CompareAndSwap(n, n+1) {
			e.stats.Accepted.Add(1)
			return true
		}
	}
}

func (e *Engine) report(err error) {
	n := e.stats.Errors.Add(1)
	if n <= 5 {
		e.log().Warn("connection.failed", "error", err)
	}
	if e.diagnostics != nil && uint64(n)%e.cfg.Log.FlowSample == 0 {
		e.log().Debug("connection.failure_sample", "error_id", n, "error", err)
	}
}

// Reserving a kernel port prevents unrelated outgoing TCP connections using a
// tunnel source port. The owned INPUT rule keeps kernel TCP out of the data path.
func reserve(n conf.Network) (io.Closer, conf.Network, error) {
	a := n.IPv4.Addr
	if a == nil {
		a = n.IPv6.Addr
	}
	if a == nil {
		return nil, n, fmt.Errorf("no local address")
	}
	var guard portGuards
	for attempts := 0; attempts < 128; attempts++ {
		port := n.Port
		if port == 0 {
			var random [2]byte
			if _, err := rand.Read(random[:]); err != nil {
				return nil, n, err
			}
			port = 32768 + int(binary.LittleEndian.Uint16(random[:])&32767)
		}
		var err error
		for _, addr := range []*net.UDPAddr{n.IPv4.Addr, n.IPv6.Addr} {
			if addr == nil {
				continue
			}
			var g *net.TCPListener
			g, err = net.ListenTCP("tcp", &net.TCPAddr{IP: addr.IP, Port: port, Zone: addr.Zone})
			if err != nil {
				break
			}
			guard = append(guard, g)
		}
		if err == nil {
			n.Port = port
			break
		}
		guard.Close()
		guard = nil
		if n.Port != 0 || !errors.Is(err, syscall.EADDRINUSE) {
			return nil, n, err
		}
	}
	if len(guard) == 0 {
		return nil, n, fmt.Errorf("cannot reserve an available tunnel source port in 32768..65535")
	}
	if n.IPv4.Addr != nil {
		a := *n.IPv4.Addr
		a.Port = n.Port
		n.IPv4.Addr = &a
	}
	if n.IPv6.Addr != nil {
		a := *n.IPv6.Addr
		a.Port = n.Port
		n.IPv6.Addr = &a
	}
	return guard, n, nil
}

type portGuards []*net.TCPListener

func (g portGuards) Close() error {
	var errs []error
	for _, l := range g {
		if err := l.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) forward(listener *net.TCPListener, f Forward) {
	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			if e.ctx.Err() != nil {
				return
			}
			if !errors.Is(err, net.ErrClosed) {
				e.stats.Errors.Add(1)
			}
			if errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) {
				timer := time.NewTimer(100 * time.Millisecond)
				select {
				case <-timer.C:
					continue
				case <-e.ctx.Done():
					timer.Stop()
					return
				}
			}
			return
		}
		if !e.acquire() {
			conn.Close()
			continue
		}
		e.launch(func() {
			trace := e.flowTrace()
			if trace != 0 {
				e.log().Debug("flow.open", "flow_id", trace, "role", "forward", "peer", f.Peer, "target", f.Target, "source", conn.RemoteAddr().String())
			}
			defer e.stats.Active.Add(-1)
			defer conn.Close()
			ctx, cancel := context.WithTimeout(e.ctx, e.cfg.Limits.OpenDuration)
			strm, err := e.peers[f.Peer].open(ctx, protocol.PTCP2, f.Target)
			cancel()
			if err != nil {
				if trace != 0 {
					e.log().Debug("flow.open_failed", "flow_id", trace, "error", err)
				}
				e.report(fmt.Errorf("open %s via %s: %w", f.Target, f.Peer, err))
				return
			}
			e.relay(conn, strm, trace)
		})
	}
}

func (e *Engine) serve(listener tnet.Listener, endpoint Endpoint) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if e.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			e.stats.Errors.Add(1)
			continue
		}
		if e.stats.Sessions.Add(1) > e.cfg.Limits.Sessions {
			e.stats.Sessions.Add(-1)
			e.stats.Rejected.Add(1)
			conn.Close()
			continue
		}
		owner := conn.(*kcp.Conn).UDPSession.GetConv()
		e.log().Debug("session.accepted", "conv", owner, "remote", conn.RemoteAddr().String(), "listener", endpoint.Address)
		listener.RegisterClient(conn.RemoteAddr(), owner)
		e.launch(func() {
			defer e.stats.Sessions.Add(-1)
			defer conn.Close()
			if endpoint.Adaptive == nil || *endpoint.Adaptive {
				e.addTuner(conn.(*kcp.Conn), endpoint.KCP.Sndwnd, endpoint.KCP.Rcvwnd)
			} else {
				e.addPassive(conn.(*kcp.Conn))
			}
			defer listener.DeleteClientSession(conn.RemoteAddr(), owner)
			stop := context.AfterFunc(e.ctx, func() { conn.Close() })
			defer stop()
			for {
				strm, err := conn.AcceptStrm()
				if err != nil {
					return
				}
				if !e.acquire() {
					strm.Close()
					continue
				}
				e.launch(func() { defer e.stats.Active.Add(-1); defer strm.Close(); e.handle(listener, strm, owner) })
			}
		})
	}
}

func (e *Engine) handle(listener tnet.Listener, strm tnet.Strm, owner uint32) {
	trace := e.flowTrace()
	strm.SetDeadline(time.Now().Add(e.cfg.Limits.OpenDuration))
	var p protocol.Proto
	if err := p.Read(strm); err != nil {
		e.report(fmt.Errorf("read stream control: %w", err))
		return
	}
	if trace != 0 {
		e.log().Debug("flow.control", "flow_id", trace, "conv", owner, "protocol", p.Type, "source", strm.RemoteAddr().String())
	}
	switch p.Type {
	case protocol.PTCPF:
		listener.SetClientTCPFSession(strm.RemoteAddr(), owner, p.TCPF)
		return
	case protocol.PPING:
		(&protocol.Proto{Type: protocol.PPONG}).Write(strm)
		return
	case protocol.PTCP2, protocol.PUDP2:
	default:
		e.stats.Errors.Add(1)
		return
	}
	// Confirm transport delivery before the potentially slow target dial. This
	// lets the client recover a stale KCP slot without disrupting healthy slots
	// merely because a destination is slow or unreachable.
	if err := writeOpeningAck(strm, 2); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.cfg.Limits.DialDuration)
	proto := "tcp"
	if p.Type == protocol.PUDP2 {
		proto = "udp"
	}
	dialer := net.Dialer{KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, proto, p.Addr.String())
	cancel()
	if err != nil {
		if trace != 0 {
			e.log().Debug("flow.target_failed", "flow_id", trace, "target", p.Addr.String(), "error", err)
		}
		writeOpeningAck(strm, 1)
		e.report(fmt.Errorf("dial %s: %w", p.Addr.String(), err))
		return
	}
	defer conn.Close()
	if err = writeOpeningAck(strm, 0); err != nil {
		return
	}
	strm.SetDeadline(time.Time{})
	if proto == "tcp" {
		e.relay(conn.(*net.TCPConn), strm, trace)
	} else {
		e.relayUDP(conn.(*net.UDPConn), strm)
	}
}

func writeOpeningAck(strm tnet.Strm, code byte) error {
	var n int
	var err error
	if p, ok := strm.(interface{ WritePriority([]byte) (int, error) }); ok {
		n, err = p.WritePriority([]byte{code})
	} else {
		n, err = strm.Write([]byte{code})
	}
	if err == nil && n != 1 {
		return io.ErrShortWrite
	}
	return err
}

func (e *Engine) startMetrics() error {
	mux := http.NewServeMux()
	if e.cfg.Profiling {
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/", pprof.Index)
	}
	mux.HandleFunc("/metrics", e.metrics)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if e.ctx.Err() != nil {
			w.WriteHeader(503)
		} else {
			io.WriteString(w, "ok\n")
		}
	})
	s := &http.Server{Addr: e.cfg.Metrics, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", e.cfg.Metrics)
	if err != nil {
		return err
	}
	e.closers = append(e.closers, listener)
	e.launch(func() {
		if err := s.Serve(listener); err != nil && e.ctx.Err() == nil {
			e.log().Error("metrics.stopped", "error", err)
		}
	})
	return nil
}
