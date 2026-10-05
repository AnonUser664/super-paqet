// File virtual_link_test.go: exercises virtual link regressions; fixtures must preserve
// cleanup and expose byte/lifecycle failures explicitly.

package kcp

import (
	"bytes"
	"container/heap"
	"encoding/binary"
	"fmt"
	"testing"
)

// Entirely virtual time: no sleeps, sockets, shared RNG or goroutine scheduling.
// Each seed must produce identical deliveries, losses and retransmissions.
type virtualProfile struct {
	name                                                     string
	up, down, delay, jitter, loss, reorder, duplicate, queue int
	burst, outage, slowReader                                bool
	ackOutage, readerPause, rateStep                         bool
}

// virtualPacket retains the virtual Packet fixture state used to expose failures without
// production network side effects.
type virtualPacket struct {
	at, id int64
	side   int
	data   []byte
}

// virtualPackets retains the virtual Packets fixture state used to expose failures without
// production network side effects.
type virtualPackets []virtualPacket

// Len reports retained heap entries for the standard heap interface.
func (p virtualPackets) Len() int { return len(p) }

// Less orders heap entries according to this queue's sequence or deadline comparator.
func (p virtualPackets) Less(i, j int) bool {
	if p[i].at == p[j].at {
		return p[i].id < p[j].id
	}
	return p[i].at < p[j].at
}

// Swap exchanges entries and maintains the heap's indexing/ownership invariants.
func (p virtualPackets) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

// Push accepts a heap element through the standard interface; callers retain the queue's
// synchronization contract.
func (p *virtualPackets) Push(v any) { *p = append(*p, v.(virtualPacket)) }

// Pop removes the final heap slot and releases membership/reference state as required by the
// queue.
func (p *virtualPackets) Pop() any { a := *p; v := a[len(a)-1]; *p = a[:len(a)-1]; return v }

// virtualResult retains the virtual Result fixture state used to expose failures without
// production network side effects.
type virtualResult struct {
	ticks                        int
	sent, dropped, retransmitted uint64
}

// runVirtualLink advances a seeded simulated clock/link until delivery or the scenario bound;
// repeated runs can compare exact outcomes.
func runVirtualLink(t *testing.T, p virtualProfile, seed uint64, paced, wrap bool) virtualResult {
	t.Helper()
	var now, id int64
	var pending virtualPackets
	var next [2]int64
	var queued [2]int
	var sent, dropped uint64
	rand := func(n int) int {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return int(seed % uint64(n))
	}
	var endpoints [2]*KCP
	defer func() {
		for _, k := range endpoints {
			if k == nil {
				continue
			}
			for _, ring := range []*RingBuffer[segment]{k.snd_queue, k.snd_buf, k.rcv_queue} {
				for ring.Len() > 0 {
					seg, _ := ring.Pop()
					k.recycleSegment(&seg)
				}
			}
			for k.rcv_buf.Len() > 0 {
				seg := heap.Pop(k.rcv_buf).(segment)
				k.recycleSegment(&seg)
			}
		}
	}()
	for side := 0; side < 2; side++ {
		endpoints[side] = NewKCP(91, func(data []byte, n int) {
			sent++
			if ((p.outage || (p.ackOutage && side == 1)) && now >= 2000000 && now < 4000000) || rand(10000) < p.loss || (p.burst && sent%31 >= 27) || queued[side] >= p.queue {
				dropped++
				return
			}
			rate := p.up
			if side == 1 {
				rate = p.down
			}
			if p.rateStep && now >= 1000000 && now < 3000000 {
				rate = max(1, rate/4)
			}
			// Include outer transport overhead, without rewriting the payload.
			next[side] = max(next[side], now) + int64(n+80)*1000000/int64(rate)
			delay := p.delay
			if p.jitter > 0 {
				delay = max(0, delay+rand(2*p.jitter+1)-p.jitter)
			}
			at := next[side] + int64(delay)*1000
			if rand(10000) < p.reorder {
				at += int64(p.delay/2+5) * 1000
			}
			id++
			heap.Push(&pending, virtualPacket{at, id, side, bytes.Clone(data[:n])})
			queued[side]++
			if rand(10000) < p.duplicate {
				id++
				heap.Push(&pending, virtualPacket{at + 7000, id, side, bytes.Clone(data[:n])})
				queued[side]++
			}
		})
		k := endpoints[side]
		k.clock = func() uint32 {
			value := uint32(now / 1000)
			if wrap {
				value += 0xfffffff0
			}
			return value
		}
		k.NoDelay(1, 10, 2, 1)
		k.ackTimestamps = paced // Exercise optional timestamp ACKs and legacy ACKs.
		k.WndSize(512, 512)
		if p.readerPause {
			k.WndSize(512, 32)
		}
		k.stream = 1
		if wrap {
			k.snd_nxt = 0xfffffff0
			k.snd_una = k.snd_nxt
			k.rcv_nxt = k.snd_nxt
		}
		if paced {
			k.pacingRate = uint64(p.up * 12 / 10)
			if side == 1 {
				k.pacingRate = uint64(p.down * 12 / 10)
			}
			k.pacingLast = k.now()
			k.pacingTokens = float64(k.mtu * 2)
		}
	}
	data := make([]byte, 512<<10)
	for i := 0; i < len(data); i += 8 {
		binary.LittleEndian.PutUint64(data[i:], uint64(i)^seed)
	}
	var received bytes.Buffer
	position := 0
	buffer := make([]byte, 65536)
	for tick := 0; tick < 180000; tick++ {
		now = int64(tick) * 1000
		for pending.Len() > 0 && pending[0].at <= now {
			packet := heap.Pop(&pending).(virtualPacket)
			queued[packet.side]--
			if code := endpoints[1-packet.side].Input(packet.data, IKCP_PACKET_REGULAR, true); code != 0 {
				t.Fatalf("input rejected valid frame: %d", code)
			}
		}
		if position < len(data) && endpoints[0].WaitSnd() < 512 {
			end := min(len(data), position+8192)
			if endpoints[0].Send(data[position:end]) != 0 {
				t.Fatal("send failed")
			}
			position = end
			endpoints[0].flush(IKCP_FLUSH_NEW)
		}
		for _, k := range endpoints {
			k.Update()
		}
		if (!p.slowReader || tick%20 == 0) && !(p.readerPause && tick >= 1000 && tick < 3000) {
			for endpoints[1].PeekSize() > 0 {
				n := endpoints[1].Recv(buffer)
				if n < 0 {
					t.Fatal("read failed")
				}
				received.Write(buffer[:n])
			}
		}
		if received.Len() == len(data) {
			if !bytes.Equal(received.Bytes(), data) {
				t.Fatal("ordered integrity failed")
			}
			for _, k := range endpoints {
				for k.PeekSize() > 0 {
					k.Recv(buffer)
				}
			}
			return virtualResult{tick, sent, dropped, endpoints[0].retransmittedSegments}
		}
	}
	t.Fatalf("stalled: profile=%s sent=%d received=%d pending=%d drops=%d", p.name, position, received.Len(), endpoints[0].WaitSnd(), dropped)
	return virtualResult{}
}

