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
	WritePacketData([]byte) error
	Close()
}

func transientTXDrop(err error) bool {
	if err == nil || runtime.GOOS != "linux" {
		return false
	}
	// gopacket/libpcap discard errno and expose pcap_geterr() as plain text.
	// Linux libpcap's packet socket reports this exact send error for ENOBUFS.
	// Other errors remain fatal rather than silently losing broken transports.
	return errors.Is(err, syscall.ENOBUFS) || strings.EqualFold(err.Error(), "send: "+syscall.ENOBUFS.Error())
}
