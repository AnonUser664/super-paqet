//go:build linux

// File reload.go: reconciles immutable config generations with separately owned
// endpoint resources. One publication changes route selection; expensive binding,
// firewall work and affected-carrier teardown never hold a packet-path lock.
package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"runtime/debug"
	"sort"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
	"paqet/internal/tnet/kcp"
)

// runtimeView is immutable after publication, pairing each forward with its pool
// so an accept cannot observe a new target with an old peer generation.
type runtimeView struct {
	// Effective configuration for admission/deadlines and diagnostics.
	cfg *Config
	// Named pools selected by this generation.
	peers map[string]*peer
	// Bind-keyed routing decisions; existing streams retain their captured binding.
	routes map[string]routeBinding
}

// routeBinding pins the forward target and peer generation for a new TCP/UDP flow.
type routeBinding struct {
	// Application routing contract captured at acceptance.
	forward Forward
	// Pool carrying this flow until completion or explicit endpoint replacement.
	peer *peer
}

// resourceSpec describes one bind/pool without sharing mutable lifetime state.
type resourceSpec struct {
	// Resource kind and stable identity, independent of slice ordering.
	kind, key string
	// Prepared transport settings for peer/listener resources.
	endpoint Endpoint
	// Local socket identity for a forward; routing is read from runtimeView.
	forward Forward
	// Optional local metrics bind.
	address string
	// Generation-owned firewall policy.
	firewall bool
}

// liveResource owns exactly one pool or bind and its firewall journal. Resources
// are prepared dormant, activated only after the full transaction can commit.
type liveResource struct {
	// Immutable specification used for matching and rollback.
	spec resourceSpec
	// Live endpoint template shared with future dial/accept and tuner registration.
	settings atomic.Pointer[Endpoint]
	// Incoming listener sockets used for current telemetry and admission updates.
	listener *kcp.Listener
	// Outgoing pool, when this resource is a peer.
	peer *peer
	// Endpoint-owned rule state; slots use the pool's journal for later growth.
	fw *firewall
	// Starts accept loops once; failed staged generations are never activated.
	start func()
	// Releases sockets/guards and cancels accepted work before rule removal.
	release func()
	// Prevents repeated socket teardown while allowing failed rule cleanup retries.
	released bool
	// Optional post-rule teardown keeps a retired recovery source reserved if
	// firewall cleanup fails; another application must not inherit its rules.
	finalize func()
}

// current reads settings without a lock on the admission and relay paths; the
// fallback permits existing unit fixtures to construct small engines directly.
func (e *Engine) current() *Config {
	if v := e.view.Load(); v != nil {
		return v.cfg
	}
	return e.cfg
}

// route captures one coherent binding, never returning a mutable map to callers.
func (e *Engine) route(key string) (routeBinding, bool) {
	v := e.view.Load()
	if v == nil {
		return routeBinding{}, false
	}
	binding, ok := v.routes[key]
	return binding, ok && binding.peer != nil
}

// firewallEnabled resolves the public nil-means-enabled default.
func firewallEnabled(c *Config) bool { return c.Firewall == nil || *c.Firewall }

// forwardKey separates TCP and UDP binds while permitting both on the same port.
func forwardKey(f Forward) string { return "forward/" + f.Protocol + "/" + f.Listen }

// specifications removes ordering as a lifecycle concern. Forward target edits
// reuse their listening socket and affect only future flow admissions.
func specifications(c *Config) map[string]resourceSpec {
	out := make(map[string]resourceSpec)
	for name, endpoint := range c.Peers {
		key := "peer/" + name
		out[key] = resourceSpec{kind: "peer", key: key, endpoint: endpoint, firewall: firewallEnabled(c)}
	}
	for _, endpoint := range c.Listeners {
		key := "listener/" + endpoint.Address
		out[key] = resourceSpec{kind: "listener", key: key, endpoint: endpoint, firewall: firewallEnabled(c)}
	}
	for _, f := range c.Forwards {
		key := forwardKey(f)
		out[key] = resourceSpec{kind: "forward", key: key, forward: Forward{Listen: f.Listen, Protocol: f.Protocol}}
	}
	if c.Metrics != "" {
		out["metrics"] = resourceSpec{kind: "metrics", key: "metrics", address: c.Metrics}
	}
	return out
}

