// File pacing.go: paces data with bounded byte credit and injectable time; control traffic
// stays independently serviceable.

package kcp

import (
	"math"
	"time"
)

// A paced application write can defer data while the periodic callback is
// still ten milliseconds away. Bring that wake forward under the session
// lock. Superseded callbacks check updateDue before touching the core.
func (s *UDPSession) schedulePacingLocked() {
	k := s.kcp
	if k.pacingRate == 0 || !k.pacingDeferred || s.isClosed() {
		return
	}
	delay := max(1, _itimediff(k.pacingDue, k.now()))
	due := time.Now().Add(time.Duration(delay) * time.Millisecond)
	if !s.updateDue.IsZero() && !due.Before(s.updateDue) {
		return
	}
	s.updateDue = due
	SystemTimedSched.Put(s.update, due)
}

// now uses the injected simulation clock or normal monotonic clock so protocol tests need no
// wall-clock sleeping.
func (k *KCP) now() uint32 {
	if k.clock != nil {
		return k.clock()
	}
	return currentMs()
}

// SetPacingRate paces data and retransmissions; ACK/probe traffic is exempt.
// Zero preserves upstream scheduling. Queue/window ceilings remain independent.
func (s *UDPSession) SetPacingRate(bytesPerSecond uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.kcp
	if k.pacingRate == 0 && bytesPerSecond > 0 {
		k.pacingLast = k.now()
		k.pacingTokens = float64(2 * k.mtu)
	}
	k.pacingRate = bytesPerSecond
	s.updateWriteBudgetLocked()
}

// refillPacing replenishes bounded byte credit from elapsed time so idle periods cannot create
// an unbounded output burst.
func (k *KCP) refillPacing(now uint32) {
	if k.pacingRate == 0 {
		return
	}
	elapsed := uint32(now - k.pacingLast)
	k.pacingLast = now
	// Limit accumulated credit to two milliseconds. Idle time cannot
	// release an entire congestion window as a burst when traffic resumes.
	burst := max(float64(2*k.mtu), float64(k.pacingRate)*2/1000)
	k.pacingTokens = min(burst, k.pacingTokens+float64(k.pacingRate)*float64(elapsed)/1000)
}

// pacingDelay computes the next eligible data-send time without delaying ACK/window-control
// traffic.
func (k *KCP) pacingDelay(bytes int) uint32 {
	if k.pacingRate == 0 {
		return 0
	}
	if k.pacingTokens >= float64(bytes) {
		k.pacingTokens -= float64(bytes)
		return 0
	}
	return uint32(max(1, math.Ceil((float64(bytes)-k.pacingTokens)*1000/float64(k.pacingRate))))
}
