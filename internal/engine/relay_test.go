//go:build linux

// File relay_test.go: exercises relay regressions; fixtures must preserve cleanup and expose
// byte/lifecycle failures explicitly.

package engine

import (
	"bytes"
	"context"
	"github.com/xtaci/smux"
	"io"
	"net"
	"paqet/internal/tnet/kcp"
	"testing"
	"time"
)

// TestTCPHalfClose checks TCP Half Close so a change cannot silently weaken the recorded
// regression contract.
func TestTCPHalfClose(t *testing.T) {
	left, right := net.Pipe()
	cfg := smux.DefaultConfig()
	cfg.Version = 2
	cfg.HalfClose = true
	cfg.MaxStreamBuffer = 4096
	client, err := smux.Client(left, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := smux.Server(right, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	a, err := client.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := server.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	a.SetDeadline(time.Now().Add(5 * time.Second))
	b.SetDeadline(time.Now().Add(5 * time.Second))
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	local, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	tunnel, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	local.SetDeadline(time.Now().Add(5 * time.Second))
	e := &Engine{}
	done := make(chan struct{})
	go func() { e.relay(tunnel, &kcp.Strm{Stream: a}); close(done) }()
	response := bytes.Repeat([]byte("response"), 32768)
	errCh := make(chan error, 1)
	go func() {
		request, err := io.ReadAll(b)
		if err == nil && !bytes.Equal(request, []byte("request")) {
			err = io.ErrUnexpectedEOF
		}
		if err == nil {
			_, err = b.Write(response)
		}
		if err == nil {
			err = b.CloseWrite()
		}
		errCh <- err
	}()
	if _, err := local.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := local.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(local)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("response truncated: %d/%d", len(got), len(response))
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("relay failed to close")
	}
}

// TestDatagramBoundaries checks Datagram Boundaries so a change cannot silently weaken the
// recorded regression contract.
func TestDatagramBoundaries(t *testing.T) {
	packets := [][]byte{nil, []byte("hello"), bytes.Repeat([]byte{0x89}, 65507), []byte("last")}
	var wire bytes.Buffer
	for _, p := range packets {
		if err := writeDatagram(&wire, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range packets {
		p, err := readDatagram(&wire)
		if err != nil {
			t.Fatal(err)
		}
		got := append([]byte(nil), (*p.b)[:p.n]...)
		copyPools[p.class].Put(p.b)
		if !bytes.Equal(got, want) {
			t.Fatalf("datagram length %d != %d", len(got), len(want))
		}
	}
	if _, err := readDatagram(&wire); err != io.EOF {
		t.Fatal(err)
	}
	if err := writeDatagram(io.Discard, make([]byte, 65508)); err == nil {
		t.Fatal("accepted oversized UDP")
	}
}

// TestRejectTruncatedDatagram checks Reject Truncated Datagram so a change cannot silently
// weaken the recorded regression contract.
func TestRejectTruncatedDatagram(t *testing.T) {
	for _, b := range [][]byte{{0}, {0, 4, 1, 2}, {255, 255}} {
		if _, err := readDatagram(bytes.NewReader(b)); err == nil {
			t.Fatalf("accepted malformed %x", b)
		}
	}
}

// TestHalfClosedRelayEndsOnGenerationCancellation leaves the application's
// write direction deliberately idle after remote EOF. Removing its generation
// must release the relay without imposing a new normal half-close deadline.
func TestHalfClosedRelayEndsOnGenerationCancellation(t *testing.T) {
	for _, cause := range []string{"context", "carrier"} {
		t.Run(cause, func(t *testing.T) {
			left, right := net.Pipe()
			cfg := smux.DefaultConfig()
			cfg.Version = 2
			cfg.HalfClose = true
			client, err := smux.Client(left, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			server, err := smux.Server(right, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			a, err := client.OpenStream()
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := server.AcceptStream()
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			local, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
			if err != nil {
				t.Fatal(err)
			}
			defer local.Close()
			tcp, err := listener.AcceptTCP()
			if err != nil {
				t.Fatal(err)
			}
			defer tcp.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e := &Engine{}
			done := make(chan struct{})
			go func() { e.relayContext(ctx, tcp, &kcp.Strm{Stream: a}); close(done) }()
			if err = b.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			local.SetReadDeadline(time.Now().Add(time.Second))
			var buf [1]byte
			if _, err = local.Read(buf[:]); err != io.EOF {
				t.Fatalf("directional EOF missing: %v", err)
			}
			select {
			case <-done:
				t.Fatal("ordinary directional EOF prematurely closed opposite leg")
			case <-time.After(25 * time.Millisecond):
			}
			if cause == "context" {
				cancel()
			} else {
				client.Close()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("generation cancellation retained idle half-closed relay")
			}
		})
	}

}

// BenchmarkTCPToStream measures throughput and allocations of the relay tcpToStream loop.
func BenchmarkTCPToStream(b *testing.B) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()

	payload := make([]byte, 32768)
	totalBytes := int64(b.N) * int64(len(payload))
	b.SetBytes(int64(len(payload)))

	client, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	server, err := listener.AcceptTCP()
	if err != nil {
		b.Fatal(err)
	}
	defer server.Close()

	b.ResetTimer()
	b.ReportAllocs()

	errCh := make(chan error, 1)
	go func() {
		for i := 0; i < b.N; i++ {
			if _, err := client.Write(payload); err != nil {
				errCh <- err
				return
			}
		}
		_ = client.CloseWrite()
		errCh <- nil
	}()

	n, err := tcpToStream(io.Discard, server)
	if err != nil {
		b.Fatal(err)
	}
	if writeErr := <-errCh; writeErr != nil {
		b.Fatal(writeErr)
	}
	if n != totalBytes {
		b.Fatalf("transferred %d bytes != expected %d", n, totalBytes)
	}
}

// TestTCPToStreamVaryingBursts exercises scratch growth/shrink while preserving
// exact bytes and EOF. TCP may coalesce writes; the relay must not depend on the
// sender's chunk boundaries or a queued-byte ioctl to size its next read.
func TestTCPToStreamVaryingBursts(t *testing.T) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	local, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	source, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := local.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := source.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	for repeat := 0; repeat < 32; repeat++ {
		for _, size := range []int{1, 4095, 4096, 8192, 16384, 65536, 5, 131072, 65} {
			for offset := 0; offset < size; offset++ {
				want.WriteByte(byte(repeat*31 + offset))
			}
		}
	}
	done := make(chan error, 1)
	go func() {
		payload := want.Bytes()
		for len(payload) > 0 {
			// An intentionally awkward write length crosses every scratch class.
			n, writeErr := local.Write(payload[:min(len(payload), 17003)])
			payload = payload[n:]
			if writeErr != nil {
				done <- writeErr
				return
			}
		}
		done <- local.CloseWrite()
	}()
	var got bytes.Buffer
	n, err := tcpToStream(&got, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n != int64(want.Len()) || !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatalf("adaptive read lost or changed bytes: accepted=%d got=%d want=%d", n, got.Len(), want.Len())
	}
}
