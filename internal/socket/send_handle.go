// File send_handle.go: builds fabricated outer headers and serializes pcap injection; KCP
// alone provides reliable byte delivery.

package socket

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"

	"paqet/internal/conf"
	"paqet/internal/pkg/iterator"
)

// tcpF keeps default/per-peer flag iterators and conversation ownership under one registration
// lock.
type tcpF struct {
	// Default outer flag cycle; peer-specific overrides and ownership are tracked separately.
	tcpF iterator.Iterator[conf.TCPF]
	// Per-remote flag cycle overrides, guarded with conversation ownership.
	clientTCPF map[netip.AddrPort]*iterator.Iterator[conf.TCPF]
	// Current conversation generation per remote key, preventing stale setup/cleanup from
	// changing replacements.
	owners map[netip.AddrPort]uint32
	// Shared-source tuples retain every live lane, so one closure cannot clear
	// flags used by its siblings. Empty sets are deleted on final lane closure.
	groups map[netip.AddrPort]map[uint32]struct{}
	// Protects peer flag maps and owner generations; iterators maintain their own atomic
	// positions.
	mu sync.RWMutex
}

// encoder reuses Ethernet/IP/TCP serialization storage; encoded bytes must be consumed before
// the object returns to its pool.
type encoder struct {
	// Ethernet header metadata; source/destination MAC ownership belongs to the selected
	// physical path.
	eth layers.Ethernet
	// IPv4 header storage used by the retained raw envelope.
	ip4 layers.IPv4
	// IPv6 header storage used by the retained raw envelope.
	ip6 layers.IPv6
	// TCP header representation; its fabricated fields do not provide application reliability.
	tcp layers.TCP

	// Reusable TCP option slots retaining the original SYN/ordinary header layout.
	opts [5]layers.TCPOption
	// TCP timestamp option bytes encoded in network order.
	ts [8]byte
	// Advertised SYN MSS bytes; this is outer header shape rather than KCP's usable payload
	// size.
	mss [2]byte
	// Advertised SYN window-scale option bytes retained for wire compatibility.
	ws [1]byte

	// Reusable serialized frame storage; it cannot outlive a returned encoder pool object.
	buf gopacket.SerializeBuffer
}

// SendHandle owns injection state, shared fabricated counters and generation-checked peer flag
// cycles.
type SendHandle struct {
	// Owned injection handle, serialized with writers/close; raw encoders have no pcap handle.
	handle packetInjector
	// Atomic local transmit-queue loss count, separate from network retransmission statistics.
	txDrops atomic.Uint64
	// Serializes injection and closure because pcap writes are not guaranteed concurrently safe.
	writeMu sync.Mutex
	// Configured local IPv4 source; raw injection does not ask kernel IP routing to choose it.
	srcIPv4 net.IP
	// Configured IPv4 next-hop MAC, which can differ from IPv6 routing.
	srcIPv4RHWA net.HardwareAddr
	// Configured local IPv6 source for raw header construction.
	srcIPv6 net.IP
	// Configured IPv6 next-hop hardware address.
	srcIPv6RHWA net.HardwareAddr
	// Reserved local tunnel source port shared by configured address families.
	srcPort uint16
	// Retained initial fabricated timestamp/number seed; it is not a synchronized peer clock.
	time uint32
	// Atomic original outer counter shared where packet-worker semantics require it.
	tsCounter atomic.Uint32
	// Default/per-peer flag cycles and generation ownership shared by packet writers.
	tcpF tcpF
	// Reuses complete serializer state; each borrowed encoder is returned after injection.
	ePool sync.Pool
}

