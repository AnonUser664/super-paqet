//go:build linux

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

const firewallDirectory = "/run/super-paqet"

type firewallChain struct {
	Binary, Table, Name, Hook string
	Jump                      []string
}
type firewallJournal struct {
	PID         int
	Start, Boot string
	Namespace   uint64
	Chains      []firewallChain
}

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
func namespaceID() (uint64, error) {
	s, err := os.Stat("/proc/self/ns/net")
	if err != nil {
		return 0, err
	}
	return s.Sys().(*syscall.Stat_t).Ino, nil
}
func bootID() string {
	b, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b))
}

func firewallCommand(binary, table string, args ...string) error {
	out, err := exec.Command(binary, append([]string{"-w", "5", "-t", table}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", binary, args, err, out)
	}
	return nil
}

func isMissing(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

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
	check := append([]string{"-C", c.Hook}, c.Jump...)
	if err := run(c.Binary, c.Table, check...); err == nil {
		if err := run(c.Binary, c.Table, append([]string{"-D", c.Hook}, c.Jump...)...); err != nil {
			return err
		}
	} else if !isMissing(err) {
		return err
	}
	if err := run(c.Binary, c.Table, "-S", c.Name); err != nil {
		if isMissing(err) {
			return nil
		}
		return err
	}
	if err := run(c.Binary, c.Table, "-F", c.Name); err != nil {
		return err
	}
	return run(c.Binary, c.Table, "-X", c.Name)
}

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
