// File recv_handle.go: captures and decodes incoming pcap frames while protecting zero-copy
// buffer lifetime with a read lock.

package socket

import (
	"fmt"
	"net"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"

	"paqet/internal/conf"
)

// decoder reuses layer parser storage while borrowed pcap packet data remains protected by the
// read lock.
type decoder struct {
	// Reusable layer decoder operating on the protected borrowed pcap frame.
	parser *gopacket.DecodingLayerParser
	// Ethernet header metadata; source/destination MAC ownership belongs to the selected
	// physical path.
	eth layers.Ethernet
	// IPv4 header storage used by the retained raw envelope.
	ip4 layers.IPv4
	// IPv6 header storage used by the retained raw envelope.
	ip6 layers.IPv6
	// TCP header representation; its fabricated fields do not provide application reliability.
	tcp layers.TCP
	// Layer IDs produced by the reusable parser, not an owned copy of packet payload.
	decoded []gopacket.LayerType
}

// RecvHandle owns the incoming pcap handle and its borrowed-buffer lifetime.
type RecvHandle struct {
	// Owned capture handle whose borrowed data lifetime is protected by mu.
	handle *pcap.Handle
	// Reuses decoder metadata while the capture read lock protects underlying borrowed bytes.
	dPool sync.Pool
	// Serializes capture reads and close so borrowed frame memory cannot be reused concurrently.
	mu sync.Mutex
}

// NewRecvHandle activates incoming-only pcap capture and installs the endpoint filter before
// exposing the handle.
func NewRecvHandle(cfg *conf.Network) (*RecvHandle, error) {
	handle, err := newHandle(cfg, cfg.PCAP.Sockbuf, 65536, 100*time.Millisecond)
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
		if err := handle.SetDirection(pcap.DirectionIn); err != nil {
			return nil, fmt.Errorf("failed to set pcap direction in: %v", err)
		}
	}

	filter := captureFilter(cfg)
	if err := handle.SetBPFFilter(filter); err != nil {
		return nil, fmt.Errorf("failed to set BPF filter: %w", err)
	}

	h := &RecvHandle{handle: handle}
	h.dPool.New = func() any {
		d := &decoder{decoded: make([]gopacket.LayerType, 0, 4)}
		d.parser = gopacket.NewDecodingLayerParser(layers.LayerTypeEthernet, &d.eth, &d.ip4, &d.ip6, &d.tcp)
		d.parser.IgnoreUnsupported = true
		return d
	}

	ready = true
	return h, nil
}

// Read holds the capture lock while decoding/copying borrowed packet bytes so the next read
// cannot overwrite them.
func (h *RecvHandle) Read(data []byte) (int, net.Addr, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.handle == nil {
		return 0, nil, net.ErrClosed
	}

	zdata, _, err := h.handle.ZeroCopyReadPacketData()
	if err != nil {
		return 0, nil, err
	}

	d := h.dPool.Get().(*decoder)
	defer h.dPool.Put(d)

	if err := d.parser.DecodeLayers(zdata, &d.decoded); err != nil {
		return 0, nil, errNoPayload
	}

	addr := &net.UDPAddr{}
	var payload []byte
	for _, t := range d.decoded {
		switch t {
		case layers.LayerTypeIPv4:
			addr.IP = slices.Clone(d.ip4.SrcIP)
		case layers.LayerTypeIPv6:
			addr.IP = slices.Clone(d.ip6.SrcIP)
		case layers.LayerTypeTCP:
			addr.Port = int(d.tcp.SrcPort)
			payload = d.tcp.Payload
		}
	}

	if addr.IP == nil || len(payload) == 0 {
		return 0, nil, errNoPayload
	}

	return copy(data, payload), addr, nil
}

// Close serializes handle destruction with reads so shutdown cannot free a borrowed capture
// buffer.
func (h *RecvHandle) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.handle != nil {
		h.handle.Close()
		h.handle = nil
	}
}