// NewSendHandle prepares outgoing injection, reusable encoders and generation-owned peer flag
// state.
func NewSendHandle(cfg *conf.Network) (*SendHandle, error) {
	handle, err := newHandle(cfg, 256*1024, 128, pcap.BlockForever)
	if err != nil {
		return nil, fmt.Errorf("failed to open pcap handle: %w", err)
	}
	ready := false
	defer func() {
		if !ready {
			handle.Close()
		}
	}()

	// SetDirection is not fully supported on Windows Npcap, so skip it
	if runtime.GOOS != "windows" {
		if err := handle.SetDirection(pcap.DirectionOut); err != nil {
			return nil, fmt.Errorf("failed to set pcap direction out: %v", err)
		}
	}

	if err := handle.SetBPFFilter("less 0"); err != nil {
		return nil, fmt.Errorf("failed to set BPF filter: %w", err)
	}

	sh := &SendHandle{
		handle:  handle,
		srcPort: uint16(cfg.Port),
		tcpF:    tcpF{tcpF: iterator.Iterator[conf.TCPF]{Items: cfg.TCP.LF}, clientTCPF: make(map[netip.AddrPort]*iterator.Iterator[conf.TCPF])},
		time:    uint32(time.Now().UnixNano() / int64(time.Millisecond)),
		ePool: sync.Pool{
			New: func() any {
				return &encoder{
					eth: layers.Ethernet{SrcMAC: cfg.Interface.HardwareAddr},
					mss: [2]byte{0x05, 0xb4},
					ws:  [1]byte{8},
					buf: gopacket.NewSerializeBuffer(),
				}
			},
		},
	}
	if cfg.IPv4.Addr != nil {
		sh.srcIPv4 = cfg.IPv4.Addr.IP
		sh.srcIPv4RHWA = cfg.IPv4.Router
	}
	if cfg.IPv6.Addr != nil {
		sh.srcIPv6 = cfg.IPv6.Addr.IP
		sh.srcIPv6RHWA = cfg.IPv6.Router
	}
	ready = true
	return sh, nil
}

// buildIPv4Header fills the retained IPv4 TCP/TTL/TOS/DF envelope; this is raw framing rather
// than kernel routing.
func (h *SendHandle) buildIPv4Header(e *encoder, dstIP net.IP) {
	e.ip4 = layers.IPv4{
		Version:  4,
		IHL:      5,
		TOS:      184,
		TTL:      64,
		Flags:    layers.IPv4DontFragment,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    h.srcIPv4,
		DstIP:    dstIP,
	}
}

// buildIPv6Header fills the corresponding IPv6 TCP/hop-limit/traffic-class envelope.
func (h *SendHandle) buildIPv6Header(e *encoder, dstIP net.IP) {
	e.ip6 = layers.IPv6{
		Version:      6,
		TrafficClass: 184,
		HopLimit:     64,
		NextHeader:   layers.IPProtocolTCP,
		SrcIP:        h.srcIPv6,
		DstIP:        dstIP,
	}
}

