// File receive_path_test.go checks receive fast paths against the original
// heap-only implementation, including payload ownership and emitted feedback.
package kcp

import (
	"bytes"
	"container/heap"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"
)

// heapOnlyParseData is the pre-optimization receive algorithm. It deliberately
// pushes ordered input and pops/reinserts a blocked root as a behavioral oracle.
func heapOnlyParseData(k *KCP, seg segment) bool {
	if _itimediff(seg.sn, k.rcv_nxt+k.rcv_wnd) >= 0 || _itimediff(seg.sn, k.rcv_nxt) < 0 {
		return true
	}
	repeat := k.rcv_buf.Has(seg.sn)
	if !repeat {
		owned := defaultBufferPool.Get()[:len(seg.data)]
		copy(owned, seg.data)
		seg.data = owned
		heap.Push(k.rcv_buf, seg)
	}
	heapOnlyDrain(k)
	return repeat
}

// heapOnlyDrain preserves the original root removal/reinsertion so comparisons
// cannot accidentally share the candidate's gap or backpressure implementation.
func heapOnlyDrain(k *KCP) {
	for k.rcv_buf.Len() > 0 {
		seg := heap.Pop(k.rcv_buf).(segment)
		if seg.sn == k.rcv_nxt && k.rcv_queue.Len() < int(k.rcv_wnd) {
			k.rcv_queue.Push(seg)
			k.rcv_nxt++
		} else {
			heap.Push(k.rcv_buf, seg)
			break
		}
	}
}

// heapOnlyRecv retains the original reader/error/window-reopening behavior.
func heapOnlyRecv(k *KCP, buffer []byte) int {
	size := k.PeekSize()
	if size < 0 {
		return -1
	}
	if size > len(buffer) {
		return -2
	}
	wasFull := k.rcv_queue.Len() >= int(k.rcv_wnd)
	n := 0
	for {
		seg, ok := k.rcv_queue.Pop()
		if !ok {
			break
		}
		copy(buffer[n:], seg.data)
		n += len(seg.data)
		k.recycleSegment(&seg)
		if seg.frg == 0 {
			break
		}
	}
	heapOnlyDrain(k)
	if k.rcv_queue.Len() < int(k.rcv_wnd) && wasFull {
		k.probe |= IKCP_ASK_TELL
	}
	return n
}

// releaseReceiveFixture returns every owned payload, including undeliverable
// fragments/gaps, so long traces do not bias following tests or benchmarks.
func releaseReceiveFixture(k *KCP) {
	for k.rcv_queue.Len() > 0 {
		seg, _ := k.rcv_queue.Pop()
		k.recycleSegment(&seg)
	}
	for k.rcv_buf.Len() > 0 {
		seg := heap.Pop(k.rcv_buf).(segment)
		k.recycleSegment(&seg)
	}
}

// assertReceiveState compares logical queue/heap membership rather than heap
// layout. Equal retained metadata, bytes, window and next SN imply equal reads.
func assertReceiveState(t *testing.T, a, b *KCP, step int) {
	t.Helper()
	if a.rcv_nxt != b.rcv_nxt || a.probe != b.probe || a.receivedBytes != b.receivedBytes || a.PeekSize() != b.PeekSize() {
		t.Fatalf("reader/window mismatch at step %d", step)
	}
	queue := func(k *KCP) []segment {
		var out []segment
		for seg := range k.rcv_queue.ForEach {
			out = append(out, *seg)
		}
		return out
	}
	retained := func(k *KCP) []segment {
		if k.rcv_buf.Len() == 0 {
			return nil
		}
		out := slices.Clone(k.rcv_buf.segments)
		slices.SortFunc(out, func(a, b segment) int { return int(_itimediff(a.sn, b.sn)) })
		return out
	}
	if !reflect.DeepEqual(queue(a), queue(b)) || !reflect.DeepEqual(retained(a), retained(b)) || !reflect.DeepEqual(a.rcv_buf.marks, b.rcv_buf.marks) {
		t.Fatalf("retained metadata/ownership mismatch at step %d", step)
	}
}

