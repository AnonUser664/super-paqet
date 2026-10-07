// File protocol_test.go: exercises protocol regressions; fixtures must preserve cleanup and
// expose byte/lifecycle failures explicitly.

package protocol

import (
	"bytes"
	"io"
	"paqet/internal/conf"
	"paqet/internal/tnet"
	"testing"
)

// TestRoundTripAndMalformedControls checks Round Trip And Malformed Controls so a change
// cannot silently weaken the recorded regression contract.
func TestRoundTripAndMalformedControls(t *testing.T) {
	for _, p := range []Proto{{Type: PMTOKEN}, {Type: PMCHECK, Capability: [32]byte{1}, Epoch: 7}, {Type: PMMOVE, Capability: [32]byte{2}, Epoch: 8}, {Type: PMREPLY, Capability: [32]byte{3}, Epoch: 9, Status: 1}, {Type: PPING}, {Type: PPONG}, {Type: PTCP2, Addr: &tnet.Addr{Host: "example.com", Port: 443}}, {Type: PUDP2, Addr: &tnet.Addr{Host: "::1", Port: 53}}, {Type: PTCPF, TCPF: []conf.TCPF{{PSH: true, ACK: true}, {SYN: true}}}} {
		var wire bytes.Buffer
		if err := p.Write(&wire); err != nil {
			t.Fatal(err)
		}
		encoded := bytes.Clone(wire.Bytes())
		var got Proto
		if err := got.Read(&wire); err != nil {
			t.Fatal(err)
		}
		var again bytes.Buffer
		if err := got.Write(&again); err != nil || !bytes.Equal(encoded, again.Bytes()) {
			t.Fatal("control round trip failed")
		}
		for i := 0; i < len(encoded); i++ {
			if err := got.Read(bytes.NewReader(encoded[:i])); err == nil {
				t.Fatal("accepted truncated control")
			}
		}
	}
	for _, b := range [][]byte{{MAGIC, VERSION, PPING, 0, 1, 0}, {MAGIC, VERSION, PTCP2, 255, 255}, {0, VERSION, PPING, 0, 0}, {MAGIC, 99, PPING, 0, 0}, {MAGIC, VERSION, PTCPF, 0, 1, 0}, {MAGIC, VERSION, PMTOKEN, 0, 1, 0}, {MAGIC, VERSION, PMMOVE, 0, 0}} {
		var p Proto
		if p.Read(bytes.NewReader(b)) == nil {
			t.Fatal("accepted malformed control")
		}
	}
}

// shortWriter retains the short Writer fixture state used to expose failures without
// production network side effects.
type shortWriter struct{}

// Write simulates a partial successful write so control framing must reject silent truncation.
func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

// TestControlShortWrite checks Control Short Write so a change cannot silently weaken the
// recorded regression contract.
func TestControlShortWrite(t *testing.T) {
	if err := (&Proto{Type: PPING}).Write(shortWriter{}); err != io.ErrShortWrite {
		t.Fatal(err)
	}
}

// FuzzControlRead exercises Control Read with generated inputs to catch malformed-input
// crashes and unsafe boundary assumptions.
func FuzzControlRead(f *testing.F) {
	f.Add([]byte{MAGIC, VERSION, PPING, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) { var p Proto; _ = p.Read(bytes.NewReader(b)) })
}
