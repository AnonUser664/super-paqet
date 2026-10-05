// File reorder.go: bounds the grace for gap evidence independently of timeout recovery.

package kcp

// SetReorderGrace postpones gap-based retransmission briefly so delayed original
// packets/ACKs can arrive. It does not postpone timer retransmission.
func (s *UDPSession) SetReorderGrace(milliseconds uint32) {
	s.mu.Lock()
	s.kcp.reorderGrace = min(50, milliseconds)
	s.mu.Unlock()
}

// reorderReady allows gap-based retries only after the configured grace; timeout retries
// remain independent.
func (k *KCP) reorderReady(seg *segment, now uint32) bool {
	return k.reorderGrace == 0 || _itimediff(now, seg.gapAt) >= int32(k.reorderGrace)
}
