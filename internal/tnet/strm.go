// File strm.go: defines logical application/control streams with connection semantics and a
// stable mux stream ID.

package tnet

import (
	"net"
)

// Strm adds engine stream identity to the mux stream without adding per-stream packet sockets.
type Strm interface {
	// Embedded connection/stream contract; no extra raw socket is allocated per logical stream.
	net.Conn
	// Session-local logical stream identifier used for correlation, not a kernel socket ID.
	SID() int
}
