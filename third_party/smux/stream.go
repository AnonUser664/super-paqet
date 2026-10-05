// MIT License
//
// Copyright (c) 2016-2017 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// File stream.go: provides logical connection reads/writes, pooled buffers, modular credits
// and directional/full-close semantics.

package smux

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// wrapper for GC
type Stream struct {
	// Embedded internal stream state; the exported wrapper retains the same shared session.
	*stream
}

// Stream implements net.Conn
type stream struct {
	id uint32 // Stream identifier
	// Owning mux session; individual streams do not own or close its carrier socket.
	sess *Session

	bufferRing bufferRing // ring buffer for ordered incoming data

	bufferLock sync.Mutex // Mutex to protect access to buffers
	// Current advertised byte capacity, bounded by the configured stream ceiling.
	receiveWindow uint32
	// Last drain-rate observation time; no dedicated timer is retained per idle stream.
	windowLast time.Time
	// Bytes consumed during the current receive-window observation interval.
	windowBytes uint64
	frameSize   int // Maximum frame size for the stream

	// wakeup channels
	chReaderWakeup chan struct{}
	// Coalesced wakeup for credit/close changes, avoiding sender busy loops.
	chWriterWakeup chan struct{}

	// stream closing
	die     chan struct{}
	dieOnce sync.Once // Ensures die channel is closed only once
	// Rejects later application writes after directional EOF begins.
	writeClosed atomic.Bool
	// Records accepted ordered FIN so full close can distinguish graceful completion from reset.
	finSent atomic.Bool
	// Records remote full abandonment; later frames must not resurrect the stream.
	resetReceived atomic.Bool
	// Ensures concurrent directional-close attempts send at most one ordered FIN.
	writeCloseOnce sync.Once
	// Retains the first directional-close result for subsequent callers.
	writeCloseErr error

	// to handle FIN event(i.e. EOF)
	chFinEvent   chan struct{}
	finEventOnce sync.Once // Ensures chFinEvent is closed only once

	// read/write deadline
	readDeadline atomic.Value
	// Atomic output deadline retained independently of the read direction.
	writeDeadline atomic.Value

	// v2 stream fields(flow control)
	numRead    uint32 // count num of bytes read
	numWritten uint32 // count num of bytes written
	incr       uint32 // bytes sent since last window update

	// UPD command
	peerConsumed uint32        // num of bytes the peer has consumed
	peerWindow   uint32        // peer window, initialized to 256KB, updated by peer
	chUpdate     chan struct{} // notify of remote data consuming and window update
}

// bufferRing retains pooled receive slices in FIFO order with small on-demand metadata
// capacity.
type bufferRing struct {
	// Unread payload slices in FIFO order; their allocator handles live in heads.
	bufs [][]byte
	// Original allocator handles used to recycle partially consumed slices correctly.
	heads []*[]byte
	// Index of the oldest unread slice.
	head int
	// Next insertion slot; growth preserves logical FIFO order.
	tail int
	// Number of live retained slices, separate from ring capacity.
	size int
}

// newBufferRing starts with small metadata storage so idle streams do not retain the full
// receive ceiling.
func newBufferRing(capacity int) bufferRing {
	if capacity < 1 {
		capacity = 1
	}
	return bufferRing{
		bufs:  make([][]byte, capacity),
		heads: make([]*[]byte, capacity),
	}
}

// len reports retained slices; callers use the stream buffer lock to protect mutation.
func (r *bufferRing) len() int {
	return r.size
}

// grow expands metadata while preserving unread FIFO order and pooled slice ownership.
func (r *bufferRing) grow() {
	newCap := len(r.bufs) * 2
	if newCap < 1 {
		newCap = 1
	}
	newBufs := make([][]byte, newCap)
	newHeads := make([]*[]byte, newCap)
	for i := 0; i < r.size; i++ {
		idx := (r.head + i) % len(r.bufs)
		newBufs[i] = r.bufs[idx]
		newHeads[i] = r.heads[idx]
	}
	r.bufs = newBufs
	r.heads = newHeads
	r.head = 0
	r.tail = r.size
}

