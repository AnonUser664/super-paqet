//go:build linux

// File static_pressure_test.go proves that static reliability does not disable
// capacity-aware carrier selection/growth, using real UDP send pressure.
package engine

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	kcplib "github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
	"paqet/internal/conf"
	"paqet/internal/tnet/kcp"
)

// TestStaticReliabilityStillGrowsHotPool exercises registration, the actual
// tuner tick and bounded pool growth. An idle sibling remains the next choice.
func TestStaticReliabilityStillGrowsHotPool(t *testing.T) {
	e := reloadFixture(t)
	ctx, cancel := context.WithCancel(e.ctx)
	e.ctx = ctx
	defer cancel()
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	sink, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	udp, err := kcplib.NewConn3(123, sink.LocalAddr(), nil, 0, 0, packet)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	udp.SetWindowSize(128, 128)
	left, right := net.Pipe()
	defer right.Close()
	go io.Copy(io.Discard, right)
	session, err := smux.Client(left, smux.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	conn := &kcp.Conn{UDPSession: udp, Session: session}
	defer conn.Close()
	stream, err := session.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	settings := &atomic.Pointer[Endpoint]{}
	settings.Store(&Endpoint{Adaptive: new(bool), KCP: conf.KCP{Mode: "fast3", Sndwnd: 128, Rcvwnd: 128, WriteBatchMS: 20, ACKDelayMaxMS: 20}})
	hotSlot := &slot{}
	hotSlot.conn.Store(conn)
	e.addEndpoint(conn, settings, hotSlot)
	if _, err := udp.Write(make([]byte, 10000)); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); e.tune() }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	for hotSlot.score.Load() < busyCarrier && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hotSlot.score.Load() < busyCarrier {
		t.Fatal("static carrier pressure was not sampled")
	}
	peer := &peer{engine: e, endpoint: Endpoint{MaxSessions: 2}, slots: []*slot{hotSlot}}
	spare := &slot{}
	peer.createSlot = func(context.Context) (*slot, error) { return spare, nil }
	if selected, err := peer.selectSlot(ctx); err != nil || selected != spare || len(peer.slots) != 2 {
		t.Fatal("hot static pool did not grow", err)
	}
	if selected, err := peer.selectSlot(ctx); err != nil || selected != spare {
		t.Fatal("idle spare not selected", err)
	}
}
