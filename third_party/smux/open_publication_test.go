// File open_publication_test.go checks receive-side publication before a SYN
// write completes; a fast reply must not be discarded as an unknown stream.
package smux

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// heldSYNConn delivers SYN bytes but delays Write's completion until the peer
// has delivered and the receiver has parsed a reply. This is permitted by the
// full-duplex connection contract and removes scheduler luck from the fixture.
type heldSYNConn struct {
	net.Conn
	replied <-chan struct{}
	failure error
}

func (c *heldSYNConn) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	if err == nil && len(data) >= headerSize && data[1] == cmdSYN {
		<-c.replied
		if c.failure != nil {
			return n, c.failure
		}
	}
	return n, err
}

// earlyReplyOpen builds the deterministic duplex publication race, optionally
// reporting a write failure after early receive data has already arrived.
func earlyReplyOpen(t *testing.T, failure error) (*Session, *Stream, error) {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() { right.Close() })
	right.SetDeadline(time.Now().Add(time.Second))
	replied := make(chan struct{})
	cfg := DefaultConfig()
	cfg.Version, cfg.KeepAliveDisabled = 2, true
	client, err := Client(&heldSYNConn{Conn: left, replied: replied, failure: failure}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	peerResult := make(chan error, 1)
	go func() {
		defer close(replied)
		header := make([]byte, headerSize)
		if _, err := io.ReadFull(right, header); err != nil {
			peerResult <- err
			return
		}
		sid := binary.LittleEndian.Uint32(header[4:])
		header[0], header[1] = 2, cmdPSH
		binary.LittleEndian.PutUint16(header[2:], 1)
		binary.LittleEndian.PutUint32(header[4:], sid)
		if _, err := right.Write(append(header, 7)); err != nil {
			peerResult <- err
			return
		}
		// Consumption of this next header proves the preceding data frame
		// was processed before the held SYN write is allowed to return.
		header[1] = cmdNOP
		binary.LittleEndian.PutUint16(header[2:], 0)
		_, err := right.Write(header)
		peerResult <- err
	}()
	stream, openErr := client.OpenStream()
	if err := <-peerResult; err != nil {
		t.Fatal(err)
	}
	return client, stream, openErr
}

// TestOpenPublishesBeforeSYNWriteReturns exercises a reply racing local write
// completion without loss, WAN timers or GC behavior.
func TestOpenPublishesBeforeSYNWriteReturns(t *testing.T) {
	_, stream, err := earlyReplyOpen(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	stream.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	var reply [1]byte
	if _, err := io.ReadFull(stream, reply[:]); err != nil || reply[0] != 7 {
		t.Fatalf("early reply was discarded before local publication: reply=%v error=%v", reply, err)
	}
}

// TestFailedOpenReclaimsEarlyReceiveData prevents failed unreturned streams
// from retaining application-buffer tokens or appearing as carrier users.
func TestFailedOpenReclaimsEarlyReceiveData(t *testing.T) {
	failure := errors.New("injected SYN completion failure")
	client, stream, err := earlyReplyOpen(t, failure)
	if stream != nil || !errors.Is(err, failure) {
		t.Fatalf("failed open escaped: stream=%v error=%v", stream, err)
	}
	if client.NumStreams() != 0 || atomic.LoadInt32(&client.bucket) != int32(client.config.MaxReceiveBuffer) {
		t.Fatal("failed open retained stream ownership or receive tokens")
	}
}
