//go:build linux

package socket

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
	"paqet/internal/conf"
	"paqet/internal/pkg/iterator"
)

const rawBatchSize = 64

type mmsg struct {
	Hdr unix.Msghdr
	Len uint32
}

type rawPacket struct {
	packets, drops atomic.Uint64
	txDrops        atomic.Uint64
	statsMu        sync.Mutex
	file           *os.File
	raw            syscall.RawConn
	send           *SendHandle
	mac            net.HardwareAddr
	local          *net.UDPAddr
	rxMu, txMu     sync.Mutex
	rxFrames       [rawBatchSize][2048]byte
	rxHdr          [rawBatchSize]mmsg
	rxVec          [rawBatchSize]unix.Iovec
	txHeaders      [rawBatchSize][96]byte
	txHdr          [rawBatchSize]mmsg
	txVec          [rawBatchSize][2]unix.Iovec
	encoder        encoder
	cache          map[netip.AddrPort]*net.UDPAddr
}

func newRawPacket(cfg *conf.Network) (*rawPacket, error) {
	// AF_PACKET takes a network-order protocol in a native integer. A fixed
	// byte swap would break the big-endian Linux architectures in release CI.
	var protocolBytes [2]byte
	binary.BigEndian.PutUint16(protocolBytes[:], unix.ETH_P_ALL)
	protocol := binary.NativeEndian.Uint16(protocolBytes[:])
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, int(protocol))
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "super-paqet-packet")
	fail := func(err error) (*rawPacket, error) { f.Close(); return nil, err }
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Ifindex: cfg.Interface.Index, Protocol: protocol}); err != nil {
		return fail(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_IGNORE_OUTGOING, 1); err != nil {
		return fail(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, cfg.PCAP.Sockbuf); err != nil {
		return fail(err)
	}
	// Raise only this socket's budget; do not alter global kernel sysctls.
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, cfg.PCAP.Sockbuf)
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, cfg.PCAP.Sockbuf); err != nil {
		return fail(err)
	}
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUFFORCE, cfg.PCAP.Sockbuf)
	program, err := pcap.CompileBPFFilter(layers.LinkTypeEthernet, 2048, captureFilter(cfg))
	if err != nil {
		return fail(err)
	}
	filter := make([]unix.SockFilter, len(program))
	for i, p := range program {
		filter[i] = unix.SockFilter{Code: p.Code, Jt: p.Jt, Jf: p.Jf, K: p.K}
	}
	if err := unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}); err != nil {
		return fail(err)
	}
	if cfg.FanoutEnabled || cfg.FanoutID != 0 || cfg.FanoutUnique {
		flags := unix.PACKET_FANOUT_HASH
		if cfg.FanoutUnique {
			flags |= unix.PACKET_FANOUT_FLAG_UNIQUEID
		}
		if err := unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_FANOUT, int(cfg.FanoutID)|(flags<<16)); err != nil {
			return fail(err)
		}
		if cfg.FanoutUnique {
			value, err := unix.GetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_FANOUT)
			if err != nil {
				return fail(err)
			}
			cfg.FanoutID = uint16(value)
		}
	}
	raw, err := f.SyscallConn()
	if err != nil {
		return fail(err)
	}
	send := &SendHandle{srcPort: uint16(cfg.Port), time: uint32(time.Now().UnixNano() / int64(time.Millisecond)), tcpF: tcpF{tcpF: iterator.Iterator[conf.TCPF]{Items: cfg.TCP.LF}, clientTCPF: make(map[netip.AddrPort]*iterator.Iterator[conf.TCPF])}}
	if cfg.IPv4.Addr != nil {
		send.srcIPv4 = cfg.IPv4.Addr.IP
		send.srcIPv4RHWA = cfg.IPv4.Router
	}
	if cfg.IPv6.Addr != nil {
		send.srcIPv6 = cfg.IPv6.Addr.IP
		send.srcIPv6RHWA = cfg.IPv6.Router
	}
	local := cfg.IPv4.Addr
	if local == nil {
		local = cfg.IPv6.Addr
	}
	p := &rawPacket{file: f, raw: raw, send: send, mac: cfg.Interface.HardwareAddr, local: local, cache: make(map[netip.AddrPort]*net.UDPAddr)}
	for i := range p.rxHdr {
		p.rxVec[i].Base = &p.rxFrames[i][0]
		p.rxVec[i].SetLen(len(p.rxFrames[i]))
		p.rxHdr[i].Hdr.Iov = &p.rxVec[i]
		p.rxHdr[i].Hdr.SetIovlen(1)
	}
	return p, nil
}

