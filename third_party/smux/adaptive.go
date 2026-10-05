// File adaptive.go: bounds stream receive windows/frame sizes from drain rate and current
// carrier budget.

package smux

import "time"

// writeFrameLimit caps mux payload to current carrier capacity so one bulk frame cannot bury
// newly queued controls.
func (s *stream) writeFrameLimit() int {
	limit := s.frameSize
	if budget := s.sess.config.TransportWriteLimit; budget != nil {
		limit = min(limit, max(1, budget()-headerSize))
	}
	return limit
}

// windowUpdateThreshold uses the advertised live window rather than its ceiling when deciding
// when readers return credit.
func (s *stream) windowUpdateThreshold() uint32 {
	if s.sess.config.AdaptiveReceive {
		return max(1, s.receiveWindow/4)
	}
	return max(1, s.receiveWindow/2)
}

// Called under bufferLock. No timer or allocation is retained per stream.
func (s *stream) consumeWindow(n int, now time.Time) bool {
	if !s.sess.config.AdaptiveReceive || n <= 0 {
		return false
	}
	if s.windowLast.IsZero() {
		s.windowLast = now
	}
	s.windowBytes += uint64(n)
	elapsed := now.Sub(s.windowLast)
	if elapsed < 100*time.Millisecond {
		return false
	}
	rtt := time.Millisecond
	if f := s.sess.config.TransportRTT; f != nil {
		rtt = max(rtt, f())
	}
	// Credit updates share a reliable carrier with opposite-direction data.
	// Its queue/recovery delay can exceed the packet RTT; keep headroom for
	// that feedback loop. This is advertised credit, not upfront allocation.
	target := uint64(2*float64(s.windowBytes)*float64(rtt)/float64(elapsed)) + 65536
	ceiling := uint64(s.sess.config.MaxStreamBuffer)
	target = min(ceiling, target)
	previous := uint64(s.receiveWindow)
	// Growth is bounded; shrink only 25% per sample. Existing queued data is
	// still drained, and session tokens bound aggregate receive memory.
	if target > previous {
		target = min(target, previous*2)
	} else {
		target = max(target, previous*3/4)
	}
	s.receiveWindow = uint32(max(uint64(min(65536, s.sess.config.MaxStreamBuffer)), target))
	s.windowLast = now
	s.windowBytes = 0
	return uint64(s.receiveWindow) != previous
}
