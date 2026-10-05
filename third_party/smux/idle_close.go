// File idle_close.go: retires a mux carrier only when stream registration cannot race the idle
// decision.

package smux

// CloseIfIdle atomically excludes new stream registration before closing an
// empty session. A NumStreams/Close pair would race with concurrent OpenStream.
func (s *Session) CloseIfIdle() bool {
	s.streamLock.Lock()
	if len(s.streams) != 0 {
		s.streamLock.Unlock()
		return false
	}
	s.dieOnce.Do(func() { close(s.die) })
	s.streamLock.Unlock()
	s.clearCredits()
	s.conn.Close()
	return true
}
