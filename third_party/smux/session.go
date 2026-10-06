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

// File session.go: demultiplexes logical streams and coordinates shared receive tokens, output
// scheduling and session failures.

package smux

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultAcceptBacklog bounds streams awaiting application acceptance without one unbounded
	// admission queue.
	defaultAcceptBacklog = 1024
	// minShaperNotifySize defines when pending output should wake the shared sender rather than
	// notifying for every byte.
	minShaperNotifySize = 16
	// maxShaperSize bounds queued sender requests and propagates backpressure to writers.
	maxShaperSize    = 1024
	openCloseTimeout = 30 * time.Second // Timeout for opening/closing streams
)

// resultChanPool reduces allocation of result channels
var resultChanPool = sync.Pool{
	New: func() any {
		return make(chan writeResult, 1)
	},
}

// CLASSID represents the class of a frame
type CLASSID int

const (
	CLSCTRL CLASSID = iota // prioritized control signal
	// CLSDATA labels ordered application/lifecycle work separately from prioritized control
	// requests.
	CLSDATA
)

// timeoutError representing timeouts for operations such as accept, read and write
//
// To better cooperate with the standard library, timeoutError should implement the standard library's `net.Error`.
//
// For example, using smux to implement net.Listener and work with http.Server, the keep-alive connection (*smux.Stream) will be unexpectedly closed.
// For more details, see https://github.com/xtaci/smux/pull/99.
type timeoutError struct{}

// Error formats the stored failure for callers without changing its error classification.
func (timeoutError) Error() string { return "timeout" }

// Temporary exposes the error's transient classification through the compatibility network-
// error contract.
func (timeoutError) Temporary() bool { return true }

// Timeout identifies deadline expiry for callers that distinguish timeouts from permanent
// transport failures.
func (timeoutError) Timeout() bool { return true }

var (
	// ErrInvalidProtocol reports a mux version/command/layout violation that prevents safe
	// continued parsing.
	ErrInvalidProtocol = errors.New("invalid protocol")
	// ErrConsumed reports a peer consumed-byte count inconsistent with sent data.
	ErrConsumed = errors.New("peer consumed more than sent")
	// ErrGoAway reports rejection of further session work after the session stops accepting
	// streams.
	ErrGoAway = errors.New("stream id overflows, should start a new connection")
	// ErrTimeout reports logical stream deadline expiry rather than silently abandoning queued
	// work.
	ErrTimeout net.Error = &timeoutError{}
	// ErrWouldBlock lets nonblocking stream readers distinguish no available bytes from EOF.
	ErrWouldBlock = errors.New("operation would block on IO")
)

// writeRequest represents a request to write a frame
type writeRequest struct {
	// Scheduling category separating control from ordered data work.
	class CLASSID
	// Output frame retained until the shared sender reports completion.
	frame Frame
	// Scheduling sequence preserving order among comparable requests.
	seq uint32
	// One request completion signal reporting accepted bytes or a carrier error.
	result chan writeResult
}

// writeResult represents the result of a write request
type writeResult struct {
	// Accepted output byte count, not the size of an attempted buffer.
	n int
	// Failure propagated with the result rather than silently dropping lifecycle/output errors.
	err error
}

