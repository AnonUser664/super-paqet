package kcp

import "time"

func (s *UDPSession) SetACKDelay(delay time.Duration) {
	s.mu.Lock()
	s.ackDelay = min(20*time.Millisecond, max(0, delay))
	s.kcp.ackDelay = uint32(s.ackDelay / time.Millisecond)
	s.mu.Unlock()
}

func (s *UDPSession) ackImmediately() bool {
	return s.ackNoDelay && s.ackDelay == 0 && !s.batchACK.Load() && (s.l == nil || !s.l.batchACK.Load())
}
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

func (s *UDPSession) sendDelayedACK() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackScheduled = false
	if !s.isClosed() && len(s.kcp.acklist) > 0 {
		s.kcp.flush(IKCP_FLUSH_ACKONLY)
	}
}
