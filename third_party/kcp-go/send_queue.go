// File send_queue.go: bounds a growing encryption/output FIFO without a fixed channel that
// silently loses ordinary bursts.

package kcp

import "sync"

// sendQueue grows only for queued work. Its logical ceiling follows the KCP
// send window, with a small ACK/control allowance. Producers never block under
// the session lock; overflow remains observable and recoverable through ARQ.
type sendQueue struct {
	// Protects queue membership, close state and notification decisions.
	mu sync.Mutex
	// FIFO storage owned by the postprocessing queue until pop/drain.
	ring *RingBuffer[sendRequest]
	// Coalesced queue wakeup signal; producers do not block on repeated notifications.
	ready chan struct{}
	// Broadcast lifecycle signal observed by pending work and shutdown.
	done chan struct{}
	// Rejects future work after lifecycle shutdown has begun.
	closed bool
}

// newSendQueue starts a small FIFO that can grow within a live send-window budget instead of
// dropping normal output bursts.
func newSendQueue() *sendQueue {
	return &sendQueue{ring: NewRingBuffer[sendRequest](64), ready: make(chan struct{}, 1), done: make(chan struct{})}
}

// push admits queued output only while open and within the explicit packet budget.
func (q *sendQueue) push(req sendRequest, window int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.ring.Len() >= max(devBacklog, window+maxBatchSize) {
		return false
	}
	empty := q.ring.Len() == 0
	q.ring.Push(req)
	if empty {
		select {
		case q.ready <- struct{}{}:
		default:
		}
	}
	return true
}

// pop removes the oldest pending output without changing FIFO order.
func (q *sendQueue) pop() (sendRequest, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.ring.Pop()
}

// popBatch removes up to len(dst) pending requests under a single lock acquisition.
func (q *sendQueue) popBatch(dst []sendRequest) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for n < len(dst) {
		req, ok := q.ring.Pop()
		if !ok {
			break
		}
		dst[n] = req
		n++
	}
	return n
}

// empty reports whether the queue has no pending output under the lock.
func (q *sendQueue) empty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.ring.Len() == 0
}

// len reads pending output count under the queue's synchronization contract.
func (q *sendQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.ring.Len()
}

// close rejects future pushes and signals the consumer to drain/recycle retained output.
func (q *sendQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.done)
	}
}
