// File no_encryption_test.go: exercises no encryption regressions; fixtures must preserve
// cleanup and expose byte/lifecycle failures explicitly.

package conf

import "testing"

// TestExplicitNullEncryption checks Explicit Null Encryption so a change cannot silently
// weaken the recorded regression contract.
func TestExplicitNullEncryption(t *testing.T) {
	for _, k := range []KCP{{Block_: "null"}, {Enc: "null"}} {
		if err := PrepareKCP(&k, "client"); err != nil {
			t.Fatal(err)
		}
		if k.Block != nil {
			t.Fatal("null mode installed a packet cipher")
		}
	}
	k := KCP{}
	if PrepareKCP(&k, "client") == nil {
		t.Fatal("omitting an encryption mode and key enabled anonymous operation")
	}
	k = KCP{Key: "test-only-key"}
	if err := PrepareKCP(&k, "client"); err != nil {
		t.Fatal(err)
	}
	if k.Block == nil {
		t.Fatal("default encrypted mode installed no cipher")
	}
}
