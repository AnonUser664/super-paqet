//go:build linux

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
	Listeners []Endpoint          `yaml:"listeners"`
	Peers     map[string]Endpoint `yaml:"peers"`
	Forwards  []Forward           `yaml:"forwards"`
	Limits    Limits              `yaml:"limits"`
	Firewall  *bool               `yaml:"firewall"`
	Metrics   string              `yaml:"metrics"`
	Profiling bool                `yaml:"profiling"`
	Log       LogConfig           `yaml:"log"`
}

type Endpoint struct {
	PacketWorkers int          `yaml:"packet_workers"`
	Adaptive      *bool        `yaml:"adaptive"`
	Address       string       `yaml:"address"`
	Key           string       `yaml:"key"`
	KeyEnv        string       `yaml:"key_env"`
	Sessions      int          `yaml:"sessions"`
	MaxSessions   int          `yaml:"max_sessions"`
	Network       conf.Network `yaml:"network"`
	KCP           conf.KCP     `yaml:"kcp"`
	Enc           string       `yaml:"enc"`
}

type Forward struct {
	Listen   string `yaml:"listen"`
	Peer     string `yaml:"peer"`
	Target   string `yaml:"target"`
	Protocol string `yaml:"protocol"`
}

type Limits struct {
	Connections  int64         `yaml:"connections"`
	Sessions     int64         `yaml:"sessions"`
	MemoryMiB    int64         `yaml:"memory_mib"`
	OpenTimeout  string        `yaml:"open_timeout"`
	DialTimeout  string        `yaml:"dial_timeout"`
	UDPIdle      string        `yaml:"udp_idle"`
	OpenDuration time.Duration `yaml:"-"`
	DialDuration time.Duration `yaml:"-"`
	UDPDuration  time.Duration `yaml:"-"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.UnmarshalWithOptions(b, &c, yaml.Strict()); err != nil {
		return nil, err
	}
	if err := c.prepare(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) prepare() error {
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
	for i := range c.Listeners {
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

func (e *Endpoint) prepare(listener bool) error {
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
		if e.Network.Port != 0 || (e.Adaptive != nil && !*e.Adaptive) {
			e.MaxSessions = e.Sessions
		}
	}
	if e.MaxSessions < e.Sessions || e.MaxSessions > 256 {
		return fmt.Errorf("max_sessions must be sessions..256")
	}
	if listener && e.Network.Port != a.Port {
		return fmt.Errorf("network port must match listener port")
	}
	if !listener && e.Network.Port != 0 && e.Sessions != 1 {
		return fmt.Errorf("a fixed source port requires sessions: 1")
	}
	if !listener && e.Network.Port != 0 && e.MaxSessions != 1 {
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
