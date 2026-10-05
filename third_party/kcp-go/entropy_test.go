// File entropy_test.go: exercises entropy regressions; fixtures must preserve cleanup and
// expose byte/lifecycle failures explicitly.

package kcp

import (
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"io"
	"testing"
)

// TestEntropyAES checks Entropy AES so a change cannot silently weaken the recorded regression
// contract.
func TestEntropyAES(t *testing.T) {
	r := NewEntropyAES()
	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 16 {
		t.Fatalf("expected 16 bytes, got %d", n)
	}

	// Test empty read
	n, err = r.Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}

// TestEntropyChacha8 checks Entropy Chacha8 so a change cannot silently weaken the recorded
// regression contract.
func TestEntropyChacha8(t *testing.T) {
	r := NewEntropyChacha8()
	buf := make([]byte, 32)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 32 {
		t.Fatalf("expected 32 bytes, got %d", n)
	}

	// Test empty read
	n, err = r.Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}

// TestSetEntropy checks Set Entropy so a change cannot silently weaken the recorded regression
// contract.
func TestSetEntropy(t *testing.T) {
	// Save original entropy
	orig := entropy
	defer SetEntropy(orig)

	// Set custom entropy
	custom := NewEntropyChacha8()
	SetEntropy(custom)

	if entropy != custom {
		t.Fatal("SetEntropy failed")
	}

	// Test fillRand with custom entropy
	buf := make([]byte, 10)
	fillRand(buf)
	// We can't easily verify the content is from custom, but we can verify it didn't panic
}

// TestFillRand checks Fill Rand so a change cannot silently weaken the recorded regression
// contract.
func TestFillRand(t *testing.T) {
	buf := make([]byte, 100)
	fillRand(buf)

	// Check if buffer is not all zeros (probabilistic, but highly likely)
	allZero := true
	for _, b := range buf {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("fillRand produced all zeros")
	}

	// Test empty buffer
	fillRand(nil)
}

// mockReader retains the mock Reader fixture state used to expose failures without production
// network side effects.
type mockReader struct{}

// Read fills predictable entropy bytes so nonce buffering/refill behavior can be checked
// independently of randomness.
func (m *mockReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0xAA
	}
	return len(p), nil
}

// TestSetEntropyMock checks Set Entropy Mock so a change cannot silently weaken the recorded
// regression contract.
func TestSetEntropyMock(t *testing.T) {
	orig := entropy
	defer SetEntropy(orig)

	SetEntropy(&mockReader{})
	buf := make([]byte, 10)
	fillRand(buf)
	for _, b := range buf {
		if b != 0xAA {
			t.Fatalf("expected 0xAA, got %x", b)
		}
	}
}

// BenchmarkCsprngSystem measures Csprng System with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkCsprngSystem(b *testing.B) {
	data := make([]byte, md5.Size)
	b.SetBytes(int64(len(data)))

	for b.Loop() {
		io.ReadFull(rand.Reader, data)
	}
}

// BenchmarkCsprngAES128 measures Csprng AES128 with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkCsprngAES128(b *testing.B) {
	var data [aes.BlockSize]byte
	b.SetBytes(aes.BlockSize)

	r := NewEntropyAES()
	for b.Loop() {
		io.ReadFull(r, data[:])
	}
}

// BenchmarkCsprngChacha8 measures Csprng Chacha8 with the fixture's workload; results must be
// interpreted with its buffer and transport settings.
func BenchmarkCsprngChacha8(b *testing.B) {
	var data [8]byte
	b.SetBytes(8)

	r := NewEntropyChacha8()
	for b.Loop() {
		io.ReadFull(r, data[:])
	}
}
