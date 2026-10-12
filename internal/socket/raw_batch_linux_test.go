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
	"sync"
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

// TestRawBatchDeadlineThenReuse fills a real datagram send queue to make
// RawConn.Write wait for readiness. A timed-out batch must not report a previous
// success, retain caller buffers, or corrupt the next successful submission.
func TestRawBatchDeadlineThenReuse(t *testing.T) {
	p, sender, receiver := rawBatchPair(t)
	if err := p.raw.Control(func(fd uintptr) {
		if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUF, 4096); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	addr := &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 49999}
	payload := bytes.Repeat([]byte{0x45}, 1316)
	ms := []ipv4.Message{{Buffers: [][]byte{payload}, Addr: addr}}
	sender.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	accepted := 0
	for attempts := 0; attempts < 4096; attempts++ {
		n, dropped, err := p.writeBatchWithDrops(ms, 0)
		if dropped != 0 {
			t.Fatal("Unix readiness pressure was misclassified as packet loss")
		}
		if err != nil {
			timeout, ok := err.(net.Error)
			if !ok || !timeout.Timeout() || n != 0 {
				t.Fatalf("deadline outcome: %d/%v", n, err)
			}
			break
		}
		accepted += n
		if attempts == 4095 {
			t.Fatal("fixture failed to fill its bounded datagram queue")
		}
	}
	if accepted == 0 || p.txVec[0][1].Base != nil {
		t.Fatal("fixture had no prior success or retained its payload pointer")
	}
	receiver.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame := make([]byte, 2048)
	for i := 0; i < accepted; i++ {
		if _, err := receiver.Read(frame); err != nil {
			t.Fatal(err)
		}
	}
	sender.SetWriteDeadline(time.Time{})
	ms[0].Buffers[0] = []byte("after deadline")
	if n, dropped, err := p.writeBatchWithDrops(ms, 0); n != 1 || dropped != 0 || err != nil {
		t.Fatalf("reuse outcome: %d/%d/%v", n, dropped, err)
	}
	n, err := receiver.Read(frame)
	if err != nil {
		t.Fatal(err)
	}
	got, _, ok := decodeFrame(frame[:n], uint16(addr.Port))
	if !ok || !bytes.Equal(got, ms[0].Buffers[0]) {
		t.Fatal("timed-out batch changed the next submission")
	}
}

// TestRawBatchConcurrentWriters verifies that concurrent carriers sharing one
// physical writer cannot mix callback results, encoded headers or payloads.
func TestRawBatchConcurrentWriters(t *testing.T) {
	p, sender, receiver := rawBatchPair(t)
	sender.SetWriteDeadline(time.Now().Add(3 * time.Second))
	receiver.SetReadDeadline(time.Now().Add(3 * time.Second))
	const writers, batches, size = 4, 8, 4
	addr := &net.UDPAddr{IP: net.IPv4(198, 18, 0, 1), Port: 49999}
	errors := make(chan error, writers)
	var group sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for batch := 0; batch < batches; batch++ {
				ms := make([]ipv4.Message, size)
				for i := range ms {
					ms[i] = ipv4.Message{Buffers: [][]byte{[]byte(fmt.Sprintf("%d/%d/%d", writer, batch, i))}, Addr: addr}
				}
				for len(ms) > 0 {
					n, dropped, err := p.writeBatchWithDrops(ms, 0)
					if err != nil || dropped != 0 || n <= 0 {
						errors <- fmt.Errorf("submission: %d/%d/%v", n, dropped, err)
						return
					}
					ms = ms[n:]
				}
			}
		}()
	}
	seen := make(map[string]bool)
	frame := make([]byte, 2048)
	for i := 0; i < writers*batches*size; i++ {
		n, err := receiver.Read(frame)
		if err != nil {
			t.Fatal(err)
		}
		payload, _, ok := decodeFrame(frame[:n], uint16(addr.Port))
		key := string(payload)
		if !ok || seen[key] {
			t.Fatalf("invalid or duplicated frame: %q", key)
		}
		seen[key] = true
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	for writer := 0; writer < writers; writer++ {
		for batch := 0; batch < batches; batch++ {
			for i := 0; i < size; i++ {
				if !seen[fmt.Sprintf("%d/%d/%d", writer, batch, i)] {
					t.Fatal("shared writer lost an accepted payload")
				}
			}
		}
	}
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
