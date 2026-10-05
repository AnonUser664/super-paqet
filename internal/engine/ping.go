//go:build linux

// File ping.go: creates a temporary configured peer probe and releases its sockets/owned rules
// afterward.

package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"paqet/internal/protocol"
	"paqet/internal/tnet/kcp"
)

// Ping opens a temporary peer control stream and waits for a pong within the configured
// opening deadline.
func Ping(ctx context.Context, cfg *Config, name string) (err error) {
	if cfg.Firewall == nil || *cfg.Firewall {
		if err := RecoverFirewall(); err != nil {
			return err
		}
	}
	if name == "" && len(cfg.Peers) == 1 {
		for n := range cfg.Peers {
			name = n
		}
	}
	ep, ok := cfg.Peers[name]
	if !ok {
		return fmt.Errorf("select a configured peer with --peer")
	}
	guard, n, err := reserve(ep.Network)
	if err != nil {
		return err
	}
	defer guard.Close()
	fw := &firewall{}
	defer func() { err = errors.Join(err, fw.close()) }()
	if cfg.Firewall == nil || *cfg.Firewall {
		if err := fw.add(&n); err != nil {
			return err
		}
	}
	a, err := net.ResolveUDPAddr("udp", ep.Address)
	if err != nil {
		return err
	}
	c, err := kcp.Dial(a, &ep.KCP, n)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	s, err := c.OpenStrm()
	if err != nil {
		return err
	}
	defer s.Close()
	deadline, _ := ctx.Deadline()
	s.SetDeadline(deadline)
	p := protocol.Proto{Type: protocol.PPING}
	if err := p.Write(s); err != nil {
		return err
	}
	if err := p.Read(s); err != nil {
		return err
	}
	if p.Type != protocol.PPONG {
		return fmt.Errorf("unexpected response")
	}
	return nil
}
