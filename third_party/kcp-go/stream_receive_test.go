// File stream_receive_test.go covers completed exp1 stream batching: preserved
// byte order, intact pool ownership, message boundaries and receive-window reopening.
package kcp

import (
	"bytes"
	"fmt"
	"testing"
)

// queuedReceive constructs owned, in-order segments through real reassembly.
func queuedReceive(t *testing.T, stream bool, payloads ...[]byte) *KCP {
	t.Helper()
	k := NewKCP(91, func([]byte, int) {})
	if stream {
		k.stream = 1
	}
	for i, p := range payloads {
		k.parse_data(segment{sn: uint32(i), data: p})
	}
	t.Cleanup(func() { releaseReceiveFixture(k) })
	return k
}

func TestStreamReceiveWholeSegmentsAndShortBuffer(t *testing.T) {
	k := queuedReceive(t, true, []byte("abc"), []byte("defgh"), []byte("ij"))
	if n := k.Recv(make([]byte, 2)); n != -2 || k.rcv_queue.Len() != 3 {
		t.Fatal("short buffer changed queue", n)
	}
	b := make([]byte, 7)
	if n := k.Recv(b); n != 3 || string(b[:n]) != "abc" {
		t.Fatal("partial segment consumed", n)
	}
	seg, _ := k.rcv_queue.Peek()
	if cap(seg.data) != mtuLimit || string(seg.data) != "defgh" {
		t.Fatal("pooled segment ownership changed")
	}
	b = make([]byte, 8)
	if n := k.Recv(b); n != 7 || string(b[:n]) != "defghij" || k.rcv_queue.Len() != 0 {
		t.Fatal("batch byte order/count lost", n)
	}
	if n := k.Recv(b); n != -1 {
		t.Fatal("empty receive contract changed", n)
	}
}

func TestStreamReceiveSessionCountsAndSmallReads(t *testing.T) {
	for _, sizes := range [][]int{{32}, {1, 2, 3, 1, 2, 32}} {
		k := queuedReceive(t, true, []byte("abc"), []byte("defgh"), []byte("ijkl"))
		s := &UDPSession{kcp: k}
		var got []byte
		for _, size := range sizes {
			if len(got) == 12 {
				break
			}
			b := make([]byte, size)
			n, err := s.Read(b)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, b[:n]...)
		}
		if string(got) != "abcdefghijkl" {
			t.Fatalf("session byte count hid input: %q", got)
		}
	}
}

func TestStreamReceiveWindowReopensAfterBatch(t *testing.T) {
	k := NewKCP(91, func([]byte, int) {})
	k.stream, k.rcv_wnd = 1, 2
	t.Cleanup(func() { releaseReceiveFixture(k) })
	for i := 0; i < 3; i++ {
		k.parse_data(segment{sn: uint32(i), data: []byte{byte(i)}})
	}
	if k.rcv_queue.Len() != 2 || k.rcv_buf.Len() != 1 {
		t.Fatal("fixture not backpressured")
	}
	b := make([]byte, 16)
	if n := k.Recv(b); n != 2 || !bytes.Equal(b[:n], []byte{0, 1}) {
		t.Fatal("queued batch lost", n)
	}
	if k.rcv_nxt != 3 || k.rcv_queue.Len() != 1 || k.rcv_buf.Len() != 0 || k.probe&IKCP_ASK_TELL == 0 {
		t.Fatal("window/gap drain did not recover")
	}
	if n := k.Recv(b); n != 1 || b[0] != 2 {
		t.Fatal("reassembled tail lost", n)
	}
}

func TestMessageReceiveStillReturnsOneMessage(t *testing.T) {
	k := queuedReceive(t, false, []byte("a"), []byte("bc"), []byte("def"), []byte("last"))
	for i, frg := range []uint8{2, 1, 0, 0} {
		seg, _ := k.rcv_queue.At(i)
		seg.frg = frg
	}
	if n := k.Recv(make([]byte, 5)); n != -2 {
		t.Fatal("fragment short-buffer contract changed", n)
	}
	b := make([]byte, 32)
	if n := k.Recv(b); n != 6 || string(b[:n]) != "abcdef" {
		t.Fatal("message boundary changed", n)
	}
	if n := k.Recv(b); n != 4 || string(b[:n]) != "last" {
		t.Fatal("second message boundary changed", n)
	}
}

// BenchmarkStreamReceiveBatch isolates receiver work from network scheduling.
// The heap-only oracle returns one segment per call; bytes/ownership are equal.
func BenchmarkStreamReceiveBatch(b *testing.B) {
	for _, old := range []bool{true, false} {
		b.Run(fmt.Sprintf("one-segment-%t", old), func(b *testing.B) {
			k := NewKCP(91, func([]byte, int) {})
			k.stream = 1
			k.rcv_wnd = 128
			defer releaseReceiveFixture(k)
			payload := make([]byte, 1326)
			out := make([]byte, 1326*32)
			b.SetBytes(int64(len(out)))
			b.ReportAllocs()
			for b.Loop() {
				base := k.rcv_nxt
				for i := uint32(0); i < 32; i++ {
					k.parse_data(segment{sn: base + i, data: payload})
				}
				if old {
					for range 32 {
						heapOnlyRecv(k, out)
					}
				} else if n := k.Recv(out); n != len(out) {
					b.Fatal(n)
				}
			}
		})
	}
}
