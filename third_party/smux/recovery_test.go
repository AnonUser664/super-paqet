// File recovery_test.go exercises finite watchdog extensions with an injected
// clock and real session cleanup. Healthy data delivery is tested separately.
package smux

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// watchdogSession disables only the automatic clock so tests can drive exact
// boundaries without sleeps, while all ordinary session queues still run.
func watchdogSession(t *testing.T, grace time.Duration, streams bool) *Session {
	t.Helper()
	a, b := net.Pipe()
	cfg := DefaultConfig()
	cfg.KeepAliveDisabled, cfg.RecoveryGrace = true, grace
	s, err := Client(a, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if streams {
		s.streamLock.Lock()
		s.streams[1] = &stream{die: make(chan struct{})}
		s.streamLock.Unlock()
	}
	t.Cleanup(func() { s.Close(); b.Close() })
	return s
}

// TestRecoveryGraceRequiresNegotiationAndLiveUsers prevents a YAML value from
// extending ordinary/legacy or idle sessions, including a configured zero.
func TestRecoveryGraceRequiresNegotiationAndLiveUsers(t *testing.T) {
	for _, test := range []struct {
		name           string
		grace          time.Duration
		armed, streams bool
	}{
		{"unnegotiated", time.Minute, false, true},
		{"idle", time.Minute, true, false},
		{"disabled", 0, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := watchdogSession(t, test.grace, test.streams)
			if test.armed {
				s.EnableRecoveryGrace()
			}
			if !s.checkKeepaliveTimeout(time.Now()) || !s.IsClosed() || s.EndCause().Reason != "keepalive_timeout" {
				t.Fatal("ordinary expiry changed")
			}
		})
	}
}

// TestRecoveryGraceHasOneFixedDeadline proves repeated probes/watchdog checks
// cannot retain dead sockets indefinitely; the exact boundary closes the stream.
func TestRecoveryGraceHasOneFixedDeadline(t *testing.T) {
	s := watchdogSession(t, time.Minute, true)
	s.EnableRecoveryGrace()
	now := time.Now()
	if s.checkKeepaliveTimeout(now) {
		t.Fatal("grace not granted")
	}
	for i := 1; i < 60; i++ {
		s.EnableRecoveryGrace()
		if s.checkKeepaliveTimeout(now.Add(time.Duration(i) * time.Second)) {
			t.Fatal("early expiry")
		}
	}
	if active, starts := s.RecoveryGraceStats(); !active || starts != 1 {
		t.Fatal("grace restarted", active, starts)
	}
	if !s.checkRecoveryDeadline(now.Add(time.Minute)) || s.EndCause().Reason != "recovery_grace_expired" {
		t.Fatal("deadline not enforced")
	}
	if active, _ := s.RecoveryGraceStats(); active {
		t.Fatal("closed grace remains active")
	}
}

// TestRecoveryGraceResumesAfterRealInput clears retention on observed inbound
// activity and permits a new bounded extension for a distinct later outage.
func TestRecoveryGraceResumesAfterRealInput(t *testing.T) {
	s := watchdogSession(t, time.Minute, true)
	s.EnableRecoveryGrace()
	now := time.Now()
	s.checkKeepaliveTimeout(now)
	atomic.StoreInt32(&s.sessionIsActive, 1)
	if s.checkRecoveryDeadline(now.Add(10 * time.Second)) {
		t.Fatal("input closed session")
	}
	if active, _ := s.RecoveryGraceStats(); active {
		t.Fatal("input did not clear grace")
	}
	if s.checkKeepaliveTimeout(now.Add(30 * time.Second)) {
		t.Fatal("activity check closed session")
	}
	if s.checkKeepaliveTimeout(now.Add(time.Minute)) {
		t.Fatal("later grace unavailable")
	}
	if active, starts := s.RecoveryGraceStats(); !active || starts != 2 {
		t.Fatal("later outage not distinct")
	}
}

// TestRecoveryGraceRespectsReceiverBackpressure and explicit cancellation keep
// normal flow control/cleanup authoritative over the extra retention budget.
func TestRecoveryGraceRespectsReceiverBackpressure(t *testing.T) {
	s := watchdogSession(t, time.Minute, true)
	s.EnableRecoveryGrace()
	now := time.Now()
	atomic.StoreInt32(&s.bucket, 0)
	if s.checkKeepaliveTimeout(now) {
		t.Fatal("flow control expired session")
	}
	atomic.StoreInt32(&s.bucket, 1)
	s.checkKeepaliveTimeout(now)
	atomic.StoreInt32(&s.bucket, 0)
	if s.checkRecoveryDeadline(now.Add(2*time.Minute)) || s.IsClosed() {
		t.Fatal("grace overrode flow control")
	}
	s.Close()
	if s.EndCause().Reason != "local_close" {
		t.Fatal("explicit closure deferred")
	}
}

// TestSessionEndKeepsFirstCause covers failure/cleanup races without relying on
// error strings; enabling grace never changes permanent IO error evidence.
func TestSessionEndKeepsFirstCause(t *testing.T) {
	s := watchdogSession(t, time.Minute, true)
	s.EnableRecoveryGrace()
	err := errors.New("injected permanent write failure")
	s.notifyWriteError(err)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Close() }()
	}
	wg.Wait()
	if c := s.EndCause(); c.Reason != "transport_write" || !errors.Is(c.Error, err) {
		t.Fatal("cleanup overwrote failure", c)
	}
}

// TestRecoveryGraceStopsWhenUsersLeave proves an application cancellation does
// not retain an empty migration-capable session for the rest of the budget.
func TestRecoveryGraceStopsWhenUsersLeave(t *testing.T) {
	s := watchdogSession(t, time.Minute, true)
	s.EnableRecoveryGrace()
	now := time.Now()
	s.checkKeepaliveTimeout(now)
	s.streamLock.Lock()
	delete(s.streams, 1)
	s.streamLock.Unlock()
	if !s.checkRecoveryDeadline(now.Add(time.Second)) {
		t.Fatal("idle grace retained")
	}
}

// TestRecoveryGraceRejectsUnboundedBudgets fixes both library boundaries.
func TestRecoveryGraceRejectsUnboundedBudgets(t *testing.T) {
	for _, d := range []time.Duration{-1, 2*time.Minute + 1} {
		cfg := DefaultConfig()
		cfg.RecoveryGrace = d
		if VerifyConfig(cfg) == nil {
			t.Fatal("unbounded grace accepted")
		}
	}
}
