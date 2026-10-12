// File session_writer.go defines optional listener output ownership without
// replacing the listener's input dispatcher, addressing or encryption state.
package kcp

import (
	"context"
	"golang.org/x/net/ipv4"
	"time"
)

// SessionWriter owns one carrier's optional batch-only output descriptor. A
// listener remains responsible for receive dispatch and shared packet shaping.
type SessionWriter interface {
	WriteBatch([]ipv4.Message, int) (int, error)
	WriteBatchWithDrops([]ipv4.Message, int) (int, int, error)
	SetWriteDeadline(time.Time) error
	Close() error
}

// sessionWriterProvider is optional: generic UDP/pcap transports keep their
// existing output path, and failed construction safely falls back to it.
type sessionWriterProvider interface{ NewSessionWriter() (SessionWriter, error) }

// sessionOutputContext cancels rate-limiter waits on close without another
// goroutine or per-packet allocation; the queued final output can then drain.
type sessionOutputContext struct{ done <-chan struct{} }

func (c sessionOutputContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c sessionOutputContext) Done() <-chan struct{}       { return c.done }
func (c sessionOutputContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}
func (c sessionOutputContext) Value(any) any { return nil }

// HasSessionWriter exposes current physical ownership to warning-level metrics.
func (s *UDPSession) HasSessionWriter() bool { return s.route.Load().platform.hasSessionWriter() }