// buildTCPHeader fills fabricated flags, options and number fields; KCP reliability must not
// depend on outer TCP sequence semantics.
func (h *SendHandle) buildTCPHeader(e *encoder, dstPort uint16, f conf.TCPF) {
	e.tcp = layers.TCP{
		SrcPort: layers.TCPPort(h.srcPort),
		DstPort: layers.TCPPort(dstPort),
		FIN:     f.FIN, SYN: f.SYN, RST: f.RST, PSH: f.PSH, ACK: f.ACK, URG: f.URG, ECE: f.ECE, CWR: f.CWR, NS: f.NS,
		Window: 65535,
	}

	// These retained counters imitate outer TCP fields; their increments are not delivered application byte counts. Changing them needs wire/detectability qualification.
	counter := h.tsCounter.Add(1)
	tsVal := h.time + (counter >> 3)
	opts := e.opts[:0]
	if f.SYN {
		binary.BigEndian.PutUint32(e.ts[0:4], tsVal)
		binary.BigEndian.PutUint32(e.ts[4:8], 0)
		opts = append(opts,
			layers.TCPOption{OptionType: layers.TCPOptionKindMSS, OptionLength: 4, OptionData: e.mss[:]},
			layers.TCPOption{OptionType: layers.TCPOptionKindSACKPermitted, OptionLength: 2},
			layers.TCPOption{OptionType: layers.TCPOptionKindTimestamps, OptionLength: 10, OptionData: e.ts[:]},
			layers.TCPOption{OptionType: layers.TCPOptionKindNop},
			layers.TCPOption{OptionType: layers.TCPOptionKindWindowScale, OptionLength: 3, OptionData: e.ws[:]},
		)
		e.tcp.Seq = 1 + (counter & 0x7)
		e.tcp.Ack = 0
		if f.ACK {
			e.tcp.Ack = e.tcp.Seq + 1
		}
	} else {
		tsEcr := tsVal - (counter%200 + 50)
		binary.BigEndian.PutUint32(e.ts[0:4], tsVal)
		binary.BigEndian.PutUint32(e.ts[4:8], tsEcr)
		opts = append(opts,
			layers.TCPOption{OptionType: layers.TCPOptionKindNop},
			layers.TCPOption{OptionType: layers.TCPOptionKindNop},
			layers.TCPOption{OptionType: layers.TCPOptionKindTimestamps, OptionLength: 10, OptionData: e.ts[:]},
		)
		seq := h.time + (counter << 7)
		e.tcp.Seq = seq
		e.tcp.Ack = seq - (counter & 0x3FF) + 1400
	}
	e.tcp.Options = opts
}

// Write serializes an outer frame and injects it under the writer lock; recognized queue
// pressure is counted as recoverable loss.
func (h *SendHandle) Write(payload []byte, addr *net.UDPAddr) error {
	e := h.ePool.Get().(*encoder)
	defer func() {
		e.buf.Clear()
		h.ePool.Put(e)
	}()

	dstIP := addr.IP
	dstPort := uint16(addr.Port)

	h.buildTCPHeader(e, dstPort, h.getClientTCPF(dstIP, dstPort))

	var ipLayer gopacket.SerializableLayer
	if dstIP.To4() != nil {
		h.buildIPv4Header(e, dstIP)
		ipLayer = &e.ip4
		e.tcp.SetNetworkLayerForChecksum(&e.ip4)
		e.eth.DstMAC = h.srcIPv4RHWA
		e.eth.EthernetType = layers.EthernetTypeIPv4
	} else {
		h.buildIPv6Header(e, dstIP)
		ipLayer = &e.ip6
		e.tcp.SetNetworkLayerForChecksum(&e.ip6)
		e.eth.DstMAC = h.srcIPv6RHWA
		e.eth.EthernetType = layers.EthernetTypeIPv6
	}

	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(e.buf, opts, &e.eth, ipLayer, &e.tcp, gopacket.Payload(payload)); err != nil {
		return err
	}

	// pcap_sendpacket is not guaranteed thread-safe.
	h.writeMu.Lock()
	if h.handle == nil {
		h.writeMu.Unlock()
		return net.ErrClosed
	}
	err := h.handle.WritePacketData(e.buf.Bytes())
	if transientTXDrop(err) {
		h.txDrops.Add(1)
		err = nil
	}
	h.writeMu.Unlock()
	return err
}

// getClientTCPF selects the peer cycle or default cycle while protecting concurrent flag
// registration.
func (h *SendHandle) getClientTCPF(dstIP net.IP, dstPort uint16) conf.TCPF {
	h.tcpF.mu.RLock()
	defer h.tcpF.mu.RUnlock()
	if ff := h.tcpF.clientTCPF[peerAddress(dstIP, dstPort)]; ff != nil {
		return ff.Next()
	}
	return h.tcpF.tcpF.Next()
}

// setClientTCPF updates the compatibility flag cycle for one remote endpoint.
func (h *SendHandle) setClientTCPF(addr net.Addr, f []conf.TCPF) {
	a, ok := addr.(*net.UDPAddr)
	if !ok {
		return
	}
	h.tcpF.mu.Lock()
	h.tcpF.clientTCPF[peerAddress(a.IP, uint16(a.Port))] = &iterator.Iterator[conf.TCPF]{Items: f}
	h.tcpF.mu.Unlock()
}

