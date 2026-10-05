// File flow_stats.go: tracks write-credit wait cost without holding a timer/goroutine per idle
// stream.

package smux

// FlowControlStats measures only blocked writes, without per-stream counters.
func (s *Session) FlowControlStats() (count, nanoseconds uint64) {
	return s.flowWaitCount.Load(), s.flowWaitNanoseconds.Load()
}
