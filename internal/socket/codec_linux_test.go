//go:build linux

package socket

import (
	"bytes"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"net"
	"net/netip"
	"paqet/internal/conf"
	"paqet/internal/pkg/iterator"
	"testing"
)

func testSend(f conf.TCPF) *SendHandle {
	return &SendHandle{srcPort: 29999, time: 0x12345678, srcIPv4: net.IPv4(198, 18, 0, 2), srcIPv6: net.ParseIP("2001:db8::2"), srcIPv4RHWA: net.HardwareAddr{2, 0, 0, 0, 0, 1}, srcIPv6RHWA: net.HardwareAddr{2, 0, 0, 0, 0, 1}, tcpF: tcpF{tcpF: iterator.Iterator[conf.TCPF]{Items: []conf.TCPF{f}}, clientTCPF: make(map[netip.AddrPort]*iterator.Iterator[conf.TCPF])}}
}

func referenceFrame(h *SendHandle, payload []byte, addr *net.UDPAddr, mac net.HardwareAddr) []byte {
	e := encoder{eth: layers.Ethernet{SrcMAC: mac}, mss: [2]byte{5, 180}, ws: [1]byte{8}, buf: gopacket.NewSerializeBuffer()}
	h.buildTCPHeader(&e, uint16(addr.Port), h.getClientTCPF(addr.IP, uint16(addr.Port)))
	var ip gopacket.SerializableLayer
	if addr.IP.To4() != nil {
		h.buildIPv4Header(&e, addr.IP)
		ip = &e.ip4
		e.tcp.SetNetworkLayerForChecksum(&e.ip4)
		e.eth.EthernetType = layers.EthernetTypeIPv4
		e.eth.DstMAC = h.srcIPv4RHWA
	} else {
		h.buildIPv6Header(&e, addr.IP)
		ip = &e.ip6
		e.tcp.SetNetworkLayerForChecksum(&e.ip6)
		e.eth.EthernetType = layers.EthernetTypeIPv6
		e.eth.DstMAC = h.srcIPv6RHWA
	}
	if err := gopacket.SerializeLayers(e.buf, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, &e.eth, ip, &e.tcp, gopacket.Payload(payload)); err != nil {
		panic(err)
	}
	return e.buf.Bytes()
}

func TestPacketShapeMatchesBaseline(t *testing.T) {
	mac := net.HardwareAddr{2, 0, 0, 0, 0, 2}
	for _, ip := range []net.IP{net.IPv4(198, 18, 0, 1), net.ParseIP("2001:db8::1")} {
		for _, flags := range []conf.TCPF{{PSH: true, ACK: true}, {SYN: true}, {SYN: true, ACK: true}, {FIN: true, RST: true, URG: true, ECE: true, CWR: true, NS: true}} {
			for _, length := range []int{1, 2, 31, 1330} {
				addr := &net.UDPAddr{IP: ip, Port: 32001}
				payload := make([]byte, length)
				for i := range payload {
					payload[i] = byte(i * 31)
				}
				baseline, optimized := testSend(flags), testSend(flags)
				var e encoder
				for i := 0; i < 20; i++ {
					want := referenceFrame(baseline, payload, addr, mac)
					var hdr [96]byte
					n, err := optimized.encodeHeader(hdr[:], payload, addr, mac, &e)
					if err != nil {
						t.Fatal(err)
					}
					got := append(append([]byte{}, hdr[:n]...), payload...)
					if !bytes.Equal(got, want) {
						t.Fatalf("packet shape mismatch: ip=%s flags=%+v length=%d packet=%d\ngot %x\nwant %x", ip, flags, length, i, got, want)
					}
					decoded, source, ok := decodeFrame(got, 32001)
					if !ok || !bytes.Equal(decoded, payload) || int(source.Port()) != 29999 {
						t.Fatal("packet decoder disagrees")
					}
				}
			}
		}
	}
}

func TestMalformedFrames(t *testing.T) {
	frame := referenceFrame(testSend(conf.TCPF{PSH: true, ACK: true}), []byte("hello"), &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 32001}, net.HardwareAddr{2, 0, 0, 0, 0, 2})
	for i := 0; i < len(frame); i++ {
		if _, _, ok := decodeFrame(frame[:i], 32001); ok {
			t.Fatalf("accepted truncated frame length %d", i)
		}
	}
	if _, _, ok := decodeFrame(frame, 32002); ok {
		t.Fatal("accepted wrong port")
	}
	frame[20] = 0x20
	if _, _, ok := decodeFrame(frame, 32001); ok {
		t.Fatal("accepted fragmented frame")
	}
}

func TestFlagOwnershipAcrossReconnect(t *testing.T) {
	h := testSend(conf.TCPF{PSH: true, ACK: true})
	addr := &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 32000}
	h.registerClient(addr, 1)
	h.setClientTCPFSession(addr, 1, []conf.TCPF{{SYN: true}})
	h.registerClient(addr, 2)
	h.setClientTCPFSession(addr, 2, []conf.TCPF{{ACK: true}})
	h.deleteClientSession(addr, 1)
	h.setClientTCPFSession(addr, 1, []conf.TCPF{{RST: true}})
	if f := h.getClientTCPF(addr.IP, uint16(addr.Port)); !f.ACK || f.RST || f.SYN {
		t.Fatal("old session changed replacement flag state")
	}
	h.deleteClientSession(addr, 2)
	if f := h.getClientTCPF(addr.IP, uint16(addr.Port)); !f.PSH || !f.ACK {
		t.Fatal("current session cleanup failed")
	}
}

func FuzzDecodeFrame(f *testing.F) {
	f.Add(referenceFrame(testSend(conf.TCPF{PSH: true, ACK: true}), []byte("hello"), &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 32001}, net.HardwareAddr{2, 0, 0, 0, 0, 2}))
	f.Fuzz(func(t *testing.T, b []byte) { decodeFrame(b, 32001) })
}

func BenchmarkEncodeHeader(b *testing.B) {
	h := testSend(conf.TCPF{PSH: true, ACK: true})
	payload := make([]byte, 1350)
	addr := &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 32001}
	mac := net.HardwareAddr{2, 0, 0, 0, 0, 2}
	var e encoder
	var hdr [96]byte
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := h.encodeHeader(hdr[:], payload, addr, mac, &e); err != nil {
			b.Fatal(err)
		}
	}
}
