// File accepted_lifetime_test.go ensures session-owned streams survive garbage
// collection of a returned convenience wrapper while their I/O is still live.
package smux

import (
	"io"
	"net"
	"runtime"
	"testing"
	"time"
)

// acceptedBacking returns the embedded stream as promoted method calls do; the
// temporary exported wrapper has no remaining owner after this function returns.
func acceptedBacking(s *Session) (*stream, error) {
	wrapper, err := s.AcceptStream()
	if err != nil {
		return nil, err
	}
	return wrapper.stream, nil
}

// TestAcceptedStreamSurvivesWrapperCollection forces the lifecycle race that can
// otherwise surface intermittently as EOF during an active opening or relay.
func TestAcceptedStreamSurvivesWrapperCollection(t *testing.T) {
	left, right := net.Pipe()
	client, err := Client(left, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := Server(right, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	local, err := client.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	remote, err := acceptedBacking(server)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	runtime.GC()
	runtime.GC()
	select {
	case <-remote.GetDieCh():
		t.Fatal("wrapper collection closed a live session-owned stream")
	case <-time.After(50 * time.Millisecond):
	}
	local.SetDeadline(time.Now().Add(time.Second))
	remote.SetDeadline(time.Now().Add(time.Second))
	done := make(chan error, 1)
	go func() { _, err := local.Write([]byte("alive")); done <- err }()
	var data [5]byte
	if _, err := io.ReadFull(remote, data[:]); err != nil || string(data[:]) != "alive" {
		t.Fatalf("live data lost: %q %v", data, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
