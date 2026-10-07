// File shared.go owns one raw source socket with independently scheduled KCP
// lanes, preserving the outer packet encoder and fixed source-port contract.
package kcp

import (
	"net"
	"paqet/internal/conf"
	"paqet/internal/socket"
	"sync"

	kcp "github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

// SharedDialer owns the physical socket and demultiplexer for a whole peer pool.
// Closing one Conn releases only its conversation; pool teardown releases all.
type SharedDialer struct {
	packet    *socket.PacketConn
	lanes     *kcp.Listener
	closeOnce sync.Once
}

// NewSharedDialer opens resources during staging, before the peer is published.
func NewSharedDialer(cfg *conf.KCP, network conf.Network, maximum int) (*SharedDialer, error) {
	packet, err := socket.New(&network)
	if err != nil {
		return nil, err
	}
	lanes, err := kcp.ServeConversationConn(cfg.Block, cfg.Dshard, cfg.Pshard, packet, true)
	if err != nil {
		packet.Close()
		return nil, err
	}
	lanes.SetMaxSessions(maximum)
	return &SharedDialer{packet: packet, lanes: lanes}, nil
}

// Dial creates an independent reliable lane and mux session on the shared
// source. Cipher, FEC and endpoint identity remain immutable per generation.
func (d *SharedDialer) Dial(remote net.Addr, cfg *conf.KCP) (*Conn, error) {
	conn, err := d.lanes.DialConversation(remote)
	if err != nil {
		return nil, err
	}
	if err := aplConf(conn, cfg); err != nil {
		conn.Close()
		return nil, err
	}
	session, err := smux.Client(conn, smuxConf(cfg, conn))
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &Conn{PacketConn: d.packet, UDPSession: conn, Session: session, sharedPacket: true}, nil
}

// Close retires all lanes before releasing the physical capture/injection socket.
func (d *SharedDialer) Close() { d.closeOnce.Do(func() { d.lanes.Close(); d.packet.Close() }) }

// Adopt preserves a live KCP/mux object while changing its outgoing socket.
// Both sockets must have the same framing/cipher contract and separate ownership.
func (d *SharedDialer) Adopt(c *Conn) error {
	if err := d.lanes.MoveSession(c.UDPSession, c.RemoteAddr()); err != nil {
		return err
	}
	c.livePacket.Store(d.packet)
	// The backend commit reply wakes pending ARQ; avoid a forced bulk burst
	// before the receiving dispatcher has adopted this conversation.
	return nil
}
