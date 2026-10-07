//go:build linux

// File engine.go: owns runtime startup, admission, incoming control dispatch and ordered
// shutdown of one tunnel instance.

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

	"paqet/internal/conf"
	"paqet/internal/protocol"
	"paqet/internal/tnet"
	"paqet/internal/tnet/kcp"
)

// Stats stores atomic process counters so concurrent flows can report lifecycle and accepted
// bytes without one global relay lock.
type Stats struct {
	// Atomic live/admission/failure/byte/carrier counters; control streams can briefly count as
	// active work.
	Active, Accepted, Rejected, Errors, Aborted, Sent, Received, Sessions atomic.Int64
	// Counts new-opening recovery attempts without counting them as application failures.
	OpenRetries atomic.Int64
}

// Engine owns one process runtime and its resource lifetimes; carrier and flow state remain
// separate objects.
type Engine struct {
	// Protects controller registration and listener-observer snapshots shared by
	// tuning/diagnostic tasks.
	tuneMu sync.Mutex
	// Per-carrier controller/telemetry registry; closed sessions are removed by the shared loop.
	tuners map[*kcp.Conn]*controller
	// Listener-owned worker sockets registered under tuneMu for shared drop telemetry.
	packetObservers []observedPacket
	// Initial immutable configuration; current() uses the atomically published runtime view.
	cfg *Config
	// Engine cancellation propagated to endpoint/flow work.
	ctx context.Context
	// Cancels the engine child context before resource teardown.
	cancel context.CancelFunc
	// Atomic lifecycle/byte/admission counters shared by concurrent flows and diagnostics.
	stats Stats
	// Serializes resource transactions and shutdown, never per-packet forwarding.
	reloadMu sync.Mutex
	// One atomic publication keeps configuration and route-to-pool selection coherent.
	view atomic.Pointer[runtimeView]
	// Endpoint and local bind ownership, mutated only under reloadMu.
	resources map[string]*liveResource
	// Optional deterministic resource fault seam; production uses prepareResource.
	resourceFactory func(resourceSpec) (*liveResource, error)
	// Retains failed rule cleanups for retries and final shutdown.
	retired []*liveResource
	// Wakes diagnostics immediately when the configured sample interval changes.
	observeChanged chan struct{}
	// Reload admission, outcome and revision counters exported by metrics.
	reloadApplied, reloadRejected, revision atomic.Uint64
	// Health is degraded only if rollback could not reconstruct an old resource.
	degraded atomic.Bool
	// Tracks engine tasks so rules/resources are not finalized while relays still run.
	wg sync.WaitGroup
	// Bounded asynchronous diagnostic output and its shutdown/drop state.
	diagnostics *diagnostics
	// Time of the last warning cause; later incidents must remain visible at warn level.
	failureWarnAt atomic.Int64
	// Atomic debug correlation IDs; absent debug tracing avoids per-flow records.
	flowIDs atomic.Uint64
	// Bounds concurrent unpublished source-tuple probes independently of peer count.
	recoverySlots chan struct{}
	// Optional deterministic probe seam; production proves PPONG over the real carrier.
	pathProbe func(context.Context, *peer) error
	// Cumulative bounded-probe outcomes, including discarded stale candidates.
	pathRecoveryAttempts, pathRecoverySucceeded, pathRecoveryRejected atomic.Uint64
}

// Run owns one engine lifecycle, including admission resources and cleanup after partial
// startup or cancellation.
func Run(ctx context.Context, cfg *Config) error { return run(ctx, cfg, nil) }

