// File socket.go: adapts raw TCP capture/injection to KCP's PacketConn API; UDPAddr values
// represent endpoints, not wire UDP.

package socket

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/gopacket/gopacket/pcap"

	"paqet/internal/conf"
)

// PacketConn presents a packet interface over raw TCP capture/injection; UDPAddr is endpoint
// metadata only.
type PacketConn struct {
	// Netpoll-aware packet file access; this is not an outer kernel TCP connection.
	raw net.PacketConn
	// Prepared source/driver metadata; its address/port identifies the physical packet endpoint.
	cfg *conf.Network
	// Owned pcap injection state, absent when the AF_PACKET driver is active.
	sendHandle *SendHandle
	// Owned pcap capture state, absent when the AF_PACKET driver is active.
	recvHandle *RecvHandle
	// Atomic input deadline; underlying netpoll/capture handling enforces cancellation.
	readDeadline atomic.Value
	// Atomic output deadline retained independently of the read direction.
	writeDeadline atomic.Value
}

// New constructs the configured packet driver while keeping UDP-shaped library addresses
// separate from physical raw TCP frames.
func New(cfg *conf.Network) (*PacketConn, error) {
	if cfg.Port == 0 {
		cfg.Port = 32768 + rand.Intn(32768)
	}
	if cfg.Backend == "packet" {
		raw, err := newRawPacket(cfg)
		if err != nil {
			return nil, err
		}
		return &PacketConn{cfg: cfg, raw: raw}, nil
	}

	sendHandle, err := NewSendHandle(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create send handle on %s: %v", cfg.Interface.Name, err)
	}

	recvHandle, err := NewRecvHandle(cfg)
	if err != nil {
		sendHandle.Close()
		return nil, fmt.Errorf("failed to create receive handle on %s: %v", cfg.Interface.Name, err)
	}

	conn := &PacketConn{
		cfg:        cfg,
		sendHandle: sendHandle,
		recvHandle: recvHandle,
	}

	return conn, nil
}

// ReadFrom returns one decapsulated packet payload/address while retaining the active driver's
// deadline and buffer rules.
func (c *PacketConn) ReadFrom(data []byte) (n int, addr net.Addr, err error) {
	if c.raw != nil {
		return c.raw.ReadFrom(data)
	}
	for {
		if d, ok := c.readDeadline.Load().(time.Time); ok && !d.IsZero() && !time.Now().Before(d) {
			return 0, nil, os.ErrDeadlineExceeded
		}

		n, addr, err := c.recvHandle.Read(data)
		if err != nil {
			if errors.Is(err, pcap.NextErrorTimeoutExpired) || errors.Is(err, errNoPayload) {
				continue
			}
			return 0, nil, err
		}

		return n, addr, nil
	}
}

// WriteTo submits one packet to the active driver; reliable retry remains the surrounding KCP
// session's responsibility.
func (c *PacketConn) WriteTo(data []byte, addr net.Addr) (n int, err error) {
	if c.raw != nil {
		return c.raw.WriteTo(data, addr)
	}
	if d, ok := c.writeDeadline.Load().(time.Time); ok && !d.IsZero() && !time.Now().Before(d) {
		return 0, os.ErrDeadlineExceeded
	}

	daddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, net.InvalidAddrError("invalid address")
	}

	err = c.sendHandle.Write(data, daddr)
	if err != nil {
		return 0, err
	}

	return len(data), nil
}

// Close releases this object's owned resources or signals its lifecycle once; shared listener
// ownership is handled by its wrapper.
func (c *PacketConn) Close() error {
	if c.raw != nil {
		return c.raw.Close()
	}
	if c.sendHandle != nil {
		c.sendHandle.Close()
	}
	if c.recvHandle != nil {
		c.recvHandle.Close()
	}
	return nil
}

// LocalAddr reports this object's local endpoint without transferring packet socket ownership.
func (c *PacketConn) LocalAddr() net.Addr {
	if c.raw != nil {
		return c.raw.LocalAddr()
	}
	addr := c.cfg.IPv4.Addr
	if addr == nil {
		addr = c.cfg.IPv6.Addr
	}
	if addr == nil {
		return &net.UDPAddr{Port: c.cfg.Port}
	}
	return &net.UDPAddr{IP: append(net.IP(nil), addr.IP...), Port: c.cfg.Port, Zone: addr.Zone}
}

// SetDeadline updates both read and write deadlines and delegates cancellation to the
// underlying connection.
func (c *PacketConn) SetDeadline(t time.Time) error {
	if c.raw != nil {
		return c.raw.SetDeadline(t)
	}
	c.readDeadline.Store(t)
	c.writeDeadline.Store(t)
	return nil
}

// SetReadDeadline sets input expiry and wakes blocked I/O through the underlying connection
// contract.
func (c *PacketConn) SetReadDeadline(t time.Time) error {
	if c.raw != nil {
		return c.raw.SetReadDeadline(t)
	}
	c.readDeadline.Store(t)
	return nil
}

// SetWriteDeadline sets output expiry so backpressure cannot ignore caller cancellation
// indefinitely.
func (c *PacketConn) SetWriteDeadline(t time.Time) error {
	if c.raw != nil {
		return c.raw.SetWriteDeadline(t)
	}
	c.writeDeadline.Store(t)
	return nil
}

// SetDSCP preserves the PacketConn API; raw header DSCP is already fixed by the retained
// envelope encoder.
func (c *PacketConn) SetDSCP(dscp int) error {
	return nil
}

// SetClientTCPF delegates the compatibility peer flag override to the active encoder.
func (c *PacketConn) SetClientTCPF(addr net.Addr, f []conf.TCPF) {
	if c.raw != nil {
		c.raw.(*rawPacket).send.setClientTCPF(addr, f)
		return
	}
	c.sendHandle.setClientTCPF(addr, f)
}

// DeleteClientTCPF delegates removal of a compatibility peer flag override.
func (c *PacketConn) DeleteClientTCPF(addr net.Addr) {
	if c.raw != nil {
		c.raw.(*rawPacket).send.deleteClientTCPF(addr)
		return
	}
	c.sendHandle.deleteClientTCPF(addr)
}

// sendState returns the encoder/flag owner behind either driver so session generation rules
// stay consistent.
func (c *PacketConn) sendState() *SendHandle {
	if c.raw != nil {
		return c.raw.(*rawPacket).send
	}
	return c.sendHandle
}

// RegisterClient delegates generation ownership to the encoder shared by this packet
// connection.
func (c *PacketConn) RegisterClient(addr net.Addr, owner uint32) {
	c.sendState().registerClient(addr, owner)
}

// SetClientTCPFSession delegates a generation-checked outer flag update to the active driver.
func (c *PacketConn) SetClientTCPFSession(addr net.Addr, owner uint32, f []conf.TCPF) {
	c.sendState().setClientTCPFSession(addr, owner, f)
}

// DeleteClientSession delegates teardown without letting an older conversation alter a
// replacement.
func (c *PacketConn) DeleteClientSession(addr net.Addr, owner uint32) {
	c.sendState().deleteClientSession(addr, owner)
}
