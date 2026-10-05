//go:build linux

// File reload_test.go: deterministically exercises transaction boundaries and
// content watching without root, reserving real raw-socket checks for netns tests.
package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"paqet/internal/conf"
	"paqet/internal/tnet/kcp"
)

// reloadFixture owns a small engine and restores process-wide memory settings.
func reloadFixture(t *testing.T) *Engine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cfg := &Config{Limits: Limits{Connections: 10, Sessions: 5}, Peers: map[string]Endpoint{}}
	if err := cfg.Log.prepare(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Reload.prepare(); err != nil {
		t.Fatal(err)
	}
	oldMemory := debug.SetMemoryLimit(-1)
	e := &Engine{cfg: cfg, ctx: ctx, cancel: cancel, resources: map[string]*liveResource{}, tuners: map[*kcp.Conn]*controller{}, observeChanged: make(chan struct{}, 1)}
	e.resourceFactory = func(s resourceSpec) (*liveResource, error) {
		p := &peer{engine: e, endpoint: s.endpoint}
		return &liveResource{spec: s, peer: p, start: func() {}, release: p.close}, nil
	}
	t.Cleanup(func() {
		cancel()
		if err := e.close(); err != nil {
			t.Error(err)
		}
		e.wg.Wait()
		debug.SetMemoryLimit(oldMemory)
	})
	return e
}

// fixtureConfig copies maps/slices so tests never mutate a published generation.
func fixtureConfig(c *Config) *Config {
	next := *c
	next.Peers = make(map[string]Endpoint)
	for name, endpoint := range c.Peers {
		next.Peers[name] = endpoint
	}
	next.Forwards = append([]Forward(nil), c.Forwards...)
	next.Listeners = append([]Endpoint(nil), c.Listeners...)
	return &next
}

// TestReloadPreservesUnchangedPoolsAndBinds verifies additive edits and target
// changes retain identity while an endpoint edit retires only that endpoint.
func TestReloadPreservesUnchangedPoolsAndBinds(t *testing.T) {
	e := reloadFixture(t)
	c := fixtureConfig(e.cfg)
	c.Peers = map[string]Endpoint{"a": {Address: "192.0.2.1:29999"}, "b": {Address: "192.0.2.2:29999"}}
	c.Forwards = []Forward{{Listen: "127.0.0.1:9001", Protocol: "tcp", Peer: "a", Target: "localhost:80"}}
	if err := e.apply(c); err != nil {
		t.Fatal(err)
	}
	a, b, bind := e.resources["peer/a"], e.resources["peer/b"], e.resources[forwardKey(c.Forwards[0])]
	captured, _ := e.route(forwardKey(c.Forwards[0]))
	next := fixtureConfig(c)
	next.Peers["c"] = Endpoint{Address: "192.0.2.3:29999"}
	next.Forwards[0].Target = "localhost:443"
	next.Forwards = append(next.Forwards, Forward{Listen: "127.0.0.1:9003", Protocol: "udp", Peer: "c", Target: "localhost:53"})
	if err := e.apply(next); err != nil {
		t.Fatal(err)
	}
	if e.resources["peer/a"] != a || e.resources["peer/b"] != b || e.resources[forwardKey(c.Forwards[0])] != bind || a.released {
		t.Fatal("additive edit rebuilt live resources")
	}
	if captured.forward.Target != "localhost:80" {
		t.Fatal("existing binding mutated")
	}
	current, _ := e.route(forwardKey(c.Forwards[0]))
	if current.forward.Target != "localhost:443" {
		t.Fatal("future route was not updated")
	}
	changed := fixtureConfig(next)
	endpoint := changed.Peers["a"]
	endpoint.KCP.Resend = 1
	changed.Peers["a"] = endpoint
	if err := e.apply(changed); err != nil {
		t.Fatal(err)
	}
	if !a.released || b.released || e.resources["peer/b"] != b || e.resources[forwardKey(c.Forwards[0])] != bind {
		t.Fatal("endpoint edit escaped its scope")
	}
	removed := fixtureConfig(changed)
	removed.Forwards = nil
	delete(removed.Peers, "c")
	if err := e.apply(removed); err != nil {
		t.Fatal(err)
	}
	if !bind.released || e.resources["peer/b"] != b {
		t.Fatal("route removal leaked a bind or closed unrelated peer")
	}
}

