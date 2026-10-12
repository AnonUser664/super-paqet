//go:build linux

// File opening_retry_observation.go retains bounded interval evidence for
// recovered openings; a successful retry is not an application failure.
package engine

import "time"

// openingRetryObservation belongs to the single observer goroutine. It tracks
// attempt phases without inspecting streams, targets or bearer capabilities.
type openingRetryObservation struct {
	at                                              time.Time
	retries, accepted, syn, receipt, result, closed int64
}

// openingRetryBurst describes measured counter deltas, not a diagnosed cause.
type openingRetryBurst struct {
	elapsed                                         time.Duration
	retries, accepted, syn, receipt, result, closed int64
}

// sample reports significant retry cohorts once per existing observer interval.
// The absolute floor suppresses isolated recoveries; the admission-relative
// floor scales with busy hosts. Baselines also advance on quiet intervals and
// counter resets so lifetime totals never look like a new incident.
func (o *openingRetryObservation) sample(now time.Time, s *Stats) (openingRetryBurst, bool) {
	next := openingRetryObservation{at: now, retries: s.OpenRetries.Load(), accepted: s.Accepted.Load(),
		syn: s.OpenSYNTimeouts.Load(), receipt: s.OpenReceiptTimeouts.Load(), result: s.OpenResultTimeouts.Load(), closed: s.OpenClosed.Load()}
	old := *o
	*o = next
	if old.at.IsZero() || !now.After(old.at) || next.retries < old.retries || next.accepted < old.accepted {
		return openingRetryBurst{}, false
	}
	burst := openingRetryBurst{elapsed: now.Sub(old.at), retries: next.retries - old.retries, accepted: next.accepted - old.accepted,
		syn: max(0, next.syn-old.syn), receipt: max(0, next.receipt-old.receipt), result: max(0, next.result-old.result), closed: max(0, next.closed-old.closed)}
	return burst, burst.retries >= max(int64(10), burst.accepted/100)
}
