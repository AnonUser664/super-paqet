//go:build linux

// File firewall.go: creates and removes only instance-owned chains, preserving unrelated host
// firewall and VPN rules.

package engine

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"paqet/internal/conf"
	"strconv"
	"sync"
)

// firewall owns only rules it created, with rollback on partial startup failure.
type firewall struct {
	// Private journal directory and this instance's atomic intent-file path.
	directory, path string
	// Owned chains and owner identity retained until cleanup succeeds.
	journal firewallJournal
	// Serializes journal/rule mutation and teardown.
	mu sync.Mutex
}

// add journals and installs scoped notrack, reset suppression and input isolation; partial
// failure rolls back owned additions.
func (f *firewall) add(n *conf.Network) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := len(f.journal.Chains)
	rollback := func() {
		for i := len(f.journal.Chains) - 1; i >= start; i-- {
			_ = cleanFirewallChain(f.journal.Chains[i], firewallCommand)
		}
		// Keep intent in the journal until full cleanup succeeds. It is safe to
		// replay cleanup, including after an interrupted rollback.
	}
	var token [6]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	chain := "SPQ_" + hex.EncodeToString(token[:])
	port := strconv.Itoa(n.Port)
	for _, a := range []*net.UDPAddr{n.IPv4.Addr, n.IPv6.Addr} {
		if a == nil {
			continue
		}
		binary := "iptables"
		if a.IP.To4() == nil {
			binary = "ip6tables"
		}
		ip := a.IP.String()
		for _, spec := range []struct {
			table, hook string
			match, body []string
		}{
			{"raw", "PREROUTING", []string{"-i", n.Interface.Name, "-d", ip, "-p", "tcp", "--dport", port}, []string{"-j", "CT", "--notrack"}},
			{"raw", "OUTPUT", []string{"-o", n.Interface.Name, "-s", ip, "-p", "tcp", "--sport", port}, []string{"-j", "CT", "--notrack"}},
			{"mangle", "OUTPUT", []string{"-o", n.Interface.Name, "-s", ip, "-p", "tcp", "--sport", port}, []string{"-p", "tcp", "--tcp-flags", "RST", "RST", "-j", "DROP"}},
			{"filter", "INPUT", []string{"-i", n.Interface.Name, "-d", ip, "-p", "tcp", "--dport", port}, []string{"-j", "DROP"}},
		} {
			// Separate chains per hook (raw has two hooks).
			name := chain + spec.hook[:1]
			jump := append(append([]string{}, spec.match...), "-j", name)
			f.journal.Chains = append(f.journal.Chains, firewallChain{binary, spec.table, name, spec.hook, jump})
			if err := f.persist(); err != nil {
				rollback()
				return err
			}
			cmd := func(args ...string) error { return firewallCommand(binary, spec.table, args...) }
			if err := cmd("-N", name); err != nil {
				rollback()
				return err
			}
			if err := cmd(append([]string{"-A", name}, spec.body...)...); err != nil {
				rollback()
				return err
			}
			if err := cmd(append([]string{"-I", spec.hook, "1"}, jump...)...); err != nil {
				rollback()
				return err
			}
		}
	}
	return nil
}

// close removes recorded owned chains and retains recovery intent until all cleanup steps
// succeed.
func (f *firewall) close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var errs []error
	for i := len(f.journal.Chains) - 1; i >= 0; i-- {
		if err := cleanFirewallChain(f.journal.Chains[i], firewallCommand); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		f.journal.Chains = nil
		if f.path != "" {
			if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