// Session defines a multiplexed connection for streams
type Session struct {
	// Cumulative waits for per-stream remote credit.
	flowWaitCount atomic.Uint64
	// Cumulative credit-blocked duration without a dedicated per-stream timer.
	flowWaitNanoseconds atomic.Uint64
	// Count of expedited feedback attempts; reliable fallback remains independently queued.
	creditHintsSent atomic.Uint64
	// Count of decoded feedback hints, subject to cumulative-credit validation.
	creditHintsReceived atomic.Uint64
	// Shared carrier owned by this mux session; logical streams do not close it individually.
	conn io.ReadWriteCloser

	// Prepared mux configuration treated as immutable after session construction.
	config       *Config
	goAway       int32  // flag id exhausted
	nextStreamID uint32 // next stream identifier
	// Serializes new stream-ID assignment across concurrent opens.
	nextStreamIDLock sync.Mutex

	bucket       int32         // token bucket
	bucketNotify chan struct{} // used for waiting for tokens
	// True only while a parsed data frame waits for application receive capacity.
	receiveBlocked atomic.Bool

	streams    map[uint32]*stream // all streams in this session
	streamLock sync.Mutex         // locks streams

	die chan struct{} // flag session has died
	// Closes lifecycle signals once so failure/explicit close can race safely.
	dieOnce sync.Once

	// socket error handling
	socketReadError atomic.Value
	// Published carrier output failure waking pending work instead of silently losing ownership.
	socketWriteError atomic.Value
	// Broadcasts permanent carrier input failure to blocked operations.
	chSocketReadError chan struct{}
	// Broadcasts permanent carrier output failure to blocked operations.
	chSocketWriteError chan struct{}
	// Publishes the first read failure only once.
	socketReadErrorOnce sync.Once
	// Publishes the first output failure only once.
	socketWriteErrorOnce sync.Once

	// smux protocol errors
	protoError atomic.Value
	// Broadcasts an unsafe mux framing/version error.
	chProtoError chan struct{}
	// Publishes the first protocol violation once.
	protoErrorOnce sync.Once

	// Bounded queue of streams awaiting application acceptance.
	chAccepts chan *stream

	sessionIsActive int32        // flag session is active
	acceptDeadline  atomic.Value // deadline for Accept()

	requestID uint32            // Monotonic increasing write request ID
	shaper    chan writeRequest // a shaper for writing
	// Shared output scheduler used by the sole carrier-writing loop.
	sq *shaperQueue
	// Coalesces signals that the sender has queued data/control work.
	chShaperPending chan struct{}
	// Signals scheduler capacity released so blocked enqueue operations can progress.
	chShaperConsumed chan struct{}
	// Protects pending update membership and intrusive FIFO links, not carrier I/O.
	creditMu sync.Mutex
	// At most one coalesced cumulative update per live stream.
	credits map[uint32]*creditUpdate
	// Oldest pending update selected by the shared sender.
	creditHead *creditUpdate
	// Newest pending update, allowing constant-time admission/unlink.
	creditTail *creditUpdate
	// Optional expedited feedback adapter; absence retains ordinary reliable controls.
	creditTransport creditHintTransport
}

// newSession initializes mux queues/credits/lifecycle and launches shared session tasks around
// one carrier.
func newSession(config *Config, conn io.ReadWriteCloser, client bool) *Session {
	s := new(Session)
	s.die = make(chan struct{})
	s.conn = conn
	s.config = config
	s.streams = make(map[uint32]*stream)
	s.chAccepts = make(chan *stream, defaultAcceptBacklog)
	s.bucket = int32(config.MaxReceiveBuffer)
	s.bucketNotify = make(chan struct{}, 1)
	s.shaper = make(chan writeRequest, maxShaperSize)
	s.chSocketReadError = make(chan struct{})
	s.chSocketWriteError = make(chan struct{})
	s.chProtoError = make(chan struct{})
	s.chShaperPending = make(chan struct{}, 1)
	s.chShaperConsumed = make(chan struct{}, 1)
	s.sq = NewShaperQueue()
	s.sq.prioritizeControl = config.PrioritizeControl
	if config.AsyncWindowUpdates {
		s.credits = make(map[uint32]*creditUpdate)
	}
	if config.CreditHints && config.Version == 2 {
		if carrier, ok := conn.(creditHintTransport); ok {
			s.creditTransport = carrier
			carrier.SetCreditHintHandler(s.receiveCreditHint)
		}
	}

	if client {
		s.nextStreamID = 1
	} else {
		s.nextStreamID = 0
	}

	go s.shaperLoop()
	go s.recvLoop()
	go s.sendLoop()
	if !config.KeepAliveDisabled {
		go s.keepalive()
	}
	return s
}

