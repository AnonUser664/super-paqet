package kcp

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestEncryptedCreditControlCallbackOutsideCarrierLock(t *testing.T) {
	for _, fec := range [][2]int{{0, 0}, {10, 3}} {
		block, _ := NewAESGCMCrypt(make([]byte, 16))
		packet, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listener, err := ServeConn(block, fec[0], fec[1], packet)
		if err != nil {
			t.Fatal(err)
		}
		client, err := DialWithOptions(packet.LocalAddr().String(), block, fec[0], fec[1])
		if err != nil {
			t.Fatal(err)
		}
		client.SetWriteDelay(false)
		client.Write([]byte{1})
		listener.SetReadDeadline(time.Now().Add(3 * time.Second))
		server, err := listener.AcceptKCP()
		if err != nil {
			t.Fatal(err)
		}
		server.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.ReadFull(server, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
		got := make(chan [3]uint32, 1)
		client.SetCreditHintHandler(func(sid, consumed, window uint32) { got <- [3]uint32{sid, consumed, window} })
		server.SetCreditHintHandler(func(sid, consumed, window uint32) {
			server.GetSRTT() // Re-enter the carrier lock: deadlocks if dispatched under it.
			server.SendCreditHint(sid, consumed, window)
		})
		if err := client.SendCreditHint(3, 123456, 65536); err != nil {
			t.Fatal(err)
		}
		select {
		case values := <-got:
			if values != [3]uint32{3, 123456, 65536} {
				t.Fatal(values)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("credit control lost or callback lock inversion")
		}
		client.Close()
		server.Close()
		listener.Close()
		packet.Close()
	}
}

func TestControlCannotCreateOrReplaceCarrierGeneration(t *testing.T) {
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	listener, err := ServeConn(nil, 0, 0, packet)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:9999")
	for _, cmd := range []uint8{IKCP_CMD_ACK, IKCP_CMD_WINS, IKCP_CMD_WASK} {
		var raw [IKCP_OVERHEAD]byte
		seg := segment{conv: 999, cmd: cmd, wnd: 128}
		seg.encode(raw[:])
		listener.packetInput(raw[:], addr)
	}
	listener.sessionLock.RLock()
	n := len(listener.sessions)
	listener.sessionLock.RUnlock()
	if n != 0 {
		t.Fatal("control packet created new carrier")
	}
	client, err := DialWithOptions(packet.LocalAddr().String(), nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetWriteDelay(false)
	client.Write([]byte{1})
	listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	server, err := listener.AcceptKCP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for _, cmd := range []uint8{IKCP_CMD_ACK, IKCP_CMD_WINS, IKCP_CMD_WASK} {
		var raw [IKCP_OVERHEAD]byte
		seg := segment{conv: server.GetConv() + 1, cmd: cmd, wnd: 128}
		seg.encode(raw[:])
		listener.packetInput(raw[:], server.RemoteAddr())
		if server.isClosed() {
			t.Fatal("late control reset newer carrier")
		}
	}
}

func TestWriteBudgetTracksWindowAndPacing(t *testing.T) {
	k := NewKCP(1, func([]byte, int) {})
	s := &UDPSession{kcp: k}
	s.SetWindowSize(128, 128)
	if s.WriteBudget() != 65535 {
		t.Fatal("fast unpaced transport lost batching")
	}
	s.SetPacingRate(100000)
	if s.WriteBudget() != 2000 {
		t.Fatal("paced frame exceeds 20ms budget")
	}
	s.SetPacingRate(1000)
	if s.WriteBudget() != int(k.mss) {
		t.Fatal("slow frames waste bandwidth below one MSS")
	}
	s.SetPacingRate(0)
	s.SetWindowSize(4, 128)
	if s.WriteBudget() != int(4*k.mss) {
		t.Fatal("live send window ignored")
	}
}
