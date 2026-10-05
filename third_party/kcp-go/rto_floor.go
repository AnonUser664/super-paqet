// File rto_floor.go: sets a bounded retransmission floor without changing KCP wire format or
// fast retry thresholds.

package kcp

// SetMinRTO changes the retransmission floor without changing KCP wire format.
// Fast ACK-based retransmission remains enabled independently.
func (s *UDPSession) SetMinRTO(milliseconds uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.rx_minrto = min(IKCP_RTO_MAX, max(uint32(10), milliseconds))
	s.kcp.rx_rto = max(s.kcp.rx_rto, s.kcp.rx_minrto)
}
