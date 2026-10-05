// File strm.go: adapts a mux stream ID to the engine stream contract without adding another
// transport.

package kcp

import (
	"github.com/xtaci/smux"
)

// Strm adds engine stream identity to the mux stream without adding per-stream packet sockets.
type Strm struct {
	// Embedded connection/stream contract; no extra raw socket is allocated per logical stream.
	*smux.Stream
}

// SID returns the stable mux stream ID used for control correlation and flow diagnostics.
func (s *Strm) SID() int {
	return int(s.ID())
}
