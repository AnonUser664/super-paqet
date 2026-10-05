// File network.go: turns network strings into validated interface, address and next-hop
// objects for raw frame construction.

package conf

import (
	"fmt"
	"net"
	"runtime"
)

// Addr keeps the configured source/next-hop strings and their parsed raw-network address/MAC
// values.
type Addr struct {
	// Configured local IP:port string; zero source port permits automatic reservation.
	Addr_ string `yaml:"addr"`
	// Configured next-hop Ethernet MAC string, not a distant routed peer MAC.
	RouterMac_ string `yaml:"router_mac"`
	// Resolved configured source endpoint, separate from its original YAML string.
	Addr *net.UDPAddr `yaml:"-"`
	// Parsed next-hop hardware address for direct Ethernet injection.
	Router net.HardwareAddr `yaml:"-"`
}

// Network holds configured and discovered physical endpoint state; internal fanout fields are
// not YAML knobs.
type Network struct {
	// Kernel packet-group identifier shared by fixed listener workers.
	FanoutID uint16 `yaml:"-"`
	// Requests a kernel-selected group ID rather than risking collision with another listener.
	FanoutUnique bool `yaml:"-"`
	// Distinguishes enabled fanout from an unset or numerically zero group ID.
	FanoutEnabled bool `yaml:"-"`
	// Selected packet driver; both choices retain fabricated raw TCP frames.
	Backend string `yaml:"backend"`
	// Configured physical Ethernet interface name before discovery/lookup.
	Interface_ string `yaml:"interface"`
	// Legacy capture device identity; Linux uses the selected interface name.
	GUID string `yaml:"guid"`
	// Local IPv4 endpoint and its next-hop MAC, if this family is configured.
	IPv4 Addr `yaml:"ipv4"`
	// Local IPv6 endpoint and its next-hop MAC, if this family is configured.
	IPv6 Addr `yaml:"ipv6"`
	// Capture/socket storage budget used by the selected packet driver.
	PCAP PCAP `yaml:"pcap"`
	// Prepared outer flag cycles; they do not establish a TCP handshake.
	TCP TCP `yaml:"tcp"`
	// Resolved interface attributes used for source MAC, binding and MTU checks.
	Interface *net.Interface `yaml:"-"`
	// Prepared local tunnel port, shared by configured address families.
	Port int `yaml:"-"`
}

// setDefaults prepares capture budgets and outer flag defaults as separate network-layer
// contracts.
func (n *Network) setDefaults(role string) {
	n.PCAP.setDefaults(role)
	n.TCP.setDefaults()
}

// validate resolves configured interface/address/MAC objects and enforces Ethernet and dual-
// family port invariants.
func (n *Network) validate() []error {
	var errors []error

	if n.Interface_ == "" {
		errors = append(errors, fmt.Errorf("network interface is required"))
	}
	if len(n.Interface_) > 15 {
		errors = append(errors, fmt.Errorf("network interface name too long (max 15 characters): '%s'", n.Interface_))
	}
	lIface, err := net.InterfaceByName(n.Interface_)
	if err != nil {
		errors = append(errors, fmt.Errorf("failed to find network interface %s: %v", n.Interface_, err))
	}
	n.Interface = lIface
	if lIface != nil && len(lIface.HardwareAddr) != 6 {
		errors = append(errors, fmt.Errorf("raw transport requires an Ethernet interface with a 6-byte MAC address"))
	}

	if runtime.GOOS == "windows" && n.GUID == "" {
		errors = append(errors, fmt.Errorf("guid is required on windows"))
	}

	if n.IPv4.Addr_ == "" && n.IPv6.Addr_ == "" {
		errors = append(errors, fmt.Errorf("at least one address family (IPv4 or IPv6) must be configured"))
		return errors
	}
	if n.IPv4.Addr_ != "" {
		errors = append(errors, n.IPv4.validate()...)
	}
	if n.IPv6.Addr_ != "" {
		errors = append(errors, n.IPv6.validate()...)
	}

	ipv4OK := n.IPv4.Addr != nil
	ipv6OK := n.IPv6.Addr != nil
	if ipv4OK && (n.IPv4.Addr.IP.To4() == nil || n.IPv4.Addr.IP.IsUnspecified()) {
		errors = append(errors, fmt.Errorf("network.ipv4.addr needs a concrete IPv4 address"))
	}
	if ipv6OK && (n.IPv6.Addr.IP.To16() == nil || n.IPv6.Addr.IP.To4() != nil || n.IPv6.Addr.IP.IsUnspecified()) {
		errors = append(errors, fmt.Errorf("network.ipv6.addr needs a concrete IPv6 address"))
	}

	if ipv4OK && ipv6OK && n.IPv4.Addr.Port != n.IPv6.Addr.Port {
		errors = append(errors, fmt.Errorf("IPv4 port (%d) and IPv6 port (%d) must match when both are configured", n.IPv4.Addr.Port, n.IPv6.Addr.Port))
	}
	if ipv4OK {
		n.Port = n.IPv4.Addr.Port
	}
	if ipv6OK {
		n.Port = n.IPv6.Addr.Port
	}

	errors = append(errors, n.PCAP.validate()...)
	errors = append(errors, n.TCP.validate()...)

	return errors
}

// validate resolves a configured local endpoint and parses the next-hop MAC used by raw
// injection.
func (n *Addr) validate() []error {
	var errors []error

	l, err := validateAddr(n.Addr_, false)
	if err != nil {
		errors = append(errors, err)
	}
	n.Addr = l

	if n.RouterMac_ == "" {
		errors = append(errors, fmt.Errorf("router MAC address is required"))
	}

	hwAddr, err := net.ParseMAC(n.RouterMac_)
	if err != nil {
		errors = append(errors, fmt.Errorf("invalid Router MAC address '%s': %v", n.RouterMac_, err))
	}
	n.Router = hwAddr

	return errors
}
