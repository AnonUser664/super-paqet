package kcp

import "sync"

// sendQueue grows only for queued work. Its logical ceiling follows the KCP
// send window, with a small ACK/control allowance. Producers never block under
// the session lock; overflow remains observable and recoverable through ARQ.
type sendQueue struct {
	mu     sync.Mutex
	ring   *RingBuffer[sendRequest]
	ready  chan struct{}
	done   chan struct{}
	closed bool
}

func newSendQueue() *sendQueue {
	return &sendQueue{ring: NewRingBuffer[sendRequest](64), ready: make(chan struct{}, 1), done: make(chan struct{})}
}

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

func (q *sendQueue) pop() (sendRequest, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.ring.Pop()
}

func (q *sendQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.ring.Len()
}

func (q *sendQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.done)
	}
}
