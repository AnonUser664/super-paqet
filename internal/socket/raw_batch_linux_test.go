//go:build linux

// File raw_batch_linux_test.go: exercises actual batch syscalls on owned Unix
// sockets without raw-socket privileges, and measures batch allocation overhead.
package socket

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"paqet/internal/conf"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

// rawBatchPair gives sendmmsg a real connected datagram descriptor. The emitted
// Ethernet/TCP bytes are carried as Unix payloads, preserving encoder coverage
// while keeping the fixture independent of host interfaces/firewall rules.
func rawBatchPair(t testing.TB) (*rawPacket, *net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	left := os.NewFile(uintptr(fds[0]), "raw-batch-sender")
	right := os.NewFile(uintptr(fds[1]), "raw-batch-receiver")
	defer left.Close()
	defer right.Close()
	a, err := net.FileConn(left)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	b, err := net.FileConn(right)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	sender, receiver := a.(*net.UnixConn), b.(*net.UnixConn)
	raw, err := sender.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	return &rawPacket{raw: raw, send: testSend(conf.TCPF{PSH: true, ACK: true}), mac: net.HardwareAddr{2, 0, 0, 0, 0, 2}}, sender, receiver
}

// TestRawBatchPayloadAndPermanentFailure checks real scatter/gather transmission,
// payload order, reusable-pointer cleanup and terminal errors distinct from loss.
func TestRawBatchPayloadAndPermanentFailure(t *testing.T) {
	p, _, receiver := rawBatchPair(t)
	addr := &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 49999}
	ms := []ipv4.Message{{Buffers: [][]byte{[]byte("first")}, Addr: addr}, {Buffers: [][]byte{[]byte("second")}, Addr: addr}}
	n, dropped, err := p.writeBatchWithDrops(ms, 0)
	if n != 2 || dropped != 0 || err != nil {
		t.Fatalf("healthy outcome: %d/%d/%v", n, dropped, err)
	}
	receiver.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame := make([]byte, 2048)
	for _, message := range ms {
		n, err := receiver.Read(frame)
		if err != nil {
			t.Fatal(err)
		}
		payload, _, ok := decodeFrame(frame[:n], uint16(addr.Port))
		if !ok || !bytes.Equal(payload, message.Buffers[0]) {
			t.Fatal("batch changed payload order or frame contents")
		}
	}
	receiver.Close()
	n, dropped, err = p.writeBatchWithDrops(ms, 0)
	if n != 0 || dropped != 0 || err == nil || p.txDrops.Load() != 0 {
		t.Fatalf("permanent error was consumed as soft loss: %d/%d/%v", n, dropped, err)
	}
	for i := range ms {
		if p.txVec[i][1].Base != nil {
			t.Fatal("batch retained a caller buffer after failure")
		}
	}
}

// BenchmarkRawBatchHealthy compares identical real syscall/encoding work with
// and without the captured loss scalar. Receiver work is allocation-free after
// startup; measurements are per batch, not per packet or tunnel goodput.
func BenchmarkRawBatchHealthy(b *testing.B) {
	for _, count := range []int{1, 32} {
		b.Run(fmt.Sprintf("packets=%d", count), func(b *testing.B) {
			p, sender, receiver := rawBatchPair(b)
			done := make(chan struct{})
			go func() {
				defer close(done)
				buffer := make([]byte, 2048)
				for {
					if _, err := receiver.Read(buffer); err != nil {
						return
					}
				}
			}()
			defer func() { b.StopTimer(); sender.Close(); receiver.Close(); <-done }()
			payload := make([]byte, 1316)
			addr := &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 49999}
			ms := make([]ipv4.Message, count)
			for i := range ms {
				ms[i] = ipv4.Message{Buffers: [][]byte{payload}, Addr: addr}
			}
			b.ReportAllocs()
			b.SetBytes(int64(count * len(payload)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				remaining := ms
				for len(remaining) != 0 {
					n, dropped, err := p.writeBatchWithDrops(remaining, 0)
					if err != nil || dropped != 0 || n == 0 {
						b.Fatalf("healthy batch: %d/%d/%v", n, dropped, err)
					}
					remaining = remaining[n:]
				}
			}
		})
	}
}
