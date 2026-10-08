//go:build linux

// File relay.go: relays TCP directions with lazy scratch storage and distinguishes write EOF
// from full connection abandonment.

package engine

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
	"paqet/internal/tnet"
)

// copyPools reuse five power-of-two TCP/UDP scratch sizes; idle flows do not retain a checked-
// out buffer.
var copyPools [5]sync.Pool

// getBuffer reuses a power-of-two scratch class sized to requested bytes rather than allocating
// per idle flow.
func getBuffer(size int) (*[]byte, int) {
	class := 0
	for 4096<<class < size && class < len(copyPools)-1 {
		class++
	}
	if v := copyPools[class].Get(); v != nil {
		return v.(*[]byte), class
	}
	b := make([]byte, 4096<<class)
	return &b, class
}

// tcpToStream returns scratch before waiting in netpoll. Idle TCP connections
// retain no copy buffer. Successful reads adapt the next scratch size
// without a second syscall to query queued bytes on every poll attempt.
func tcpToStream(dst io.Writer, src *net.TCPConn) (int64, error) {
	raw, err := src.SyscallConn()
	if err != nil {
		return 0, err
	}
	var total int64
	readSize := 4096
	for {
		var b *[]byte
		var class, n int
		var readErr error
		err = raw.Read(func(fd uintptr) bool {
			// Read directly: ioctl plus read costs two syscalls even when the
			// socket is empty. Scratch remains borrowed only for this attempt.
			b, class = getBuffer(readSize)
			n, readErr = unix.Read(int(fd), *b)
			if errors.Is(readErr, unix.EAGAIN) || errors.Is(readErr, unix.EINTR) {
				copyPools[class].Put(b)
				b = nil
				return false
			}
			if n == len(*b) && readSize < 4096<<(len(copyPools)-1) {
				// Repeated full reads grow to the existing bounded bulk class.
				readSize *= 2
			} else if n > 0 && n <= readSize/4 && readSize > 4096 {
				// Quarter-full reads shrink one class; hysteresis avoids
				// bouncing between adjacent classes on ordinary partial reads.
				readSize /= 2
			}
			return true
		})
		if b != nil {
			if n > 0 {
				written, e := dst.Write((*b)[:n])
				total += int64(written)
				copyPools[class].Put(b)
				if e != nil {
					return total, e
				}
				if written != n {
					return total, io.ErrShortWrite
				}
			} else {
				copyPools[class].Put(b)
			}
		}
		if err != nil {
			return total, err
		}
		if readErr != nil {
			return total, readErr
		}
		if n == 0 {
			return total, nil
		}
	}
}

// countedWriter wraps a destination and credits only bytes actually accepted by its Write
// call.
type countedWriter struct {
	// Embedded destination writer; this wrapper owns counting, not independent transport
	// storage.
	io.Writer
	// Destination-accepted byte counter, not attempted buffer length.
	count *atomic.Int64
}

// Write counts bytes actually accepted by its destination, including short writes.
func (w countedWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.count.Add(int64(n))
	return n, err
}

// relay copies both TCP directions, propagates directional EOF and waits for the opposite copy
// task before full teardown.
func (e *Engine) relay(tcp *net.TCPConn, strm tnet.Strm, traces ...uint64) {
	e.relayContext(e.ctx, tcp, strm, traces...)
}

// relayContext retains directional EOF while honoring an explicitly removed
// endpoint generation. It waits on existing lifecycle channels, adding no
// per-flow cancellation callbacks, timers or extra goroutines.
func (e *Engine) relayContext(ctx context.Context, tcp *net.TCPConn, strm tnet.Strm, traces ...uint64) {
	var trace uint64
	var conv uint32
	var started time.Time
	if len(traces) > 0 {
		trace = traces[0]
	}
	if trace != 0 {
		if c, ok := strm.(interface{ ConversationID() uint32 }); ok {
			conv = c.ConversationID()
		}
	}
	if e.traceFlow(trace) {
		started = time.Now()
		e.log().Debug("flow.relay", "flow_id", trace, "conv", conv, "stream_id", strm.SID(), "tcp_remote", tcp.RemoteAddr().String(), "tunnel_remote", strm.RemoteAddr().String())
	}
	defer tcp.Close()
	defer strm.Close()
	result := make(chan error, 1)
	var uplinkBytes int64
	go func() {
		var err error
		uplinkBytes, err = tcpToStream(countedWriter{strm, &e.stats.Sent}, tcp)
		if err == nil {
			if cw, ok := strm.(interface{ CloseWrite() error }); ok {
				err = cw.CloseWrite()
			}
		}
		if err != nil {
			tcp.Close()
			strm.Close()
		}
		result <- err
	}()
	// smux.WriterTo transfers queued slices without a per-idle-stream scratch buffer.
	downlinkBytes, err := io.Copy(countedWriter{tcp, &e.stats.Received}, strm)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err == nil {
		err = tcp.CloseWrite()

	}
	if err != nil {
		tcp.Close()
		strm.Close()
	}
	// Directional EOF permits the opposite writer to remain idle. Full mux
	// closure or endpoint cancellation means it can no longer make progress;
	// wake TCP netpoll instead of retaining its relay indefinitely.
	var canceled, streamClosed <-chan struct{}
	if ctx != nil {
		canceled = ctx.Done()
	}
	if lifecycle, ok := strm.(interface{ GetDieCh() <-chan struct{} }); ok {
		streamClosed = lifecycle.GetDieCh()
	}
	var other error
	select {
	case other = <-result:
	case <-canceled:
		tcp.Close()
		strm.Close()
		other = <-result
	case <-streamClosed:
		tcp.Close()
		strm.Close()
		other = <-result
	}
	if e.traceFlow(trace) {
		e.log().Debug("flow.closed", "flow_id", trace, "conv", conv, "stream_id", strm.SID(), "elapsed_ms", time.Since(started).Milliseconds(), "sent_bytes", uplinkBytes, "received_bytes", downlinkBytes, "read_error", err, "write_error", other)
	}
	if (err != nil && !errors.Is(err, net.ErrClosed)) || (other != nil && !errors.Is(other, net.ErrClosed)) {
		e.log().Debug("flow.relay_failed", "flow_id", trace, "conv", conv, "stream_id", strm.SID(), "read_error", err, "write_error", other)
		e.stats.Aborted.Add(1)
	}
}