// OpenStream is used to create a new stream
func (s *Session) OpenStream() (*Stream, error) {
	if s.IsClosed() {
		return nil, io.ErrClosedPipe
	}

	// generate stream id
	s.nextStreamIDLock.Lock()
	if s.goAway > 0 {
		s.nextStreamIDLock.Unlock()
		return nil, ErrGoAway
	}

	// check for stream id overflow
	if s.nextStreamID+2 < s.nextStreamID {
		s.goAway = 1
		s.nextStreamIDLock.Unlock()
		return nil, ErrGoAway
	}

	// allocate next stream id
	s.nextStreamID += 2
	sid := s.nextStreamID
	s.nextStreamIDLock.Unlock()

	stream := newStream(sid, s.config.MaxFrameSize, s)
	// A duplex carrier may deliver the peer's reply before its SYN Write
	// reports completion. Publish receive ownership first so early data and
	// credit are retained, and pending opens count as live carrier users.
	s.streamLock.Lock()
	var err error
	select {
	case <-s.chSocketReadError:
		err = s.socketReadError.Load().(error)
	case <-s.chSocketWriteError:
		err = s.socketWriteError.Load().(error)
	case <-s.die:
		err = io.ErrClosedPipe
	default:
		s.streams[sid] = stream
	}
	s.streamLock.Unlock()
	if err != nil {
		return nil, err
	}

	_, err = s.writeControlFrame(newFrame(byte(s.config.Version), cmdSYN, sid))
	if err == nil {
		select {
		case <-s.chSocketReadError:
			err = s.socketReadError.Load().(error)
		case <-s.chSocketWriteError:
			err = s.socketWriteError.Load().(error)
		case <-s.die:
			err = io.ErrClosedPipe
		default:
		}
	}
	if err != nil {
		// No wrapper escapes on failure. Reclaim any early receive tokens
		// and pending credits without waiting for another control write.
		stream.sessionClose()
		s.streamClosed(sid)
		return nil, err
	}
	return &Stream{stream: stream}, nil
}

// Open returns a generic ReadWriteCloser
func (s *Session) Open() (io.ReadWriteCloser, error) {
	return s.OpenStream()
}

// AcceptStream is used to block until the next available stream
// is ready to be accepted.
func (s *Session) AcceptStream() (*Stream, error) {
	var deadline <-chan time.Time
	if d, ok := s.acceptDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer := time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	select {
	case stream := <-s.chAccepts:
		// The session owns the backing stream until explicit Close/session teardown.
		// Promoted I/O methods retain that backing stream, not necessarily this
		// wrapper. A wrapper finalizer could therefore close active I/O during GC.
		// Match OpenStream's explicit ownership rather than using GC as a reset.
		return &Stream{stream: stream}, nil
	case <-deadline:
		return nil, ErrTimeout
	case <-s.chSocketReadError:
		return nil, s.socketReadError.Load().(error)
	case <-s.chProtoError:
		return nil, s.protoError.Load().(error)
	case <-s.die:
		return nil, io.ErrClosedPipe
	}
}

// Accept Returns a generic ReadWriteCloser instead of smux.Stream
func (s *Session) Accept() (io.ReadWriteCloser, error) {
	return s.AcceptStream()
}

// Close is used to close the session and all streams.
func (s *Session) Close() error {
	var once bool
	s.dieOnce.Do(func() {
		close(s.die)
		once = true
	})

	if !once {
		return io.ErrClosedPipe
	}

	s.streamLock.Lock()
	for k := range s.streams {
		s.streams[k].sessionClose()
	}
	s.streamLock.Unlock()
	s.clearCredits()
	return s.conn.Close()
}

// CloseChan can be used by someone who wants to be notified immediately when this
// session is closed
func (s *Session) CloseChan() <-chan struct{} {
	return s.die
}

// notifyBucket notifies recvLoop that bucket is available
func (s *Session) notifyBucket() {
	select {
	case s.bucketNotify <- struct{}{}:
	default:
	}
}

// notifyReadError publishes input failure and releases pending stream reads/accepts.
func (s *Session) notifyReadError(err error) {
	s.socketReadErrorOnce.Do(func() {
		s.socketReadError.Store(err)
		close(s.chSocketReadError)
	})
}

// notifyWriteError publishes asynchronous carrier failure and wakes blocked readers/writers
// while clearing pending work.
func (s *Session) notifyWriteError(err error) {
	s.socketWriteErrorOnce.Do(func() {
		s.socketWriteError.Store(err)
		close(s.chSocketWriteError)
	})
}

// notifyProtoError reports invalid frame semantics so malformed input does not continue on an
// ambiguous session.
func (s *Session) notifyProtoError(err error) {
	s.protoErrorOnce.Do(func() {
		s.protoError.Store(err)
		close(s.chProtoError)
	})
}

