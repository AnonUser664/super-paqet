package smux

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func TestDirectionalEOFAndPriorityAccounting(t *testing.T) {
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
	defer client.Close()
	server, err := Server(b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for i := 0; i < 20; i++ {
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
		request := bytes.Repeat([]byte("input"), 20000)
		response := bytes.Repeat([]byte("output"), 20000)
		done := make(chan error, 1)
		go func() {
			got, err := io.ReadAll(s)
			if err == nil && !bytes.Equal(got, append([]byte("header"), request...)) {
				err = io.ErrUnexpectedEOF
			}
			if err == nil {
				_, err = s.WritePriority([]byte("ok"))
			}
			if err == nil {
				_, err = s.Write(response)
			}
			if err == nil {
				err = s.CloseWrite()
			}
			done <- err
		}()
		if _, err := c.WritePriority([]byte("header")); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Write(request); err != nil {
			t.Fatal(err)
		}
		if err := c.CloseWrite(); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Write([]byte("after EOF")); err != io.ErrClosedPipe {
			t.Fatalf("write after EOF: %v", err)
		}
		got, err := io.ReadAll(c)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, append([]byte("ok"), response...)) {
			t.Fatal("directional response truncated")
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		c.Close()
		s.Close()
	}
}
