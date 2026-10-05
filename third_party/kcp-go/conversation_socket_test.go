// File conversation_socket_test.go verifies lane isolation, encryption,
// unsolicited-input bounds and shared-socket ownership over real loopback UDP.
package kcp

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestSharedConversationsPreserveSiblingAndSocket exchanges verified payloads
// concurrently through one source tuple, then closes and replaces one lane.
func TestSharedConversationsPreserveSiblingAndSocket(t *testing.T) {
	for _, encryption := range []string{"null", "aes", "aes-gcm"} {
		t.Run(encryption, func(t *testing.T) {
			var block BlockCrypt
			var err error
			if encryption == "aes" {
				block, err = NewAESBlockCrypt(bytes.Repeat([]byte{7}, 16))
			}
			if encryption == "aes-gcm" {
				block, err = NewAESGCMCrypt(bytes.Repeat([]byte{7}, 16))
			}
			if err != nil {
				t.Fatal(err)
			}
			serverSocket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer serverSocket.Close()
			clientSocket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer clientSocket.Close()
			server, err := ServeConversationConn(block, 0, 0, serverSocket, false)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			client, err := ServeConversationConn(block, 0, 0, clientSocket, true)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			client.SetMaxSessions(8)
			var handlers sync.WaitGroup
			acceptDone := make(chan struct{})
			go func() {
				defer close(acceptDone)
				for {
					c, err := server.AcceptKCP()
					if err != nil {
						return
					}
					c.SetNoDelay(1, 10, 2, 1)
					c.SetWriteDelay(false)
					handlers.Add(1)
					go func() { defer handlers.Done(); defer c.Close(); io.Copy(c, c) }()
				}
			}()
			var lanes []*UDPSession
			for range 8 {
				c, err := client.DialConversation(serverSocket.LocalAddr())
				if err != nil {
					t.Fatal(err)
				}
				c.SetNoDelay(1, 10, 2, 1)
				c.SetWriteDelay(false)
				lanes = append(lanes, c)
			}
			if _, err := client.DialConversation(serverSocket.LocalAddr()); err == nil {
				t.Fatal("lane ceiling ignored")
			}
			var workers sync.WaitGroup
			for i, c := range lanes {
				workers.Add(1)
				go func() {
					defer workers.Done()
					payload := bytes.Repeat([]byte{byte(i + 1)}, 2048)
					got := make([]byte, len(payload))
					for range 32 {
						c.SetDeadline(time.Now().Add(5 * time.Second))
						if _, err := c.Write(payload); err != nil {
							t.Error(err)
							return
						}
						if _, err := io.ReadFull(c, got); err != nil {
							t.Error(err)
							return
						}
						if !bytes.Equal(payload, got) {
							t.Error("cross-lane corruption")
							return
						}
					}
				}()
			}
			workers.Wait()
			lanes[0].Close()
			replacement, err := client.DialConversation(serverSocket.LocalAddr())
			if err != nil {
				t.Fatal("closed lane retained admission", err)
			}
			replacement.Close()
			c := lanes[1]
			c.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write([]byte("alive")); err != nil {
				t.Fatal(err)
			}
			var got [5]byte
			if _, err := io.ReadFull(c, got[:]); err != nil || string(got[:]) != "alive" {
				t.Fatal("sibling closure disrupted lane", err)
			}
			client.Close()
			server.Close()
			<-acceptDone
			handlers.Wait()
			if _, err := client.DialConversation(serverSocket.LocalAddr()); err == nil {
				t.Fatal("closed group admitted a lane")
			}
			// Closing a group leaves the caller-owned physical socket usable.
			if _, err := clientSocket.WriteToUDP([]byte("socket retained"), serverSocket.LocalAddr().(*net.UDPAddr)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestSharedConversationsRejectFECAndUnsolicitedInput bounds the unsupported
// parity contract and confirms unknown packets never allocate outgoing lanes.
func TestSharedConversationsRejectFECAndUnsolicitedInput(t *testing.T) {
	packet, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	if _, err := ServeConversationConn(nil, 10, 3, packet, false); err == nil {
		t.Fatal("ambiguous FEC admitted")
	}
	l, err := ServeConversationConn(nil, 0, 0, packet, true)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	frame := make([]byte, IKCP_OVERHEAD+1)
	frame[4] = IKCP_CMD_PUSH
	binary.LittleEndian.PutUint32(frame[20:], 1)
	for i := uint32(1); i <= 1000; i++ {
		binary.LittleEndian.PutUint32(frame, i)
		l.packetInput(frame, packet.LocalAddr())
	}
	l.sessionLock.RLock()
	count := len(l.sessions)
	l.sessionLock.RUnlock()
	if count != 0 {
		t.Fatal("unsolicited input created lanes", count)
	}
}
