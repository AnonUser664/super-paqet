//go:build linux

// File opening_retry_observation_test.go distinguishes a recovered disruption
// from sparse retries, lifetime totals and a new counter generation.
package engine

import (
	"testing"
	"time"
)

// TestOpeningRetryObservationBoundsNoiseAndRetainsBurst replays the observed
// 381-retry cohort without timers, traffic or per-customer log generation.
func TestOpeningRetryObservationBoundsNoiseAndRetainsBurst(t *testing.T) {
	var stats Stats
	stats.Accepted.Store(10000)
	stats.OpenRetries.Store(100)
	var observation openingRetryObservation
	now := time.Unix(100, 0)
	if _, ok := observation.sample(now, &stats); ok {
		t.Fatal("historical counters reported as a new incident")
	}
	stats.Accepted.Add(1000)
	stats.OpenRetries.Add(381)
	stats.OpenSYNTimeouts.Add(40)
	stats.OpenReceiptTimeouts.Add(340)
	stats.OpenClosed.Add(1)
	now = now.Add(10 * time.Second)
	burst, ok := observation.sample(now, &stats)
	if !ok || burst.retries != 381 || burst.accepted != 1000 || burst.syn != 40 || burst.receipt != 340 || burst.closed != 1 || burst.elapsed != 10*time.Second {
		t.Fatalf("lost interval evidence: %+v, reported=%t", burst, ok)
	}
	now = now.Add(10 * time.Second)
	if _, ok := observation.sample(now, &stats); ok {
		t.Fatal("same cohort reported twice")
	}
	stats.Accepted.Add(100000)
	stats.OpenRetries.Add(100)
	now = now.Add(10 * time.Second)
	if _, ok := observation.sample(now, &stats); ok {
		t.Fatal("small retry fraction on busy host became a burst warning")
	}
	stats.Accepted.Store(1)
	stats.OpenRetries.Store(1)
	now = now.Add(10 * time.Second)
	if _, ok := observation.sample(now, &stats); ok {
		t.Fatal("counter reset created an incident")
	}
	stats.Accepted.Add(10)
	stats.OpenRetries.Add(3)
	now = now.Add(time.Second)
	if _, ok := observation.sample(now, &stats); ok {
		t.Fatal("isolated retries created noisy aggregate warning")
	}
}
