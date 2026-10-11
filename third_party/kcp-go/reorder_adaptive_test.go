package kcp

import "testing"

// TestExtendedGapGraceHasFiniteCeiling prevents a noisy observation or caller
// overflow from disabling gap recovery indefinitely.
func TestExtendedGapGraceHasFiniteCeiling(t *testing.T) {
	s := &UDPSession{kcp: NewKCP(91, func([]byte, int) {})}
	s.SetReorderGrace(^uint32(0))
	if s.kcp.reorderGrace != MaxReorderGraceMS {
		t.Fatal("reorder tolerance exceeded its finite bound")
	}
	s.SetReorderGrace(0)
	if s.kcp.reorderGrace != 0 {
		t.Fatal("zero-grace scheduling changed")
	}
}

// TestLateTimerACKWithoutGapDoesNotInventReorderDelay distinguishes a late
// timer ACK from evidence of a missing sequence ahead of acknowledged data.
// With no gap timestamp, subtracting its zero value measures session clock
// time and can pin the learned tolerance to its maximum.
func TestLateTimerACKWithoutGapDoesNotInventReorderDelay(t *testing.T) {
	now := uint32(1000)
	k := NewKCP(91, func([]byte, int) {})
	k.clock = func() uint32 { return now }
	k.NoDelay(0, 30, 2, 1)
	defer recycleGapFixture(k)
	k.Send([]byte("one packet"))
	k.flush(IKCP_FLUSH_NEW)
	first, _ := k.snd_buf.Peek()
	now = first.resendts
	k.flush(IKCP_FLUSH_FULL)
	now += 5
	ack := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: 0, ts: 1000}
	raw := make([]byte, IKCP_OVERHEAD)
	ack.encode(raw)
	if ret := k.Input(raw, IKCP_PACKET_REGULAR, false); ret != 0 {
		t.Fatalf("late original ACK rejected: %d", ret)
	}
	if k.spuriousRetransmissions != 1 || k.observedReorderDelay != 0 {
		t.Fatalf("timer ACK invented gap delay: spurious=%d delay=%d", k.spuriousRetransmissions, k.observedReorderDelay)
	}
}

// TestExtendedGapGraceLetsLateOriginalACKWin models a reordered ACK arriving
// after the historical 50 ms gap limit but before the caller's learned grace.
// It checks the actual retry/ACK path, rather than just the setter's value.
func TestExtendedGapGraceLetsLateOriginalACKWin(t *testing.T) {
	var now uint32
	k := NewKCP(91, func([]byte, int) {})
	k.clock = func() uint32 { return now }
	k.NoDelay(0, 30, 2, 1)
	defer recycleGapFixture(k)
	s := &UDPSession{kcp: k}
	s.SetReorderGrace(100)
	k.Send(make([]byte, k.mss*3))
	k.flush(IKCP_FLUSH_NEW)
	now = 40
	k.parse_fastack(1, 0)
	k.parse_fastack(2, 0)
	now = 100 // 60 ms of gap evidence; the original ACK is still in flight.
	k.flush(IKCP_FLUSH_FULL)
	first, _ := k.snd_buf.Peek()
	if first.xmit != 1 {
		t.Fatalf("retry raced the learned reordering bound: xmit=%d", first.xmit)
	}
	now = 110
	ack := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: 0, ts: 0}
	raw := make([]byte, IKCP_OVERHEAD)
	ack.encode(raw)
	if ret := k.Input(raw, IKCP_PACKET_REGULAR, false); ret != 0 {
		t.Fatalf("original ACK rejected: %d", ret)
	}
	if first.acked != 1 || k.retransmittedSegments != 0 || k.spuriousRetransmissions != 0 {
		t.Fatal("original ACK failed to avoid a spurious recovery")
	}
}

// TestExtendedGapGraceStillRecoversRealLoss bounds both gap recovery and timer
// recovery. Extra reorder tolerance must not turn missing data into a stall.
func TestExtendedGapGraceStillRecoversRealLoss(t *testing.T) {
	for _, timer := range []bool{false, true} {
		t.Run(map[bool]string{false: "gap", true: "timer"}[timer], func(t *testing.T) {
			var now uint32
			k := NewKCP(91, func([]byte, int) {})
			k.clock = func() uint32 { return now }
			k.NoDelay(0, 30, 2, 1)
			defer recycleGapFixture(k)
			s := &UDPSession{kcp: k}
			s.SetReorderGrace(100)
			k.Send(make([]byte, k.mss*3))
			k.flush(IKCP_FLUSH_NEW)
			now = 40
			k.parse_fastack(1, 0)
			k.parse_fastack(2, 0)
			first, _ := k.snd_buf.Peek()
			if timer {
				first.resendts = 70
				now = 70 // Timer expiry is independent of the longer gap grace.
			} else {
				now = 140 // Missing original: retry when the grace expires.
			}
			k.flush(IKCP_FLUSH_FULL)
			if first.xmit != 2 {
				t.Fatalf("missing data did not recover: timer=%t xmit=%d", timer, first.xmit)
			}
		})
	}
}

// recycleGapFixture returns all fixture-owned send segments to the library
// pool, including deliberately missing data retained after the assertion.
func recycleGapFixture(k *KCP) {
	for _, ring := range []*RingBuffer[segment]{k.snd_queue, k.snd_buf} {
		for ring.Len() > 0 {
			seg, _ := ring.Pop()
			k.recycleSegment(&seg)
		}
	}
}
