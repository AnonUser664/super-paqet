// File write_ownership_test.go forces timeout and buffer reuse while the carrier
// writer is held. A returned Write must never retain the caller's payload.
package smux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// heldVectorConn delays vector consumption, matching a KCP writer awaiting send
// capacity. It records the eventual wire bytes without relying on scheduling.
type heldVectorConn struct {
	net.Conn
	entered   chan struct{}
	release   chan struct{}
	closed    chan struct{}
	frames    chan []byte
	enterOnce sync.Once
	closeOnce sync.Once
}

func (c *heldVectorConn) WriteBuffers(v [][]byte) (int, error) {
	c.enterOnce.Do(func() { close(c.entered) })
	select {
	case <-c.release:
	case <-c.closed:
		return 0, io.ErrClosedPipe
	}
	var b []byte
	for _, p := range v {
		b = append(b, p...)
	}
	c.frames <- b
	return len(b), nil
}
func (c *heldVectorConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

// heldWriterSession supplies a bounded, explicitly released stalled writer.
func heldWriterSession(t *testing.T) (*Session, *heldVectorConn) {
	t.Helper()
	a, b := net.Pipe()
	c := &heldVectorConn{Conn: a, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}), frames: make(chan []byte, 8)}
	cfg := DefaultConfig()
	cfg.KeepAliveDisabled = true
	s, err := Client(c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); b.Close() })
	return s, c
}

// TestTimedOutVectorWriteOwnsPayload proves that buffer recycling after timeout
// cannot alter bytes still retained by a blocked carrier write.
func TestTimedOutVectorWriteOwnsPayload(t *testing.T) {
	s, c := heldWriterSession(t)
	payload := []byte("original")
	f := newFrame(1, cmdPSH, 2)
	f.data = payload
	deadline := time.NewTimer(30 * time.Millisecond)
	defer deadline.Stop()
	result := make(chan error, 1)
	go func() { _, err := s.writeFrameInternal(f, deadline.C, CLSDATA); result <- err }()
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	if err := <-result; err != ErrTimeout {
		t.Fatalf("write error: %v", err)
	}
	copy(payload, "recycled")
	close(c.release)
	select {
	case b := <-c.frames:
		if !bytes.Equal(b[headerSize:], []byte("original")) {
			t.Fatalf("caller buffer retained after return: %q", b[headerSize:])
		}
	case <-time.After(time.Second):
		t.Fatal("held write did not finish")
	}
}

// TestCanceledQueuedWriteNeverReachesCarrier prevents a timed-out queued frame
// from consuming bandwidth or arriving after a later successful write.
func TestCanceledQueuedWriteNeverReachesCarrier(t *testing.T) {
	s, c := heldWriterSession(t)
	first := make(chan error, 1)
	go func() { _, err := s.writeFrameInternal(newFrame(1, cmdNOP, 0), nil, CLSCTRL); first <- err }()
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	f := newFrame(1, cmdPSH, 2)
	f.data = []byte("expired")
	f.credit = newStream(2, s.config.MaxFrameSize, s)
	atomic.StoreUint32(&f.credit.numWritten, uint32(len(f.data)))
	if _, err := s.writeFrameInternal(f, timer.C, CLSDATA); err != ErrTimeout {
		t.Fatalf("queued error: %v", err)
	}
	close(c.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	timer2 := time.NewTimer(time.Second)
	defer timer2.Stop()
	if _, err := s.writeFrameInternal(newFrame(1, cmdNOP, 99), timer2.C, CLSDATA); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case b := <-c.frames:
			if b[1] != cmdNOP {
				t.Fatalf("expired queued frame reached carrier: command=%d", b[1])
			}
		case <-time.After(time.Second):
			t.Fatal("missing live frame")
		}
	}

	if credit := atomic.LoadUint32(&f.credit.numWritten); credit != 0 {
		t.Fatalf("canceled queued frame retained stream credit: %d", credit)
	}
}

// TestOpenContextIncludesSYNWrite applies the caller's short budget even when
// the underlying sender has not returned, and reclaims the pending stream.
func TestOpenContextIncludesSYNWrite(t *testing.T) {
	s, c := heldWriterSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	stream, err := s.OpenStreamContext(ctx)
	if stream != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("open result: %v %v", stream, err)
	}
	if time.Since(started) > 200*time.Millisecond || s.NumStreams() != 0 {
		t.Fatalf("opening overrun/ownership: %s streams=%d", time.Since(started), s.NumStreams())
	}
	select {
	case <-c.entered:
	default:
		t.Fatal("SYN was not held in the carrier")
	}
}

// TestCanceledOpenDoesNotRegister prevents canceled callers from publishing
// streams or sending a SYN before their context is checked.
func TestCanceledOpenDoesNotRegister(t *testing.T) {
	s, c := heldWriterSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if stream, err := s.OpenStreamContext(ctx); stream != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("open result: %v %v", stream, err)
	}
	if s.NumStreams() != 0 {
		t.Fatal("canceled opening retained a stream")
	}
	select {
	case <-c.entered:
		t.Fatal("canceled opening touched carrier")
	default:
	}
}

// TestAbortWithFullQueueReleasesLocally verifies bounded best-effort reset
// admission does not make a failed opening wait or retain receive ownership.
func TestAbortWithFullQueueReleasesLocally(t *testing.T) {
	s, c := heldWriterSession(t)
	go s.writeFrameInternal(newFrame(1, cmdNOP, 0), nil, CLSCTRL)
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("writer not held")
	}
	// Block heap publication after the scheduler consumes one marker, so it
	// cannot race reset admission by draining an input slot.
	s.sq.mu.Lock()
	defer s.sq.mu.Unlock()
	s.shaper <- writeRequest{frame: newFrame(1, cmdNOP, 0), class: CLSCTRL}
	until := time.Now().Add(time.Second)
	for len(s.shaper) != 0 {
		if time.Now().After(until) {
			t.Fatal("scheduler did not consume marker")
		}
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < cap(s.shaper); i++ {
		s.shaper <- writeRequest{frame: newFrame(1, cmdNOP, 0), class: CLSCTRL}
	}
	stream := newStream(5, s.config.MaxFrameSize, s)
	s.streamLock.Lock()
	s.streams[5] = stream
	s.streamLock.Unlock()
	if err := stream.Abort(); err != ErrWouldBlock {
		t.Fatalf("full-queue reset: %v", err)
	}
	if s.NumStreams() != 0 {
		t.Fatal("aborted stream retained ownership")
	}
	_, drops := s.WriteCancellationStats()
	if drops != 1 {
		t.Fatalf("reset pressure hidden: %d", drops)
	}
}

// BenchmarkWriteOwnershipSnapshot isolates the pooled snapshot cost from WAN
// scheduling. Integration bulk checks separately measure its throughput effect.
func BenchmarkWriteOwnershipSnapshot(b *testing.B) {
	for _, size := range []int{22, 32768, 65535} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			payload := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				f := Frame{data: payload}
				o := newWriteOwnership(&f)
				o.release()
				o.release()
			}
		})
	}
}
