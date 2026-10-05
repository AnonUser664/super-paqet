//go:build linux

// File firewall_journal.go: persists firewall intent before mutation and identifies dead
// owners using boot, PID start and namespace identity.

package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// firewallDirectory selects the private runtime journal directory used by startup and
// abnormal-exit recovery.
const firewallDirectory = "/run/super-paqet"

// firewallChain records one owned chain and exact jump arguments for narrowly scoped recovery.
type firewallChain struct {
	// Exact owned chain location; validation prevents cleanup of unrelated/shared chains.
	Binary, Table, Name, Hook string
	// Exact built-in hook reference used to attach/detach this owned chain.
	Jump []string
}

// firewallJournal records boot/PID-start/namespace identity and mutation intent so crashes do
// not leave untraceable rules.
type firewallJournal struct {
	// Recorded process owner; combined with start/boot identity to prevent PID-reuse mistakes.
	PID int
	// Process-start and system-boot identities guarding journal ownership.
	Start, Boot string
	// Network namespace inode limiting recovery to the recorded namespace.
	Namespace uint64
	// Ordered mutation intents replayed in reverse during rollback/cleanup.
	Chains []firewallChain
}

// processStart reads the kernel process start identity so PID reuse cannot make a dead journal
// look like the current owner.
func processStart(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return ""
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}

// namespaceID reads the current network namespace inode so recovery cannot intentionally cross
// namespace boundaries.
func namespaceID() (uint64, error) {
	s, err := os.Stat("/proc/self/ns/net")
	if err != nil {
		return 0, err
	}
	return s.Sys().(*syscall.Stat_t).Ino, nil
}

// bootID reads the boot identity used to distinguish stale journals from ownership in the
// current boot.
func bootID() string {
	b, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b))
}

// firewallCommand executes one bounded-lock iptables operation and retains stderr for
// actionable startup/cleanup errors.
func firewallCommand(binary, table string, args ...string) error {
	out, err := exec.Command(binary, append([]string{"-w", "5", "-t", table}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", binary, args, err, out)
	}
	return nil
}

// isMissing recognizes the normal missing-rule status while leaving permissions and other
// failures visible.
func isMissing(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

// cleanFirewallChain checks an owned chain before its jump target, then removes only that
// chain and reference; absent chains are already clean.
func cleanFirewallChain(c firewallChain, run func(string, string, ...string) error) error {
	// Never flush a shared table or a built-in chain.
	if !strings.HasPrefix(c.Name, "SPQ_") || len(c.Name) != 17 {
		return fmt.Errorf("invalid owned firewall chain %q", c.Name)
	}
	if c.Binary != "iptables" && c.Binary != "ip6tables" {
		return fmt.Errorf("invalid firewall binary")
	}
	if c.Table != "raw" && c.Table != "mangle" && c.Table != "filter" {
		return fmt.Errorf("invalid firewall table")
	}
	// Check the owned chain before referring to it in a jump. nftables
	// returns exit 2 for -C rules naming an absent target; -S reports absence
	// normally. A missing chain cannot have surviving references.
	if err := run(c.Binary, c.Table, "-S", c.Name); err != nil {
		if isMissing(err) {
			return nil
		}
		return err
	}
	check := append([]string{"-C", c.Hook}, c.Jump...)
	if err := run(c.Binary, c.Table, check...); err == nil {
		if err := run(c.Binary, c.Table, append([]string{"-D", c.Hook}, c.Jump...)...); err != nil {
			return err
		}
	} else if !isMissing(err) {
		return err
	}
	if err := run(c.Binary, c.Table, "-F", c.Name); err != nil {
		return err
	}
	return run(c.Binary, c.Table, "-X", c.Name)
}

// persist atomically stores restrictive ownership/intent metadata before a firewall mutation
// can outlive the process.
func (f *firewall) persist() error {
	if f.directory == "" {
		f.directory = firewallDirectory
	}
	if err := os.MkdirAll(f.directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(f.directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("firewall state directory must be owned by this user and not writable by others")
	}
	if f.path == "" {
		ns, err := namespaceID()
		if err != nil {
			return err
		}
		f.journal.PID = os.Getpid()
		f.journal.Start = processStart(os.Getpid())
		f.journal.Boot = bootID()
		f.journal.Namespace = ns
		f.path = filepath.Join(f.directory, strconv.Itoa(os.Getpid())+"-"+f.journal.Chains[0].Name+".json")
	}
	b, err := json.Marshal(f.journal)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.directory, ".journal-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), f.path)
}

// RecoverFirewall removes journals belonging to dead processes in THIS network
// namespace. PID start time and boot ID prevent deleting a live instance's rules.
func RecoverFirewall() error { return recoverFirewall(firewallDirectory, firewallCommand) }

// recoverFirewall replays cleanup only for trusted dead-owner journals in this namespace,
// preserving live instances.
func recoverFirewall(directory string, run func(string, string, ...string) error) error {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("untrusted firewall journal directory")
	}
	ns, err := namespaceID()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
			return fmt.Errorf("untrusted firewall journal %s", path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var j firewallJournal
		if err := json.Unmarshal(b, &j); err != nil {
			return err
		}
		if j.Namespace != ns {
			continue
		}
		if j.Boot != bootID() {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		if j.Start != "" && processStart(j.PID) == j.Start {
			continue
		}
		for i := len(j.Chains) - 1; i >= 0; i-- {
			if err := cleanFirewallChain(j.Chains[i], run); err != nil {
				return err
			}
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}
