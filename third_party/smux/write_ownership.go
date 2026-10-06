// File write_ownership.go preserves payload and completion ownership when a
// caller times out before the shared carrier writer has finished.
package smux

import (
	"sync"
	"sync/atomic"
)

// writeOwnership has one caller reference and one queued/sender reference.
// Pool reuse is legal only after both release it, including abandoned results.
type writeOwnership struct {
	refs     atomic.Int32
	canceled atomic.Bool
	result   chan writeResult
	payload  *[]byte
}

// writeOwnershipPool reuses metadata and completion channels on the normal path.
// Payloads use bounded size classes; tiny controls do not reserve a bulk buffer.
var writeOwnershipPool = sync.Pool{New: func() any { return &writeOwnership{result: make(chan writeResult, 1)} }}

// newWriteOwnership snapshots payload before it crosses the asynchronous queue.
// A timed-out vector write can remain blocked in KCP; recycling its caller's
// scratch buffer must not change the eventual transmitted bytes.
func newWriteOwnership(f *Frame) *writeOwnership {
	o := writeOwnershipPool.Get().(*writeOwnership)
	o.refs.Store(2)
	o.canceled.Store(false)
	if len(f.data) > 0 {
		o.payload = defaultAllocator.Get(len(f.data))
		copy(*o.payload, f.data)
		f.data = *o.payload
	}
	return o
}

// release returns storage only when neither producer nor sender can touch it.
// A timeout may leave a completion buffered, so drain it before channel reuse.
func (o *writeOwnership) release() {
	if o.refs.Add(-1) != 0 {
		return
	}
	if o.payload != nil {
		defaultAllocator.Put(o.payload)
		o.payload = nil
	}
	select {
	case <-o.result:
	default:
	}
	writeOwnershipPool.Put(o)
}

// releasePendingWrites releases admitted work on session/output termination.
// Wait for heap publication to stop before draining both bounded queues. A
// producer racing termination may publish after this drain; such a dead-session
// entry remains GC-owned, rather than being prematurely returned to a pool.
func (s *Session) releasePendingWrites() {
	<-s.shaperDone
	for {
		r, ok := s.sq.Pop()
		if !ok {
			break
		}
		if r.owner != nil {
			refundUnsentFrame(r.frame)
			r.owner.release()
		}
	}
	for {
		select {
		case r := <-s.shaper:
			if r.owner != nil {
				refundUnsentFrame(r.frame)
				r.owner.release()
			}
		default:
			return
		}
	}
}

// refundUnsentFrame releases a stream's pre-reserved byte credit only when its
// frame was never dispatched. In-flight timeout retains credit because those
// bytes can still be accepted by the underlying full-duplex carrier writer.
func refundUnsentFrame(f Frame) {
	if f.credit == nil || len(f.data) == 0 {
		return
	}
	atomic.AddUint32(&f.credit.numWritten, uint32(0)-uint32(len(f.data)))
	select {
	case f.credit.chUpdate <- struct{}{}:
	default:
	}
}
