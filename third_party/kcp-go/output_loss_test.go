//go:build linux

// File output_loss_test.go verifies per-carrier soft-loss attribution on a
// shared physical worker without parsing cleartext/encrypted packet payloads.
package kcp

import (
	"golang.org/x/net/ipv4"
	"io"
	"net"
	"sync"
	"testing"
)

// selectiveDropBatch mimics one shared worker with a failing destination tuple.
// It consumes soft losses without turning them into fatal transport errors.
type selectiveDropBatch struct{ net.PacketConn }

func (p *selectiveDropBatch) ReadBatch(ms []ipv4.Message, flags int) (int, error) { return 0, io.EOF }
func (p *selectiveDropBatch) WriteBatch(ms []ipv4.Message, flags int) (int, error) {
	return len(ms), nil
}
func (p *selectiveDropBatch) WriteBatchWithDrops(ms []ipv4.Message, flags int) (int, int, error) {
	dropped := 0
	for _, m := range ms {
		if m.Addr.(*net.UDPAddr).Port == 9000 {
			dropped++
		}
	}
	return len(ms), dropped, nil
}

// TestSharedOutputLossIsAttributedToItsCarrier runs simultaneous batches over
// the same socket. Only the failing tuple can accumulate local output loss.
// Changing physical routes retains each logical carrier's lifetime count.
func TestSharedOutputLossIsAttributedToItsCarrier(t *testing.T) {
	p := new(selectiveDropBatch)
	bad, good := new(UDPSession), new(UDPSession)
	for i, s := range []*UDPSession{bad, good} {
		s.route.Store(&sessionRoute{conn: p, remote: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9000 + i}, platform: makePlatform(p)})
	}
	const batches = 64
	const width = 4
	var wg sync.WaitGroup
	for _, s := range []*UDPSession{bad, good} {
		for i := 0; i < batches; i++ {
			wg.Add(1)
			go func(s *UDPSession) {
				defer wg.Done()
				ms := make([]ipv4.Message, width)
				for i := range ms {
					ms[i].Buffers = [][]byte{[]byte("opaque-ciphertext-or-kcp-payload")}
				}
				s.tx(ms)
			}(s)
		}
	}
	wg.Wait()
	if bad.LocalOutputDrops() != batches*width || good.LocalOutputDrops() != 0 {
		t.Fatalf("shared loss crossed carriers: bad=%d good=%d", bad.LocalOutputDrops(), good.LocalOutputDrops())
	}
	next := new(selectiveDropBatch)
	bad.route.Store(&sessionRoute{conn: next, remote: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9001}, platform: makePlatform(next)})
	bad.tx([]ipv4.Message{{Buffers: [][]byte{[]byte("after-physical-move")}}})
	if bad.LocalOutputDrops() != batches*width {
		t.Fatal("physical move reset or added logical loss")
	}
}