// push enqueues a received slice and its allocator handle without copying the payload.
func (r *bufferRing) push(buf []byte, head *[]byte) {
	if r.size == len(r.bufs) {
		r.grow()
	}
	r.bufs[r.tail] = buf
	r.heads[r.tail] = head
	r.tail = (r.tail + 1) % len(r.bufs)
	r.size++
}

// pop releases the oldest retained slice/reference and keeps empty-ring indices consistent.
func (r *bufferRing) pop() (buf []byte, head *[]byte, ok bool) {
	if r.size == 0 {
		return nil, nil, false
	}
	buf = r.bufs[r.head]
	head = r.heads[r.head]
	r.bufs[r.head] = nil
	r.heads[r.head] = nil
	r.head = (r.head + 1) % len(r.bufs)
	r.size--
	if r.size == 0 {
		r.tail = r.head
	}
	return buf, head, true
}

// newStream initializes and returns a new Stream.
func newStream(id uint32, frameSize int, sess *Session) *stream {
	s := new(stream)
	s.id = id
	s.chReaderWakeup = make(chan struct{}, 1)
	s.chWriterWakeup = make(chan struct{}, 1)
	s.chUpdate = make(chan struct{}, 1)
	s.frameSize = frameSize
	s.sess = sess
	s.die = make(chan struct{})
	s.chFinEvent = make(chan struct{})
	s.peerWindow = initialPeerWindow // set to initial window size
	// pre-allocate ring buffer to reduce allocations during data transfer
	s.bufferRing = newBufferRing(2)
	s.receiveWindow = uint32(sess.config.MaxStreamBuffer)
	if sess.config.AdaptiveReceive {
		s.receiveWindow = uint32(min(65536, sess.config.MaxStreamBuffer))
	}

	return s
}

// ID returns the stream's unique identifier.
func (s *stream) ID() uint32 {
	return s.id
}

// ConversationID allows diagnostics to correlate both endpoints without
// retaining additional per-stream transport metadata.
func (s *stream) ConversationID() uint32 {
	if carrier, ok := s.sess.conn.(interface{ GetConv() uint32 }); ok {
		return carrier.GetConv()
	}
	return 0
}

// Read reads data from the stream into the provided buffer.
func (s *stream) Read(b []byte) (n int, err error) {
	for {
		switch s.sess.config.Version {
		case 2:
			n, err = s.tryReadV2(b)
		default:
			n, err = s.tryReadV1(b)
		}

		if err != ErrWouldBlock {
			return n, err
		}

		if ew := s.waitRead(); ew != nil {
			return 0, ew
		}
	}
}

// tryReadV1 copies available legacy-version data and returns tokens without waiting for a
// future frame.
func (s *stream) tryReadV1(b []byte) (n int, err error) {
	if len(b) == 0 {
		return 0, nil
	}

	// A critical section to copy data from buffers to b
	s.bufferLock.Lock()
	if s.bufferRing.len() > 0 {
		n = copy(b, s.bufferRing.bufs[s.bufferRing.head])
		s.bufferRing.bufs[s.bufferRing.head] = s.bufferRing.bufs[s.bufferRing.head][n:]

		// recycle buffer when fully consumed
		if len(s.bufferRing.bufs[s.bufferRing.head]) == 0 {
			defaultAllocator.Put(s.bufferRing.heads[s.bufferRing.head])
			s.bufferRing.bufs[s.bufferRing.head] = nil
			s.bufferRing.heads[s.bufferRing.head] = nil
			s.bufferRing.head = (s.bufferRing.head + 1) % len(s.bufferRing.bufs)
			s.bufferRing.size--
			if s.bufferRing.size == 0 {
				s.bufferRing.tail = s.bufferRing.head
			}
		}
	}
	s.bufferLock.Unlock()

	// return tokens to session to allow more data to be received
	if n > 0 {
		s.sess.returnTokens(n)
		return n, nil
	}

	// even if the stream has been closed, we try to deliver all buffered data first.
	// only when there's no data left in buffer, we return EOF to reader.
	select {
	case <-s.die:
		return 0, io.EOF
	default:
		return 0, ErrWouldBlock
	}
}

