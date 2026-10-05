// File config_test.go: verifies validation CLI output, failure exits and absence
// of socket binding or prepared-key output during successful checks.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateReportsInvalidFiles checks machine and human errors remain errors
// rather than a success message or a partially started engine.
func TestValidateReportsInvalidFiles(t *testing.T) {
	for _, data := range []string{"unknown: true\n", "peers: {a: {enc: null}}\n", "reload: {interval: 1ms}\n"} {
		t.Run(strings.TrimSpace(data), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "conf.yaml")
			os.WriteFile(path, []byte(data), 0600)
			cmd := NewCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"validate", "-c", path, "--json"})
			if err := cmd.Execute(); err == nil {
				t.Fatal("invalid config exited successfully")
			}
			var result map[string]any
			// Cobra may emit the human error to stderr; the first line is the JSON contract.
			if err := json.Unmarshal(bytes.Split(output.Bytes(), []byte("\n"))[0], &result); err != nil {
				t.Fatal(err)
			}
			if result["valid"] != false || result["path"] != path || result["error"] == nil {
				t.Fatal("missing machine-readable failure")
			}
		})
	}
	cmd := NewCommand()
	cmd.SetArgs([]string{"validate", "-c", filepath.Join(t.TempDir(), "missing.yaml")})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if cmd.Execute() == nil {
		t.Fatal("missing file passed validation")
	}
}

// TestValidateChecksPreparedSchemaWithoutBinding holds the configured TCP bind
// while validating an explicit endpoint, proving validation is preparation-only.
func TestValidateChecksPreparedSchemaWithoutBinding(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	iface := ""
	for _, candidate := range ifaces {
		if len(candidate.HardwareAddr) == 6 {
			iface = candidate.Name
			break
		}
	}
	if iface == "" {
		t.Skip("no Ethernet interface for strict validation")
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	path := filepath.Join(t.TempDir(), "conf.yaml")
	data := fmt.Sprintf("peers:\n  a:\n    address: 192.0.2.1:29999\n    enc: 'null'\n    key: PRIVATE-FIXTURE-KEY\n    sessions: 1\n    network: {interface: %s, ipv4: {addr: '192.0.2.10:0', router_mac: '02:00:00:00:00:01'}}\nforwards:\n  - {listen: '%s', peer: a, target: 'localhost:80'}\n", iface, occupied.Addr())
	os.WriteFile(path, []byte(data), 0600)
	for _, machine := range []bool{false, true} {
		cmd := NewCommand()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		args := []string{"validate", "-c", path}
		if machine {
			args = append(args, "--json")
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "PRIVATE-FIXTURE-KEY") {
			t.Fatal("prepared key printed")
		}
		if machine {
			var result map[string]any
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["valid"] != true || result["peers"] != float64(1) || result["forwards"] != float64(1) {
				t.Fatal("wrong validation summary")
			}
		} else if !strings.Contains(output.String(), "configuration valid:") {
			t.Fatal("missing human summary")
		}
	}
}
