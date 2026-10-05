//go:build linux

// File config.go: defines strict unified YAML and validates endpoint/resource contracts before
// startup; see docs/CONFIGURATION.md.

package engine

import (
	"fmt"
	"math"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/goccy/go-yaml"
	"paqet/internal/conf"
	"paqet/internal/tnet"
)

// Config supports listening and dialing from the same process.
type Config struct {
	// Incoming tunnel endpoints sharing this engine lifecycle.
	Listeners []Endpoint `yaml:"listeners"`
	// Named outgoing pools selected by local forward rules.
	Peers map[string]Endpoint `yaml:"peers"`
	// Local TCP/UDP binds with explicit peer and remote target.
	Forwards []Forward `yaml:"forwards"`
	// Process admission, memory and operation deadline configuration.
	Limits Limits `yaml:"limits"`
	// Nil enables owned-rule management; false delegates equivalent management externally.
	Firewall *bool `yaml:"firewall"`
	// Optional explicit loopback HTTP bind for liveness, counters and profiling.
	Metrics string `yaml:"metrics"`
	// Enables local pprof routes only when a metrics HTTP listener is configured.
	Profiling bool `yaml:"profiling"`
	// Structured diagnostic levels, cadence and lifecycle sampling.
	Log LogConfig `yaml:"log"`
	// File polling/debounce settings; explicit SIGHUP remains available when disabled.
	Reload ReloadConfig `yaml:"reload"`
}

// Endpoint describes either an incoming listener or an outgoing peer, with explicit
// driver/adaptation/ownership constraints.
type Endpoint struct {
	// Fixed incoming packet-worker count established before conversations are accepted.
	PacketWorkers int `yaml:"packet_workers"`
	// Nil enables endpoint adaptation; false preserves static link-control choices.
	Adaptive *bool `yaml:"adaptive"`
	// Remote peer or assigned local listener address, distinct from the forwarded application
	// target.
	Address string `yaml:"address"`
	// Shared secret supplied by configuration/environment, never diagnostic payload.
	Key string `yaml:"key"`
	// Environment variable supplying the endpoint key, mutually exclusive with an inline key.
	KeyEnv string `yaml:"key_env"`
	// Initial outgoing carrier count; application connection count is independently admitted.
	Sessions int `yaml:"sessions"`
	// Maximum outgoing pool size; fixed source ports require one carrier.
	MaxSessions int `yaml:"max_sessions"`
	// Optional ordered, distinct peer source ports: carrier i reserves port i.
	// This permits parallel carriers on paths requiring reproducible source ports.
	SourcePorts []int `yaml:"source_ports"`
	// Shares one peer raw socket/source tuple across independent KCP conversations.
	// Both peers and listeners must enable this; parity-only FEC is incompatible.
	SharedSource bool `yaml:"shared_source"`
	// Physical interface/source/next-hop metadata prepared before raw socket construction.
	Network conf.Network `yaml:"network"`
	// Reliability/cipher/mux settings for each carrier at this endpoint.
	KCP conf.KCP `yaml:"kcp"`
	// Configuration alias for cipher mode; precedence is resolved before transport construction.
	Enc string `yaml:"enc"`
}

// Forward binds a local protocol/address to one named peer and a target resolved at the
// accepting server.
type Forward struct {
	// Local application bind; it is not the remote tunnel listener address.
	Listen string `yaml:"listen"`
	// Name of the outgoing pool carrying this forward.
	Peer string `yaml:"peer"`
	// Host/port resolved and dialed by the accepting server.
	Target string `yaml:"target"`
	// Application TCP or length-framed UDP; the outer transport remains KCP/raw TCP.
	Protocol string `yaml:"protocol"`
}