// tryReadV2 is the non-blocking version of Read for version 2 streams.
func (s *stream) tryReadV2(b []byte) (n int, err error) {
	if len(b) == 0 {
		return 0, nil
	}

	var notifyConsumed uint32
	var notify bool
	s.bufferLock.Lock()
	if s.bufferRing.len() > 0 {
		n = copy(b, s.bufferRing.bufs[s.bufferRing.head])
		s.bufferRing.bufs[s.bufferRing.head] = s.bufferRing.bufs[s.bufferRing.head][n:]

		// recycle buffer when fully consumed
		if len(s.bufferRing.bufs[s.bufferRing.head]) == 0 {
			defaultAllocator.Put(s.bufferRing.heads[s.bufferRing.head])
			s.bufferRing.bufs[s.bufferRing.head] = nil
			s.bufferRing.heads[s.bufferRing.head] = nil
			s.bufferRing.head = (s.bufferRing.head + 1) % len(s.bufferRing.bufs)
			s.bufferRing.size--
			if s.bufferRing.size == 0 {
				s.bufferRing.tail = s.bufferRing.head
			}
		}
	}

	// In an ideal environment:
	// If more than half of the buffer has been consumed, send a read ACK to the peer.
	// With the ACK round-trip time taken into account, a continuous data stream
	// will not slow down due to waiting for ACKs, as long as the consumer
	// continues reading data.
	//
	// s.numRead == n indicates that this is the initial read.
	s.numRead += uint32(n)
	s.incr += uint32(n)
	windowChanged := s.consumeWindow(n, time.Now())

	// send window update if the increased bytes exceed half of the buffer size
	// or this is the initial read.
	if windowChanged || s.incr >= s.windowUpdateThreshold() || s.numRead == uint32(n) {
		notify = true
		notifyConsumed = s.numRead
		s.incr = 0 // reset incr counter
	}
	s.bufferLock.Unlock()

	if n > 0 {
		s.sess.returnTokens(n)

		// send window update if necessary
		if notify {
			return n, s.sendWindowUpdate(notifyConsumed)
		}
		return n, nil
	}

	select {
	case <-s.die:
		return 0, io.EOF
	default:
		return 0, ErrWouldBlock
	}
}

// WriteTo implements io.WriteTo
// WriteTo writes data to w until there's no more data to write or when an error occurs.
// The return value n is the number of bytes written. Any error encountered during the write is also returned.
// WriteTo calls Write in a loop until there is no more data to write or when an error occurs.
// If the underlying stream is a v2 stream, it will send window update to peer when necessary.
// If the underlying stream is a v1 stream, it will not send window update to peer.
func (s *stream) WriteTo(w io.Writer) (n int64, err error) {
	switch s.sess.config.Version {
	case 2:
		return s.writeToV2(w)
	default:
		return s.writeToV1(w)
	}
}

// check comments in WriteTo
func (s *stream) writeToV1(w io.Writer) (n int64, err error) {
	for {
		var buf []byte
		var head *[]byte

		// get the next buffer to write
		s.bufferLock.Lock()
		if s.bufferRing.len() > 0 {
			buf, head, _ = s.bufferRing.pop()
		}
		s.bufferLock.Unlock()

		// write the buffer to w
		if buf != nil {
			nw, ew := w.Write(buf)
			// NOTE: WriteTo is a reader, so we need to return tokens here
			s.sess.returnTokens(len(buf))
			defaultAllocator.Put(head)
			if nw > 0 {
				n += int64(nw)
			}

			if ew != nil {
				return n, ew
			}
		} else if ew := s.waitRead(); ew != nil {
			return n, ew
		}
	}
}