// TestReloadFailedAdditionDoesNotInterruptRetiringResources guards against an
// external busy port being mistaken for an owned replacement conflict.
func TestReloadFailedAdditionDoesNotInterruptRetiringResources(t *testing.T) {
	e := reloadFixture(t)
	c := fixtureConfig(e.cfg)
	c.Peers["a"] = Endpoint{Address: "192.0.2.1:29999"}
	if err := e.apply(c); err != nil {
		t.Fatal(err)
	}
	old := e.resources["peer/a"]
	next := fixtureConfig(c)
	next.Peers["b"] = Endpoint{Address: "192.0.2.2:29999"}
	delete(next.Peers, "a")
	next.Forwards = []Forward{{Listen: "127.0.0.1:9002", Protocol: "tcp", Peer: "b", Target: "localhost:80"}}
	original := e.resourceFactory
	e.resourceFactory = func(s resourceSpec) (*liveResource, error) {
		if s.kind == "forward" {
			return nil, syscall.EADDRINUSE
		}
		return original(s)
	}
	if err := e.apply(next); !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("expected busy bind: %v", err)
	}
	if old.released || e.current() != c || e.resources["peer/a"] != old || e.revision.Load() != 1 {
		t.Fatal("failed addition changed running generation")
	}
}

// TestReloadReplacementRollbackRestoresOldRoutes injects failure after a fixed
// port is released, proving old settings return while another pool stays intact.
func TestReloadReplacementRollbackRestoresOldRoutes(t *testing.T) {
	for _, failRestore := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "degraded"}[failRestore], func(t *testing.T) {
			e := reloadFixture(t)
			c := fixtureConfig(e.cfg)
			n := conf.Network{Port: 29998, IPv4: conf.Addr{Addr: &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 29998}}}
			c.Peers = map[string]Endpoint{"a": {Address: "192.0.2.1:29999", Network: n}, "b": {Address: "192.0.2.2:29999"}}
			if err := e.apply(c); err != nil {
				t.Fatal(err)
			}
			old, unrelated := e.resources["peer/a"], e.resources["peer/b"]
			next := fixtureConfig(c)
			endpoint := next.Peers["a"]
			endpoint.KCP.MTU = 128
			next.Peers["a"] = endpoint
			original := e.resourceFactory
			e.resourceFactory = func(s resourceSpec) (*liveResource, error) {
				if s.key == "peer/a" {
					if !old.released {
						return nil, syscall.EADDRINUSE
					}
					if s.endpoint.KCP.MTU == 128 || failRestore {
						return nil, syscall.ENOMEM
					}
				}
				return original(s)
			}
			if err := e.apply(next); !errors.Is(err, syscall.ENOMEM) {
				t.Fatalf("missing injected failure: %v", err)
			}
			if e.current() != c || unrelated.released || e.resources["peer/b"] != unrelated || e.revision.Load() != 1 {
				t.Fatal("rollback changed unrelated routes/config")
			}
			if e.degraded.Load() != failRestore {
				t.Fatal("rollback health does not reflect restoration outcome")
			}
			if !failRestore && (e.resources["peer/a"] == old || e.resources["peer/a"].spec.endpoint.KCP.MTU != 0) {
				t.Fatal("old endpoint was not rebuilt")
			}
		})
	}
}

// TestReloadConcurrentRouteReaders never permits a forward to mix its target
// with a pool belonging to another generation while hundreds of edits commit.
func TestReloadConcurrentRouteReaders(t *testing.T) {
	e := reloadFixture(t)
	c := fixtureConfig(e.cfg)
	c.Peers["a"] = Endpoint{Address: "192.0.2.1:29999", KCP: conf.KCP{MTU: 100}}
	c.Forwards = []Forward{{Listen: "127.0.0.1:9001", Protocol: "tcp", Peer: "a", Target: "old:80"}}
	if err := e.apply(c); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	done := make(chan struct{})
	var invalid atomic.Bool
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				binding, ok := e.route(forwardKey(c.Forwards[0]))
				if !ok || (binding.forward.Target == "old:80") != (binding.peer.endpoint.KCP.MTU == 100) {
					invalid.Store(true)
				}
			}
		}()
	}
	for i := 0; i < 100; i++ {
		next := fixtureConfig(c)
		if i%2 == 0 {
			endpoint := next.Peers["a"]
			endpoint.KCP.MTU = 128
			next.Peers["a"] = endpoint
			next.Forwards[0].Target = "new:80"
		}
		if err := e.apply(next); err != nil {
			t.Fatal(err)
		}
	}
	close(done)
	wg.Wait()
	if invalid.Load() {
		t.Fatal("route reader observed mixed generations")
	}
}

// TestReloadLogAndLimitsAreLive checks dynamic diagnostics and admission without
// touching the unchanged pool; lowering a limit never kills admitted work.
func TestReloadLogAndLimitsAreLive(t *testing.T) {
	e := reloadFixture(t)
	d := newDiagnostics(e.cfg.Log, io.Discard)
	e.diagnostics = d
	t.Cleanup(d.close)
	c := fixtureConfig(e.cfg)
	c.Peers["a"] = Endpoint{Address: "192.0.2.1:29999"}
	if err := e.apply(c); err != nil {
		t.Fatal(err)
	}
	old := e.resources["peer/a"]
	e.stats.Active.Store(3)
	next := fixtureConfig(c)
	next.Limits.Connections = 2
	next.Log = LogConfig{Level: "debug", Format: "text", Interval: "100ms", FlowSample: 1}
	next.Log.prepare()
	if err := e.apply(next); err != nil {
		t.Fatal(err)
	}
	if e.acquire() || e.stats.Active.Load() != 3 || e.resources["peer/a"] != old || !e.log().Enabled(e.ctx, -4) {
		t.Fatal("dynamic settings restarted pools or evicted admitted work")
	}
	e.stats.Active.Store(0)
	if !e.acquire() {
		t.Fatal("lowered limit rejected all new work")
	}
	e.stats.Active.Add(-1)
}

