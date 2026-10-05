// File transit.go: estimates relative directional transit/ACK scheduling without synchronized
// clocks.

package kcp

// Relative one-way queue delay needs no synchronized wall clocks: subtract
// the minimum observed cross-clock transit offset from each new offset.
// Modular arithmetic handles process clock offsets and 32-bit clock wrap.
type transitMinimum struct {
	// Relative minimum and its aging epoch in a wrapping clock.
	value, epoch uint32
	// Distinguishes an initialized timing baseline from zero-valued storage.
	valid bool
}

// queue compares a relative transit observation with recent minima without requiring
// synchronized endpoint clocks.
func (m *transitMinimum) queue(offset, now uint32) uint32 {
	if !m.valid || now-m.epoch >= 30000 {
		m.value, m.epoch, m.valid = offset, now, true
	} else if _itimediff(offset, m.value) < 0 {
		m.value = offset
	}
	return uint32(min(60000, max(0, _itimediff(offset, m.value))))
}

// recordTransit records peer/local relative timing used for directional queue attribution.
func (k *KCP) recordTransit(sent, received, now uint32) {
	k.recordTransitWithACKDelay(sent, received, received, now)
}

// recordTransitWithACKDelay separates peer ACK scheduling from propagation/queue growth before
// feeding congestion estimates.
func (k *KCP) recordTransitWithACKDelay(sent, received, emitted, now uint32) {
	if delay := _itimediff(now, sent); delay < 0 || delay > 60000 {
		return
	}
	forward := k.forwardTransit.queue(received-sent, now)
	delay := uint32(min(1000, max(0, _itimediff(emitted, received))))
	k.peerACKDelayEstimate = (7*k.peerACKDelayEstimate + float64(delay)) / 8
	k.peerACKDelay = uint32(k.peerACKDelayEstimate + .5)
	reverse := k.reverseTransit.queue(now-emitted, now)
	k.forwardQueue = (7*k.forwardQueue + forward) / 8
	k.reverseQueue = (7*k.reverseQueue + reverse) / 8
	k.transitSamples++
}

// ACK timestamps are an encrypted, optional eight-byte ACK payload. Standard
// KCP parsers already skip the ACK's length field; ordinary data is unchanged.
func (s *UDPSession) SetACKTimestamps(enabled bool) {
	s.mu.Lock()
	s.kcp.ackTimestamps = enabled
	s.mu.Unlock()
}
