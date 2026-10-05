// File fastack_index_test.go benchmarks sparse outstanding gaps and compares
// indexed fast-ACK evidence with the original linear scan, including SN wrap.
package kcp

import (
	"math/rand"
	"testing"
)

// BenchmarkSparseFastACK isolates the large-window/reordered-ACK hot path seen
// in the deployed MTU128 profile; acknowledged tombstones carry no payload.
func BenchmarkSparseFastACK(b *testing.B) {
	k := NewKCP(1, func([]byte, int) {})
	k.clock = func() uint32 { return 1000 }
	k.snd_una, k.snd_nxt = 0, 8192
	for sn := uint32(0); sn < 8192; sn++ {
		acked := uint32(1)
		if sn%512 == 0 {
			acked = 0
		}
		k.snd_buf.Push(segment{sn: sn, ts: 100, xmit: 1, acked: acked})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k.parse_fastack(8191, 100)
	}
}

// linearFastACK retains the pre-optimization algorithm as an independent
// behavioral oracle. It deliberately visits acknowledged tombstones.
func linearFastACK(kcp *KCP, sn, ts uint32) int {
	ready := 0
	if _itimediff(sn, kcp.snd_una) < 0 || _itimediff(sn, kcp.snd_nxt) >= 0 {
		return 0
	}
	for seg := range kcp.snd_buf.ForEach {
		if _itimediff(sn, seg.sn) < 0 {
			break
		}
		if sn != seg.sn && seg.xmit > 0 && seg.acked == 0 && _itimediff(seg.ts, ts) <= 0 && seg.fastack != 0xffffffff {
			if seg.fastack == 0 {
				seg.gapAt = kcp.now()
			}
			seg.fastack++
			if seg.fastack >= uint32(kcp.fastresend) && kcp.reorderReady(seg, kcp.now()) {
				ready = 1
			}
		}
	}
	return ready
}

// TestFastACKIndexMatchesLinearTrace covers selective and cumulative ACKs,
// fresh appends, ring growth/wrap, SN wrap, unsent paced entries and timestamp
// gating. Every evidence counter and retry decision must match the old scan.
func TestFastACKIndexMatchesLinearTrace(t *testing.T) {
	for _, base := range []uint32{0, 0xffffff00} {
		for _, seed := range []int64{7, 42, 7411} {
			rng := rand.New(rand.NewSource(seed))
			now := uint32(1000)
			indexed := NewKCP(1, func([]byte, int) {})
			linear := NewKCP(1, func([]byte, int) {})
			for _, k := range []*KCP{indexed, linear} {
				k.snd_una, k.snd_nxt = base, base
				k.clock = func() uint32 { return now }
				k.fastresend = 2
				k.reorderGrace = 25
			}
			appendSegments := func(count int) {
				for range count {
					seg := segment{sn: indexed.snd_nxt, ts: now - uint32(rng.Intn(100)), xmit: uint32(rng.Intn(3))}
					for _, k := range []*KCP{indexed, linear} {
						k.snd_buf.Push(seg)
						if k.pendingIndexed {
							stored, _ := k.snd_buf.At(k.snd_buf.Len() - 1)
							k.linkPending(stored)
						}
						k.snd_nxt++
					}
				}
			}
			appendSegments(2048)
			indexed.indexPending()
			for step := 0; step < 3000; step++ {
				now += uint32(rng.Intn(8))
				if step%5 == 0 {
					una := indexed.snd_una + uint32(rng.Intn(8))
					if indexed.parse_una(una) != linear.parse_una(una) {
						t.Fatal("UNA mismatch")
					}
					indexed.shrink_buf()
					linear.shrink_buf()
					appendSegments(rng.Intn(9))
				}
				sn := indexed.snd_una + uint32(rng.Intn(indexed.snd_buf.Len()+16)-8)
				ts := now - uint32(rng.Intn(150))
				if rng.Intn(2) == 0 {
					indexed.parse_ack(sn)
					linear.parse_ack(sn)
				}
				if indexed.parse_fastack(sn, ts) != linearFastACK(linear, sn, ts) {
					t.Fatalf("decision mismatch base=%x seed=%d step=%d", base, seed, step)
				}
				for i := 0; i < indexed.snd_buf.Len(); i++ {
					a, _ := indexed.snd_buf.At(i)
					b, _ := linear.snd_buf.At(i)
					if a.fastack != b.fastack || a.gapAt != b.gapAt || a.acked != b.acked {
						t.Fatalf("evidence mismatch base=%x seed=%d step=%d slot=%d", base, seed, step, i)
					}
				}
			}
		}
	}
}
