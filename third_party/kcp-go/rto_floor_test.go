// File rto_floor_test.go: exercises rto floor regressions; fixtures must preserve cleanup and
// expose byte/lifecycle failures explicitly.

package kcp

import "testing"

// TestMinRTOBoundsAndRecovery checks Min RTO Bounds And Recovery so a change cannot silently
// weaken the recorded regression contract.
func TestMinRTOBoundsAndRecovery(t *testing.T) {
	s := &UDPSession{kcp: NewKCP(1, func([]byte, int) {})}
	s.kcp.NoDelay(1, 10, 2, 1)
	s.SetMinRTO(0)
	if s.kcp.rx_minrto != 10 {
		t.Fatal("RTO floor allowed a busy-loop timer")
	}
	s.SetMinRTO(1000)
	s.kcp.update_ack(100)
	if s.kcp.rx_rto < 1000 {
		t.Fatal("RTT estimator bypassed configured floor")
	}
	s.SetMinRTO(30)
	for i := 0; i < 100; i++ {
		s.kcp.update_ack(10)
	}
	if s.kcp.rx_rto < 30 || s.kcp.rx_rto > 100 {
		t.Fatalf("timer failed to recover after delay fell: %d", s.kcp.rx_rto)
	}
	s.SetMinRTO(^uint32(0))
	if s.kcp.rx_minrto != IKCP_RTO_MAX {
		t.Fatal("RTO exceeded protocol ceiling")
	}
}