// run owns startup, optional file watching and ordered endpoint teardown. Runtime
// generations retain immutable settings; only current admission settings are shared.
func run(ctx context.Context, cfg *Config, watch func(*Engine)) (err error) {
	d := newDiagnostics(cfg.Log, nil)
	defer d.close()
	if firewallEnabled(cfg) {
		if err := RecoverFirewall(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	e := &Engine{cfg: cfg, ctx: ctx, cancel: cancel, resources: make(map[string]*liveResource), tuners: make(map[*kcp.Conn]*controller), diagnostics: d, observeChanged: make(chan struct{}, 1)}
	oldMemory := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(oldMemory)
	defer func() {
		cancel()
		err = errors.Join(err, e.close())
		e.wg.Wait()
		e.log().Info("engine.stopped", "active", e.stats.Active.Load(), "errors", e.stats.Errors.Load(), "aborted", e.stats.Aborted.Load(), "cleanup_error", err, "log_dropped", d.dropped.Load())
	}()
	if err := e.apply(cfg); err != nil {
		return err
	}
	e.log().Info("engine.start", "cpus", runtime.GOMAXPROCS(0), "connection_limit", cfg.Limits.Connections, "session_limit", cfg.Limits.Sessions, "log_flow_sample", cfg.Log.FlowSample)
	e.launch(e.tune)
	e.launch(e.observe)
	e.recoverySlots = make(chan struct{}, 4)
	e.launch(e.recoverPaths)
	if watch != nil {
		e.launch(func() { watch(e) })
	}
	<-ctx.Done()
	return nil
}

// launch adds a task to the engine wait group so shutdown waits for its completion.
func (e *Engine) launch(fn func()) { e.wg.Add(1); go func() { defer e.wg.Done(); fn() }() }

// close releases each endpoint-owned socket/guard and rule journal under the
// reload lock, including earlier failed cleanups; run then joins tracked work.
func (e *Engine) close() error {
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	var errs []error
	for key, resource := range e.resources {
		errs = append(errs, resource.close())
		delete(e.resources, key)
	}
	for _, resource := range e.retired {
		errs = append(errs, resource.close())
	}
	return errors.Join(errs...)
}

// acquire atomically admits active work up to the process limit, preventing concurrent accepts
// from overshooting capacity.
func (e *Engine) acquire() bool {
	for {
		n := e.stats.Active.Load()
		if n >= e.current().Limits.Connections {
			e.stats.Rejected.Add(1)
			return false
		}
		if e.stats.Active.CompareAndSwap(n, n+1) {
			e.stats.Accepted.Add(1)
			return true
		}
	}
}

// report records a failed operation and its diagnostic cause without terminating unrelated
// healthy carriers.
func (e *Engine) report(err error) {
	n := e.stats.Errors.Add(1)
	if e.warnFailure(n, time.Now().UnixNano()) {
		e.log().Warn("connection.failed", "error_id", n, "error", err)
	}
	if e.diagnostics != nil && uint64(n)%e.current().Log.FlowSample == 0 {
		e.log().Debug("connection.failure_sample", "error_id", n, "error", err)
	}
}

// warnFailure preserves the first five causes and at most one subsequent cause
// per ten seconds. Counters retain every failure, including suppressed records.
// Atomic admission avoids a global logging lock on concurrent opening failures.
func (e *Engine) warnFailure(n int64, now int64) bool {
	if n <= 5 {
		e.failureWarnAt.Store(now)
		return true
	}
	previous := e.failureWarnAt.Load()
	return now-previous >= int64(10*time.Second) && e.failureWarnAt.CompareAndSwap(previous, now)
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

// portGuards retains kernel listeners used only to reserve tunnel source ports, not to carry
// application data.
type portGuards []*net.TCPListener

// Close releases every reserved kernel port and combines failures so dual-family reservations
// do not leak.
func (g portGuards) Close() error {
	var errs []error
	for _, l := range g {
		if err := l.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// forward accepts local TCP flows, applies admission/opening deadlines and relays each
// successful remote stream.
func (e *Engine) forward(listener *net.TCPListener, key string) {
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
		// Capture target and pool together at acceptance; later edits cannot redirect
		// an established stream or accidentally combine two config generations.
		binding, ok := e.route(key)
		if !ok {
			conn.Close()
			continue
		}
		f, p := binding.forward, binding.peer
		if !e.acquire() {
			conn.Close()
			continue
		}
		e.launch(func() {
			trace := e.flowTrace()
			if e.traceFlow(trace) {
				e.log().Debug("flow.open", "flow_id", trace, "role", "forward", "peer", f.Peer, "target", f.Target, "source", conn.RemoteAddr().String())
			}
			defer e.stats.Active.Add(-1)
			defer conn.Close()
			ctx, cancel := context.WithTimeout(p.lifecycle(), e.current().Limits.OpenDuration)
			strm, err := p.open(ctx, protocol.PTCP2, f.Target)
			cancel()
			if err != nil {
				e.log().Debug("flow.open_failed", "flow_id", trace, "peer", f.Peer, "target", f.Target, "error", err)
				e.report(fmt.Errorf("open %s via %s: %w", f.Target, f.Peer, err))
				return
			}
			e.relayContext(p.lifecycle(), conn, strm, trace)
		})
	}
}

// serve accepts incoming carriers and dispatches their mux streams while preserving
// generation-owned flag state.
func (e *Engine) serve(ctx context.Context, listener tnet.Listener, resource *liveResource) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			e.stats.Errors.Add(1)
			continue
		}
		if e.stats.Sessions.Add(1) > e.current().Limits.Sessions {
			e.stats.Sessions.Add(-1)
			e.stats.Rejected.Add(1)
			conn.Close()
			continue
		}
		endpoint := resource.settings.Load()
		// Accepted carriers share listener sockets. Conversation ownership protects peer flags, not ownership of that socket.
		owner := conn.(*kcp.Conn).UDPSession.GetConv()
		e.log().Debug("session.accepted", "conv", owner, "remote", conn.RemoteAddr().String(), "listener", endpoint.Address)
		listener.RegisterClient(conn.RemoteAddr(), owner)
		e.launch(func() {
			defer e.stats.Sessions.Add(-1)
			defer conn.Close()
			e.addEndpoint(conn.(*kcp.Conn), &resource.settings)
			defer listener.DeleteClientSession(conn.RemoteAddr(), owner)
			stop := context.AfterFunc(ctx, func() { conn.Close() })
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
				e.launch(func() { defer e.stats.Active.Add(-1); defer strm.Close(); e.handle(ctx, listener, strm, owner) })
			}
		})
	}
}

