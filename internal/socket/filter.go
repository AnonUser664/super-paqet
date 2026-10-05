// File filter.go: scopes packet capture to configured local tunnel addresses/ports before
// kernel TCP handling.

package socket

import (
	"fmt"
	"paqet/internal/conf"
	"strings"
)

// captureFilter restricts capture to local tunnel addresses and ports while allowing the
// original outer flag behavior.
func captureFilter(cfg *conf.Network) string {
	var hosts []string
	if cfg.IPv4.Addr != nil {
		hosts = append(hosts, "dst host "+cfg.IPv4.Addr.IP.String())
	}
	if cfg.IPv6.Addr != nil {
		hosts = append(hosts, "dst host "+cfg.IPv6.Addr.IP.String())
	}
	filter := fmt.Sprintf("tcp and dst port %d", cfg.Port)
	if len(hosts) > 0 {
		filter += " and (" + strings.Join(hosts, " or ") + ")"
	}
	return filter
}
