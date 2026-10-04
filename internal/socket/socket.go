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

type PacketConn struct {
	raw           net.PacketConn
	cfg           *conf.Network
	sendHandle    *SendHandle
	recvHandle    *RecvHandle
	readDeadline  atomic.Value
	writeDeadline atomic.Value
}

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

func (c *PacketConn) LocalAddr() net.Addr {
	if c.raw != nil {
		return c.raw.LocalAddr()
	}
	return nil
	// return &net.UDPAddr{
	// 	IP:   append([]byte(nil), c.cfg.PrimaryAddr().IP...),
	// 	Port: c.cfg.PrimaryAddr().Port,
	// 	Zone: c.cfg.PrimaryAddr().Zone,
	// }
}

func (c *PacketConn) SetDeadline(t time.Time) error {
	if c.raw != nil {
		return c.raw.SetDeadline(t)
	}
	c.readDeadline.Store(t)
	c.writeDeadline.Store(t)
	return nil
}

func (c *PacketConn) SetReadDeadline(t time.Time) error {
	if c.raw != nil {
		return c.raw.SetReadDeadline(t)
	}
	c.readDeadline.Store(t)
	return nil
}

func (c *PacketConn) SetWriteDeadline(t time.Time) error {
	if c.raw != nil {
		return c.raw.SetWriteDeadline(t)
	}
	c.writeDeadline.Store(t)
	return nil
}

func (c *PacketConn) SetDSCP(dscp int) error {
	return nil
}

func (c *PacketConn) SetClientTCPF(addr net.Addr, f []conf.TCPF) {
	if c.raw != nil {
		c.raw.(*rawPacket).send.setClientTCPF(addr, f)
		return
	}
	c.sendHandle.setClientTCPF(addr, f)
}

func (c *PacketConn) DeleteClientTCPF(addr net.Addr) {
	if c.raw != nil {
		c.raw.(*rawPacket).send.deleteClientTCPF(addr)
		return
	}
	c.sendHandle.deleteClientTCPF(addr)
}

func (c *PacketConn) sendState() *SendHandle {
	if c.raw != nil {
		return c.raw.(*rawPacket).send
	}
	return c.sendHandle
}
func (c *PacketConn) RegisterClient(addr net.Addr, owner uint32) {
	c.sendState().registerClient(addr, owner)
}
func (c *PacketConn) SetClientTCPFSession(addr net.Addr, owner uint32, f []conf.TCPF) {
	c.sendState().setClientTCPFSession(addr, owner, f)
}
func (c *PacketConn) DeleteClientSession(addr net.Addr, owner uint32) {
	c.sendState().deleteClientSession(addr, owner)
}