// IsClosed does a safe check to see if we have shutdown
func (s *Session) IsClosed() bool {
	select {
	case <-s.die:
		return true
	default:
		return false
	}
}

// NumStreams returns the number of currently open streams
func (s *Session) NumStreams() int {
	if s.IsClosed() {
		return 0
	}
	s.streamLock.Lock()
	defer s.streamLock.Unlock()
	return len(s.streams)
}

// SetDeadline sets a deadline used by Accept* calls.
// A zero time value disables the deadline.
func (s *Session) SetDeadline(t time.Time) error {
	s.acceptDeadline.Store(t)
	return nil
}

// LocalAddr satisfies net.Conn interface
func (s *Session) LocalAddr() net.Addr {
	if ts, ok := s.conn.(interface {
		LocalAddr() net.Addr
	}); ok {
		return ts.LocalAddr()
	}
	return nil
}

// RemoteAddr satisfies net.Conn interface
func (s *Session) RemoteAddr() net.Addr {
	if ts, ok := s.conn.(interface {
		RemoteAddr() net.Addr
	}); ok {
		return ts.RemoteAddr()
	}
	return nil
}

// notify the session that a stream has closed
func (s *Session) streamClosed(sid uint32) {
	s.streamLock.Lock()
	defer s.streamLock.Unlock()

	stream, ok := s.streams[sid]
	if !ok {
		return
	}

	if n := stream.recycleTokens(); n > 0 {
		// return remaining tokens to the bucket
		if atomic.AddInt32(&s.bucket, int32(n)) > 0 {
			s.notifyBucket()
		}
	}
	delete(s.streams, sid)
	s.removeCredit(sid)
}

// returnTokens is called by stream to return token after read
func (s *Session) returnTokens(n int) {
	if atomic.AddInt32(&s.bucket, int32(n)) > 0 {
		s.notifyBucket()
	}
}

// recvLoop parses control feedback independently of application receive tokens;
// payload allocation waits for capacity before reading the next data body.
func (s *Session) recvLoop() {
	var hdr rawHeader
	var updHdr updHeader

	for {
		// Parse control headers even when application buffers are full. Credits,
		// FIN and reset feedback do not consume data tokens; blocking them here
		// can deadlock the opposite direction behind unrelated slow readers.

		// Read one fixed-size header; payload admission remains bounded below.
		_, err := io.ReadFull(s.conn, hdr[:])
		if err != nil {
			s.notifyReadError(err)
			return
		}

		// Mark the session as active
		atomic.StoreInt32(&s.sessionIsActive, 1)

		// validate protocol version
		if hdr.Version() != byte(s.config.Version) {
			s.notifyProtoError(ErrInvalidProtocol)
			return
		}

		// handle different command types
		sid := hdr.StreamID()
		switch hdr.Cmd() {
		case cmdNOP:
		case cmdSYN: // stream opening
			var accepted *stream
			s.streamLock.Lock()
			if s.IsClosed() {
				s.streamLock.Unlock()
				return
			}
			if _, ok := s.streams[sid]; !ok {
				stream := newStream(sid, s.config.MaxFrameSize, s)
				s.streams[sid] = stream
				accepted = stream
			}
			s.streamLock.Unlock()

			if accepted != nil {
				select {
				case s.chAccepts <- accepted:
				case <-s.die:
				}
			}

		case cmdFIN: // stream closing
			s.streamLock.Lock()
			if stream, ok := s.streams[sid]; ok {
				stream.fin() // fin unblocks the readers and writers
			}
			s.streamLock.Unlock()

		case cmdRST:
			if !s.config.HalfClose || hdr.Length() != 0 {
				s.notifyProtoError(ErrInvalidProtocol)
				return
			}
			s.streamLock.Lock()
			if stream, ok := s.streams[sid]; ok {
				stream.reset()
			}
			s.streamLock.Unlock()

		case cmdPSH: // data frame
			if hdr.Length() == 0 {
				continue
			}

			// Data retains the existing receive bound. Only the already parsed
			// fixed-size header sits outside it while payload admission waits.
			s.receiveBlocked.Store(atomic.LoadInt32(&s.bucket) <= 0)
			for atomic.LoadInt32(&s.bucket) <= 0 && !s.IsClosed() {
				select {
				case <-s.bucketNotify:
				case <-s.die:
					// If it returns here, Accept() and OpenStream() are unblocked with io.ErrClosedPipe,
					// causing recvLoop to exit gracefully. If recvLoop is blocked in io.ReadFull, however,
					// it will be unblocked by a socket read error instead.
					s.receiveBlocked.Store(false)
					return
				}
			}

			s.receiveBlocked.Store(false)
			// read payload from the underlying connection
			pNewbuf := defaultAllocator.Get(int(hdr.Length()))
			written, err := io.ReadFull(s.conn, *pNewbuf)
			if err != nil {
				s.notifyReadError(err)

				// recycle the buffer immediately.
				defaultAllocator.Put(pNewbuf)
				return
			}

			// push data to the corresponding stream
			s.streamLock.Lock()
			if stream, ok := s.streams[sid]; ok && !stream.resetReceived.Load() {
				stream.pushBytes(pNewbuf)
				// deduct tokens from the bucket
				atomic.AddInt32(&s.bucket, -int32(written))
				stream.wakeupReader()
			} else {
				// data directed to a missing/closed stream, recycle the buffer immediately.
				defaultAllocator.Put(pNewbuf)
			}
			s.streamLock.Unlock()

		case cmdUPD: // a window update signal (v2 only)
			if s.config.Version != 2 {
				s.notifyProtoError(ErrInvalidProtocol)
				return
			}

			_, err := io.ReadFull(s.conn, updHdr[:])
			if err != nil {
				s.notifyReadError(err)
				return
			}

			// update the window size for the corresponding stream
			s.streamLock.Lock()
			if stream, ok := s.streams[sid]; ok {
				stream.update(updHdr.Consumed(), updHdr.Window())
			}
			s.streamLock.Unlock()

		default:
			s.notifyProtoError(ErrInvalidProtocol)
			return
		}
	}
}

