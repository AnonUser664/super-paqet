//go:build linux

package engine

import (
	"encoding/json"
	"net"
	"os"
	"paqet/internal/conf"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverOnlyDeadOwner(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	ns, err := namespaceID()
	if err != nil {
		t.Fatal(err)
	}
	chain := firewallChain{"iptables", "raw", "SPQ_0123456789abP", "PREROUTING", []string{"-p", "tcp", "--dport", "29999", "-j", "SPQ_0123456789abP"}}
	dead := firewallJournal{PID: 99999999, Start: "old", Boot: bootID(), Namespace: ns, Chains: []firewallChain{chain}}
	live := dead
	live.PID = os.Getpid()
	live.Start = processStart(os.Getpid())
	for name, j := range map[string]firewallJournal{"dead.json": dead, "live.json": live} {
		b, _ := json.Marshal(j)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var calls []string
	run := func(binary, table string, args ...string) error {
		calls = append(calls, binary+" "+table+" "+strings.Join(args, " "))
		return nil
	}
	if err := recoverFirewall(dir, run); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 5 {
		t.Fatalf("unexpected cleanup commands: %v", calls)
	}
	for _, call := range calls {
		if strings.Contains(call, "-F PREROUTING") || !strings.Contains(call, chain.Name) {
			t.Fatalf("cleanup touched an unrelated chain: %s", call)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "dead.json")); !os.IsNotExist(err) {
		t.Fatal("dead journal remained")
	}
	if _, err := os.Stat(filepath.Join(dir, "live.json")); err != nil {
		t.Fatal("live journal deleted")
	}
}

func testLocalNetwork() conf.Network {
	return conf.Network{IPv4: conf.Addr{Addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}}}
}

func TestRejectUntrustedJournalDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := recoverFirewall(dir, func(string, string, ...string) error { return nil }); err == nil {
		t.Fatal("accepted writable journal directory")
	}
}

func TestPortReservationPreservesHighRange(t *testing.T) {
	// Namespace benchmarks expand the ephemeral range; auto tunnel ports must
	// still preserve the original 32768..65535 distribution.
	for i := 0; i < 16; i++ {
		guard, n, err := reserve(testLocalNetwork())
		if err != nil {
			t.Fatal(err)
		}
		if n.Port < 32768 || n.Port > 65535 {
			t.Fatal("source port outside baseline range")
		}
		guard.Close()
	}
}