// TestReceivePathsMatchHeapOnlyTrace covers gaps, duplicates, sequence/time wrap,
// queue exhaustion, window changes, fragmentation and small destination errors.
// Every operation checks state and both endpoints emit byte-identical feedback.
func TestReceivePathsMatchHeapOnlyTrace(t *testing.T) {
	for _, base := range []uint32{0, 0xfffffff0} {
		for _, seed := range []int64{1, 7, 7411} {
			t.Run(fmt.Sprintf("base-%x/seed-%d", base, seed), func(t *testing.T) {
				rng := rand.New(rand.NewSource(seed))
				now := uint32(0xfffffff0)
				var wire [2]bytes.Buffer
				var ends [2]*KCP
				for i := range ends {
					ends[i] = NewKCP(91, func(data []byte, n int) { wire[i].Write(data[:n]) })
					ends[i].clock = func() uint32 { return now }
					ends[i].rcv_nxt, ends[i].rcv_wnd = base, 16
					defer releaseReceiveFixture(ends[i])
				}
				a, b := ends[0], ends[1]
				for step := 0; step < 6000; step++ {
					now += uint32(rng.Intn(8))
					if step%113 == 0 {
						a.rcv_wnd = uint32(1 + rng.Intn(64))
						b.rcv_wnd = a.rcv_wnd
					}
					if rng.Intn(3) != 0 {
						sn := a.rcv_nxt + uint32(rng.Intn(72)-4)
						if step%4 == 0 {
							sn = a.rcv_nxt // force gap filling as well as sparse input
						}
						payload := make([]byte, 1+rng.Intn(256))
						rng.Read(payload)
						seg := segment{conv: 91, cmd: IKCP_CMD_PUSH, sn: sn, ts: now, frg: uint8(rng.Intn(4)), data: payload}
						if _itimediff(sn, a.rcv_nxt+a.rcv_wnd) < 0 {
							a.ack_push(sn, now)
							b.ack_push(sn, now)
						}
						ar, br := a.parse_data(seg), heapOnlyParseData(b, seg)
						if ar != br {
							t.Fatalf("duplicate decision at step %d", step)
						}
						if !ar {
							a.receivedBytes += uint64(len(payload))
							b.receivedBytes += uint64(len(payload))
						}
						clear(payload) // neither endpoint may retain the caller's scratch
					} else {
						bufA := make([]byte, rng.Intn(1024))
						bufB := make([]byte, len(bufA))
						an, bn := a.Recv(bufA), heapOnlyRecv(b, bufB)
						if an != bn || !bytes.Equal(bufA, bufB) {
							t.Fatalf("read mismatch at step %d", step)
						}
					}
					assertReceiveState(t, a, b, step)
					a.flush(IKCP_FLUSH_ACKONLY)
					b.flush(IKCP_FLUSH_ACKONLY)
					if !bytes.Equal(wire[0].Bytes(), wire[1].Bytes()) {
						t.Fatalf("wire feedback mismatch at step %d", step)
					}
					wire[0].Reset()
					wire[1].Reset()
				}
			})
		}
	}
}

// BenchmarkReceivePaths separates ordered delivery, persistent gaps and full
// queues. The oracle and candidate share payload/reader work, not heap decisions.
func BenchmarkReceivePaths(b *testing.B) {
	for _, name := range []string{"ordered", "gap", "full", "reverse-64"} {
		for _, original := range []bool{true, false} {
			b.Run(fmt.Sprintf("%s/heap-only-%t", name, original), func(b *testing.B) {
				k := NewKCP(91, func([]byte, int) {})
				k.rcv_wnd = 128
				defer releaseReceiveFixture(k)
				payload, buffer := make([]byte, 1326), make([]byte, 1326)
				parse, recv := (*KCP).parse_data, (*KCP).Recv
				if original {
					parse, recv = heapOnlyParseData, heapOnlyRecv
				}
				if name == "gap" {
					parse(k, segment{sn: 1, data: payload})
				} else if name == "full" {
					k.rcv_wnd = 1
					parse(k, segment{sn: 0, data: payload})
					parse(k, segment{sn: 1, data: payload})
				}
				b.ReportAllocs()
				if name == "ordered" {
					b.SetBytes(int64(len(payload)))
				} else if name == "reverse-64" {
					b.SetBytes(int64(64 * len(payload)))
				}
				b.ResetTimer()
				for range b.N {
					switch name {
					case "ordered":
						parse(k, segment{sn: k.rcv_nxt, data: payload})
						recv(k, buffer)
					case "gap", "full":
						parse(k, segment{sn: 1, data: payload})
					case "reverse-64":
						base := k.rcv_nxt
						for i := 63; i >= 0; i-- {
							parse(k, segment{sn: base + uint32(i), data: payload})
						}
						for range 64 {
							recv(k, buffer)
						}
					}
				}
			})
		}
	}
}
