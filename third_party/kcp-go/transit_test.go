package kcp

import (
	"bytes"
	"testing"
)

func TestTransitSeparatesForwardReverseQueuesAcrossClockOffsets(t *testing.T) {
	for _, offset := range []uint32{1000, 0xfffffff0, 0x7fffffff} {
		k := NewKCP(1, func([]byte, int) {})
		k.recordTransit(100, 110+offset, 120)
		for i := uint32(0); i < 64; i++ {
			// Forward path stays 10ms; ACK reverse path acquires 200ms.
			k.recordTransit(200+i, 210+i+offset, 420+i)
		}
		if k.forwardQueue != 0 || k.reverseQueue < 190 {
			t.Fatal("reverse congestion attributed to sender", k.forwardQueue, k.reverseQueue)
		}
		for i := uint32(0); i < 64; i++ {
			k.recordTransit(500+i, 550+i+offset, 760+i)
		}
		if k.forwardQueue < 30 || k.reverseQueue < 190 {
			t.Fatal("forward queue was not detected")
		}
	}
}

func TestACKTimestampOptionalInteroperability(t *testing.T) {
	for _, enabled := range [][2]bool{{true, true}, {true, false}, {false, true}, {false, false}} {
		var fromLeft, fromRight []byte
		var nowLeft uint32 = 0xfffffff0
		var nowRight uint32 = nowLeft + 12345 + 10
		left := NewKCP(1, func(p []byte, n int) { fromLeft = append([]byte(nil), p[:n]...) })
		right := NewKCP(1, func(p []byte, n int) { fromRight = append([]byte(nil), p[:n]...) })
		left.clock = func() uint32 { return nowLeft }
		right.clock = func() uint32 { return nowRight }
		left.ackTimestamps, right.ackTimestamps = enabled[0], enabled[1]
		left.NoDelay(1, 10, 2, 1)
		right.NoDelay(1, 10, 2, 1)
		want := []byte("ordered application bytes")
		left.Send(want)
		left.flush(IKCP_FLUSH_NEW)
		if ret := right.Input(fromLeft, IKCP_PACKET_REGULAR, true); ret != 0 {
			t.Fatal(ret)
		}
		got := make([]byte, len(want))
		if n := right.Recv(got); n != len(want) || !bytes.Equal(got, want) {
			t.Fatal("application data changed")
		}
		nowLeft += 20
		if ret := left.Input(fromRight, IKCP_PACKET_REGULAR, true); ret != 0 {
			t.Fatal(ret)
		}
		if left.WaitSnd() != 0 {
			t.Fatal("optional ACK payload broke reliable delivery")
		}
		if (left.transitSamples > 0) != (enabled[0] && enabled[1]) {
			t.Fatal("timestamp fallback incorrect")
		}
	}
}
