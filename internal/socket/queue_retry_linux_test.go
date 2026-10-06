//go:build linux

// File queue_retry_linux_test.go checks transient recovery, partial-send safety
// and persistent/readiness failures without mutating a host's live qdisc.
package socket

import (
	"golang.org/x/sys/unix"
	"testing"
)

// TestQueueRetryRecoversTransientPressure verifies an initially rejected batch
// is admitted once after capacity returns, avoiding a forced network timeout.
func TestQueueRetryRecoversTransientPressure(t *testing.T) {
	available := false
	accepted := 0
	calls := 0
	sent, errno, retries := sendWithQueueRetry(func() (int, unix.Errno) {
		calls++
		if !available {
			return -1, unix.ENOBUFS
		}
		accepted += 16
		return 16, 0
	}, func() { available = true })
	if sent != 16 || errno != 0 || retries != 1 || accepted != 16 || calls != 2 {
		t.Fatalf("transient recovery: sent=%d error=%v retries=%d accepted=%d calls=%d", sent, errno, retries, accepted, calls)
	}
}

// TestQueueRetryDoesNotReplayPartialSend keeps partial-send cursor ownership at
// the caller, rather than accidentally duplicating already accepted datagrams.
func TestQueueRetryDoesNotReplayPartialSend(t *testing.T) {
	calls := 0
	sent, errno, retries := sendWithQueueRetry(func() (int, unix.Errno) { calls++; return 3, 0 }, func() { t.Fatal("partial send retried") })
	if sent != 3 || errno != 0 || retries != 0 || calls != 1 {
		t.Fatal("partial send cursor changed")
	}
}

// TestQueueRetryPreservesBoundedFailureAndReadiness checks that an unavailable
// interface cannot create an unbounded busy loop or bypass netpoll cancellation.
func TestQueueRetryPreservesBoundedFailureAndReadiness(t *testing.T) {
	for _, failure := range []unix.Errno{unix.ENOBUFS, unix.EAGAIN, unix.EINTR, unix.ENETDOWN} {
		calls, waits := 0, 0
		_, errno, retries := sendWithQueueRetry(func() (int, unix.Errno) { calls++; return -1, failure }, func() { waits++ })
		expected := 0
		if failure == unix.ENOBUFS {
			expected = 3
		}
		if errno != failure || retries != expected || waits != expected || calls != expected+1 {
			t.Fatalf("failure %v: retries=%d waits=%d calls=%d", failure, retries, waits, calls)
		}
	}
}
