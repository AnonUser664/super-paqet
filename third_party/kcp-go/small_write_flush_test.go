// File small_write_flush_test.go verifies the interactive-write exception
// without timers: the pure KCP output hook records exactly what each write emits.
package kcp

import (
	"bytes"
	"testing"
)

// TestSmallWriteFlushPreservesBulkAndDefault distinguishes aggregate vector
// size from individual slices and keeps the legacy zero-threshold behavior.
func TestSmallWriteFlushPreservesBulkAndDefault(t *testing.T) {
	for _, row := range []struct {
		threshold int
		parts     []int
		flush     bool
	}{
		{0, []int{32}, false}, {128, []int{32}, true}, {128, []int{1024}, false},
		{128, []int{64, 64}, true}, {128, []int{64, 65}, false},
	} {
		outputs := 0
		k := NewKCP(123, func([]byte, int) { outputs++ })
		k.NoDelay(0, 30, 2, 1)
		k.WndSize(128, 128)
		s := &UDPSession{kcp: k, die: make(chan struct{}), writeDelay: true}
		s.SetSmallWriteFlush(row.threshold)
		var vectors [][]byte
		for _, size := range row.parts {
			vectors = append(vectors, make([]byte, size))
		}
		if _, err := s.WriteBuffers(vectors); err != nil {
			t.Fatal(err)
		}
		if (outputs > 0) != row.flush {
			t.Fatalf("threshold=%d parts=%v outputs=%d", row.threshold, row.parts, outputs)
		}
	}
}

// TestSmallWriteFlushDoesNotOvertakeQueuedData changes the threshold live
// after a batched write and validates the delivered byte sequence at the peer.
func TestSmallWriteFlushDoesNotOvertakeQueuedData(t *testing.T) {
	var packets [][]byte
	k := NewKCP(123, func(b []byte, n int) { packets = append(packets, bytes.Clone(b[:n])) })
	k.NoDelay(0, 30, 2, 1)
	k.WndSize(128, 128)
	k.stream = 1
	s := &UDPSession{kcp: k, die: make(chan struct{}), writeDelay: true}
	if _, err := s.Write([]byte("bulk-before-control")); err != nil {
		t.Fatal(err)
	}
	if len(packets) != 0 {
		t.Fatal("bulk unexpectedly flushed")
	}
	s.SetSmallWriteFlush(8)
	if _, err := s.Write([]byte("control")); err != nil {
		t.Fatal(err)
	}
	receiver := NewKCP(123, func([]byte, int) {})
	receiver.stream = 1
	for _, packet := range packets {
		if receiver.Input(packet, IKCP_PACKET_REGULAR, false) != 0 {
			t.Fatal("invalid emitted packet")
		}
	}
	var output [64]byte
	n := receiver.Recv(output[:])
	if string(output[:n]) != "bulk-before-controlcontrol" {
		t.Fatal("small write overtook queued bytes", n)
	}
}