// registerClient records the active conversation generation so stale cleanup cannot remove a
// newer peer configuration.
func (h *SendHandle) registerClient(addr net.Addr, owner uint32) {
	a, ok := addr.(*net.UDPAddr)
	if !ok {
		return
	}
	key := peerAddress(a.IP, uint16(a.Port))
	h.tcpF.mu.Lock()
	defer h.tcpF.mu.Unlock()
	if h.tcpF.owners == nil {
		h.tcpF.owners = make(map[netip.AddrPort]uint32)
	}
	h.tcpF.owners[key] = owner
	delete(h.tcpF.clientTCPF, key)
}

// setClientTCPFSession applies requested flags only if the calling conversation still owns
// that remote endpoint.
func (h *SendHandle) setClientTCPFSession(addr net.Addr, owner uint32, f []conf.TCPF) {
	a, ok := addr.(*net.UDPAddr)
	if !ok {
		return
	}
	key := peerAddress(a.IP, uint16(a.Port))
	h.tcpF.mu.Lock()
	defer h.tcpF.mu.Unlock()
	_, groupOwner := h.tcpF.groups[key][owner]
	current, registered := h.tcpF.owners[key]
	if !groupOwner && (!registered || current != owner) {
		return
	}
	h.tcpF.clientTCPF[key] = &iterator.Iterator[conf.TCPF]{Items: f}
}

// deleteClientSession removes peer state only for its current owner, preventing teardown races
// across reconnects.
func (h *SendHandle) deleteClientSession(addr net.Addr, owner uint32) {
	a, ok := addr.(*net.UDPAddr)
	if !ok {
		return
	}
	key := peerAddress(a.IP, uint16(a.Port))
	h.tcpF.mu.Lock()
	defer h.tcpF.mu.Unlock()
	if owners := h.tcpF.groups[key]; owners != nil {
		delete(owners, owner)
		if len(owners) != 0 {
			return
		}
		delete(h.tcpF.groups, key)
		delete(h.tcpF.clientTCPF, key)
		return
	}
	if h.tcpF.owners[key] != owner {
		return
	}
	delete(h.tcpF.owners, key)
	delete(h.tcpF.clientTCPF, key)
}

// registerClientGroup retains independent lanes sharing a tuple. Reconnect
// teardown removes its exact owner without resetting sibling flag iterators.
func (h *SendHandle) registerClientGroup(addr net.Addr, owner uint32) {
	a, ok := addr.(*net.UDPAddr)
	if !ok {
		return
	}
	key := peerAddress(a.IP, uint16(a.Port))
	h.tcpF.mu.Lock()
	defer h.tcpF.mu.Unlock()
	if h.tcpF.groups == nil {
		h.tcpF.groups = make(map[netip.AddrPort]map[uint32]struct{})
	}
	if h.tcpF.groups[key] == nil {
		h.tcpF.groups[key] = make(map[uint32]struct{})
	}
	h.tcpF.groups[key][owner] = struct{}{}
}

// deleteClientTCPF removes the compatibility flag override without changing the default cycle.
func (h *SendHandle) deleteClientTCPF(addr net.Addr) {
	a, ok := addr.(*net.UDPAddr)
	if !ok {
		return
	}
	h.tcpF.mu.Lock()
	delete(h.tcpF.clientTCPF, peerAddress(a.IP, uint16(a.Port)))
	h.tcpF.mu.Unlock()
}

// Close serializes injection-handle closure with writers; raw callers may have no pcap handle
// to release.
func (h *SendHandle) Close() {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.handle != nil {
		h.handle.Close()
		h.handle = nil
	}
}

// peerAddress canonicalizes IPv4-mapped and native endpoint keys so registration and lookup
// refer to the same peer.
func peerAddress(ip net.IP, port uint16) netip.AddrPort {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(addr.Unmap(), port)
}
