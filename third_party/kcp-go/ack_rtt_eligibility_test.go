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
