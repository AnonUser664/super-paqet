// File conn.go: defines the carrier contract used by the engine independently of concrete KCP
// and mux types.

package tnet

import (
	"net"
	"time"
)

// Conn defines logical stream operations and connection lifecycle independently of concrete
// transport implementation.
type Conn interface {
	// Creates one logical stream on an existing carrier.
	OpenStrm() (Strm, error)
	// Receives one logical stream while the shared carrier stays alive.
	AcceptStrm() (Strm, error)
	// Optionally waits for a matching control response within the caller's deadline.
	Ping(wait bool) error
	// Releases owned resources; accepted carrier wrappers never own the listener packet socket.
	Close() error
	// Reports local endpoint metadata without transferring socket ownership.
	LocalAddr() net.Addr
	// Reports the remote carrier/stream endpoint used for routing and diagnostics.
	RemoteAddr() net.Addr
	// Controls both read/write expiry through the connection contract.
	SetDeadline(t time.Time) error
	// Controls input expiry without closing the opposite direction.
	SetReadDeadline(t time.Time) error
	// Controls output expiry so sender backpressure observes cancellation.
	SetWriteDeadline(t time.Time) error
}
