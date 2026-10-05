// File pcap_tx.go: recognizes only known Linux queue-full injection failures as recoverable
// loss; other errors remain fatal.

package socket

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
)

// packetInjector is the write side of a pcap handle. Receive handles remain
// concrete; this small interface also permits deterministic injection faults.
type packetInjector interface {
	// Injects one complete Ethernet frame; recognized queue-full loss is handled by the
	// surrounding adapter.
	WritePacketData([]byte) error
	// Releases owned resources; accepted carrier wrappers never own the listener packet socket.
	Close()
}

// transientTXDrop recognizes typed or known libpcap-text Linux ENOBUFS only, allowing KCP
// recovery without hiding permanent injection failures.
func transientTXDrop(err error) bool {
	if err == nil || runtime.GOOS != "linux" {
		return false
	}
	// gopacket/libpcap discard errno and expose pcap_geterr() as plain text.
	// Linux libpcap's packet socket reports this exact send error for ENOBUFS.
	// Other errors remain fatal rather than silently losing broken transports.
	return errors.Is(err, syscall.ENOBUFS) || strings.EqualFold(err.Error(), "send: "+syscall.ENOBUFS.Error())
}
