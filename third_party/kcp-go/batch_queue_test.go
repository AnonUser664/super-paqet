// File batch_queue_test.go checks bounded FIFO batches across growth, shutdown
// and concurrent producers; popped storage must not retain payload references.
package kcp

import (
	"encoding/binary"
	"runtime"
	"testing"
)

func TestSendQueueBatchConcurrentDrain(t *testing.T) {
	q := newSendQueue()
	const count = 20000
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < count; i++ {
			b := make([]byte, 8)
			binary.LittleEndian.PutUint64(b, uint64(i))
			for !q.push(sendRequest{buffer: b}, 4096) {
				runtime.Gosched()
			}
		}
		q.close()
	}()
	var batch [17]sendRequest
	for next := 0; next < count; {
		n := q.popBatch(batch[:])
		if n == 0 {
			runtime.Gosched()
			continue
		}
		for i := 0; i < n; i++ {
			if binary.LittleEndian.Uint64(batch[i].buffer) != uint64(next) {
				t.Fatalf("FIFO mismatch at %d", next)
			}
			batch[i] = sendRequest{}
			next++
		}
	}
	<-done
	if !q.empty() || q.popBatch(batch[:]) != 0 || q.push(sendRequest{}, 4096) {
		t.Fatal("closed queue did not drain")
	}
	for _, r := range q.ring.elements {
		if r.buffer != nil {
			t.Fatal("queue retained popped payload")
		}
	}
}

func TestSendQueueBatchEmptyDestination(t *testing.T) {
	q := newSendQueue()
	q.push(sendRequest{buffer: []byte{1}}, 1)
	if q.popBatch(nil) != 0 || q.empty() {
		t.Fatal("empty destination consumed work")
	}
	var b [1]sendRequest
	if q.popBatch(b[:]) != 1 || b[0].buffer[0] != 1 || !q.empty() {
		t.Fatal("single-item drain failed")
	}
}
