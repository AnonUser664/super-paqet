//go:build linux

// File queue_retry_linux_test.go checks transient recovery, partial-send safety
// and persistent/readiness failures without mutating a host's live qdisc.
package socket

import (
	"golang.org/x/sys/unix"
	"reflect"
	"testing"
)

// TestQueueBatchShrinksOnlyRejectedPrefix preserves accepted-prefix ownership
// and the three-retry bound, including one-packet batches and readiness errors.
func TestQueueBatchShrinksOnlyRejectedPrefix(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		batch, room             int
		failure                 unix.Errno
		prefixes                []int
		wantSent, wantAttempted int
	}{
		{"healthy", 64, 64, 0, []int{64}, 64, 64},
		{"partial", 64, 3, 0, []int{64}, 3, 64},
		{"recover", 64, 16, unix.ENOBUFS, []int{64, 32, 16}, 16, 16},
		{"persistent", 64, 0, unix.ENOBUFS, []int{64, 32, 16, 8}, -1, 8},
		{"one", 1, 0, unix.ENOBUFS, []int{1, 1, 1, 1}, -1, 1},
		{"readiness", 64, 0, unix.EAGAIN, []int{64}, -1, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []int
			waits := 0
			sent, errno, retries, attempted := sendBatchWithQueueRetry(tc.batch, func(n int) (int, unix.Errno) {
				got = append(got, n)
				if tc.failure == unix.ENOBUFS && n > tc.room || tc.room == 0 {
					return -1, tc.failure
				}
				return min(n, tc.room), 0
			}, func() { waits++ })
			wantErr := unix.Errno(0)
			if tc.wantSent < 0 {
				wantErr = tc.failure
			}
			if !reflect.DeepEqual(got, tc.prefixes) || sent != tc.wantSent || attempted != tc.wantAttempted || errno != wantErr || retries != len(got)-1 || waits != retries {
				t.Fatalf("prefixes=%v sent=%d attempted=%d errno=%v retries=%d waits=%d", got, sent, attempted, errno, retries, waits)
			}
		})
	}
}

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
