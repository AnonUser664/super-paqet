// File send_queue_test.go: exercises send queue regressions; fixtures must preserve cleanup
// and expose byte/lifecycle failures explicitly.

package kcp

import (
	"encoding/binary"
	"runtime"
	"testing"
)

// TestSendQueueGrowthBudgetAndClose checks Send Queue Growth Budget And Close so a change
// cannot silently weaken the recorded regression contract.
func TestSendQueueGrowthBudgetAndClose(t *testing.T) {
	q := newSendQueue()
	const window = 8192
	for i := 0; i < window+maxBatchSize; i++ {
		if !q.push(sendRequest{buffer: []byte{byte(i)}}, window) {
			t.Fatal("dropped within window budget")
		}
	}
	if q.push(sendRequest{}, window) {
		t.Fatal("queue escaped memory budget")
	}
	q.close()
	q.close()
	if q.push(sendRequest{}, window) {
		t.Fatal("accepted work after consumer shutdown")
	}
	for i := 0; i < window+maxBatchSize; i++ {
		r, ok := q.pop()
		if !ok || r.buffer[0] != byte(i) {
			t.Fatal("queue reordered during growth or shutdown")
		}
	}
	if q.len() != 0 {
		t.Fatal("queue did not drain")
	}
	for _, r := range q.ring.elements {
		if r.buffer != nil {
			t.Fatal("drained queue retained packet memory")
		}
	}
}

// TestSendQueueConcurrentOrdering checks Send Queue Concurrent Ordering so a change cannot
// silently weaken the recorded regression contract.
func TestSendQueueConcurrentOrdering(t *testing.T) {
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
	for i := 0; i < count; {
		r, ok := q.pop()
		if !ok {
			runtime.Gosched()
			continue
		}
		if binary.LittleEndian.Uint64(r.buffer) != uint64(i) {
			t.Fatalf("out of order at %d", i)
		}
		i++
	}
	<-done
}
