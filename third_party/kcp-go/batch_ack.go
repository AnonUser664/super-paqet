// File batch_ack.go: coalesces/deadlines ACK output while ensuring control feedback is not
// trapped behind bulk pacing.

package kcp

import "time"

// SetACKDelay bounds deferred ACK scheduling under the carrier lock; control feedback must
// still progress when data is paced.
func (s *UDPSession) SetACKDelay(delay time.Duration) {
	s.mu.Lock()
	limit := s.ackDelayLimit
	if limit == 0 {
		limit = 20 * time.Millisecond
	}
	s.ackDelay = min(limit, max(0, delay))
	s.kcp.ackDelay = uint32(s.ackDelay / time.Millisecond)
	s.mu.Unlock()
}

// SetACKDelayLimit sets the maximum ACK deferral separately from the adaptive chosen delay.
func (s *UDPSession) SetACKDelayLimit(limit time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackDelayLimit = min(20*time.Millisecond, max(time.Millisecond, limit))
}

// ackImmediately checks whether configured immediate ACKs are compatible with batch and
// delayed-ACK state.
func (s *UDPSession) ackImmediately() bool {
	if !s.ackNoDelay || s.ackDelay != 0 || s.batchACK.Load() {
		return false
	}
	// The original listener stays immutable for dedicated-reader compatibility;
	// batching decisions must follow the current owner after a physical move.
	owner := s.l
	if route := s.route.Load(); route != nil {
		owner = route.listener
	}
	return owner == nil || !owner.batchACK.Load()
}

// flushBatchACK flushes accumulated feedback after a receive batch rather than emitting one
// syscall per received datagram.
func (s *UDPSession) flushBatchACK() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isClosed() && s.ackNoDelay && len(s.kcp.acklist) > 0 {
		if s.ackDelay > 0 {
			if !s.ackScheduled {
				s.ackScheduled = true
				SystemTimedSched.Put(s.sendDelayedACK, time.Now().Add(s.ackDelay))
			}
			return
		}
		s.kcp.flush(IKCP_FLUSH_ACKONLY)
	}
}

// sendDelayedACK services a scheduled feedback deadline without requiring another application
// write.
func (s *UDPSession) sendDelayedACK() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackScheduled = false
	if !s.isClosed() && len(s.kcp.acklist) > 0 {
		s.kcp.flush(IKCP_FLUSH_ACKONLY)
	}
}
