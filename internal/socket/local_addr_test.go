package socket

import (
	"net"
	"paqet/internal/conf"
	"testing"
)

func TestPCAPLocalAddressIsUsableAndDoesNotAliasConfiguration(t *testing.T) {
	for _, text := range []string{"198.18.0.1", "fd42::1"} {
		cfg := &conf.Network{Port: 35000}
		addr := &net.UDPAddr{IP: net.ParseIP(text)}
		if addr.IP.To4() != nil {
			cfg.IPv4.Addr = addr
		} else {
			cfg.IPv6.Addr = addr
		}
		c := &PacketConn{cfg: cfg}
		got := c.LocalAddr().(*net.UDPAddr)
		if got.Port != 35000 || !got.IP.Equal(addr.IP) || got.String() == "" {
			t.Fatal("pcap address missing")
		}
		got.IP[0] ^= 255
		if !addr.IP.Equal(net.ParseIP(text)) {
			t.Fatal("local address changed configured source")
		}
	}
}