// sameResource compares effective endpoint values, ignoring newly constructed
// cipher objects and key-env indirection after their effective key is resolved.
func sameResource(a, b resourceSpec) bool {
	a.endpoint.KCP.Block, b.endpoint.KCP.Block = nil, nil
	a.endpoint.KeyEnv, b.endpoint.KeyEnv = "", ""
	// Preparation has already resolved these aliases/overrides. Cosmetic changes
	// must not rebuild a healthy path when the effective contract is identical.
	a.endpoint.Enc, b.endpoint.Enc = "", ""
	a.endpoint.KCP.Enc, b.endpoint.KCP.Enc = "", ""
	a.endpoint.KCP.AdaptiveBuffersOverride, b.endpoint.KCP.AdaptiveBuffersOverride = nil, nil
	if enabledFlag(a.endpoint.Adaptive) == enabledFlag(b.endpoint.Adaptive) {
		a.endpoint.Adaptive = b.endpoint.Adaptive
	}
	if enabledFlag(a.endpoint.KCP.CreditHints) == enabledFlag(b.endpoint.KCP.CreditHints) {
		a.endpoint.KCP.CreditHints = b.endpoint.KCP.CreditHints
	}
	if enabledFlag(a.endpoint.KCP.ACKTimestamps) == enabledFlag(b.endpoint.KCP.ACKTimestamps) {
		a.endpoint.KCP.ACKTimestamps = b.endpoint.KCP.ACKTimestamps
	}
	if a.firewall == b.firewall {
		a.endpoint.Network.IPv4.Addr_, b.endpoint.Network.IPv4.Addr_ = "", ""
		a.endpoint.Network.IPv6.Addr_, b.endpoint.Network.IPv6.Addr_ = "", ""
		a.endpoint.Network.IPv4.RouterMac_, b.endpoint.Network.IPv4.RouterMac_ = "", ""
		a.endpoint.Network.IPv6.RouterMac_, b.endpoint.Network.IPv6.RouterMac_ = "", ""
	}
	return reflect.DeepEqual(a, b)
}

// close first stops the owning generation, then removes only its recorded rules.
// Rule failures retain the resource for another cleanup attempt.
func (r *liveResource) close() error {
	if !r.released {
		r.released = true
		r.release()
	}
	var slotErr error
	if r.peer != nil {
		slotErr = r.peer.closeSlotRules()
		r.peer.allocationMu.Lock()
		defer r.peer.allocationMu.Unlock()
	}
	if r.fw != nil {
		slotErr = errors.Join(slotErr, r.fw.close())
	}
	if slotErr == nil && r.finalize != nil {
		r.finalize()
		r.finalize = nil
	}
	return slotErr
}

// buildResource reserves resources without admitting traffic. Each failure
// closes only its own provisional handles and retains failed cleanup intent.
func (e *Engine) buildResource(spec resourceSpec) (*liveResource, error) {
	if e.resourceFactory != nil {
		return e.resourceFactory(spec)
	}
	return e.prepareResource(spec)
}

