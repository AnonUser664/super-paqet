// File endpoint_key_test.go checks binary demultiplexing identity independently
// of live transport timing, including address mutation and generic fallbacks.
package kcp

import (
	"net"
	"testing"
)

type namedEndpoint string

func (a namedEndpoint) Network() string { return "test" }
func (a namedEndpoint) String() string  { return string(a) }

func TestBinaryConversationEndpointIdentity(t *testing.T) {
	l := &Listener{multiConversation: true}
	a := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 29999}
	key := l.conversationKey(a, 7)
	mapped := &net.UDPAddr{IP: net.ParseIP("::ffff:192.0.2.1"), Port: 29999}
	if key != l.conversationKey(mapped, 7) {
		t.Fatal("mapped IPv4 identity changed")
	}
	for _, other := range []net.Addr{
		&net.UDPAddr{IP: net.IPv4(192, 0, 2, 2), Port: 29999},
		&net.UDPAddr{IP: a.IP, Port: 30000},
		&net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 29999},
		namedEndpoint("generic"),
	} {
		if key == l.conversationKey(other, 7) {
			t.Fatal("distinct endpoints collided", other)
		}
	}
	if key == l.conversationKey(a, 8) {
		t.Fatal("conversations collided")
	}
	a.IP[0] ^= 1
	if key != l.conversationKey(mapped, 7) {
		t.Fatal("key borrowed mutable address bytes")
	}
	z1 := &net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 42, Zone: "eth0"}
	z2 := &net.UDPAddr{IP: z1.IP, Port: 42, Zone: "eth1"}
	if l.conversationKey(z1, 1) == l.conversationKey(z2, 1) {
		t.Fatal("IPv6 zones collided")
	}
	l.multiConversation = false
	if l.conversationKey(mapped, 1) != l.conversationKey(mapped, 2) {
		t.Fatal("ordinary listener changed conversation policy")
	}
	if l.conversationKey(namedEndpoint("a"), 1) == l.conversationKey(namedEndpoint("b"), 1) {
		t.Fatal("generic endpoint fallback collided")
	}
}

func BenchmarkBinaryConversationEndpointKey(b *testing.B) {
	l := &Listener{multiConversation: true}
	a := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 29999}
	b.ReportAllocs()
	for b.Loop() {
		_ = l.conversationKey(a, 7)
	}
}
