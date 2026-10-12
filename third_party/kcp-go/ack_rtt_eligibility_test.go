// File ack_rtt_eligibility_test.go protects RTT evidence from retired ACKs
// while retaining reliable delivery and high-delay retransmission bootstrap.

package kcp

import "testing"

// ackRTTFixture establishes one live original packet with an injectable clock.
func ackRTTFixture(t *testing.T) (*KCP, *uint32) {
	t.Helper()
	now := uint32(1)
	k := NewKCP(91, func([]byte, int) {})
	k.clock = func() uint32 { return now }
	k.NoDelay(0, 30, 2, 1)
	t.Cleanup(func() { recycleGapFixture(k) })
	if k.Send([]byte("original")) != 0 {
		t.Fatal("send failed")
	}
	k.flush(IKCP_FLUSH_NEW)
	return k, &now
}

// inputRTTACK injects one ordinary ACK with explicit sequence, echo and UNA.
func inputRTTACK(t *testing.T, k *KCP, sn, ts, una uint32) {
	t.Helper()
	raw := make([]byte, IKCP_OVERHEAD)
	ack := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: sn, ts: ts, una: una}
	ack.encode(raw)
	if ret := k.Input(raw, IKCP_PACKET_REGULAR, false); ret != 0 {
		t.Fatalf("ACK rejected: %d", ret)
	}
}

// Duplicate feedback for retired data proves no new round trip.
func TestACKRTTRetiredDuplicatePreservesEstimate(t *testing.T) {
	k, now := ackRTTFixture(t)
	*now = 101
	inputRTTACK(t, k, 0, 1, 1)
	if k.rx_srtt != 100 {
		t.Fatalf("valid cumulative ACK lost RTT: %d", k.rx_srtt)
	}
	srtt, variation, rto := k.rx_srtt, k.rx_rttvar, k.rx_rto
	*now = 10001
	inputRTTACK(t, k, 0, 1, 1)
	if k.rx_srtt != srtt || k.rx_rttvar != variation || k.rx_rto != rto {
		t.Fatalf("retired ACK changed estimate: RTT %d -> %d RTO %d -> %d", srtt, k.rx_srtt, rto, k.rx_rto)
	}
}

// Echoed timestamps of live old attempts retain long-path RTT bootstrap.
func TestACKRTTOlderAttemptDeliversAndSamples(t *testing.T) {
	k, now := ackRTTFixture(t)
	seg, _ := k.snd_buf.Peek()
	*now = seg.resendts
	k.flush(IKCP_FLUSH_FULL)
	*now = 1001
	inputRTTACK(t, k, 0, 1, 1)
	if k.ackedBytes != 8 || k.WaitSnd() != 0 {
		t.Fatal("late original failed to acknowledge payload")
	}
	if k.rx_srtt != 1000 {
		t.Fatalf("late original failed RTT bootstrap: RTT=%d", k.rx_srtt)
	}
}

// An exact echo of the most recent retry supplies usable RTT evidence.
func TestACKRTTLatestRetrySuppliesEstimate(t *testing.T) {
	k, now := ackRTTFixture(t)
	seg, _ := k.snd_buf.Peek()
	*now = seg.resendts
	k.flush(IKCP_FLUSH_FULL)
	ts := seg.ts
	*now += 75
	inputRTTACK(t, k, 0, ts, 1)
	if k.rx_srtt != 75 || k.WaitSnd() != 0 {
		t.Fatalf("latest retry lost RTT/delivery: RTT=%d pending=%d", k.rx_srtt, k.WaitSnd())
	}
}

// A nonexistent sequence has no live local send timestamp to measure.
func TestACKRTTUnsentSequencePreservesEstimate(t *testing.T) {
	k, now := ackRTTFixture(t)
	before := k.rx_srtt
	*now = 101
	inputRTTACK(t, k, 100, 1, 0)
	if k.rx_srtt != before {
		t.Fatalf("unsent ACK changed RTT: %d", k.rx_srtt)
	}
}

