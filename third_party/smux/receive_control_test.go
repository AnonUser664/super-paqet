// File receive_control_test.go exercises receive-budget exhaustion independently
// of loss/timing: unrelated blocked readers must not stop reverse flow credits.
package smux

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// TestFullReceiveBudgetStillProcessesReverseCredits reproduces duplex credit
// starvation while two undrained streams consume the whole receive allowance.
func TestFullReceiveBudgetStillProcessesReverseCredits(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Version = 2
	cfg.MaxFrameSize = 1024
	cfg.MaxReceiveBuffer = 4096
	cfg.MaxStreamBuffer = 2048
	cfg.AsyncWindowUpdates = true
	cfg.HalfClose = true
	cfg.PrioritizeControl = true
	left, right := net.Pipe()
	client, err := Client(left, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := Server(right, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	// Do not drain these buffers: they represent slow customer TCP destinations.
	for i := 0; i < 2; i++ {
		stream, err := client.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = server.AcceptStream(); err != nil {
			t.Fatal(err)
		}
		stream.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err = stream.Write(make([]byte, 2048)); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&server.bucket) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if atomic.LoadInt32(&server.bucket) > 0 {
		t.Fatal("test did not exhaust the receive budget")
	}
	outgoing, err := server.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := client.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	// More than one initial per-stream window requires reverse credit feedback.
	outgoing.SetWriteDeadline(time.Now().Add(time.Second))
	incoming.SetReadDeadline(time.Now().Add(time.Second))
	done := make(chan error, 1)
	go func() { _, err := outgoing.Write(make([]byte, initialPeerWindow+4096)); done <- err }()
	data := make([]byte, initialPeerWindow+4096)
	if _, err := io.ReadFull(incoming, data); err != nil {
		t.Fatalf("unrelated full receive buffers stopped reverse delivery: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&server.bucket) != 0 {
		t.Fatal("control feedback changed the data receive budget")
	}
}

// TestReceiveControlChangeDoesNotBypassPayloadBudget ensures permitting control
// headers never permits unrelated application data to allocate past the budget.
func TestReceiveControlChangeDoesNotBypassPayloadBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Version = 2
	cfg.MaxFrameSize = 1024
	cfg.MaxReceiveBuffer = 4096
	cfg.MaxStreamBuffer = 2048
	cfg.AsyncWindowUpdates = true
	cfg.HalfClose = true
	left, right := net.Pipe()
	client, err := Client(left, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := Server(right, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var held *Stream
	for i := 0; i < 2; i++ {
		local, err := client.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		remote, err := server.AcceptStream()
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			held = remote
		}
		local.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err = local.Write(make([]byte, 2048)); err != nil {
			t.Fatal(err)
		}
	}
	until := time.Now().Add(time.Second)
	for atomic.LoadInt32(&server.bucket) > 0 && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	extra, err := client.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	received, err := server.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	extra.SetWriteDeadline(time.Now().Add(time.Second))
	received.SetReadDeadline(time.Now().Add(time.Second))
	held.SetReadDeadline(time.Now().Add(time.Second))
	done := make(chan error, 1)
	go func() { _, err := extra.Write(make([]byte, 1024)); done <- err }()
	until = time.Now().Add(time.Second)
	for !server.receiveBlocked.Load() && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	capacity, buffered, blocked := server.ReceiveBufferStats()
	if !blocked || capacity != 4096 || buffered != 4096 {
		t.Fatalf("data bypassed receive bound: cap=%d buffered=%d blocked=%v", capacity, buffered, blocked)
	}
	if _, err = io.ReadFull(held, make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadFull(received, make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
