//go:build linux

// File pcap_tx_test.go: exercises pcap tx regressions; fixtures must preserve cleanup and
// expose byte/lifecycle failures explicitly.

package socket

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"golang.org/x/net/ipv4"
	"paqet/internal/conf"
)

// faultInjector retains the fault Injector fixture state used to expose failures without
// production network side effects.
type faultInjector struct {
	faults  []error
	packets [][]byte
	closed  bool
}

// WritePacketData injects scripted queue/permanent failures before retaining successful bytes,
// isolating error handling from kernel scheduling.
func (f *faultInjector) WritePacketData(data []byte) error {
	if len(f.faults) > 0 {
		err := f.faults[0]
		f.faults = f.faults[1:]
		if err != nil {
			return err
		}
	}
	f.packets = append(f.packets, append([]byte(nil), data...))
	return nil
}

// Close records resource closure so the fault test can reject leaked injector ownership.
func (f *faultInjector) Close() { f.closed = true }

// TestPCAPQueuePressureDoesNotBreakPacketConn checks PCAP Queue Pressure Does Not Break Packet
// Conn so a change cannot silently weaken the recorded regression contract.
func TestPCAPQueuePressureDoesNotBreakPacketConn(t *testing.T) {
	for _, drop := range []error{syscall.ENOBUFS, fmt.Errorf("injection: %w", syscall.ENOBUFS), errors.New("send: No buffer space available")} {
		t.Run(drop.Error(), func(t *testing.T) {
			h := testSend(conf.TCPF{PSH: true, ACK: true})
			injector := &faultInjector{faults: []error{drop, nil, syscall.EIO}}
			h.handle = injector
			h.ePool.New = func() any {
				return &encoder{eth: layers.Ethernet{SrcMAC: net.HardwareAddr{2, 0, 0, 0, 0, 2}}, buf: gopacket.NewSerializeBuffer()}
			}
			conn := &PacketConn{sendHandle: h}
			addr := &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 32001}
			lost, delivered := []byte("KCP will retransmit this datagram"), []byte("later datagram still transmitted")
			messages := []ipv4.Message{{Buffers: [][]byte{lost}, Addr: addr}, {Buffers: [][]byte{delivered}, Addr: addr}}
			n, err := conn.WriteBatch(messages, 0)
			if err != nil || n != 2 || conn.TXDrops() != 1 || len(injector.packets) != 1 {
				t.Fatalf("queue pressure broke batch: n=%d err=%v drops=%d packets=%d", n, err, conn.TXDrops(), len(injector.packets))
			}
			p := gopacket.NewPacket(injector.packets[0], layers.LayerTypeEthernet, gopacket.Default)
			tcp, ok := p.Layer(layers.LayerTypeTCP).(*layers.TCP)
			if !ok || !bytes.Equal(tcp.Payload, delivered) {
				t.Fatal("successful datagram was corrupted")
			}
			if n, err := conn.WriteTo(delivered, addr); n != 0 || !errors.Is(err, syscall.EIO) {
				t.Fatalf("permanent failure hidden: n=%d err=%v", n, err)
			}
			if conn.TXDrops() != 1 {
				t.Fatal("permanent failure incorrectly counted as a drop")
			}
			if err := conn.Close(); err != nil || !injector.closed {
				t.Fatal("injector not closed")
			}
			if _, err := conn.WriteTo(delivered, addr); !errors.Is(err, net.ErrClosed) {
				t.Fatal("closed transport silently accepted a datagram")
			}
		})
	}
}

// TestOnlyRecognizedPCAPQueueErrorsAreRecoverable checks Only Recognized PCAP Queue Errors Are
// Recoverable so a change cannot silently weaken the recorded regression contract.
func TestOnlyRecognizedPCAPQueueErrorsAreRecoverable(t *testing.T) {
	for _, err := range []error{nil, syscall.EIO, syscall.ENETDOWN, net.ErrClosed, errors.New("packet injection is not supported"), errors.New("send: Permission denied"), errors.New("capture failed: No buffer space available")} {
		if transientTXDrop(err) {
			t.Fatalf("unexpected recoverable error: %v", err)
		}
	}
}