// check comments in WriteTo
func (s *stream) writeToV2(w io.Writer) (n int64, err error) {
	for {
		var notifyConsumed uint32
		var notify bool
		var buf []byte
		var head *[]byte

		// get the next buffer to write
		s.bufferLock.Lock()
		if s.bufferRing.len() > 0 {
			buf, head, _ = s.bufferRing.pop()
		}

		// in v2, we need to track the number of bytes read
		var bufLen uint32
		if buf != nil {
			bufLen = uint32(len(buf))
		}
		s.numRead += bufLen
		s.incr += bufLen
		windowChanged := s.consumeWindow(int(bufLen), time.Now())

		// send window update if the increased bytes exceed half of the buffer size
		if windowChanged || s.incr >= s.windowUpdateThreshold() || s.numRead == bufLen {
			notify = true
			notifyConsumed = s.numRead
			s.incr = 0
		}
		s.bufferLock.Unlock()

		// same as v1, write the buffer to w
		if buf != nil {
			nw, ew := w.Write(buf)
			// NOTE: WriteTo is a reader, so we need to return tokens here
			s.sess.returnTokens(len(buf))
			defaultAllocator.Put(head)
			if nw > 0 {
				n += int64(nw)
			}

			if ew != nil {
				return n, ew
			}

			// send window update
			if notify {
				if err := s.sendWindowUpdate(notifyConsumed); err != nil {
					return n, err
				}
			}
		} else if ew := s.waitRead(); ew != nil {
			return n, ew
		}
	}
}

// sendWindowUpdate sends a window update command to the peer.
func (s *stream) sendWindowUpdate(consumed uint32) error {
	if s.sess.config.AsyncWindowUpdates {
		s.bufferLock.Lock()
		// Snapshot the latest read count while holding the same lock used
		// by readers, so concurrent readers cannot enqueue stale credit.
		err := s.sess.queueCredit(s, s.numRead, s.receiveWindow)
		consumed, window := s.numRead, s.receiveWindow
		s.bufferLock.Unlock()
		if err == nil && s.sess.creditTransport != nil {
			s.sess.creditHintsSent.Add(1)
			s.sess.creditTransport.SendCreditHint(s.id, consumed, window)
		}
		return err
	}
	var timer *time.Timer
	var deadline <-chan time.Time
	if d, ok := s.readDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer = time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	frame := newFrame(byte(s.sess.config.Version), cmdUPD, s.id)
	var hdr updHeader
	binary.LittleEndian.PutUint32(hdr[:], consumed)
	s.bufferLock.Lock()
	window := s.receiveWindow
	s.bufferLock.Unlock()
	binary.LittleEndian.PutUint32(hdr[4:], window)
	frame.data = hdr[:]
	_, err := s.sess.writeFrameInternal(frame, deadline, CLSCTRL) // <-- NOTE(x): use control channel
	return err
}

// waitRead blocks until a read event occurs or a deadline is reached.
func (s *stream) waitRead() error {
	var timer *time.Timer
	var deadline <-chan time.Time
	if d, ok := s.readDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer = time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	select {
	case <-s.chReaderWakeup: // notify some data has arrived, or closed
		return nil
	case <-s.chFinEvent:
		// BUGFIX(xtaci): Fix for https://github.com/xtaci/smux/issues/82
		s.bufferLock.Lock()
		defer s.bufferLock.Unlock()
		if s.bufferRing.len() > 0 {
			return nil
		}
		return io.EOF
	case <-s.sess.chSocketReadError:
		return s.sess.socketReadError.Load().(error)
	case <-s.sess.chProtoError:
		return s.sess.protoError.Load().(error)
	case <-deadline:
		return ErrTimeout
	case <-s.die:
		if s.resetReceived.Load() {
			s.bufferLock.Lock()
			defer s.bufferLock.Unlock()
			if s.bufferRing.len() > 0 {
				return nil
			}
			return io.EOF
		}
		return io.ErrClosedPipe
	}

}

// Write implements net.Conn
//
// Note that the behavior when multiple goroutines write concurrently is not deterministic,
// frames may interleave in random way.
func (s *stream) Write(b []byte) (n int, err error) {
	if s.writeClosed.Load() {
		return 0, io.ErrClosedPipe
	}
	switch s.sess.config.Version {
	case 2:
		return s.writeV2(b)
	default:
		return s.writeV1(b)
	}
}

