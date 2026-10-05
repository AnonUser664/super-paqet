// File reorder_test.go: exercises reorder regressions; fixtures must preserve cleanup and
// expose byte/lifecycle failures explicitly.

package kcp

import "testing"

// TestGapGraceAvoidsImmediateSpuriousRetransmission checks Gap Grace Avoids Immediate Spurious
// Retransmission so a change cannot silently weaken the recorded regression contract.
func TestGapGraceAvoidsImmediateSpuriousRetransmission(t *testing.T) {
	var now uint32
	k := NewKCP(1, func([]byte, int) {})
	k.clock = func() uint32 { return now }
	k.NoDelay(1, 10, 2, 1)
	k.Send(make([]byte, k.mss*3))
	k.flush(IKCP_FLUSH_NEW)
	k.reorderGrace = 10
	now = 20
	k.parse_fastack(1, 0)
	k.parse_fastack(2, 0)
	seg, _ := k.snd_buf.Peek()
	k.flush(IKCP_FLUSH_FULL)
	if seg.xmit != 1 {
		t.Fatal("retransmitted before reorder grace")
	}
	now = 30
	k.flush(IKCP_FLUSH_FULL)
	if seg.xmit != 2 {
		t.Fatal("gap retransmission never resumed")
	}
}

// TestTimerRetransmissionIgnoresReorderGrace checks Timer Retransmission Ignores Reorder Grace
// so a change cannot silently weaken the recorded regression contract.
func TestTimerRetransmissionIgnoresReorderGrace(t *testing.T) {
	var now uint32
	k := NewKCP(1, func([]byte, int) {})
	k.clock = func() uint32 { return now }
	k.NoDelay(1, 10, 2, 1)
	k.Send([]byte{1})
	k.flush(IKCP_FLUSH_NEW)
	k.reorderGrace = 50
	seg, _ := k.snd_buf.Peek()
	seg.resendts = 5
	now = 6
	k.flush(IKCP_FLUSH_FULL)
	if seg.xmit != 2 {
		t.Fatal("grace delayed timer recovery")
	}
}

// TestDelayedACKFlushPreservesCumulativeAcknowledgment checks Delayed ACK Flush Preserves
// Cumulative Acknowledgment so a change cannot silently weaken the recorded regression
// contract.
func TestDelayedACKFlushPreservesCumulativeAcknowledgment(t *testing.T) {
	packets := 0
	s := &UDPSession{die: make(chan struct{}), ackNoDelay: true}
	s.kcp = NewKCP(1, func([]byte, int) { packets++ })
	s.kcp.rcv_nxt = 20
	for i := uint32(1); i < 10; i++ {
		s.kcp.ack_push(i, 0)
	}
	s.ackScheduled = true
	s.sendDelayedACK()
	if s.ackScheduled || len(s.kcp.acklist) != 0 || packets != 1 {
		t.Fatal("ACK coalescing did not drain cumulative response")
	}
}