// handle validates one inner request, acknowledges transport receipt, dials the target and
// reports its actual opening outcome.
func (e *Engine) handle(ctx context.Context, listener tnet.Listener, strm tnet.Strm, owner uint32) {
	trace := e.flowTrace()
	strm.SetDeadline(time.Now().Add(e.current().Limits.OpenDuration))
	var p protocol.Proto
	if err := p.Read(strm); err != nil {
		e.report(fmt.Errorf("read stream control: %w", err))
		return
	}
	if e.traceFlow(trace) {
		e.log().Debug("flow.control", "flow_id", trace, "conv", owner, "stream_id", strm.SID(), "protocol", p.Type, "source", strm.RemoteAddr().String())
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
	// The receipt status proves this carrier delivered the request before a potentially slow target dial.
	if err := writeOpeningAck(strm, 2); err != nil {
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, e.current().Limits.DialDuration)
	proto := "tcp"
	if p.Type == protocol.PUDP2 {
		proto = "udp"
	}
	dialer := net.Dialer{KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(dialCtx, proto, p.Addr.String())
	cancel()
	if err != nil {
		e.log().Debug("flow.target_failed", "flow_id", trace, "conv", owner, "stream_id", strm.SID(), "target", p.Addr.String(), "error", err)
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
		e.relayContext(ctx, conn.(*net.TCPConn), strm, trace)
	} else {
		e.relayUDP(conn.(*net.UDPConn), strm)
	}
}

// writeOpeningAck sends the one-byte receipt/success/failure status with control priority and
// rejects partial writes.
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

// serveMetrics runs an already staged loopback bind. Its resource context closes
// HTTP connections on replacement; profiling availability follows live settings.
func (e *Engine) serveMetrics(ctx context.Context, listener net.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
		if !e.current().Profiling {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/debug/pprof/profile" {
			pprof.Profile(w, r)
		} else {
			pprof.Index(w, r)
		}
	})
	mux.HandleFunc("/metrics", e.metrics)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if e.ctx.Err() != nil || e.degraded.Load() {
			w.WriteHeader(503)
		} else {
			io.WriteString(w, "ok\n")
		}
	})
	s := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	defer s.Close()
	if err := s.Serve(listener); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
		e.log().Error("metrics.stopped", "error", err)
	}
}
