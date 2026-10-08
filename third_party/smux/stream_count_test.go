// File stream_count_test.go verifies lock-free admission snapshots against real
// local/remote stream membership, failed publication and concurrent close.
package smux

import (
	"net"
	"sync"
	"testing"
)

// TestStreamCountSnapshotLifecycle exercises both outgoing and incoming stream
// registration, duplicate local aborts and closure without controller timers.
func TestStreamCountSnapshotLifecycle(t *testing.T) {
	left, right := net.Pipe()
	cfg := DefaultConfig()
	cfg.KeepAliveDisabled = true
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
	const count = 64
	var local, remote []*Stream
	for i := 0; i < count; i++ {
		a, err := client.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		b, err := server.AcceptStream()
		if err != nil {
			t.Fatal(err)
		}
		local, remote = append(local, a), append(remote, b)
		for _, s := range []*Session{client, server} {
			if s.NumStreamsSnapshot() != i+1 || s.NumStreams() != i+1 {
				t.Fatal("stream registration omitted from snapshot")
			}
		}
	}
	var wg sync.WaitGroup
	for _, stream := range append(local, remote...) {
		wg.Add(1)
		go func() { defer wg.Done(); stream.Abort(); stream.Abort() }()
	}
	wg.Wait()
	for _, s := range []*Session{client, server} {
		if s.NumStreamsSnapshot() != 0 || s.NumStreams() != 0 {
			t.Fatal("duplicate abort leaked or underflowed population")
		}
	}
	client.Close()
	if client.NumStreamsSnapshot() != 0 {
		t.Fatal("closed session remains selectable")
	}
}

// TestStreamCountSnapshotFailedOpening shares the deterministic early-reply
// fixture: failed SYN completion must remove unpublished receive ownership.
func TestStreamCountSnapshotFailedOpening(t *testing.T) {
	client, _, err := earlyReplyOpen(t, ErrTimeout)
	if err == nil || client.NumStreamsSnapshot() != 0 {
		t.Fatal("failed opening retained admission pressure")
	}
}
