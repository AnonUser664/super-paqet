package smux

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func resetPair(t *testing.T) (*Session, *Session, *Stream, *Stream) {
	t.Helper()
	a, b := net.Pipe()
	cfg := DefaultConfig()
	cfg.Version = 2
	cfg.HalfClose = true
	cfg.KeepAliveDisabled = true
	cfg.MaxStreamBuffer = 4096
	client, err := Client(a, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server, err := Server(b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	c, err := client.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	s.SetDeadline(time.Now().Add(5 * time.Second))
	return client, server, c, s
}

func TestFullCloseUnblocksFlowControlledWriter(t *testing.T) {
	client, _, c, s := resetPair(t)
	done := make(chan error, 1)
	go func() { _, err := c.Write(bytes.Repeat([]byte{1}, 1<<20)); done <- err }()
	if _, err := s.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("writer was not blocked: %v", err)
	default:
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("reset did not abort writer: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("full close left a writer blocked")
	}
	c.Close()
	if client.NumStreams() != 0 {
		t.Fatal("reset stream remained in session map after close")
	}
}

func TestFullCloseRetainsAlreadyWrittenResponse(t *testing.T) {
	_, _, c, s := resetPair(t)
	response := bytes.Repeat([]byte("response"), 16384)
	done := make(chan error, 1)
	go func() {
		_, err := s.Write(response)
		if err == nil {
			err = s.Close()
		}
		done <- err
	}()
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("full close truncated response: %d/%d", len(got), len(response))
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	c.Close()
}
