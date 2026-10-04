package kcp

import (
	"bytes"
	"testing"
)

func TestIndexedACKWithWrap(t *testing.T) {
	k := NewKCP(1, func([]byte, int) {})
	k.snd_una = 0xfffffffc
	k.snd_nxt = 4
	for i := uint32(0); i < 8; i++ {
		k.snd_buf.Push(segment{sn: k.snd_una + i, data: defaultBufferPool.Get()[:1]})
	}
	k.parse_ack(2)
	seg, _ := k.snd_buf.At(6)
	if seg.acked != 1 || k.ackedBytes != 1 {
		t.Fatal("indexed ACK failed across wrap")
	}
	k.parse_ack(2)
	if k.ackedBytes != 1 {
		t.Fatal("duplicate ACK counted twice")
	}
	k.parse_una(4)
	if k.ackedBytes != 8 {
		t.Fatal("cumulative ACK accounting failed")
	}
}

func TestNewOnlyFlushDoesNotRetransmit(t *testing.T) {
	var packets [][]byte
	k := NewKCP(1, func(p []byte, n int) { packets = append(packets, bytes.Clone(p[:n])) })
	k.NoDelay(1, 10, 2, 1)
	k.WndSize(1024, 1024)
	k.Send([]byte("first"))
	k.flush(IKCP_FLUSH_NEW)
	if len(packets) != 1 {
		t.Fatal("first data was not sent")
	}
	seg, _ := k.snd_buf.Peek()
	seg.resendts = currentMs() - 1
	k.Send([]byte("second"))
	k.flush(IKCP_FLUSH_NEW)
	if len(packets) != 2 || seg.xmit != 1 {
		t.Fatal("new-only flush retransmitted old data")
	}
	k.flush(IKCP_FLUSH_FULL)
	if seg.xmit != 2 {
		t.Fatal("timer flush failed to retransmit old data")
	}
}

func TestACKFlushDoesNotMoveSendQueue(t *testing.T) {
	k := NewKCP(1, func([]byte, int) {})
	k.Send([]byte("data"))
	k.flush(IKCP_FLUSH_ACKONLY)
	if k.snd_buf.Len() != 0 || k.snd_queue.Len() != 1 {
		t.Fatal("ACK-only flush moved unsent segments")
	}
}
