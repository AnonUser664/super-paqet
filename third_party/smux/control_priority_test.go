// File control_priority_test.go: exercises control priority regressions; fixtures must
// preserve cleanup and expose byte/lifecycle failures explicitly.

package smux

import "testing"

// TestControlPriorityAcrossStreamsAndDataFairness checks Control Priority Across Streams And
// Data Fairness so a change cannot silently weaken the recorded regression contract.
func TestControlPriorityAcrossStreamsAndDataFairness(t *testing.T) {
	sq := NewShaperQueue()
	sq.prioritizeControl = true
	sq.Push(writeRequest{class: CLSDATA, seq: 1, frame: Frame{sid: 3}})
	for i := uint32(2); i <= 20; i++ {
		sq.Push(writeRequest{class: CLSCTRL, seq: i, frame: Frame{sid: 5 + 2*i}})
	}
	for i := uint32(2); i <= 17; i++ {
		req, ok := sq.Pop()
		if !ok || req.class != CLSCTRL || req.seq != i {
			t.Fatal("control stuck behind other stream data", req)
		}
	}
	if req, ok := sq.Pop(); !ok || req.class != CLSDATA {
		t.Fatal("control burst starved data")
	}
	for i := uint32(18); i <= 20; i++ {
		if req, ok := sq.Pop(); !ok || req.seq != i {
			t.Fatal("lost queued control")
		}
	}
	if sq.Len() != 0 || !sq.IsEmpty() {
		t.Fatal("priority queue did not drain")
	}
}

// TestTransportFrameLimitUsesLiveWindow checks Transport Frame Limit Uses Live Window so a
// change cannot silently weaken the recorded regression contract.
func TestTransportFrameLimitUsesLiveWindow(t *testing.T) {
	cfg := DefaultConfig()
	budget := 4096
	cfg.TransportWriteLimit = func() int { return budget }
	s := newStream(3, 65535, &Session{config: cfg})
	if s.writeFrameLimit() != 4096-headerSize {
		t.Fatal("frame exceeds live carrier window")
	}
	budget = 65535
	if s.writeFrameLimit() != 65535-headerSize {
		t.Fatal("frame failed to grow with carrier")
	}
	budget = 24
	if s.writeFrameLimit() != 24-headerSize {
		t.Fatal("small carrier window ignored")
	}
}
