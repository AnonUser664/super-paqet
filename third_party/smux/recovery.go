// File recovery.go retains terminal evidence and bounds an opt-in migration
// opportunity. It adds no wire commands, per-stream timers or per-packet work.
package smux

import (
	"sync/atomic"
	"time"
)

// SessionEnd identifies the first terminal event, before cleanup closes IO.
// Error is the original transport error; Reason is a bounded diagnostic label.
type SessionEnd struct {
	Reason string
	Error  error
}

// recordEnd preserves the first cause when timeout, IO failure and Close race.
func (s *Session) recordEnd(reason string, err error) {
	if s.endCause.Load() == nil {
		s.endCause.CompareAndSwap(nil, &SessionEnd{Reason: reason, Error: err})
	}
}

// EndCause reports terminal evidence without transferring mutable ownership.
func (s *Session) EndCause() SessionEnd {
	if cause := s.endCause.Load(); cause != nil {
		return *cause
	}
	return SessionEnd{}
}

// EnableRecoveryGrace is called only after a migration capability is issued or
// received. Configuration alone must not retain ordinary or legacy sessions.
func (s *Session) EnableRecoveryGrace() {
	if s.config.RecoveryGrace > 0 {
		s.recoveryArmed.Store(true)
	}
}

// RecoveryGraceStats exposes bounded extension use, not a diagnosis of filtering.
func (s *Session) RecoveryGraceStats() (active bool, starts uint64) {
	return s.recoveryUntil.Load() != 0 && !s.IsClosed(), s.recoveryStarts.Load()
}

// clearRecovery is owned by keepalive; IO only touches its existing activity bit.
func (s *Session) clearRecovery() {
	s.recoveryDeadline = time.Time{}
	s.recoveryUntil.Store(0)
}

// checkRecoveryDeadline reuses the ping cadence to bound expiry without another
// timer/goroutine. It never extends a deadline for repeated failed probes.
func (s *Session) checkRecoveryDeadline(now time.Time) bool {
	if s.recoveryDeadline.IsZero() {
		return false
	}
	if atomic.LoadInt32(&s.sessionIsActive) != 0 {
		s.clearRecovery()
		return false
	}
	if atomic.LoadInt32(&s.bucket) <= 0 {
		// Do not turn legitimate receiver backpressure into a grace expiry.
		s.clearRecovery()
		return false
	}
	if s.NumStreams() == 0 || !now.Before(s.recoveryDeadline) {
		s.recordEnd("recovery_grace_expired", nil)
		s.Close()
		return true
	}
	return false
}

// checkKeepaliveTimeout retains the ordinary activity/buffer checks. Grace is
// granted once per delivery outage, only to negotiated carriers with live users.
func (s *Session) checkKeepaliveTimeout(now time.Time) bool {
	if atomic.CompareAndSwapInt32(&s.sessionIsActive, 1, 0) {
		s.clearRecovery()
		return false
	}
	// A receiver waiting for its application buffer is flow control, not loss.
	if atomic.LoadInt32(&s.bucket) <= 0 {
		return false
	}
	if !s.recoveryDeadline.IsZero() {
		return s.checkRecoveryDeadline(now)
	}
	if s.recoveryArmed.Load() && s.NumStreams() > 0 {
		s.recoveryDeadline = now.Add(s.config.RecoveryGrace)
		s.recoveryUntil.Store(s.recoveryDeadline.UnixNano())
		s.recoveryStarts.Add(1)
		return false
	}
	s.recordEnd("keepalive_timeout", nil)
	s.Close()
	return true
}
