// File credit.go: coalesces one pending receive update per stream and supports optional
// validated credit hints.

package smux

import (
	"encoding/binary"
	"io"
)

// One entry per stream with unsent credit; no timer or goroutine per stream.
// Closed streams unlink their entry, bounding memory even during a blocked
// carrier and continuous stream churn. Only sendLoop writes to the carrier.
type creditUpdate struct {
	// Logical stream ID, cumulative consumed byte count and advertised byte capacity; modular
	// validation applies.
	sid, consumed, window uint32
	// Embedded buffer avoids heap allocation during frame creation.
	buf [8]byte
	// Intrusive links preserve credit FIFO order with one entry per stream.
	previous, next *creditUpdate
}

// creditHintTransport defines optional expedited credit feedback; reliable mux updates remain
// the compatibility fallback.
type creditHintTransport interface {
	// Registers optional expedited feedback; callbacks must not invert carrier/mux locks.
	SetCreditHintHandler(func(uint32, uint32, uint32))
	// Attempts loss-tolerant cumulative feedback while reliable UPD remains the fallback.
	SendCreditHint(uint32, uint32, uint32) error
}

// receiveCreditHint validates/applies optional cumulative credit under stream registration
// synchronization; reliable UPD remains authoritative.
func (s *Session) receiveCreditHint(sid, consumed, window uint32) {
	s.streamLock.Lock()
	if stream := s.streams[sid]; stream != nil {
		s.creditHintsReceived.Add(1)
		stream.update(consumed, window)
	}
	s.streamLock.Unlock()
}

// CreditHintStats returns atomic hint attempt/receive counters without scanning every stream.
func (s *Session) CreditHintStats() (sent, received uint64) {
	return s.creditHintsSent.Load(), s.creditHintsReceived.Load()
}

// PendingCredits reports coalesced update entries under the credit-list lock.
func (s *Session) PendingCredits() (pending int) {
	s.creditMu.Lock()
	pending = len(s.credits)
	s.creditMu.Unlock()
	return pending
}

// queueCredit retains at most one latest update per live stream and never blocks the reader on
// carrier output.
func (s *Session) queueCredit(stream *stream, consumed, window uint32) error {
	s.creditMu.Lock()
	defer s.creditMu.Unlock()
	select {
	case <-s.die:
		return io.ErrClosedPipe
	case <-stream.die:
		// Buffered bytes remain readable after stream close; no further
		// credit is needed by the peer for that stream.
		return nil
	case <-s.chSocketWriteError:
		return s.socketWriteError.Load().(error)
	default:
	}
	if entry := s.credits[stream.id]; entry != nil {
		entry.consumed, entry.window = consumed, window
	} else {
		entry := &creditUpdate{sid: stream.id, consumed: consumed, window: window, previous: s.creditTail}
		if s.creditTail != nil {
			s.creditTail.next = entry
		} else {
			s.creditHead = entry
		}
		s.creditTail = entry
		s.credits[stream.id] = entry
	}
	s.notifyShaperPending()
	return nil
}

// unlinkCredit repairs neighboring credit links and membership while the credit lock is held.
func (s *Session) unlinkCredit(entry *creditUpdate) {
	if entry.previous != nil {
		entry.previous.next = entry.next
	} else {
		s.creditHead = entry.next
	}
	if entry.next != nil {
		entry.next.previous = entry.previous
	} else {
		s.creditTail = entry.previous
	}
	delete(s.credits, entry.sid)
	// A popped entry may keep its embedded wire bytes alive until output
	// finishes. It must not retain neighbors and their pending stream credits.
	entry.previous, entry.next = nil, nil
}

// removeCredit drops an abandoned stream's pending update so churn cannot grow a blocked
// writer queue indefinitely.
func (s *Session) removeCredit(sid uint32) {
	s.creditMu.Lock()
	if entry := s.credits[sid]; entry != nil {
		s.unlinkCredit(entry)
	}
	s.creditMu.Unlock()
}

// clearCredits releases pending update references during session failure/close.
func (s *Session) clearCredits() {
	s.creditMu.Lock()
	clear(s.credits)
	s.creditHead, s.creditTail = nil, nil
	s.creditMu.Unlock()
}

// popCredit turns the oldest coalesced entry into a reliable control request; only the send
// loop writes it.
func (s *Session) popCredit() (writeRequest, bool) {
	if !s.config.AsyncWindowUpdates {
		return writeRequest{}, false
	}
	s.creditMu.Lock()
	defer s.creditMu.Unlock()
	entry := s.creditHead
	if entry == nil {
		return writeRequest{}, false
	}
	s.unlinkCredit(entry)
	frame := newFrame(byte(s.config.Version), cmdUPD, entry.sid)
	binary.LittleEndian.PutUint32(entry.buf[:4], entry.consumed)
	binary.LittleEndian.PutUint32(entry.buf[4:8], entry.window)
	frame.data = entry.buf[:8]
	return writeRequest{class: CLSCTRL, frame: frame}, true
}