// keepalive sends NOP frames periodically to keep the connection alive
func (s *Session) keepalive() {
	tickerPing := time.NewTicker(s.config.KeepAliveInterval)
	tickerTimeout := time.NewTicker(s.config.KeepAliveTimeout)
	defer tickerPing.Stop()
	defer tickerTimeout.Stop()
	for {
		select {
		case <-tickerPing.C:
			s.writeFrameInternal(newFrame(byte(s.config.Version), cmdNOP, 0), tickerPing.C, CLSCTRL)
			s.notifyBucket() // force a wakeup signal to the recvLoop
		case <-tickerTimeout.C:
			if !atomic.CompareAndSwapInt32(&s.sessionIsActive, 1, 0) {
				// recvLoop may block while bucket is 0, in this case,
				// session should not be closed.
				if atomic.LoadInt32(&s.bucket) > 0 {
					s.Close()
					return
				}
			}
		case <-s.die:
			return
		}
	}
}

// shaperLoop implements a priority queue and bandwidth shaping for write requests.
// Eg: Control messages are prioritized over data messages, and shaper tries
// it's best to keep fair bandwidth among streams.
func (s *Session) shaperLoop() {
	chShaper := s.shaper

	for {
		select {
		case <-s.die:
			return
		case r := <-chShaper:
			s.sq.Push(r)
			// notify sendLoop there are pending requests
			if len(chShaper) == 0 || s.sq.Len() > minShaperNotifySize {
				s.notifyShaperPending()
			}

			if s.sq.Len() >= maxShaperSize {
				// stop accepting new requests temporarily if shaper queue is full
				chShaper = nil
			}
		case <-s.chShaperConsumed:
			// re-enable shaper channel
			chShaper = s.shaper
		}
	}
}

// notifyShaperPending notifies sendLoop that there are pending requests
func (s *Session) notifyShaperPending() {
	select {
	case s.chShaperPending <- struct{}{}:
	default:
	}
}

// notifyShaperConsumed notifies when shaper queue is being consumed
func (s *Session) notifyShaperConsumed() {
	select {
	case s.chShaperConsumed <- struct{}{}:
	default:
	}
}

