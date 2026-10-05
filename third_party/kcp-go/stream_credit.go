// File stream_credit.go: expedites cumulative mux feedback inside KCP controls while retaining
// ordered application delivery.

package kcp

import (
	"encoding/binary"
	"io"
)

const creditMagic = 0x31515053 // SPQ1, little endian inside encrypted KCP WINS.

// WriteBudget bounds one mux data frame to the live send window, so a slow
// carrier cannot queue a full 64 KiB application frame ahead of new control.
func (s *UDPSession) WriteBudget() int { return int(s.writeBudget.Load()) }

// updateWriteBudgetLocked recomputes the mux-frame cap from current window/MSS/pacing while
// the carrier lock protects those values.
func (s *UDPSession) updateWriteBudgetLocked() {
	k := s.kcp
	limit := min(65535, uint64(k.snd_wnd)*uint64(k.mss))
	if k.pacingRate > 0 {
		// At most 20ms of paced data per mux request. Keep one full MSS
		// as the floor to avoid wasting scarce bandwidth on tiny packets.
		ms := s.writeBatchMS
		if ms == 0 {
			ms = 20
		}
		limit = min(limit, max(uint64(k.mss), k.pacingRate/1000*uint64(ms)+(k.pacingRate%1000)*uint64(ms)/1000))
	}
	s.writeBudget.Store(uint32(limit))
}

// SetWriteBatchBudget bounds the paced frame-duration budget, limiting how long bulk can
// precede new controls.
func (s *UDPSession) SetWriteBatchBudget(milliseconds uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeBatchMS = min(1000, max(1, milliseconds))
	s.updateWriteBudgetLocked()
}

// streamCredit retains one decoded cumulative stream update until callbacks can run outside
// the carrier lock.
type streamCredit struct {
	// Logical stream ID, cumulative consumed byte count and advertised byte capacity; modular
	// validation applies.
	sid, consumed, window uint32
}

// emit counts packet attempts/control output before invoking the configured wire callback.
func (k *KCP) emit(data []byte, size int, application bool) {
	k.outputPackets++
	k.outputBytes += uint64(size)
	if !application {
		k.ackPackets++
	}
	k.output(data, size)
}

// SetCreditHintHandler receives optional cumulative credit in KCP window-control
// messages. It runs outside the KCP lock, preventing mux/carrier lock inversion.
// A callback must be brief; normal reliable UPD frames remain authoritative.
func (s *UDPSession) SetCreditHintHandler(handler func(uint32, uint32, uint32)) {
	s.mu.Lock()
	s.kcp.creditHints = handler != nil
	if handler == nil {
		handler = func(uint32, uint32, uint32) {}
	}
	s.creditHintHandler.Store(handler)
	s.mu.Unlock()
}

// unlockAndDispatchCredits runs mux credit callbacks after releasing the KCP lock to avoid
// carrier/mux lock inversion.
func (s *UDPSession) unlockAndDispatchCredits() {
	credits := s.kcp.receivedCredits
	s.kcp.receivedCredits = nil
	s.mu.Unlock()
	if callback := s.creditHintHandler.Load(); callback != nil {
		for _, c := range credits {
			callback.(func(uint32, uint32, uint32))(c.sid, c.consumed, c.window)
		}
	}
}

// SendCreditHint bypasses the ordered application's send queue using a KCP
// WINS control segment. Drops/reordering are harmless with monotonic credit
// validation and the retained reliable update. Encryption/FEC/raw TX are shared.
func (s *UDPSession) SendCreditHint(sid, consumed, window uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed() {
		return io.ErrClosedPipe
	}
	var packet [IKCP_OVERHEAD + 16]byte
	data := packet[IKCP_OVERHEAD:]
	binary.LittleEndian.PutUint32(data, creditMagic)
	binary.LittleEndian.PutUint32(data[4:], sid)
	binary.LittleEndian.PutUint32(data[8:], consumed)
	binary.LittleEndian.PutUint32(data[12:], window)
	k := s.kcp
	seg := segment{conv: k.conv, cmd: IKCP_CMD_WINS, wnd: k.wnd_unused(), una: k.rcv_nxt, data: data}
	seg.encode(packet[:])
	k.emit(packet[:], len(packet), false)
	return nil
}