// TestResourceComparisonIgnoresCipherObjectIdentity checks preparing the same
// key creates no transport restart solely because the cipher object is fresh.
func TestResourceComparisonIgnoresCipherObjectIdentity(t *testing.T) {
	a := resourceSpec{kind: "peer", endpoint: Endpoint{Key: "same", KeyEnv: "KEY", KCP: conf.KCP{Block_: "null"}}}
	b := a
	b.endpoint.KeyEnv = ""
	if !sameResource(a, b) {
		t.Fatal("resolved key indirection required restart")
	}
	b.endpoint.Key = "different"
	if sameResource(a, b) {
		t.Fatal("key rotation ignored")
	}
}

// waitReload bounds asynchronous checks so a stalled watcher fails explicitly.
func waitReload(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("reload watcher deadline")
}

// watcherYAML constructs a fully explicit null endpoint; preparation needs an
// Ethernet interface but no raw sockets, discovery or privileges.
func watcherYAML(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		if len(iface.HardwareAddr) == 6 {
			return "firewall: false\nreload: {interval: 50ms, debounce: 100ms}\npeers:\n  a:\n    address: 192.0.2.1:29999\n    enc: 'null'\n    sessions: 1\n    max_sessions: 1\n    network: {interface: " + iface.Name + ", ipv4: {addr: '192.0.2.10:0', router_mac: '02:00:00:00:00:01'}}\n"
		}
	}
	t.Skip("no Ethernet interface for strict preparation")
	return ""
}

// TestWatcherDebounceRenameInvalidAndRecovery exercises actual file reads and
// strict parsing: invalid/partial edits do not affect the published revision.
func TestWatcherDebounceRenameInvalidAndRecovery(t *testing.T) {
	e := reloadFixture(t)
	var output bytes.Buffer
	d := newDiagnostics(e.cfg.Log, &output)
	e.diagnostics = d
	t.Cleanup(d.close)
	initial := watcherYAML(t)
	c, err := parseConfig([]byte(initial))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.apply(c); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte(initial), 0600)
	e.launch(func() { e.watchConfig(path, []byte(initial)) })
	old := e.view.Load().peers["a"]
	os.WriteFile(path, []byte("key: SECRET-MUST-NOT-LOG\nunknown: true\n"), 0600)
	waitReload(t, func() bool { return e.reloadRejected.Load() > 0 })
	if e.revision.Load() != 1 || e.view.Load().peers["a"] != old {
		t.Fatal("invalid edit changed live endpoint")
	}
	next := initial + "limits: {connections: 1234}\n"
	tmp := path + ".tmp"
	os.WriteFile(tmp, []byte(next), 0600)
	if err = os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	waitReload(t, func() bool { return e.revision.Load() == 2 })
	if e.current().Limits.Connections != 1234 || e.view.Load().peers["a"] != old {
		t.Fatal("rename did not preserve unchanged pool")
	}
	os.Remove(path)
	waitReload(t, func() bool { return e.reloadRejected.Load() > 1 })
	os.WriteFile(path, []byte(initial), 0600)
	waitReload(t, func() bool { return e.revision.Load() == 3 })
	// Debounce suppresses a short-lived valid intermediate configuration.
	os.WriteFile(path, []byte(next), 0600)
	time.Sleep(60 * time.Millisecond)
	os.WriteFile(path, []byte(initial), 0600)
	time.Sleep(220 * time.Millisecond)
	if e.revision.Load() != 3 {
		t.Fatal("debounce applied an intermediate edit")
	}
	e.cancel()
	e.wg.Wait()
	d.close()
	if bytes.Contains(output.Bytes(), []byte("SECRET-MUST-NOT-LOG")) {
		t.Fatal("daemon logged raw YAML secret")
	}
}

// TestReloadConfigurationBounds rejects unbounded polling and oversized files.
func TestReloadConfigurationBounds(t *testing.T) {
	for _, r := range []ReloadConfig{{Interval: "1ms"}, {Interval: "2m"}, {Debounce: "-1s"}, {Debounce: "2m"}} {
		if r.prepare() == nil {
			t.Fatal("invalid polling budget accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "large.yaml")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(maxConfigBytes + 1)
	f.Close()
	if _, err := readConfigBytes(path); err == nil {
		t.Fatal("oversized configuration accepted")
	}
}