// sendLoop sends frames over the underlying connection
func (s *Session) sendLoop() {
	var buf []byte
	var n int
	var err error
	var vec [][]byte // vector for writeBuffers
	controlBurst := 0

	bw, ok := s.conn.(interface {
		WriteBuffers(v [][]byte) (n int, err error)
	})

	if ok {
		buf = make([]byte, headerSize)
		vec = make([][]byte, 2)
	} else {
		buf = make([]byte, (1<<16)+headerSize)
	}

EVENT_LOOP:
	for {
		select {
		case <-s.die:
			return
		case <-s.chShaperPending:
			for {
				// Coalesced credit must not wait for an application reader or
				// indefinitely starve ordinary frames under many active streams.
				var request writeRequest
				var ok bool
				if controlBurst < 16 {
					request, ok = s.popCredit()
				}
				if ok {
					controlBurst++
				} else {
					controlBurst = 0
					request, ok = s.sq.Pop()
					if !ok {
						request, ok = s.popCredit()
					}
				}
				if !ok {
					// notify shaperLoop to accept new requests
					s.notifyShaperConsumed()
					goto EVENT_LOOP
				}

				buf[0] = request.frame.ver
				buf[1] = request.frame.cmd
				binary.LittleEndian.PutUint16(buf[2:], uint16(len(request.frame.data)))
				binary.LittleEndian.PutUint32(buf[4:], request.frame.sid)

				// support for scatter-gather I/O
				if len(vec) > 0 {
					vec[0] = buf[:headerSize]
					vec[1] = request.frame.data
					n, err = bw.WriteBuffers(vec)
				} else {
					copy(buf[headerSize:], request.frame.data)
					n, err = s.conn.Write(buf[:headerSize+len(request.frame.data)])
				}

				n -= headerSize
				if n < 0 {
					n = 0
				}

				result := writeResult{
					n:   n,
					err: err,
				}

				if request.result != nil {
					request.result <- result
				}

				// store conn error
				if err != nil {
					s.notifyWriteError(err)
					if s.config.AsyncWindowUpdates {
						s.Close()
					}
					return
				}
			}
		}
	}
}

// writeControlFrame writes the control frame to the underlying connection
// and returns the number of bytes written if successful
func (s *Session) writeControlFrame(f Frame) (n int, err error) {
	timer := time.NewTimer(openCloseTimeout)
	defer timer.Stop()

	return s.writeFrameInternal(f, timer.C, CLSCTRL)
}

// internal writeFrame version to support deadline used in keepalive
func (s *Session) writeFrameInternal(f Frame, deadline <-chan time.Time, class CLASSID) (int, error) {
	// get result channel from pool
	resultCh := resultChanPool.Get().(chan writeResult)

	req := writeRequest{
		class:  class,
		frame:  f,
		seq:    atomic.AddUint32(&s.requestID, 1),
		result: resultCh,
	}
	select {
	case s.shaper <- req:
	case <-s.die:
		resultChanPool.Put(resultCh)
		return 0, io.ErrClosedPipe
	case <-s.chSocketWriteError:
		resultChanPool.Put(resultCh)
		return 0, s.socketWriteError.Load().(error)
	case <-deadline:
		resultChanPool.Put(resultCh)
		return 0, ErrTimeout
	}

	select {
	case result := <-resultCh:
		resultChanPool.Put(resultCh)
		return result.n, result.err
	case <-s.die:
		// Cannot recycle channel here - sendLoop may still write to it
		return 0, io.ErrClosedPipe
	case <-s.chSocketWriteError:
		// Cannot recycle channel here - sendLoop may still write to it
		return 0, s.socketWriteError.Load().(error)
	case <-deadline:
		// Cannot recycle channel here - sendLoop may still write to it
		return 0, ErrTimeout
	}
}

// ReceiveBufferStats reports the shared application buffer budget without a
// stream scan. Buffered bytes may exceed capacity by one admitted wire frame,
// as before; blocked identifies payload admission rather than ordinary I/O wait.
func (s *Session) ReceiveBufferStats() (capacity, buffered int, blocked bool) {
	capacity = s.config.MaxReceiveBuffer
	buffered = max(0, capacity-int(atomic.LoadInt32(&s.bucket)))
	return capacity, buffered, s.receiveBlocked.Load()
}
