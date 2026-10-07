// File prepare_test.go: exercises prepare regressions; fixtures must preserve cleanup and
// expose byte/lifecycle failures explicitly.

package conf

import "testing"

// TestRecoveryGraceBounds keeps the extra retention finite and disabled by
// omission, including overflow-sized user inputs before duration conversion.
func TestRecoveryGraceBounds(t *testing.T) {
	for _, seconds := range []int{-1, 0, 1, 60, 120, 121, int(^uint(0) >> 1)} {
		cfg := KCP{Block_: "null", SmuxRecoveryGrace: seconds}
		err := PrepareKCP(&cfg, "client")
		if (err == nil) != (seconds >= 0 && seconds <= 120) {
			t.Fatalf("grace=%d: %v", seconds, err)
		}
	}
}

// TestMTUIncludesEncryptionFECAndCoreMinimum checks MTU Includes Encryption FEC And Core
// Minimum so a change cannot silently weaken the recorded regression contract.
func TestMTUIncludesEncryptionFECAndCoreMinimum(t *testing.T) {
	for _, tc := range []struct {
		block   string
		fec     bool
		minimum int
	}{{"aes-128-gcm", false, 78}, {"aes-128-gcm", true, 86}, {"aes", false, 70}, {"none", false, 70}} {
		for _, delta := range []int{-1, 0} {
			cfg := KCP{MTU: tc.minimum + delta, Block_: tc.block, Key: "test-key"}
			if tc.fec {
				cfg.Dshard, cfg.Pshard = 10, 3
			}
			err := PrepareKCP(&cfg, "client")
			if (err == nil) != (delta == 0) {
				t.Fatalf("block=%s fec=%t mtu=%d: %v", tc.block, tc.fec, cfg.MTU, err)
			}
		}
	}
}
