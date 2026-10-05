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
