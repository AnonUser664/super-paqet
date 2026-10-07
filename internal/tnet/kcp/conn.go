// File conn.go: wraps one KCP/mux carrier; accepted carriers deliberately do not own the
// listener packet socket.

package kcp

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"

	"paqet/internal/protocol"
	"paqet/internal/socket"
	"paqet/internal/tnet"
)

// Conn wraps one KCP/mux carrier; only outgoing instances own their PacketConn.
type Conn struct {
	// Owned only by outgoing carriers; accepted carriers leave this nil to preserve shared
	// listener ownership.
	PacketConn *socket.PacketConn
	// Reliable KCP conversation and its packet/FEC/crypto/update state.
	UDPSession *kcp.UDPSession
	// Mux carrier state hosting many logical application/control streams.
	Session *smux.Session
	// Shared dialer owns the packet socket; individual lane closure must retain it.
	sharedPacket bool
	// Mutable telemetry socket and negotiated control capability use atomic snapshots.
	livePacket atomic.Pointer[socket.PacketConn]
	Migration  atomic.Pointer[MigrationCapability]
}

// OpenStrm opens one logical mux stream on this carrier without creating another raw socket.
func (c *Conn) OpenStrm() (tnet.Strm, error) {
	strm, err := c.Session.OpenStream()
	if err != nil {
		return nil, err
	}
	return &Strm{strm}, nil
}

// OpenStrmContext bounds a new stream's SYN submission without setting a shared
// carrier deadline or interrupting already established streams.
func (c *Conn) OpenStrmContext(ctx context.Context) (tnet.Strm, error) {
	stream, err := c.Session.OpenStreamContext(ctx)
	if err != nil {
		return nil, err
	}
	return &Strm{stream}, nil
}

// AcceptStrm accepts a logical mux stream while the shared carrier remains active.
func (c *Conn) AcceptStrm() (tnet.Strm, error) {
	strm, err := c.Session.AcceptStream()
	if err != nil {
		return nil, err
	}
	return &Strm{strm}, nil
}

// Ping creates a short control stream and optionally requires the matching pong response.
func (c *Conn) Ping(wait bool) error {
	strm, err := c.Session.OpenStream()
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	defer strm.Close()
	if wait {
		p := protocol.Proto{Type: protocol.PPING}
		err = p.Write(strm)
		if err != nil {
			return fmt.Errorf("strm ping write failed: %w", err)
		}
		err = p.Read(strm)
		if err != nil {
			return fmt.Errorf("strm ping read failed: %w", err)
		}
		if p.Type != protocol.PPONG {
			return fmt.Errorf("strm pong failed: unexpected type %d", p.Type)
		}
	}
	return nil
}

// Close closes mux/KCP state and only the packet socket owned by an outgoing connection;
// accepted carriers share listener sockets.
func (c *Conn) Close() error {
	var err error
	if c.Session != nil {
		if e := c.Session.Close(); e != nil {
			err = e
		}
	}
	if c.UDPSession != nil {
		if e := c.UDPSession.Close(); e != nil && err == nil {
			err = e
		}
	}
	if c.PacketConn != nil && !c.sharedPacket {
		if e := c.PacketConn.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// LocalAddr reports this object's local endpoint without transferring packet socket ownership.
func (c *Conn) LocalAddr() net.Addr { return c.Session.LocalAddr() }

// RemoteAddr reports the carrier/stream remote endpoint used for diagnostics and routing.
func (c *Conn) RemoteAddr() net.Addr { return c.Session.RemoteAddr() }

// SetDeadline updates both read and write deadlines and delegates cancellation to the
// underlying connection.
func (c *Conn) SetDeadline(t time.Time) error { return c.UDPSession.SetDeadline(t) }

// SetReadDeadline sets input expiry and wakes blocked I/O through the underlying connection
// contract.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.UDPSession.SetReadDeadline(t) }

// SetWriteDeadline sets output expiry so backpressure cannot ignore caller cancellation
// indefinitely.
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.UDPSession.SetWriteDeadline(t) }

// MigrationCapability follows the logical session across physical socket moves.
type MigrationCapability struct {
	Token [32]byte
	Epoch uint64
}

// CurrentPacket returns the effective socket for telemetry/adaptive tuning.
func (c *Conn) CurrentPacket() *socket.PacketConn {
	if p := c.livePacket.Load(); p != nil {
		return p
	}
	return c.PacketConn
}
