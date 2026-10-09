//go:build linux

// File raw_linux.go: implements nonblocking Linux AF_PACKET batches, fixed scratch storage and
// packet-socket loss accounting.

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

// rawBatchSize bounds reusable frame/message arrays and each Linux multiple-message syscall
// batch.
const rawBatchSize = 64

// mmsg matches the Linux multiple-message syscall descriptor layout; its ABI must not be
// changed casually.
type mmsg struct {
	// Kernel msghdr layout used by sendmmsg/recvmmsg.
	Hdr unix.Msghdr
	// Kernel-reported accepted message length in the Linux batch ABI.
	Len uint32
}

// rawPacket owns a Linux packet socket and reusable batch storage; receive/send/stat locks
// cover separate mutable regions.
type rawPacket struct {
	// Cumulative capture attempts/drops accumulated across resetting kernel statistics reads.
	packets, drops atomic.Uint64
	// Atomic local transmit-queue loss count, separate from network retransmission statistics.
	txDrops atomic.Uint64
	// Counts bounded retries of wholly rejected transmit batches before KCP loss.
	txRetries atomic.Uint64
	// Serializes destructive kernel packet-stat reads while retaining cumulative counters.
	statsMu sync.Mutex
	// Owned packet socket file whose close wakes netpoll operations.
	file *os.File
	// Netpoll-aware packet file access; this is not an outer kernel TCP connection.
	raw syscall.RawConn
	// Encoder/flag state, shared across fixed fanout workers where required by the baseline.
	send *SendHandle
	// Physical interface source Ethernet address.
	mac net.HardwareAddr
	// Stable local packet endpoint metadata returned to KCP/diagnostics.
	local *net.UDPAddr
	// Protect reusable receive/send batch arrays independently so full duplex can progress.
	rxMu, txMu sync.Mutex
	// Fixed capture slots; decode checks prevent truncated/oversized frames from being accepted.
	rxFrames [rawBatchSize][2048]byte
	// Reusable receive syscall descriptors pointing into rxFrames.
	rxHdr [rawBatchSize]mmsg
	// Reusable receive iovecs, avoiding descriptor allocation per packet.
	rxVec [rawBatchSize]unix.Iovec
	// Reusable encoded Ethernet/IP/TCP headers separate from already-owned KCP payload slices.
	txHeaders [rawBatchSize][96]byte
	// Reusable transmit syscall descriptors for scatter/gather output.
	txHdr [rawBatchSize]mmsg
	// Header/payload iovecs consumed before the corresponding buffers can be reused.
	txVec [rawBatchSize][2]unix.Iovec
	// Reusable header representation for the serialized batch encoder.
	encoder encoder
	// Canonical remote address objects reused by the raw receive path.
	cache map[netip.AddrPort]*net.UDPAddr
}

// newRawPacket binds a nonblocking Ethernet packet socket, installs BPF/fanout and prepares
// reusable batch buffers.
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

// WriteBatch injects bounded scatter/gather frames and treats ENOBUFS as datagram loss for KCP
// recovery.
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
		r, errno, retries, attempted := sendBatchWithQueueRetry(n, func(prefix int) (int, unix.Errno) {
			r, _, errno := unix.Syscall6(unix.SYS_SENDMMSG, fd, uintptr(unsafe.Pointer(&p.txHdr[0])), uintptr(prefix), uintptr(unix.MSG_DONTWAIT), 0, 0)
			return int(r), errno
		}, waitTXQueue)
		if retries > 0 {
			p.txRetries.Add(uint64(retries))
		}
		if errno == unix.EAGAIN || errno == unix.EINTR {
			return false
		}
		// AF_PACKET reports full qdisc queues as ENOBUFS. This is datagram
		// loss, not a broken socket: KCP must retransmit rather than abort
		// every application stream sharing the session.
		if errno == unix.ENOBUFS {
			// Only this rejected prefix is lost. The KCP batch caller keeps the
			// remaining encoded payloads and advances by this exact count.
			p.txDrops.Add(uint64(attempted))
			sent = attempted
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

// ReadBatch receives into reusable frame slots, validates envelopes and returns only
// decapsulated KCP payloads.
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

// ReadFrom returns one decapsulated packet payload/address while retaining the active driver's
// deadline and buffer rules.
func (p *rawPacket) ReadFrom(b []byte) (int, net.Addr, error) {
	ms := []ipv4.Message{{Buffers: [][]byte{b}}}
	_, err := p.ReadBatch(ms, 0)
	return ms[0].N, ms[0].Addr, err
}

// WriteTo submits one packet to the active driver; reliable retry remains the surrounding KCP
// session's responsibility.
func (p *rawPacket) WriteTo(b []byte, a net.Addr) (int, error) {
	_, err := p.WriteBatch([]ipv4.Message{{Buffers: [][]byte{b}, Addr: a}}, 0)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close releases this object's owned resources or signals its lifecycle once; shared listener
// ownership is handled by its wrapper.
func (p *rawPacket) Close() error { return p.file.Close() }

// LocalAddr reports this object's local endpoint without transferring packet socket ownership.
func (p *rawPacket) LocalAddr() net.Addr { return p.local }

// SetDeadline updates both read and write deadlines and delegates cancellation to the
// underlying connection.
func (p *rawPacket) SetDeadline(t time.Time) error { return p.file.SetDeadline(t) }

// SetReadDeadline sets input expiry and wakes blocked I/O through the underlying connection
// contract.
func (p *rawPacket) SetReadDeadline(t time.Time) error { return p.file.SetReadDeadline(t) }

// SetWriteDeadline sets output expiry so backpressure cannot ignore caller cancellation
// indefinitely.
func (p *rawPacket) SetWriteDeadline(t time.Time) error { return p.file.SetWriteDeadline(t) }

// _ asserts implementation of the expected interface at compile time, catching adapter drift
// before runtime.
var _ net.PacketConn = (*rawPacket)(nil)

// packetStats accumulates kernel counters under a lock because reading packet statistics
// resets their kernel interval.
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