// Limits bounds admission and opening/UDP lifetimes; the Go memory limit excludes kernel
// socket storage.
type Limits struct {
	// Admission limit for active handled streams/flows, not an upfront allocation.
	Connections int64 `yaml:"connections"`
	// Global accepted-carrier admission limit, separate from each outgoing pool.
	Sessions int64 `yaml:"sessions"`
	// Optional soft Go memory limit; kernel socket and other process memory are additional.
	MemoryMiB int64 `yaml:"memory_mib"`
	// Configured overall target-opening deadline string.
	OpenTimeout string `yaml:"open_timeout"`
	// Configured remote target-dial deadline, shorter than overall opening grace.
	DialTimeout string `yaml:"dial_timeout"`
	// Configured UDP inactivity deadline string.
	UDPIdle string `yaml:"udp_idle"`
	// Parsed opening duration used by client and server control handling.
	OpenDuration time.Duration `yaml:"-"`
	// Parsed target connection timeout used by the accepting server.
	DialDuration time.Duration `yaml:"-"`
	// Parsed UDP read/flow-expiry duration.
	UDPDuration time.Duration `yaml:"-"`
}

// Load strictly parses public YAML and prepares endpoint contracts before any forwarding
// socket is created.
func Load(path string) (*Config, error) {
	b, err := readConfigBytes(path)
	if err != nil {
		return nil, err
	}
	return parseConfig(b)
}

// parseConfig prepares the exact bytes observed by the watcher, avoiding a second
// file read racing with an editor's rename or partial write.
func parseConfig(b []byte) (*Config, error) {
	var c Config
	if err := yaml.UnmarshalWithOptions(b, &c, yaml.Strict()); err != nil {
		return nil, err
	}
	if err := c.prepare(); err != nil {
		return nil, err
	}
	return &c, nil
}

// prepare fills process defaults and rejects invalid forwards, limits and metrics exposure
// before network startup.
func (c *Config) prepare() error {
	if err := c.Reload.prepare(); err != nil {
		return err
	}
	if err := c.Log.prepare(); err != nil {
		return err
	}
	if len(c.Listeners)+len(c.Peers) == 0 {
		return fmt.Errorf("configure at least one listener or peer")
	}
	if c.Limits.Connections == 0 {
		c.Limits.Connections = 200000
	}
	if c.Limits.Sessions == 0 {
		c.Limits.Sessions = 1024
	}
	if c.Limits.OpenTimeout == "" {
		c.Limits.OpenTimeout = "10s"
	}
	if c.Limits.DialTimeout == "" {
		c.Limits.DialTimeout = "5s"
	}
	if c.Limits.UDPIdle == "" {
		c.Limits.UDPIdle = "60s"
	}
	var err error
	c.Limits.OpenDuration, err = time.ParseDuration(c.Limits.OpenTimeout)
	if err != nil || c.Limits.OpenDuration <= 0 {
		return fmt.Errorf("limits.open_timeout must be a positive duration")
	}
	c.Limits.DialDuration, err = time.ParseDuration(c.Limits.DialTimeout)
	if err != nil || c.Limits.DialDuration <= 0 || c.Limits.DialDuration >= c.Limits.OpenDuration {
		return fmt.Errorf("limits.dial_timeout must be positive and smaller than open_timeout")
	}
	c.Limits.UDPDuration, err = time.ParseDuration(c.Limits.UDPIdle)
	if err != nil || c.Limits.UDPDuration <= 0 {
		return fmt.Errorf("limits.udp_idle must be a positive duration")
	}
	if c.Limits.Connections < 1 || c.Limits.Connections > (int64(math.MaxInt)-4096)/2 ||
		c.Limits.Sessions < 1 || c.Limits.Sessions > int64(math.MaxInt) ||
		c.Limits.MemoryMiB < 0 || c.Limits.MemoryMiB > math.MaxInt64>>20 {
		return fmt.Errorf("invalid resource limits")
	}
	seen := map[string]bool{}
	for i := range c.Forwards {
		f := c.Forwards[i]
		if f.Protocol == "" {
			f.Protocol = "tcp"
			c.Forwards[i] = f
		}
		if _, ok := c.Peers[f.Peer]; !ok {
			return fmt.Errorf("forward %s refers to unknown peer %q", f.Listen, f.Peer)
		}
		if _, err := net.ResolveTCPAddr("tcp", f.Listen); err != nil {
			return fmt.Errorf("forward listen: %w", err)
		}
		if _, _, err := net.SplitHostPort(f.Listen); err != nil {
			return fmt.Errorf("forward listen: %w", err)
		}
		if _, err := tnet.NewAddr(f.Target); err != nil {
			return fmt.Errorf("forward target: %w", err)
		}
		if _, _, err := net.SplitHostPort(f.Target); err != nil {
			return fmt.Errorf("forward target: %w", err)
		}
		if f.Protocol != "" && f.Protocol != "tcp" && f.Protocol != "udp" {
			return fmt.Errorf("forward protocol must be tcp or udp")
		}
		key := f.Protocol + f.Listen
		if seen[key] {
			return fmt.Errorf("duplicate forward %s", f.Listen)
		}
		seen[key] = true
	}
	listenerAddresses := make(map[string]bool)
	for i := range c.Listeners {
		if listenerAddresses[c.Listeners[i].Address] {
			return fmt.Errorf("duplicate listener address")
		}
		listenerAddresses[c.Listeners[i].Address] = true
		if err := c.Listeners[i].prepare(true); err != nil {
			return fmt.Errorf("listeners[%d]: %w", i, err)
		}
	}
	for name, e := range c.Peers {
		if err := e.prepare(false); err != nil {
			return fmt.Errorf("peer %s: %w", name, err)
		}
		c.Peers[name] = e
	}
	if c.Metrics != "" {
		host, _, err := net.SplitHostPort(c.Metrics)
		if err != nil {
			return fmt.Errorf("metrics: %w", err)
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("metrics must bind to an explicit loopback address")
		}
	}
	return nil
}

