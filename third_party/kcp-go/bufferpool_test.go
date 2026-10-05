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

// TestBufferPoolPutAndReuse checks Buffer Pool Put And Reuse so a change cannot silently
// weaken the recorded regression contract.
func TestBufferPoolPutAndReuse(t *testing.T) {
	bp := newBufferPool(mtuLimit)

	buf := bp.Get()
	// Modify buffer to track it
	buf[0] = 99

	// Put back to pool
	bp.Put(buf)

	// Get again; it should reuse the same buffer
	buf2 := bp.Get()

	// Check if it is reused by comparing pointer address
	if &buf2[0] != &buf[0] {
		t.Fatalf("expected buffer reuse, but got a new one")
		return
	}

	if buf2[0] != 99 {
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
