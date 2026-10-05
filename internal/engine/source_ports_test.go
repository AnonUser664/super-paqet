//go:build linux

// File source_ports_test.go verifies deterministic carrier reservations and
// reload conflict detection without raw sockets or firewall mutations.
package engine

import (
	"context"
	"net"
	"testing"

	"paqet/internal/conf"
)

// TestSourcePortValidation exercises list bounds and peer-only semantics before
// any discovery probe or kernel reservation can occur.
func TestSourcePortValidation(t *testing.T) {
	for _, ports := range [][]int{{0}, {-1}, {65536}, {29998, 29998}, make([]int, 257)} {
		e := Endpoint{SourcePorts: ports}
		if e.prepareSourcePorts(false) == nil {
			t.Fatalf("accepted %v", ports)
		}
	}
	e := Endpoint{SourcePorts: []int{29998, 29996, 29994, 29992}}
	if e.prepareSourcePorts(true) == nil {
		t.Fatal("listener accepted outgoing ports")
	}
	if err := e.prepareSourcePorts(false); err != nil {
		t.Fatal(err)
	}
	if e.Sessions < 1 || e.Sessions > 4 || e.MaxSessions != 4 {
		t.Fatal("invalid bounded defaults")
	}
	e.Sessions = 5
	if e.prepareSourcePorts(false) == nil {
		t.Fatal("initial pool exceeds list")
	}
	e.Sessions, e.MaxSessions = 1, 5
	if e.prepareSourcePorts(false) == nil {
		t.Fatal("elastic pool exceeds list")
	}
}

// TestSourcePortReservationAndReloadBinds ensures every slot guards its selected
// port, effective binds include the list, and shutdown releases all guards.
func TestSourcePortReservationAndReloadBinds(t *testing.T) {
	var ports []int
	var reservations []*net.TCPListener
	for range 3 {
		l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		reservations = append(reservations, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	for _, l := range reservations {
		l.Close()
	}
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}
	ep := Endpoint{SourcePorts: ports, MaxSessions: 3, Network: conf.Network{IPv4: conf.Addr{Addr: addr}}}
	p := &peer{engine: &Engine{ctx: context.Background()}, endpoint: ep}
	t.Cleanup(p.close)
	for i, port := range ports {
		s, err := p.allocateSlot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		p.slots = append(p.slots, s)
		if s.network.Port != port || s.network.IPv4.Addr.Port != port {
			t.Fatalf("slot %d lost source port", i)
		}
		if l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: addr.IP, Port: port}); err == nil {
			l.Close()
			t.Fatal("source guard missing")
		}
	}
	if _, err := p.allocateSlot(context.Background()); err == nil {
		t.Fatal("exhausted list reused a port")
	}
	binds := specBinds(resourceSpec{kind: "peer", endpoint: ep})
	if len(binds) != 3 || binds[1].addr.Port != ports[1] || addr.Port != 0 {
		t.Fatal("reload binds omit ports or mutate prepared address")
	}
	p.close()
	for _, port := range ports {
		l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: addr.IP, Port: port})
		if err != nil {
			t.Fatal("source guard leaked", err)
		}
		l.Close()
	}
}