// TestVirtualLinkDeterministicMatrix checks Virtual Link Deterministic Matrix so a change
// cannot silently weaken the recorded regression contract.
func TestVirtualLinkDeterministicMatrix(t *testing.T) {
	profiles := []virtualProfile{
		{name: "lan", up: 125000000, down: 125000000, delay: 1, queue: 4096},
		{name: "satellite", up: 1250000, down: 1250000, delay: 300, queue: 1024},
		{name: "asymmetric", up: 250000, down: 12500000, delay: 40, loss: 100, queue: 128},
		{name: "random-loss", up: 1250000, down: 1250000, delay: 30, loss: 1000, queue: 512},
		{name: "reorder", up: 12500000, down: 12500000, delay: 25, jitter: 8, reorder: 2000, queue: 1024},
		{name: "duplicate", up: 1250000, down: 1250000, delay: 10, duplicate: 1000, queue: 256},
		{name: "burst", up: 1250000, down: 1250000, delay: 30, burst: true, queue: 256},
		{name: "outage", up: 125000, down: 125000, delay: 50, outage: true, queue: 128},
		{name: "slow-reader", up: 12500000, down: 12500000, delay: 10, slowReader: true, queue: 512},
		{name: "heavy-loss", up: 1250000, down: 1250000, delay: 100, loss: 2000, queue: 1024},
		{name: "heavy-reorder", up: 12500000, down: 12500000, delay: 40, jitter: 20, reorder: 5000, queue: 4096},
		{name: "restricted-ack", up: 12500000, down: 125000, delay: 40, queue: 10000},
		{name: "ack-blackout", up: 125000, down: 125000, delay: 50, ackOutage: true, queue: 512},
		{name: "bandwidth-step", up: 125000, down: 125000, delay: 40, rateStep: true, queue: 512},
		{name: "reader-pause", up: 125000, down: 125000, delay: 10, readerPause: true, queue: 512},
		{name: "dialup", up: 12500, down: 12500, delay: 100, loss: 200, queue: 1024},
		{name: "gigabit-latency", up: 125000000, down: 125000000, delay: 100, queue: 32768},
		{name: "tiny-queue", up: 1250000, down: 1250000, delay: 30, queue: 32},
	}
	for _, p := range profiles {
		for _, seed := range []uint64{1, 7, 19} {
			for _, paced := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/seed-%d/paced-%t", p.name, seed, paced), func(t *testing.T) {
					a := runVirtualLink(t, p, seed, paced, false)
					b := runVirtualLink(t, p, seed, paced, false)
					if a != b {
						t.Fatalf("non-deterministic: %+v vs %+v", a, b)
					}
					t.Logf("virtual_ms=%d datagrams=%d drops=%d retransmissions=%d", a.ticks, a.sent, a.dropped, a.retransmitted)
				})
			}
		}
	}
	t.Run("sequence-and-clock-wrap", func(t *testing.T) { runVirtualLink(t, profiles[4], 7, true, true) })
}
