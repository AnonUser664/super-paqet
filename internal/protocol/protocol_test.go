package protocol

import (
	"bytes"
	"io"
	"paqet/internal/conf"
	"paqet/internal/tnet"
	"testing"
)

func TestRoundTripAndMalformedControls(t *testing.T) {
	for _, p := range []Proto{{Type: PPING}, {Type: PPONG}, {Type: PTCP2, Addr: &tnet.Addr{Host: "example.com", Port: 443}}, {Type: PUDP2, Addr: &tnet.Addr{Host: "::1", Port: 53}}, {Type: PTCPF, TCPF: []conf.TCPF{{PSH: true, ACK: true}, {SYN: true}}}} {
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
	for _, b := range [][]byte{{MAGIC, VERSION, PPING, 0, 1, 0}, {MAGIC, VERSION, PTCP2, 255, 255}, {0, VERSION, PPING, 0, 0}, {MAGIC, 99, PPING, 0, 0}, {MAGIC, VERSION, PTCPF, 0, 1, 0}} {
		var p Proto
		if p.Read(bytes.NewReader(b)) == nil {
			t.Fatal("accepted malformed control")
		}
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestControlShortWrite(t *testing.T) {
	if err := (&Proto{Type: PPING}).Write(shortWriter{}); err != io.ErrShortWrite {
		t.Fatal(err)
	}
}

func FuzzControlRead(f *testing.F) {
	f.Add([]byte{MAGIC, VERSION, PPING, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) { var p Proto; _ = p.Read(bytes.NewReader(b)) })
}
