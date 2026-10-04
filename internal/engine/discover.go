//go:build linux

package engine

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"paqet/internal/conf"
	"strconv"
	"time"
)

type route struct {
	Dev     string `json:"dev"`
	Gateway string `json:"gateway"`
	Source  string `json:"prefsrc"`
}

func discover(n *conf.Network, endpoint *net.UDPAddr, listener bool) error {
	addr := &n.IPv6
	if endpoint.IP.To4() != nil {
		addr = &n.IPv4
	}
	if n.Interface_ != "" && addr.Addr_ != "" && addr.RouterMac_ != "" {
		return nil
	}
	query := []string{"-j", "route", "get", endpoint.IP.String()}
	if listener {
		query = []string{"-j", "route", "show", "default"}
		if endpoint.IP.To4() == nil {
			query = append([]string{"-6"}, query...)
		}
	}
	data, err := exec.Command("ip", query...).Output()
	if err != nil {
		return fmt.Errorf("route discovery: %w; provide network overrides", err)
	}
	var routes []route
	if err = json.Unmarshal(data, &routes); err != nil || len(routes) == 0 {
		return fmt.Errorf("no route discovered; provide network overrides")
	}
	r := routes[0]
	if n.Interface_ == "" {
		n.Interface_ = r.Dev
	}
	if n.Interface_ != r.Dev && addr.RouterMac_ == "" {
		return fmt.Errorf("explicit interface differs from route; provide router_mac")
	}
	if addr.Addr_ == "" {
		source := r.Source
		port := 0
		if listener {
			source = endpoint.IP.String()
			port = endpoint.Port
		}
		if source == "" {
			return fmt.Errorf("route has no source address; provide network address")
		}
		addr.Addr_ = net.JoinHostPort(source, strconv.Itoa(port))
	}
	if addr.RouterMac_ != "" {
		return nil
	}
	nextHop := r.Gateway
	if nextHop == "" {
		if listener {
			return fmt.Errorf("listener next-hop MAC needs network.router_mac on a network without a default gateway")
		}
		nextHop = endpoint.IP.String()
	}
	// A short ordinary UDP probe populates ARP/NDP; tunnel packets still use raw TCP.
	probe, err := net.DialTimeout("udp", net.JoinHostPort(nextHop, "9"), time.Second)
	if err == nil {
		probe.Write([]byte{0})
		probe.Close()
	}
	for i := 0; i < 20; i++ {
		data, err = exec.Command("ip", "-j", "neigh", "show", "to", nextHop, "dev", n.Interface_).Output()
		if err != nil {
			return fmt.Errorf("neighbor discovery: %w", err)
		}
		var neighbors []struct {
			MAC string `json:"lladdr"`
		}
		if json.Unmarshal(data, &neighbors) == nil {
			for _, v := range neighbors {
				if v.MAC != "" {
					addr.RouterMac_ = v.MAC
					return nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("cannot discover MAC for %s; provide network.%s.router_mac", nextHop, map[bool]string{true: "ipv4", false: "ipv6"}[endpoint.IP.To4() != nil])
}
