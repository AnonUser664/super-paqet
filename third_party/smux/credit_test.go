package smux

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Deliberately leave the session writer unscheduled: inbound draining must
// progress independently of opposite-direction transport credit.
func TestAsyncCreditReadDoesNotWaitForCarrier(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Version, cfg.AsyncWindowUpdates = 2, true
	cfg.MaxStreamBuffer = 4096
	sess := &Session{config: cfg, die: make(chan struct{}), bucketNotify: make(chan struct{}, 1),
		chSocketWriteError: make(chan struct{}), chShaperPending: make(chan struct{}, 1), credits: make(map[uint32]*creditUpdate)}
	s := newStream(3, cfg.MaxFrameSize, sess)
	p := defaultAllocator.Get(1024)
	copy(*p, bytes.Repeat([]byte{7}, 1024))
	s.pushBytes(p)
	done := make(chan error, 1)
	go func() {
		got := make([]byte, 1024)
		n, err := s.Read(got)
		if err == nil && (n != len(got) || !bytes.Equal(got, bytes.Repeat([]byte{7}, 1024))) {
			err = io.ErrUnexpectedEOF
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("inbound read blocked behind outbound carrier")
	}
	for i := uint32(1025); i < 5000; i++ {
		if err := sess.queueCredit(s, i, 4096); err != nil {
			t.Fatal(err)
		}
	}
	if len(sess.credits) != 1 {
		t.Fatal("updates did not coalesce")
	}
	req, ok := sess.popCredit()
	if !ok || req.frame.sid != 3 || binary.LittleEndian.Uint32(req.frame.data) != 4999 {
		t.Fatal("latest credit lost")
	}
	// Verify wraparound is preserved as a cumulative unsigned count.
	sess.queueCredit(s, ^uint32(0), 4096)
	sess.queueCredit(s, 0, 4096)
	req, ok = sess.popCredit()
	if !ok || binary.LittleEndian.Uint32(req.frame.data) != 0 {
		t.Fatal("credit wrap lost")
	}
	// Closing stream churn cannot accumulate stale entries while output stalls.
	for i := uint32(5); i < 10005; i += 2 {
		stream := newStream(i, cfg.MaxFrameSize, sess)
		sess.queueCredit(stream, 1, 4096)
		stream.sessionClose()
		sess.removeCredit(i)
	}
	if len(sess.credits) != 0 || sess.creditHead != nil || sess.creditTail != nil {
		t.Fatal("closed stream credits retained")
	}
}

func TestAsyncCreditFullDuplexIntegrity(t *testing.T) {
	a, b := net.Pipe()
	cfg := DefaultConfig()
	cfg.Version, cfg.HalfClose, cfg.AsyncWindowUpdates, cfg.KeepAliveDisabled = 2, true, true, true
	cfg.CreditHints = true // No hint facility: reliable fallback must still work.
	cfg.MaxStreamBuffer = 4096
	client, _ := Client(a, cfg)
	server, _ := Server(b, cfg)
	defer client.Close()
	defer server.Close()
	c, err := client.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	s.SetDeadline(time.Now().Add(5 * time.Second))
	left, right := bytes.Repeat([]byte("left"), 65536), bytes.Repeat([]byte("right"), 65536)
	done := make(chan error, 4)
	for _, job := range []struct {
		st      *Stream
		send    []byte
		receive []byte
	}{{c, left, right}, {s, right, left}} {
		go func(st *Stream, data []byte) {
			_, err := st.Write(data)
			if err == nil {
				err = st.CloseWrite()
			}
			done <- err
		}(job.st, job.send)
		go func(st *Stream, want []byte) {
			got, err := io.ReadAll(st)
			if err == nil && !bytes.Equal(got, want) {
				err = io.ErrUnexpectedEOF
			}
			done <- err
		}(job.st, job.receive)
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	c.Close()
	s.Close()
}

func TestCreditHintsCannotRewindOrGrantUnsentBytes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CreditHints = true
	sess := &Session{config: cfg, streams: make(map[uint32]*stream)}
	s := newStream(3, cfg.MaxFrameSize, sess)
	sess.streams[3] = s
	atomic.StoreUint32(&s.numWritten, 1000)
	sess.receiveCreditHint(3, 500, 4096)
	sess.receiveCreditHint(3, 400, 16384)
	sess.receiveCreditHint(3, 1001, 16384)
	sess.receiveCreditHint(3, 600, 0xffffffff)
	if s.peerConsumed != 500 || s.peerWindow != 4096 {
		t.Fatal("stale/future credit changed writer budget")
	}
	atomic.StoreUint32(&s.peerConsumed, ^uint32(0)-100)
	atomic.StoreUint32(&s.numWritten, 100)
	sess.receiveCreditHint(3, 10, 8192)
	if s.peerConsumed != 10 || s.peerWindow != 8192 {
		t.Fatal("wrapped cumulative credit rejected")
	}
	sess.receiveCreditHint(999, 1, 1)
}

type brokenCreditCarrier struct {
	closed chan struct{}
	once   sync.Once
}

type creditFaultCarrier struct {
	net.Conn
	peer        *creditFaultCarrier
	handler     atomic.Value
	count       atomic.Uint64
	dropAll     bool
	mu          sync.Mutex
	previous    [3]uint32
	hasPrevious bool
}

func (c *creditFaultCarrier) SetCreditHintHandler(f func(uint32, uint32, uint32)) { c.handler.Store(f) }
func (c *creditFaultCarrier) SendCreditHint(sid, consumed, window uint32) error {
	n := c.count.Add(1)
	if c.dropAll || n%5 == 0 {
		return nil
	}
	value := [3]uint32{sid, consumed, window}
	c.mu.Lock()
	previous, hasPrevious := c.previous, c.hasPrevious
	c.hasPrevious = false
	if n%3 == 0 {
		c.previous, c.hasPrevious = value, true
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if f := c.peer.handler.Load(); f != nil {
		callback := f.(func(uint32, uint32, uint32))
		callback(value[0], value[1], value[2])
		callback(value[0], value[1], value[2]) // Duplicate hint.
		if hasPrevious {
			callback(previous[0], previous[1], previous[2])
		} // Late older hint.
	}
	return nil
}

func TestCreditHintFaultsPreserveReliableFullDuplex(t *testing.T) {
	for _, dropAll := range []bool{true, false} {
		a, b := net.Pipe()
		left, right := &creditFaultCarrier{Conn: a, dropAll: dropAll}, &creditFaultCarrier{Conn: b, dropAll: dropAll}
		left.peer, right.peer = right, left
		cfg := DefaultConfig()
		cfg.Version, cfg.HalfClose, cfg.AsyncWindowUpdates, cfg.CreditHints, cfg.KeepAliveDisabled = 2, true, true, true, true
		cfg.MaxStreamBuffer = 4096
		client, _ := Client(left, cfg)
		server, _ := Server(right, cfg)
		defer client.Close()
		defer server.Close()
		c, err := client.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		s, err := server.AcceptStream()
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		s.SetDeadline(time.Now().Add(5 * time.Second))
		payload := bytes.Repeat([]byte("credit faults preserve ordered data"), 16384)
		done := make(chan error, 4)
		for _, st := range []*Stream{c, s} {
			go func(st *Stream) {
				_, err := st.Write(payload)
				if err == nil {
					err = st.CloseWrite()
				}
				done <- err
			}(st)
			go func(st *Stream) {
				got, err := io.ReadAll(st)
				if err == nil && !bytes.Equal(got, payload) {
					err = io.ErrUnexpectedEOF
				}
				done <- err
			}(st)
		}
		for i := 0; i < 4; i++ {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		if left.count.Load() == 0 || right.count.Load() == 0 {
			t.Fatal("hint path was not exercised")
		}
		c.Close()
		s.Close()
		client.Close()
		server.Close()
	}
}

func (c *brokenCreditCarrier) Read([]byte) (int, error)  { <-c.closed; return 0, io.ErrClosedPipe }
func (c *brokenCreditCarrier) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (c *brokenCreditCarrier) Close() error              { c.once.Do(func() { close(c.closed) }); return nil }

func TestAsyncCreditWriteFailureWakesBlockedReader(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Version, cfg.AsyncWindowUpdates, cfg.KeepAliveDisabled = 2, true, true
	carrier := &brokenCreditCarrier{closed: make(chan struct{})}
	sess := newSession(cfg, carrier, true)
	defer sess.Close()
	s := newStream(3, cfg.MaxFrameSize, sess)
	sess.streamLock.Lock()
	sess.streams[3] = s
	sess.streamLock.Unlock()
	done := make(chan error, 1)
	go func() { _, err := s.Read(make([]byte, 1)); done <- err }()
	sess.queueCredit(s, 1, 4096)
	select {
	case err := <-done:
		if err != io.ErrClosedPipe && err != io.EOF {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("asynchronous writer failure left reader blocked")
	}
	<-carrier.closed
	if !sess.IsClosed() || sess.PendingCredits() != 0 {
		t.Fatal("failed carrier retained pending work")
	}
}

func TestAsyncCreditReadCountWrap(t *testing.T) {
	for _, writerTo := range []bool{false, true} {
		cfg := DefaultConfig()
		cfg.Version, cfg.AsyncWindowUpdates, cfg.MaxStreamBuffer = 2, true, 4096
		sess := &Session{config: cfg, die: make(chan struct{}), bucketNotify: make(chan struct{}, 1),
			chSocketWriteError: make(chan struct{}), chShaperPending: make(chan struct{}, 1), credits: make(map[uint32]*creditUpdate)}
		s := newStream(3, cfg.MaxFrameSize, sess)
		s.numRead, s.incr = ^uint32(0)-1023, 1024
		p := defaultAllocator.Get(1024)
		s.pushBytes(p)
		if writerTo {
			s.fin() // Buffered data drains before EOF.
			if n, err := s.WriteTo(io.Discard); err != io.EOF || n != 1024 {
				t.Fatal(n, err)
			}
		} else if n, err := s.Read(make([]byte, 1024)); err != nil || n != 1024 {
			t.Fatal(n, err)
		}
		req, ok := sess.popCredit()
		if !ok || binary.LittleEndian.Uint32(req.frame.data) != 0 {
			t.Fatal("zero cumulative credit lost at 4 GiB wrap")
		}
	}
}