// A leading duplicate may carry cumulative progress for later ACKs in the
// same datagram. That progress must not erase their fresh timing evidence.
func TestACKRTTBatchCumulativeProgressRetainsFreshEcho(t *testing.T) {
	k, now := ackRTTFixture(t)
	*now = 11
	if k.Send([]byte("second")) != 0 {
		t.Fatal("second send failed")
	}
	k.flush(IKCP_FLUSH_NEW)
	*now = 101
	inputRTTACK(t, k, 0, 1, 1)
	if k.rx_srtt != 100 {
		t.Fatalf("initial RTT=%d", k.rx_srtt)
	}
	*now = 201
	raw := make([]byte, 2*IKCP_OVERHEAD)
	duplicate := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: 0, ts: 1, una: 2}
	fresh := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: 1, ts: 11, una: 2}
	duplicate.encode(raw)
	fresh.encode(raw[IKCP_OVERHEAD:])
	if ret := k.Input(raw, IKCP_PACKET_REGULAR, false); ret != 0 {
		t.Fatalf("batched ACK rejected: %d", ret)
	}
	if k.WaitSnd() != 0 {
		t.Fatal("cumulative ACK lost delivery")
	}
	// The new 190 ms sample updates the 100 ms smoothed estimate to 111.
	if k.rx_srtt != 111 {
		t.Fatalf("cumulative progress erased fresh RTT: got %d, want 111", k.rx_srtt)
	}
}

// ACK arrival order does not choose an older timestamp over a newer eligible
// echo. Echo length extensions and wraparound use the same bounded scan.
func TestACKRTTBatchNewestEchoAndClockWrap(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		k, now := ackRTTFixture(t)
		seg, _ := k.snd_buf.Peek()
		first, second := uint32(1), uint32(11)
		if wrap {
			first, second = ^uint32(0)-9, 1
			seg.ts = first
		}
		*now = second
		if k.Send([]byte("second")) != 0 {
			t.Fatal("second send failed")
		}
		k.flush(IKCP_FLUSH_NEW)
		*now = second + 100
		// Newest echo first, older echo last; the first header also retires
		// both segments. A timestamp extension must not shift parsing.
		raw := make([]byte, 2*IKCP_OVERHEAD+8)
		newest := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: 1, ts: second, una: 2, data: make([]byte, 8)}
		older := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: 0, ts: first, una: 2}
		newest.encode(raw)
		older.encode(raw[IKCP_OVERHEAD+8:])
		if ret := k.Input(raw, IKCP_PACKET_REGULAR, false); ret != 0 {
			t.Fatalf("ACK rejected: %d", ret)
		}
		if k.rx_srtt != 100 || k.WaitSnd() != 0 {
			t.Fatalf("wrap=%t newest echo lost: RTT=%d pending=%d", wrap, k.rx_srtt, k.WaitSnd())
		}
	}
}

// A valid first ACK must not smuggle a timing update through malformed input;
// reconstructed FEC still supplies no direct path timing sample.
func TestACKRTTBatchMalformedAndFECDoNotSample(t *testing.T) {
	for _, fec := range []bool{false, true} {
		k, now := ackRTTFixture(t)
		*now = 101
		raw := make([]byte, IKCP_OVERHEAD)
		ack := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: 0, ts: 1, una: 1}
		ack.encode(raw)
		kind := IKCP_PACKET_REGULAR
		if fec {
			kind = IKCP_PACKET_FEC
		} else {
			bad := make([]byte, IKCP_OVERHEAD)
			ack.conv++
			ack.encode(bad)
			raw = append(raw, bad...)
		}
		ret := k.Input(raw, kind, false)
		if (!fec && ret != -1) || (fec && ret != 0) || k.rx_srtt != 0 {
			t.Fatalf("fec=%t return=%d RTT=%d", fec, ret, k.rx_srtt)
		}
	}
}

// Measure the additional scan independently from setup and send-ring mutation.
// Real throughput gates remain necessary; zero allocations alone is not acceptance.
func BenchmarkACKRTTEvidence(b *testing.B) {
	for _, count := range []int{1, 48} {
		name := "single"
		if count > 1 {
			name = "batch48"
		}
		b.Run(name, func(b *testing.B) {
			now := uint32(1)
			k := NewKCP(91, func([]byte, int) {})
			k.clock = func() uint32 { return now }
			k.NoDelay(0, 30, 2, 1)
			k.WndSize(4096, 4096)
			b.Cleanup(func() { recycleGapFixture(k) })
			raw := make([]byte, count*IKCP_OVERHEAD)
			for i := 0; i < count; i++ {
				now++
				k.Send([]byte("data"))
				k.flush(IKCP_FLUSH_NEW)
				ack := segment{conv: 91, cmd: IKCP_CMD_ACK, wnd: 4096, sn: uint32(i), ts: now, una: uint32(count)}
				ack.encode(raw[i*IKCP_OVERHEAD:])
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, eligible := k.ackRTTEvidence(raw, IKCP_PACKET_REGULAR)
				if eligible != 1 {
					b.Fatal("valid timing evidence lost")
				}
			}
		})
	}
}
