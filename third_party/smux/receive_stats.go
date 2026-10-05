// File receive_stats.go: bounds diagnostic stream scanning so monitoring does not dominate
// high connection counts.

package smux

// ReceiveWindowStats samples small sessions without walking large connection
// populations or retaining stream references. Zero means the sample was skipped.
func (s *Session) ReceiveWindowStats() (uint32, uint32) {
	s.streamLock.Lock()
	defer s.streamLock.Unlock()
	if len(s.streams) > 64 {
		return 0, 0
	}
	var minimum, maximum uint32
	for _, stream := range s.streams {
		stream.bufferLock.Lock()
		window := stream.receiveWindow
		stream.bufferLock.Unlock()
		if minimum == 0 || window < minimum {
			minimum = window
		}
		if window > maximum {
			maximum = window
		}
	}
	return minimum, maximum
}
