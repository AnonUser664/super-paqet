//go:build linux

// File config_test.go: exercises config regressions; fixtures must preserve cleanup and expose
// byte/lifecycle failures explicitly.

package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestRejectOverflowingResourceBudgets checks Reject Overflowing Resource Budgets so a change
// cannot silently weaken the recorded regression contract.
func TestRejectOverflowingResourceBudgets(t *testing.T) {
	for _, limits := range []Limits{{Connections: math.MaxInt64}, {MemoryMiB: math.MaxInt64}} {
		c := Config{Peers: map[string]Endpoint{"remote": {}}, Limits: limits}
		if err := c.prepare(); err == nil || err.Error() != "invalid resource limits" {
			t.Fatalf("resource arithmetic reached discovery: %v", err)
		}
	}
}

// TestStrictSchemaRejectsOldSocksAndUnknownFields checks Strict Schema Rejects Old Socks And
// Unknown Fields so a change cannot silently weaken the recorded regression contract.
func TestStrictSchemaRejectsOldSocksAndUnknownFields(t *testing.T) {
	for _, text := range []string{"socks5: [{listen: '127.0.0.1:1080'}]\n", "peers: {}\nunknown: true\n", "role: client\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("accepted unsupported configuration")
		}
	}
}

// TestRejectBadForwardBeforeDiscovery checks Reject Bad Forward Before Discovery so a change
// cannot silently weaken the recorded regression contract.
func TestRejectBadForwardBeforeDiscovery(t *testing.T) {
	for _, f := range []Forward{{Listen: "127.0.0.1:8080", Peer: "missing", Target: "127.0.0.1:80"}, {Listen: "127.0.0.1:8080", Peer: "remote", Target: "127.0.0.1:70000"}, {Listen: "invalid", Peer: "remote", Target: "127.0.0.1:80"}} {
		c := Config{Peers: map[string]Endpoint{"remote": {}}, Forwards: []Forward{f}}
		if c.prepare() == nil {
			t.Fatal("accepted invalid forward")
		}
	}
}
