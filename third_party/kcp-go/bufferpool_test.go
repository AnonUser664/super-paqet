// File bufferpool_test.go: exercises bufferpool regressions; fixtures must preserve cleanup
// and expose byte/lifecycle failures explicitly.

package kcp

import "testing"

// TestBufferPoolGetSize checks Buffer Pool Get Size so a change cannot silently weaken the
// recorded regression contract.
func TestBufferPoolGetSize(t *testing.T) {
	bp := newBufferPool(mtuLimit)

	buf := bp.Get()

	// Check length
	if len(buf) != mtuLimit {
		t.Fatalf("expected len=%d, got %d", mtuLimit, len(buf))
		return
	}

	// Check capacity
	if cap(buf) != mtuLimit {
		t.Fatalf("expected cap=%d, got %d", mtuLimit, cap(buf))
		return
	}
}

// TestBufferPoolPutRestoresSize checks shortened-slice return and retrieval.
// sync.Pool may discard entries at any time (including deliberate drops under
// the race detector), so identity reuse is an optimization, not its contract.
func TestBufferPoolPutRestoresSize(t *testing.T) {
	bp := newBufferPool(mtuLimit)

	buf := bp.Get()
	// Modify buffer to track it
	buf[0] = 99

	// Return a shortened payload slice; a cached entry must regain full size.
	if err := bp.Put(buf[:17]); err != nil {
		t.Fatal(err)
	}

	// A reused entry and a fresh allocation must both be ready for a full MTU.
	buf2 := bp.Get()
	if len(buf2) != mtuLimit || cap(buf2) != mtuLimit {
		t.Fatalf("returned buffer size: len=%d cap=%d", len(buf2), cap(buf2))
	}
	if &buf2[0] == &buf[0] && buf2[0] != 99 {
		t.Fatalf("expected reused buffer to keep previous data")
		return
	}
}

// TestBufferPoolPutWrongSizeIgnored checks Buffer Pool Put Wrong Size Ignored so a change
// cannot silently weaken the recorded regression contract.
func TestBufferPoolPutWrongSizeIgnored(t *testing.T) {
	bp := newBufferPool(mtuLimit)

	// Make a buffer with wrong capacity
	wrongBuf := make([]byte, 100)

	bp.Put(wrongBuf)

	// Get should still return a buffer with mtuLimit capacity
	buf := bp.Get()

	if cap(buf) != mtuLimit {
		t.Fatalf("pool accepted wrong-sized buffer; expected cap=%d, got %d", mtuLimit, cap(buf))
		return
	}
}

// TestBufferPoolPutReturnsError checks Buffer Pool Put Returns Error so a change cannot
// silently weaken the recorded regression contract.
func TestBufferPoolPutReturnsError(t *testing.T) {
	bp := newBufferPool(mtuLimit)

	// 1. Correct size
	buf := make([]byte, mtuLimit)
	if err := bp.Put(buf); err != nil {
		t.Fatalf("expected nil error for correct size, got %v", err)
	}

	// 2. Incorrect size
	wrongBuf := make([]byte, mtuLimit+1)
	if err := bp.Put(wrongBuf); err == nil {
		t.Fatalf("expected error for wrong size, got nil")
	}
}