// WritePriority writes a small opening/control payload ahead of other streams'
// queued data. Use only before writing application data on this stream, so
// priority cannot reorder bytes within it. Receive-window accounting is retained.
func (s *stream) WritePriority(b []byte) (int, error) {
	if len(b) > 512 || len(b) > s.frameSize {
		return 0, io.ErrShortBuffer
	}
	if s.writeClosed.Load() {
		return 0, io.ErrClosedPipe
	}
	select {
	case <-s.die:
		return 0, io.ErrClosedPipe
	default:
	}
	var deadline <-chan time.Time
	var timer *time.Timer
	if d, ok := s.writeDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer = time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}
	f := newFrame(byte(s.sess.config.Version), cmdPSH, s.id)
	f.data = b
	atomic.AddUint32(&s.numWritten, uint32(len(b)))
	n, err := s.sess.writeFrameInternal(f, deadline, CLSCTRL)
	return n, err
}

// writeV1 writes data to the stream for version 1 streams.
func (s *stream) writeV1(b []byte) (n int, err error) {
	// check empty input
	if len(b) == 0 {
		return 0, nil
	}

	// check if stream has closed
	select {
	case <-s.chFinEvent: // passive closing
		if !s.sess.config.HalfClose {
			return 0, io.EOF
		}
	case <-s.die:
		return 0, io.ErrClosedPipe
	default:
	}

	// create write deadline timer
	var deadline <-chan time.Time
	if d, ok := s.writeDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer := time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	// frame split and transmit
	sent := 0
	frame := newFrame(byte(s.sess.config.Version), cmdPSH, s.id)
	for len(b) > 0 {
		if s.writeClosed.Load() {
			return sent, io.ErrClosedPipe
		}
		size := len(b)
		if limit := s.writeFrameLimit(); size > limit {
			size = limit
		}

		frame.data = b[:size]
		atomic.AddUint32(&s.numWritten, uint32(size))
		n, err := s.sess.writeFrameInternal(frame, deadline, CLSDATA)
		sent += n
		if err != nil {
			return sent, err
		}

		b = b[size:]
	}

	return sent, nil
}

// writeV2 writes data to the stream for version 2 streams.
func (s *stream) writeV2(b []byte) (n int, err error) {
	// check empty input
	if len(b) == 0 {
		return 0, nil
	}

	// check if stream has closed
	select {
	case <-s.chFinEvent:
		if !s.sess.config.HalfClose {
			return 0, io.EOF
		}
	case <-s.die:
		return 0, io.ErrClosedPipe
	default:
	}

	// frame split and transmit process
	sent := 0
	frame := newFrame(byte(s.sess.config.Version), cmdPSH, s.id)

	var deadlineTimer *time.Timer
	defer func() {
		stopTimer(deadlineTimer)
	}()

	for {
		if s.writeClosed.Load() {
			return sent, io.ErrClosedPipe
		}
		deadline := (<-chan time.Time)(nil)
		if d, ok := s.writeDeadline.Load().(time.Time); ok && !d.IsZero() {
			dur := time.Until(d)
			if dur < 0 {
				dur = 0
			}
			if deadlineTimer == nil {
				deadlineTimer = time.NewTimer(dur)
			} else {
				stopTimer(deadlineTimer)
				deadlineTimer.Reset(dur)
			}
			deadline = deadlineTimer.C
		} else if deadlineTimer != nil {
			stopTimer(deadlineTimer)
			deadlineTimer = nil
		}

		// per stream sliding window control
		// [.... [consumed... numWritten] ... win... ]
		// [.... [consumed...................+rmtwnd]]
		// note:
		// even if uint32 overflow, this math still works:
		// eg1: uint32(0) - uint32(math.MaxUint32) = 1
		// eg2: int32(uint32(0) - uint32(1)) = -1
		//
		// basicially, you can take it as a MODULAR ARITHMETIC
		inflight := int32(atomic.LoadUint32(&s.numWritten) - atomic.LoadUint32(&s.peerConsumed))
		if inflight < 0 { // security check for malformed data
			return 0, ErrConsumed
		}

		// make sure you understand 'win' is calculated in modular arithmetic(2^32(4GB))
		win := int32(atomic.LoadUint32(&s.peerWindow)) - inflight

		if win > 0 {
			// determine how many bytes to send
			n := len(b)
			if n > int(win) {
				n = int(win)
			}

			// frame split and transmit
			bts := b[:n]
			for len(bts) > 0 {
				// splitting frame
				size := len(bts)
				if limit := s.writeFrameLimit(); size > limit {
					size = limit
				}
				frame.data = bts[:size]

				// transmit of frame
				atomic.AddUint32(&s.numWritten, uint32(size))
				nw, err := s.sess.writeFrameInternal(frame, deadline, CLSDATA)
				sent += nw
				if err != nil {
					return sent, err
				}

				bts = bts[size:]
			}

			b = b[n:]
		}

		// all data has been sent
		if len(b) <= 0 {
			return sent, nil
		}

		// If there is remaining data to be sent,
		// wait until the stream is closed, the window changes, or the deadline is reached.
		// This blocking behavior propagates flow control back to the upper layer (backpressure).
		if err := s.waitCredit(deadline); err != nil {
			return sent, err
		}
	}
}

