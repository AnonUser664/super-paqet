package smux

import (
	"encoding/binary"
	"io"
)

// One entry per stream with unsent credit; no timer or goroutine per stream.
// Closed streams unlink their entry, bounding memory even during a blocked
// carrier and continuous stream churn. Only sendLoop writes to the carrier.
type creditUpdate struct {
	sid, consumed, window uint32
	previous, next        *creditUpdate
}

type creditHintTransport interface {
	SetCreditHintHandler(func(uint32, uint32, uint32))
	SendCreditHint(uint32, uint32, uint32) error
}

func (s *Session) receiveCreditHint(sid, consumed, window uint32) {
	s.streamLock.Lock()
	if stream := s.streams[sid]; stream != nil {
		s.creditHintsReceived.Add(1)
		stream.update(consumed, window)
	}
	s.streamLock.Unlock()
}

func (s *Session) CreditHintStats() (sent, received uint64) {
	return s.creditHintsSent.Load(), s.creditHintsReceived.Load()
}

func (s *Session) PendingCredits() (pending int) {
	s.creditMu.Lock()
	pending = len(s.credits)
	s.creditMu.Unlock()
	return pending
}

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
}

func (s *Session) removeCredit(sid uint32) {
	s.creditMu.Lock()
	if entry := s.credits[sid]; entry != nil {
		s.unlinkCredit(entry)
	}
	s.creditMu.Unlock()
}

func (s *Session) clearCredits() {
	s.creditMu.Lock()
	clear(s.credits)
	s.creditHead, s.creditTail = nil, nil
	s.creditMu.Unlock()
}

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
	frame.data = make([]byte, 8)
	binary.LittleEndian.PutUint32(frame.data, entry.consumed)
	binary.LittleEndian.PutUint32(frame.data[4:], entry.window)
	return writeRequest{class: CLSCTRL, frame: frame}, true
}
