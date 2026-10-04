package kcp

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestPacingKeepsInitialSendsInOrder(t *testing.T) {
	var now uint32
	var sequences []uint32
	k := NewKCP(1, func(p []byte, n int) {
		for len(p) >= IKCP_OVERHEAD {
			size := IKCP_OVERHEAD + int(binary.LittleEndian.Uint32(p[20:]))
			if p[4] == IKCP_CMD_PUSH {
				sequences = append(sequences, binary.LittleEndian.Uint32(p[12:]))
			}
			p = p[size:]
		}
	})
	k.clock = func() uint32 { return now }
	k.NoDelay(1, 10, 2, 1)
	k.WndSize(64, 64)
	k.pacingRate = 500000
	k.pacingTokens = float64(k.mtu)
	k.Send(make([]byte, k.mss*3))
	k.flush(IKCP_FLUSH_NEW)
	// A short later write must not leap ahead of deferred full-sized segments.
	k.Send([]byte{1})
	k.flush(IKCP_FLUSH_NEW)
	for now = 1; now < 100; now++ {
		k.flush(IKCP_FLUSH_FULL)
	}
	if len(sequences) < 4 {
		t.Fatal("pacing never emitted queued data")
	}
	for i := 0; i < 4; i++ {
		if sequences[i] != uint32(i) {
			t.Fatalf("initial send reordered: %v", sequences)
		}
	}
}

func TestPacingBringsPeriodicWakeForward(t *testing.T) {
	k := NewKCP(1, func([]byte, int) {})
	k.pacingRate, k.pacingDeferred = 100000, true
	k.pacingDue = k.now() + 2
	s := &UDPSession{kcp: k, die: make(chan struct{}), updateDue: time.Now().Add(time.Second)}
	defer close(s.die)
	s.mu.Lock()
	s.schedulePacingLocked()
	if time.Until(s.updateDue) > 10*time.Millisecond {
		t.Fatal("paced data waits for old periodic timer")
	}
	due := s.updateDue
	s.schedulePacingLocked()
	if !due.Equal(s.updateDue) {
		t.Fatal("duplicate pacing request moved existing wake")
	}
	// A superseded callback must not flush early or schedule another chain.
	s.updateDue = time.Now().Add(time.Hour)
	s.mu.Unlock()
	s.update()
	if k.outputPackets != 0 {
		t.Fatal("obsolete callback flushed core")
	}
}

func TestPacingDeferralDoesNotCountOrSuppressRetransmission(t *testing.T) {
	var now uint32
	k := NewKCP(1, func([]byte, int) {})
	k.clock = func() uint32 { return now }
	k.NoDelay(1, 10, 2, 1)
	k.Send([]byte("data"))
	k.flush(IKCP_FLUSH_NEW)
	seg, _ := k.snd_buf.Peek()
	seg.fastack = 2
	k.pacingRate = 1000
	k.pacingTokens = 0
	k.flush(IKCP_FLUSH_FULL)
	if seg.xmit != 1 || seg.fastack != 2 || k.retransmittedSegments != 0 {
		t.Fatal("deferral changed retransmission state")
	}
	now = 100
	k.flush(IKCP_FLUSH_FULL)
	if seg.xmit != 2 || k.retransmittedSegments != 1 {
		t.Fatal("deferred retransmission did not recover")
	}
}
