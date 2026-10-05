// File adaptive_test.go: exercises adaptive regressions; fixtures must preserve cleanup and
// expose byte/lifecycle failures explicitly.

package smux

import (
	"testing"
	"time"
)

// TestAdaptiveReceiveGrowthAndCeiling checks Adaptive Receive Growth And Ceiling so a change
// cannot silently weaken the recorded regression contract.
func TestAdaptiveReceiveGrowthAndCeiling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AdaptiveReceive = true
	cfg.MaxStreamBuffer = 16 << 20
	cfg.MaxReceiveBuffer = 32 << 20
	cfg.TransportRTT = func() time.Duration { return 100 * time.Millisecond }
	s := &stream{sess: &Session{config: cfg}, receiveWindow: 65536}
	now := time.Now()
	s.consumeWindow(1, now)
	for i := 0; i < 20; i++ {
		before := s.receiveWindow
		now = now.Add(100 * time.Millisecond)
		s.consumeWindow(int(before), now)
		if s.receiveWindow > uint32(cfg.MaxStreamBuffer) || s.receiveWindow > before*2 {
			t.Fatal("window escaped growth/memory ceiling")
		}
	}
	if s.receiveWindow <= 65536 {
		t.Fatal("window failed to grow for a high-delay bulk flow")
	}
	before := s.receiveWindow
	now = now.Add(time.Second)
	s.consumeWindow(1, now)
	if s.receiveWindow < before*3/4 {
		t.Fatal("window shrank too abruptly")
	}
}
