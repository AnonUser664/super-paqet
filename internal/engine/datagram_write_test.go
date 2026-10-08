//go:build linux

// File datagram_write_test.go covers the one-write UDP optimization's record
// boundaries, write errors and pooled scratch cost.
package engine

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

type datagramRecorder struct {
	bytes.Buffer
	calls int
	short bool
	err   error
}

func (w *datagramRecorder) Write(p []byte) (int, error) {
	w.calls++
	if w.err != nil {
		return 0, w.err
	}
	if w.short {
		return len(p) / 2, nil
	}
	return w.Buffer.Write(p)
}

func TestCombinedDatagramWrites(t *testing.T) {
	for _, size := range []int{0, 1, 2048, 2049, 65507} {
		payload := bytes.Repeat([]byte{0x61}, size)
		w := &datagramRecorder{}
		if err := writeDatagram(w, payload); err != nil {
			t.Fatal(err)
		}
		wantCalls := 1
		if size > 2048 {
			wantCalls = 2
		}
		if w.calls != wantCalls {
			t.Fatalf("size=%d writes=%d", size, w.calls)
		}
		got, err := readDatagram(&w.Buffer)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal((*got.b)[:got.n], payload) {
			t.Fatal("record changed", size)
		}
		copyPools[got.class].Put(got.b)
		if err := writeDatagram(&datagramRecorder{short: true}, payload); err != io.ErrShortWrite {
			t.Fatal("short write hidden", size, err)
		}
		if err := writeDatagram(&datagramRecorder{err: io.ErrClosedPipe}, payload); err != io.ErrClosedPipe {
			t.Fatal("writer error hidden", size, err)
		}
	}
}

func BenchmarkCombinedDatagramWrite(b *testing.B) {
	for _, size := range []int{0, 64, 1400, 2048} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			p := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				if err := writeDatagram(io.Discard, p); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