func (p *rawPacket) WriteBatch(ms []ipv4.Message, flags int) (int, error) {
	p.txMu.Lock()
	defer p.txMu.Unlock()
	n := min(len(ms), rawBatchSize)
	if n == 0 {
		return 0, nil
	}
	for i := 0; i < n; i++ {
		addr, ok := ms[i].Addr.(*net.UDPAddr)
		if !ok {
			return 0, net.InvalidAddrError("raw transport needs UDPAddr endpoints")
		}
		if len(ms[i].Buffers) != 1 {
			return 0, fmt.Errorf("expected one KCP payload buffer")
		}
		payload := ms[i].Buffers[0]
		length, err := p.send.encodeHeader(p.txHeaders[i][:], payload, addr, p.mac, &p.encoder)
		if err != nil {
			return 0, err
		}
		p.txVec[i][0].Base = &p.txHeaders[i][0]
		p.txVec[i][0].SetLen(length)
		p.txVec[i][1].Base = nil
		if len(payload) > 0 {
			p.txVec[i][1].Base = &payload[0]
		}
		p.txVec[i][1].SetLen(len(payload))
		p.txHdr[i] = mmsg{}
		p.txHdr[i].Hdr.Iov = &p.txVec[i][0]
		p.txHdr[i].Hdr.SetIovlen(2)
	}
	var sent int
	var opErr error
	err := p.raw.Write(func(fd uintptr) bool {
		r, _, errno := unix.Syscall6(unix.SYS_SENDMMSG, fd, uintptr(unsafe.Pointer(&p.txHdr[0])), uintptr(n), uintptr(unix.MSG_DONTWAIT), 0, 0)
		if errno == unix.EAGAIN || errno == unix.EINTR {
			return false
		}
		// AF_PACKET reports full qdisc queues as ENOBUFS. This is datagram
		// loss, not a broken socket: KCP must retransmit rather than abort
		// every application stream sharing the session.
		if errno == unix.ENOBUFS {
			p.txDrops.Add(uint64(n))
			sent = n
			return true
		}
		if errno != 0 {
			opErr = errno
		} else {
			sent = int(r)
		}
		return true
	})
	runtime.KeepAlive(ms)
	runtime.KeepAlive(p)
	// Do not retain KCP buffer pointers after the caller recycles them.
	for i := 0; i < n; i++ {
		p.txVec[i][1].Base = nil
	}
	if err != nil {
		return sent, err
	}
	return sent, opErr
}

func (p *rawPacket) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	p.rxMu.Lock()
	defer p.rxMu.Unlock()
	n := min(len(ms), rawBatchSize)
	if n == 0 {
		return 0, nil
	}
	for {
		for i := 0; i < n; i++ {
			p.rxHdr[i].Hdr.Flags = 0
			p.rxHdr[i].Len = 0
		}
		var received int
		var opErr error
		err := p.raw.Read(func(fd uintptr) bool {
			r, _, errno := unix.Syscall6(unix.SYS_RECVMMSG, fd, uintptr(unsafe.Pointer(&p.rxHdr[0])), uintptr(n), uintptr(unix.MSG_DONTWAIT), 0, 0)
			if errno == unix.EAGAIN || errno == unix.EINTR {
				return false
			}
			if errno != 0 {
				opErr = errno
			} else {
				received = int(r)
			}
			return true
		})
		if err != nil {
			return 0, err
		}
		if opErr != nil {
			return 0, opErr
		}
		count := 0
		for i := 0; i < received; i++ {
			length := int(p.rxHdr[i].Len)
			if length > len(p.rxFrames[i]) || p.rxHdr[i].Hdr.Flags&unix.MSG_TRUNC != 0 {
				continue
			}
			payload, addr, ok := decodeFrame(p.rxFrames[i][:length], p.send.srcPort)
			if !ok {
				continue
			}
			if len(payload) > len(ms[count].Buffers[0]) {
				continue
			}
			a := p.cache[addr]
			if a == nil {
				if len(p.cache) >= 4096 {
					clear(p.cache)
				}
				a = &net.UDPAddr{IP: net.IP(addr.Addr().AsSlice()), Port: int(addr.Port())}
				p.cache[addr] = a
			}
			ms[count].N = copy(ms[count].Buffers[0], payload)
			ms[count].Addr = a
			count++
		}
		if count > 0 {
			return count, nil
		}
	}
}

func (p *rawPacket) ReadFrom(b []byte) (int, net.Addr, error) {
	ms := []ipv4.Message{{Buffers: [][]byte{b}}}
	_, err := p.ReadBatch(ms, 0)
	return ms[0].N, ms[0].Addr, err
}
func (p *rawPacket) WriteTo(b []byte, a net.Addr) (int, error) {
	_, err := p.WriteBatch([]ipv4.Message{{Buffers: [][]byte{b}, Addr: a}}, 0)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}
func (p *rawPacket) Close() error                       { return p.file.Close() }
func (p *rawPacket) LocalAddr() net.Addr                { return p.local }
func (p *rawPacket) SetDeadline(t time.Time) error      { return p.file.SetDeadline(t) }
func (p *rawPacket) SetReadDeadline(t time.Time) error  { return p.file.SetReadDeadline(t) }
func (p *rawPacket) SetWriteDeadline(t time.Time) error { return p.file.SetWriteDeadline(t) }

var _ net.PacketConn = (*rawPacket)(nil)

func (p *rawPacket) packetStats() (uint64, uint64) {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	_ = p.raw.Control(func(fd uintptr) {
		if s, err := unix.GetsockoptTpacketStats(int(fd), unix.SOL_PACKET, unix.PACKET_STATISTICS); err == nil {
			p.packets.Add(uint64(s.Packets))
			p.drops.Add(uint64(s.Drops))
		}
	})
	return p.packets.Load(), p.drops.Load()
}
