package kcp

import (
	"encoding/binary"
	"testing"
)

func TestDelayedACKSurvivesDataFlush(t *testing.T) {
	for _, start := range []uint32{0, 0xfffffffe} {
		now := start
		acks, pushes := 0, 0
		k := NewKCP(1, func(p []byte, n int) {
			p = p[:n]
			for len(p) >= IKCP_OVERHEAD {
				if p[4] == IKCP_CMD_ACK {
					acks++
				}
				if p[4] == IKCP_CMD_PUSH {
					pushes++
				}
				p = p[IKCP_OVERHEAD+int(binary.LittleEndian.Uint32(p[20:])):]
			}
		})
		k.clock = func() uint32 { return now }
		k.NoDelay(1, 10, 2, 1)
		k.ackDelay = 5
		k.ack_push(0, start)
		k.Send([]byte("outgoing"))
		k.flush(IKCP_FLUSH_NEW)
		if pushes != 1 || acks != 0 {
			t.Fatal("data flush bypassed ACK deadline")
		}
		now += 4
		k.flush(IKCP_FLUSH_FULL)
		if acks != 0 {
			t.Fatal("periodic flush bypassed ACK deadline")
		}
		now++
		k.flush(IKCP_FLUSH_FULL)
		if acks != 1 {
			t.Fatal("ACK deadline did not flush pending ACK")
		}
	}
}