// waitCredit waits for credit, deadline or closure and records blocked duration without
// spinning.
func (s *stream) waitCredit(deadline <-chan time.Time) error {
	started := time.Now()
	s.sess.flowWaitCount.Add(1)
	defer func() { s.sess.flowWaitNanoseconds.Add(uint64(time.Since(started))) }()
	select {
	case <-s.chWriterWakeup:
		return nil
	case <-s.writeFIN():
		return io.EOF
	case <-s.die:
		return io.ErrClosedPipe
	case <-deadline:
		return ErrTimeout
	case <-s.sess.chSocketWriteError:
		return s.sess.socketWriteError.Load().(error)
	case <-s.chUpdate:
		return nil
	}
}

// Close implements net.Conn
func (s *stream) Close() error {
	var once bool
	s.dieOnce.Do(func() {
		close(s.die)
		once = true
	})

	if !once {
		if s.resetReceived.Load() {
			s.sess.streamClosed(s.id)
		}
		return io.ErrClosedPipe
	}

	var err error
	if s.sess.config.HalfClose {
		s.writeClosed.Store(true)
		graceful := false
		if s.finSent.Load() {
			select {
			case <-s.chFinEvent:
				graceful = true
			default:
			}
		}
		if !graceful {
			f := newFrame(byte(s.sess.config.Version), cmdRST, s.id)
			timer := time.NewTimer(openCloseTimeout)
			_, err = s.sess.writeFrameInternal(f, timer.C, CLSCTRL)
			timer.Stop()
		}
	} else {
		err = s.closeWrite()
	}
	s.sess.streamClosed(s.id)
	return err
}

// CloseWrite sends ordered EOF while leaving the read direction open.
// Call it only after the writer has returned, as with TCP CloseWrite.
func (s *stream) CloseWrite() error {
	if !s.sess.config.HalfClose {
		return s.Close()
	}
	return s.closeWrite()
}

// closeWrite sends ordered directional EOF once, after prior writes, while leaving read
// ownership intact.
func (s *stream) closeWrite() error {
	s.writeCloseOnce.Do(func() {
		s.writeClosed.Store(true)
		f := newFrame(byte(s.sess.config.Version), cmdFIN, s.id)
		timer := time.NewTimer(openCloseTimeout)
		defer timer.Stop()
		_, s.writeCloseErr = s.sess.writeFrameInternal(f, timer.C, CLSDATA)
		if s.writeCloseErr == nil {
			s.finSent.Store(true)
		}
	})
	return s.writeCloseErr
}

// writeFIN returns the legacy full-close signal only when directional half-close is disabled.
func (s *stream) writeFIN() <-chan struct{} {
	if s.sess.config.HalfClose {
		return nil
	}
	return s.chFinEvent
}

