// File shared_flags_test.go checks flag ownership across independent lanes
// sharing one raw tuple; no packet injection or privileges are required.
package socket

import (
	"net"
	"net/netip"
	"paqet/internal/conf"
	"paqet/internal/pkg/iterator"
	"testing"
)

// TestSharedFlagOwnershipRetainsSiblings rejects stale setup and removes tuple
// state only after the last live conversation releases its reference.
func TestSharedFlagOwnershipRetainsSiblings(t *testing.T) {
	h := &SendHandle{tcpF: tcpF{clientTCPF: make(map[netip.AddrPort]*iterator.Iterator[conf.TCPF])}}
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 29998}
	key := peerAddress(addr.IP, uint16(addr.Port))
	h.registerClientGroup(addr, 1)
	h.registerClientGroup(addr, 2)
	h.setClientTCPFSession(addr, 1, []conf.TCPF{{}})
	h.deleteClientSession(addr, 1)
	if h.tcpF.clientTCPF[key] == nil || len(h.tcpF.groups[key]) != 1 {
		t.Fatal("sibling flags erased")
	}
	h.setClientTCPFSession(addr, 1, nil)
	if len(h.tcpF.clientTCPF[key].Items) != 1 {
		t.Fatal("retired lane changed flags")
	}
	h.deleteClientSession(addr, 2)
	if len(h.tcpF.groups) != 0 || len(h.tcpF.clientTCPF) != 0 {
		t.Fatal("final lane leaked tuple state")
	}
}