// prepare combines endpoint overrides, discovery and transport defaults while enforcing
// worker/source-port/cipher compatibility.
func (e *Endpoint) prepare(listener bool) error {
	if err := e.prepareSourcePorts(listener); err != nil {
		return err
	}
	if e.KCP.Key != "" {
		return fmt.Errorf("configure key/key_env at the endpoint, not under kcp")
	}
	if !listener && e.PacketWorkers > 1 {
		return fmt.Errorf("packet_workers applies to listeners; use sessions for peer parallelism")
	}
	if e.Network.Backend == "" {
		e.Network.Backend = "packet"
	}
	if e.Network.Backend != "packet" && e.Network.Backend != "pcap" {
		return fmt.Errorf("network.backend must be packet or pcap")
	}
	if e.PacketWorkers == 0 {
		e.PacketWorkers = 1
		if listener && e.Network.Backend == "packet" {
			e.PacketWorkers = min(4, runtime.GOMAXPROCS(0))
		}
	}
	if e.PacketWorkers < 1 || e.PacketWorkers > 64 {
		return fmt.Errorf("packet_workers must be between 1 and 64")
	}
	if e.Network.Backend == "pcap" && e.PacketWorkers != 1 {
		return fmt.Errorf("pcap backend requires packet_workers: 1")
	}
	e.KCP.PacketWorkers = e.PacketWorkers
	e.KCP.SharedSource = e.SharedSource
	if e.SharedSource && (len(e.SourcePorts) > 0 || e.KCP.Dshard != 0 || e.KCP.Pshard != 0) {
		return fmt.Errorf("shared_source requires FEC disabled and no source_ports list")
	}
	a, err := net.ResolveUDPAddr("udp", e.Address)
	if err != nil {
		return err
	}
	if a.IP == nil || a.IP.IsUnspecified() || a.Port == 0 {
		return fmt.Errorf("endpoint address needs a concrete IP and nonzero port")
	}
	if e.Sessions == 0 {
		e.Sessions = min(8, max(2, runtime.GOMAXPROCS(0)))
	}
	if e.Sessions < 1 || e.Sessions > 256 {
		return fmt.Errorf("sessions must be between 1 and 256")
	}
	// Resolve aliases once: an explicit kcp.block wins, followed by endpoint enc, then kcp.enc.
	if e.Enc != "" && e.KCP.Block_ == "" {
		e.KCP.Block_ = e.Enc
	}
	if e.KCP.Enc != "" && e.KCP.Block_ == "" {
		e.KCP.Block_ = e.KCP.Enc
	}
	if e.KCP.Block_ == "" {
		e.KCP.Block_ = "aes-128-gcm"
	}
	noEnc := e.KCP.Block_ == "none" || e.KCP.Block_ == "null"
	if e.KeyEnv != "" {
		if e.Key != "" {
			return fmt.Errorf("set key or key_env, not both")
		}
		e.Key = os.Getenv(e.KeyEnv)
	}
	if !noEnc && e.Key == "" {
		return fmt.Errorf("key is required")
	}
	e.KCP.Key = e.Key
	// Both enterprise ends use directional EOF; ordinary upstream mux full-close semantics would truncate or hang opposite-direction work.
	e.KCP.HalfClose = true
	e.KCP.AdaptiveBuffers = e.Adaptive == nil || *e.Adaptive
	if e.KCP.AdaptiveBuffersOverride != nil {
		e.KCP.AdaptiveBuffers = *e.KCP.AdaptiveBuffersOverride
	}
	if e.KCP.Mode == "" {
		e.KCP.Mode = "fast3"
	}
	// Window limits bound memory; actual KCP/smux storage is allocated on demand.
	if e.KCP.Sndwnd == 0 {
		e.KCP.Sndwnd = 32768
	}
	if e.KCP.Rcvwnd == 0 {
		e.KCP.Rcvwnd = 32768
	}
	if e.KCP.Streambuf == 0 {
		e.KCP.Streambuf = 16 * 1024 * 1024
	}
	if e.KCP.Smuxbuf == 0 {
		e.KCP.Smuxbuf = 32 * 1024 * 1024
	}
	if e.KCP.Smuxktimeout_ == 0 {
		e.KCP.Smuxktimeout_ = 30
	}
	role := "client"
	if listener {
		role = "server"
	}
	if err := discover(&e.Network, a, listener); err != nil {
		return err
	}
	if err := conf.PrepareNetwork(&e.Network, role); err != nil {
		return err
	}
	if e.MaxSessions == 0 {
		e.MaxSessions = min(256, max(e.Sessions, runtime.GOMAXPROCS(0)*2))
		if (e.Network.Port != 0 && !e.SharedSource) || (e.Adaptive != nil && !*e.Adaptive) {
			e.MaxSessions = e.Sessions
		}
	}
	if e.MaxSessions < e.Sessions || e.MaxSessions > 256 {
		return fmt.Errorf("max_sessions must be sessions..256")
	}
	if listener && e.Network.Port != a.Port {
		return fmt.Errorf("network port must match listener port")
	}
	if len(e.SourcePorts) > 0 && e.Network.Port != 0 {
		return fmt.Errorf("source_ports requires a zero port in network address")
	}
	if !listener && e.Network.Port != 0 && !e.SharedSource && e.Sessions != 1 {
		return fmt.Errorf("a fixed source port requires sessions: 1")
	}
	if !listener && e.Network.Port != 0 && !e.SharedSource && e.MaxSessions != 1 {
		return fmt.Errorf("a fixed source port requires max_sessions: 1")
	}
	if e.KCP.MTU == 0 {
		e.KCP.MTU = min(1350, e.Network.Interface.MTU-80)
	}
	if e.KCP.MTU > e.Network.Interface.MTU-80 {
		return fmt.Errorf("KCP mtu must leave 80 bytes for outer IP/TCP headers within interface MTU %d", e.Network.Interface.MTU)
	}
	return conf.PrepareKCP(&e.KCP, role)
}

// prepareSourcePorts rejects ambiguous reservations before discovery. Explicit
// lists bound both initial and elastic pool size; unspecified ports retain the
// existing fixed-port or random-port behavior.
func (e *Endpoint) prepareSourcePorts(listener bool) error {
	if len(e.SourcePorts) == 0 {
		return nil
	}
	if listener || len(e.SourcePorts) > 256 {
		return fmt.Errorf("source_ports is a peer-only list of at most 256 ports")
	}
	seen := make(map[int]bool, len(e.SourcePorts))
	for _, port := range e.SourcePorts {
		if port < 1 || port > 65535 || seen[port] {
			return fmt.Errorf("source_ports must contain distinct ports in 1..65535")
		}
		seen[port] = true
	}
	if e.Sessions == 0 {
		e.Sessions = min(len(e.SourcePorts), min(8, max(2, runtime.GOMAXPROCS(0))))
	}
	if e.MaxSessions == 0 {
		e.MaxSessions = len(e.SourcePorts)
	}
	if e.Sessions > len(e.SourcePorts) || e.MaxSessions > len(e.SourcePorts) {
		return fmt.Errorf("sessions and max_sessions must fit source_ports")
	}
	return nil
}
