// File addr.go: represents target host/port names without resolving them on the forwarding
// client.

package tnet

import (
	"fmt"
	"net"
	"strconv"
)

// Addr keeps a target hostname and port for resolution at the accepting server, not at the
// forwarding client.
type Addr struct {
	// Target host text sent to the accepting server for resolution.
	Host string
	// Remote target port validated against the 16-bit control representation.
	Port int
}

// NewAddr preserves a target hostname and checks its port without resolving it on the
// forwarding client.
func NewAddr(s string) (*Addr, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return nil, err
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	if host == "" || len(host) > 253 || port < 1 || port > 65535 {
		return nil, fmt.Errorf("target needs a host and port between 1 and 65535")
	}

	return &Addr{Host: host, Port: port}, nil
}

// String formats the target with correct IPv6 bracket handling for the accepting server
// dialer.
func (e *Addr) String() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}
