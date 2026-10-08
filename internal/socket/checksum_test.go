//go:build linux

// File checksum_test.go validates wider accumulation with an independent
// byte-pair oracle, including alignment, odd tails and carry-heavy payloads.
package socket

import (
	"math/rand"
	"testing"
)

func TestWideChecksumMatchesBytePairOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(81024))
	storage := make([]byte, 65536+8)
	for pattern := 0; pattern < 3; pattern++ {
		rng.Read(storage)
		if pattern < 2 {
			for i := range storage {
				storage[i] = byte(pattern * 255)
			}
		}
		for offset := 0; offset < 8; offset++ {
			for length := 0; length <= 65536; {
				b := storage[offset : offset+length]
				var want uint32
				for i := 0; i < len(b); i += 2 {
					word := uint32(b[i]) << 8
					if i+1 < len(b) {
						word |= uint32(b[i+1])
					}
					want += word
				}
				if checksum(sum16(b)) != checksum(want) {
					t.Fatalf("checksum mismatch: pattern=%d offset=%d length=%d", pattern, offset, length)
				}
				if length < 256 {
					length++
				} else if length < 65534 {
					length = min(65534, length+127)
				} else {
					length++
				}
			}
		}
	}
}
