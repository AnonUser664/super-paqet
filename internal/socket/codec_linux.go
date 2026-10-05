//go:build linux

// File codec_linux.go: encodes/checksums and bounds-checks Ethernet/IP/TCP frames without
// imposing outer TCP stream semantics.

package socket

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
)

// sum16 adds network-order words for the Internet checksum, including an odd trailing byte.
func sum16(b []byte) uint32 {
	var sum uint32
	for len(b) >= 8 {
		sum += uint32(binary.BigEndian.Uint16(b)) + uint32(binary.BigEndian.Uint16(b[2:])) + uint32(binary.BigEndian.Uint16(b[4:])) + uint32(binary.BigEndian.Uint16(b[6:]))
		b = b[8:]
	}
	for len(b) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(b))
		b = b[2:]
	}
	if len(b) == 1 {
		sum += uint32(b[0]) << 8
	}
	return sum
}

// checksum folds carries and complements the checksum accumulator used by manually encoded
// headers.
func checksum(sum uint32) uint16 {
	for sum>>16 != 0 {
		sum = (sum & 65535) + (sum >> 16)
	}
	return ^uint16(sum)
}

// encodeHeader reproduces the baseline gopacket serialization byte for byte.
// Payload is a separate iovec, so it does not need a second copy into a frame.
func (h *SendHandle) encodeHeader(dst []byte, payload []byte, addr *net.UDPAddr, srcMAC net.HardwareAddr, e *encoder) (int, error) {
	if len(srcMAC) != 6 {
		return 0, fmt.Errorf("raw transport requires an Ethernet interface")
	}
	h.buildTCPHeader(e, uint16(addr.Port), h.getClientTCPF(addr.IP, uint16(addr.Port)))
	ip4 := addr.IP.To4()
	iplen := 40
	router := h.srcIPv6RHWA
	src := h.srcIPv6.To16()
	remote := addr.IP.To16()
	if ip4 != nil {
		iplen = 20
		router = h.srcIPv4RHWA
		src = h.srcIPv4.To4()
		remote = ip4
	}
	if src == nil || len(router) != 6 || remote == nil {
		return 0, fmt.Errorf("destination address family has no configured source or next-hop")
	}
	tcplen := 32
	if e.tcp.SYN {
		tcplen = 40
	}
	length := 14 + iplen + tcplen
	if len(dst) < length || len(payload)+iplen+tcplen > 65535 {
		return 0, fmt.Errorf("packet exceeds supported MTU")
	}
	clear(dst[:length])
	copy(dst[:6], router)
	copy(dst[6:12], srcMAC)
	ip := dst[14 : 14+iplen]
	var pseudo uint32
	if ip4 != nil {
		binary.BigEndian.PutUint16(dst[12:14], 0x0800)
		ip[0] = 0x45
		ip[1] = 184
		binary.BigEndian.PutUint16(ip[2:4], uint16(iplen+tcplen+len(payload)))
		ip[6] = 0x40
		ip[8] = 64
		ip[9] = 6
		copy(ip[12:16], src)
		copy(ip[16:20], remote)
		binary.BigEndian.PutUint16(ip[10:12], checksum(sum16(ip)))
		pseudo = sum16(ip[12:20]) + 6 + uint32(tcplen+len(payload))
	} else {
		binary.BigEndian.PutUint16(dst[12:14], 0x86dd)
		ip[0] = 0x6b
		ip[1] = 0x80
		binary.BigEndian.PutUint16(ip[4:6], uint16(tcplen+len(payload)))
		ip[6] = 6
		ip[7] = 64
		copy(ip[8:24], src)
		copy(ip[24:40], remote)
		pseudo = sum16(ip[8:40]) + 6 + uint32(tcplen+len(payload))
	}
	tcp := dst[14+iplen : length]
	binary.BigEndian.PutUint16(tcp[:2], uint16(h.srcPort))
	binary.BigEndian.PutUint16(tcp[2:4], uint16(addr.Port))
	binary.BigEndian.PutUint32(tcp[4:8], e.tcp.Seq)
	binary.BigEndian.PutUint32(tcp[8:12], e.tcp.Ack)
	tcp[12] = byte(tcplen/4) << 4
	if e.tcp.NS {
		tcp[12] |= 1
	}
	for i, on := range []bool{e.tcp.FIN, e.tcp.SYN, e.tcp.RST, e.tcp.PSH, e.tcp.ACK, e.tcp.URG, e.tcp.ECE, e.tcp.CWR} {
		if on {
			tcp[13] |= 1 << i
		}
	}
	binary.BigEndian.PutUint16(tcp[14:16], 65535)
	if e.tcp.SYN {
		copy(tcp[20:], []byte{2, 4, 5, 180, 4, 2, 8, 10})
		copy(tcp[28:36], e.ts[:])
		copy(tcp[36:], []byte{1, 3, 3, 8})
	} else {
		copy(tcp[20:], []byte{1, 1, 8, 10})
		copy(tcp[24:], e.ts[:])
	}
	binary.BigEndian.PutUint16(tcp[16:18], checksum(pseudo+sum16(tcp)+sum16(payload)))
	return length, nil
}

// decodeFrame accepts unfragmented Ethernet IPv4/IPv6 TCP datagrams only.
// Reject truncated, fragmented and unsupported extension-header frames.
func decodeFrame(frame []byte, port uint16) ([]byte, netip.AddrPort, bool) {
	if len(frame) < 14 {
		return nil, netip.AddrPort{}, false
	}
	typ := binary.BigEndian.Uint16(frame[12:14])
	offset := 14
	// VLAN tags are an envelope around the same IP/TCP contract.
	for tags := 0; typ == 0x8100 || typ == 0x88a8; tags++ {
		if tags >= 2 || len(frame) < offset+4 {
			return nil, netip.AddrPort{}, false
		}
		typ = binary.BigEndian.Uint16(frame[offset+2 : offset+4])
		offset += 4
	}
	var source netip.Addr
	var tcp []byte
	switch typ {
	case 0x0800:
		ip := frame[offset:]
		if len(ip) < 20 || ip[0]>>4 != 4 || ip[9] != 6 {
			return nil, netip.AddrPort{}, false
		}
		header := int(ip[0]&15) * 4
		total := int(binary.BigEndian.Uint16(ip[2:4]))
		if header < 20 || total > len(ip) || total < header+20 || binary.BigEndian.Uint16(ip[6:8])&0x3fff != 0 {
			return nil, netip.AddrPort{}, false
		}
		source = netip.AddrFrom4([4]byte(ip[12:16]))
		tcp = ip[header:total]
	case 0x86dd:
		ip := frame[offset:]
		if len(ip) < 60 || ip[0]>>4 != 6 || ip[6] != 6 {
			return nil, netip.AddrPort{}, false
		}
		total := 40 + int(binary.BigEndian.Uint16(ip[4:6]))
		if total > len(ip) || total < 60 {
			return nil, netip.AddrPort{}, false
		}
		source = netip.AddrFrom16([16]byte(ip[8:24]))
		tcp = ip[40:total]
	default:
		return nil, netip.AddrPort{}, false
	}
	header := int(tcp[12]>>4) * 4
	if header < 20 || header >= len(tcp) || binary.BigEndian.Uint16(tcp[2:4]) != port {
		return nil, netip.AddrPort{}, false
	}
	return tcp[header:], netip.AddrPortFrom(source, binary.BigEndian.Uint16(tcp[:2])), true
}