// GetDieCh returns a readonly chan which can be readable
// when the stream is to be closed.
func (s *stream) GetDieCh() <-chan struct{} {
	return s.die
}

// SetReadDeadline sets the read deadline as defined by
// net.Conn.SetReadDeadline.
// A zero time value disables the deadline.
func (s *stream) SetReadDeadline(t time.Time) error {
	s.readDeadline.Store(t)
	s.wakeupReader()
	return nil
}

// SetWriteDeadline sets the write deadline as defined by
// net.Conn.SetWriteDeadline.
// A zero time value disables the deadline.
func (s *stream) SetWriteDeadline(t time.Time) error {
	s.writeDeadline.Store(t)
	s.wakeupWriter()
	return nil
}

// SetDeadline sets both read and write deadlines as defined by
// net.Conn.SetDeadline.
// A zero time value disables the deadlines.
func (s *stream) SetDeadline(t time.Time) error {
	if err := s.SetReadDeadline(t); err != nil {
		return err
	}
	if err := s.SetWriteDeadline(t); err != nil {
		return err
	}
	return nil
}

// session closes
func (s *stream) sessionClose() { s.dieOnce.Do(func() { close(s.die) }) }

// reset marks remote abandonment and unblocks both directions without advertising undelivered
// bytes.
func (s *stream) reset() {
	s.resetReceived.Store(true)
	s.writeClosed.Store(true)
	s.sessionClose()
}

// LocalAddr satisfies net.Conn interface
func (s *stream) LocalAddr() net.Addr {
	if ts, ok := s.sess.conn.(interface {
		LocalAddr() net.Addr
	}); ok {
		return ts.LocalAddr()
	}
	return nil
}

// RemoteAddr satisfies net.Conn interface
func (s *stream) RemoteAddr() net.Addr {
	if ts, ok := s.sess.conn.(interface {
		RemoteAddr() net.Addr
	}); ok {
		return ts.RemoteAddr()
	}
	return nil
}

// pushBytes append buf to buffers
func (s *stream) pushBytes(pbuf *[]byte) {
	s.bufferLock.Lock()
	defer s.bufferLock.Unlock()
	s.bufferRing.push(*pbuf, pbuf)
}

// recycleTokens transform remaining bytes to tokens(will truncate buffer)
func (s *stream) recycleTokens() (n int) {
	s.bufferLock.Lock()
	defer s.bufferLock.Unlock()
	for s.bufferRing.len() > 0 {
		buf, head, _ := s.bufferRing.pop()
		n += len(buf)
		defaultAllocator.Put(head)
	}
	return
}

// wakeupReader notifies read process
func (s *stream) wakeupReader() {
	select {
	case s.chReaderWakeup <- struct{}{}:
	default:
	}
}

// wakeupWriter notifies write process
func (s *stream) wakeupWriter() {
	select {
	case s.chWriterWakeup <- struct{}{}:
	default:
	}
}

// update command
func (s *stream) update(consumed uint32, window uint32) {
	// Both hint and reliable updates are serialized by session.streamLock.
	// Delayed reliable frames cannot rewind cumulative credit after a hint.
	if s.sess.config.CreditHints {
		previous := atomic.LoadUint32(&s.peerConsumed)
		written := atomic.LoadUint32(&s.numWritten)
		if int32(consumed-previous) < 0 || int32(written-consumed) < 0 || window > 0x7fffffff {
			return
		}
	}
	// update peer consumed and window size immediately
	atomic.StoreUint32(&s.peerConsumed, consumed)
	atomic.StoreUint32(&s.peerWindow, window)

	// notify write process
	select {
	case s.chUpdate <- struct{}{}:
	default:
	}
}

// mark this stream has been closed in protocol, i.e. receive EOF
func (s *stream) fin() {
	s.finEventOnce.Do(func() {
		close(s.chFinEvent)
	})
}

// stopTimer stops the supplied timer and drains its channel if needed.
func stopTimer(t *time.Timer) {
	if t == nil {
		return
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}