// prepareResource implements real socket, guard and firewall ownership; the
// wrapper above allows deterministic allocation-failure tests without root.
func (e *Engine) prepareResource(spec resourceSpec) (_ *liveResource, err error) {
	r := &liveResource{spec: spec, release: func() {}, start: func() {}}
	defer func() {
		if err != nil {
			if cleanupErr := r.close(); cleanupErr != nil {
				e.retired = append(e.retired, r)
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	switch spec.kind {
	case "peer":
		r.settings.Store(&spec.endpoint)
		ctx, cancel := context.WithCancel(e.ctx)
		p := &peer{engine: e, ctx: ctx, cancel: cancel, endpoint: spec.endpoint, settings: &r.settings, manageFirewall: spec.firewall}
		r.peer, r.fw, r.release = p, &p.fw, p.close
		for i := 0; i < spec.endpoint.Sessions; i++ {
			s, allocErr := p.allocateSlot(e.ctx)
			if allocErr != nil {
				return nil, allocErr
			}
			p.slots = append(p.slots, s)
		}
	case "listener":
		r.settings.Store(&spec.endpoint)
		guard, network, reserveErr := reserve(spec.endpoint.Network)
		if reserveErr != nil {
			return nil, reserveErr
		}
		r.fw = &firewall{}
		r.release = func() { guard.Close() }
		if spec.firewall {
			if err = r.fw.add(&network); err != nil {
				return nil, err
			}
		}
		cfg := spec.endpoint.KCP
		cfg.MaxSessions = int(e.current().Limits.Sessions)
		listener, listenErr := kcp.Listen(&cfg, network)
		if listenErr != nil {
			return nil, listenErr
		}
		ctx, cancel := context.WithCancel(e.ctx)
		r.release = func() { cancel(); listener.Close(); guard.Close() }
		r.start = func() { e.launch(func() { e.serve(ctx, listener, r) }) }
		// Listener packet observers are rebuilt from live resources at commit.
		r.listener = listener.(*kcp.Listener)
	case "forward":
		ctx, cancel := context.WithCancel(e.ctx)
		if spec.forward.Protocol == "udp" {
			var conn *net.UDPConn
			addr, resolveErr := net.ResolveUDPAddr("udp", spec.forward.Listen)
			if resolveErr != nil {
				cancel()
				return nil, resolveErr
			}
			conn, err = net.ListenUDP("udp", addr)
			if err != nil {
				cancel()
				return nil, err
			}
			r.release = func() { cancel(); conn.Close() }
			r.start = func() { e.serveUDP(ctx, conn, spec.key) }
		} else {
			addr, resolveErr := net.ResolveTCPAddr("tcp", spec.forward.Listen)
			if resolveErr != nil {
				cancel()
				return nil, resolveErr
			}
			conn, listenErr := net.ListenTCP("tcp", addr)
			if listenErr != nil {
				cancel()
				return nil, listenErr
			}
			r.release = func() { cancel(); conn.Close() }
			r.start = func() { e.launch(func() { e.forward(conn, spec.key) }) }
		}
	case "metrics":
		listener, listenErr := net.Listen("tcp", spec.address)
		if listenErr != nil {
			return nil, listenErr
		}
		// Closing the server also closes idle/active HTTP sockets on metrics edits.
		ctx, cancel := context.WithCancel(e.ctx)
		r.release = func() { cancel(); listener.Close() }
		r.start = func() { e.launch(func() { e.serveMetrics(ctx, listener) }) }
	default:
		return nil, fmt.Errorf("unknown runtime resource kind")
	}
	return r, nil
}

// publish installs all route choices in one atomic operation and rebuilds the
// small listener telemetry registry without retaining removed generations.
func (e *Engine) publish(c *Config) {
	v := &runtimeView{cfg: c, peers: make(map[string]*peer), routes: make(map[string]routeBinding)}
	for name := range c.Peers {
		if r := e.resources["peer/"+name]; r != nil {
			v.peers[name] = r.peer
		}
	}
	for _, f := range c.Forwards {
		v.routes[forwardKey(f)] = routeBinding{f, v.peers[f.Peer]}
	}
	e.view.Store(v)
	e.tuneMu.Lock()
	e.packetObservers = nil
	keys := make([]string, 0, len(e.resources))
	for key := range e.resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	index := 0
	for _, key := range keys {
		r := e.resources[key]
		if r.listener != nil {
			for worker, packet := range r.listener.PacketConnections() {
				e.packetObservers = append(e.packetObservers, observedPacket{index, worker, packet})
			}
			index++
		}
	}
	e.tuneMu.Unlock()
}

// apply stages every nonconflicting change first. Only explicit replacements
// sharing an occupied bind require a break-before-make phase; failed replacements
// reconstruct the old affected resources while unrelated resources remain live.
func (e *Engine) apply(c *Config) (err error) {
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if e.resources == nil {
		e.resources = make(map[string]*liveResource)
	}
	// Raise descriptor capacity before staging potentially large pools. Failure
	// leaves all existing routes intact and no provisional resources to clean.
	if err = ensureFileLimit(c.Limits.Connections); err != nil {
		return err
	}
	var fileLimit unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_NOFILE, &fileLimit) == nil && fileLimit.Cur < uint64(c.Limits.Connections*2+4096) {
		e.log().Warn("config.file_limit", "files", fileLimit.Cur, "required", c.Limits.Connections*2+4096)
	}
	previous := e.current()
	initial := e.view.Load() == nil
	wanted := specifications(c)
	staged := make(map[string]*liveResource)
	stopped := make(map[string]*liveResource)
	retiring := make(map[string]*liveResource)
	deferred := make(map[string]resourceSpec)
	liveUpdates := make(map[string]*liveResource)
	for key, old := range e.resources {
		if spec, ok := wanted[key]; !ok || !sameResource(old.spec, spec) && !canUpdateReliability(old.spec, spec) {
			retiring[key] = old
		}
	}
	defer func() {
		if err == nil {
			return
		}
		for _, r := range staged {
			if cleanupErr := r.close(); cleanupErr != nil {
				e.retired = append(e.retired, r)
				err = errors.Join(err, cleanupErr)
			}
		}
		// Reopening cannot restore interrupted streams; it restores their old routes.
		for key, old := range stopped {
			replacement, restoreErr := e.buildResource(old.spec)
			if restoreErr != nil {
				e.degraded.Store(true)
				e.log().Error("config.rollback_failed", "resource", key, "error", restoreErr)
				delete(e.resources, key)
				err = errors.Join(err, fmt.Errorf("restore %s: %w", key, restoreErr))
			} else {
				e.resources[key] = replacement
				replacement.start()
			}
		}
		if !initial {
			e.publish(previous)
		}
	}()
	keys := make([]string, 0, len(wanted))
	for key := range wanted {
		keys = append(keys, key)
	}
	// Stage rule-owning transport resources before local admission sockets;
	// a later bind failure must exercise and unwind provisional firewall state.
	rank := map[string]int{"listener": 0, "peer": 1, "forward": 2, "metrics": 3}
	sort.Slice(keys, func(i, j int) bool {
		a, b := rank[wanted[keys[i]].kind], rank[wanted[keys[j]].kind]
		if a != b {
			return a < b
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		spec := wanted[key]
		if old := e.resources[key]; old != nil {
			if sameResource(old.spec, spec) {
				continue
			}
			if canUpdateReliability(old.spec, spec) {
				liveUpdates[key] = old
				continue
			}
		}
		r, buildErr := e.buildResource(spec)
		if buildErr != nil {
			if errors.Is(buildErr, syscall.EADDRINUSE) && conflictsWithRetiring(spec, retiring) {
				deferred[key] = spec
				continue
			}
			return fmt.Errorf("prepare %s: %w", key, buildErr)
		}
		staged[key] = r
	}
	if len(deferred) > 0 {
		e.log().Warn("config.replacing", "affected_resources", sortedResourceKeys(retiring), "reason", "occupied bind requires scoped restart")
		for key, old := range retiring {
			stopped[key] = old
			if closeErr := old.close(); closeErr != nil {
				e.retired = append(e.retired, old)
				return fmt.Errorf("release %s: %w", key, closeErr)
			}
			delete(e.resources, key)
		}
		for _, key := range keys {
			if spec, ok := deferred[key]; ok {
				r, buildErr := e.buildResource(spec)
				if buildErr != nil {
					return fmt.Errorf("replace %s: %w", key, buildErr)
				}
				staged[key] = r
			}
		}
	}
	for key, r := range staged {
		e.resources[key] = r
	}
	// Remove retired binds from the registry before publication. Their handles
	// remain owned until closure immediately after the new routes are visible.
	for key := range retiring {
		if _, replaced := staged[key]; !replaced {
			delete(e.resources, key)
		}
	}
	// All failure-prone work is complete before any live transport setters run.
	// Registration uses the same tuner lock, so a concurrently accepted/dialed
	// carrier receives the latest template even if its socket opened earlier.
	e.tuneMu.Lock()
	for key, resource := range liveUpdates {
		spec := wanted[key]
		resource.spec = spec
		resource.settings.Store(&spec.endpoint)
		if resource.listener != nil {
			cfg := spec.endpoint.KCP
			resource.listener.ConfigureReliability(&cfg)
		}
		for conn, controller := range e.tuners {
			if controller.endpoint == &resource.settings && !conn.Session.IsClosed() {
				kcp.ReconfigureReliability(conn.UDPSession, &spec.endpoint.KCP)
				controller.ackScheduleBudget = reliabilityACKBudget(spec.endpoint.KCP)
			}
		}
	}
	e.tuneMu.Unlock()
	e.publish(c)
	if e.diagnostics != nil {
		e.diagnostics.configure(c.Log)
	}
	if c.Limits.MemoryMiB == 0 {
		debug.SetMemoryLimit(math.MaxInt64)
	} else {
		debug.SetMemoryLimit(c.Limits.MemoryMiB << 20)
	}
	select {
	case e.observeChanged <- struct{}{}:
	default:
	}
	for _, r := range staged {
		r.start()
	}
	for key, r := range retiring {
		if cleanupErr := r.close(); cleanupErr != nil {
			// Recovery journals remain on disk. Keep retry ownership in-process too.
			e.retired = append(e.retired, r)
			e.log().Error("config.cleanup_failed", "resource", key, "error", cleanupErr)
		}
	}
	kept := e.retired[:0]
	for _, r := range e.retired {
		if r.close() != nil {
			kept = append(kept, r)
		}
	}
	e.retired = kept
	for _, r := range e.resources {
		if r.listener != nil {
			r.listener.SetMaxSessions(int(c.Limits.Sessions))
		}
	}
	e.degraded.Store(false)
	revision := e.revision.Add(1)
	if !initial {
		e.reloadApplied.Add(1)
	}
	e.log().Info("config.applied", "revision", revision, "changed_resources", sortedResourceKeys(staged), "updated_resources", sortedResourceKeys(liveUpdates), "retired_resources", sortedResourceKeys(retiring), "interrupted_resources", sortedResourceKeys(stopped), "peers", len(c.Peers), "forwards", len(c.Forwards), "listeners", len(c.Listeners))
	return nil
}

// sortedResourceKeys emits deterministic identities, never keys or cipher state.
func sortedResourceKeys(resources map[string]*liveResource) []string {
	keys := make([]string, 0, len(resources))
	for key := range resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ensureFileLimit requests descriptor capacity without lowering the existing
// hard/soft limits; the configured application admission limit stays independent.
func ensureFileLimit(connections int64) error {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}
	required := uint64(connections*2 + 4096)
	if limit.Cur < required {
		limit.Cur = min(required, limit.Max)
		if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
			return fmt.Errorf("raise file limit: %w", err)
		}
	}
	return nil
}

// localBind describes the kernel reservation that could prevent replacement;
// UDP and TCP can share a numeric port without requiring disruption.
type localBind struct {
	// Socket protocol and actual address/port held by this resource.
	protocol string
	addr     *net.UDPAddr
}

// specBinds describes prospective fixed binds; random peer sources have no
// replacement conflict because reserve chooses another free port.
func specBinds(s resourceSpec) []localBind {
	if s.kind == "peer" || s.kind == "listener" {
		ports := s.endpoint.SourcePorts
		if len(ports) == 0 && s.endpoint.Network.Port != 0 {
			ports = []int{s.endpoint.Network.Port}
		}
		if len(ports) == 0 {
			return nil
		}
		var out []localBind
		for _, port := range ports {
			for _, addr := range []*net.UDPAddr{s.endpoint.Network.IPv4.Addr, s.endpoint.Network.IPv6.Addr} {
				if addr != nil {
					copy := *addr
					copy.Port = port
					out = append(out, localBind{"tcp", &copy})
				}
			}
		}
		return out
	}
	protocol, address := "tcp", s.address
	if s.kind == "forward" {
		protocol, address = s.forward.Protocol, s.forward.Listen
	}
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil
	}
	return []localBind{{protocol, addr}}
}

// conflictsWithRetiring distinguishes an owned replacement from an externally
// occupied new bind. An unrelated occupied addition never tears down live paths.
func conflictsWithRetiring(want resourceSpec, retiring map[string]*liveResource) bool {
	for _, requested := range specBinds(want) {
		for _, old := range retiring {
			occupied := specBinds(old.spec)
			if old.peer != nil {
				old.peer.mu.RLock()
				for _, slot := range old.peer.slots {
					for _, addr := range []*net.UDPAddr{slot.network.IPv4.Addr, slot.network.IPv6.Addr} {
						if addr != nil {
							occupied = append(occupied, localBind{"tcp", addr})
						}
					}
				}
				old.peer.mu.RUnlock()
			}
			for _, held := range occupied {
				if requested.protocol == held.protocol && requested.addr.Port == held.addr.Port && (requested.addr.IP.Equal(held.addr.IP) || len(requested.addr.IP) == 0 || len(held.addr.IP) == 0 || requested.addr.IP.IsUnspecified() || held.addr.IP.IsUnspecified()) {
					return true
				}
			}
		}
	}
	return false
}

// canUpdateReliability explicitly whitelists runtime setters. Every other
// endpoint difference requires replacement, including any wire/mux contract.
func canUpdateReliability(a, b resourceSpec) bool {
	if a.kind != "peer" && a.kind != "listener" {
		return false
	}
	k, n := &a.endpoint.KCP, b.endpoint.KCP
	k.Mode, k.NoDelay, k.Interval, k.Resend, k.NoCongestion = n.Mode, n.NoDelay, n.Interval, n.Resend, n.NoCongestion
	k.WDelay, k.AckNoDelay = n.WDelay, n.AckNoDelay
	k.WriteBatchMS, k.ACKDelayMaxMS = n.WriteBatchMS, n.ACKDelayMaxMS
	k.SmallWriteFlush = n.SmallWriteFlush
	return sameResource(a, b)
}

// enabledFlag resolves the common nil-means-enabled endpoint option contract.
func enabledFlag(flag *bool) bool { return flag == nil || *flag }
