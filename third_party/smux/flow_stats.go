package smux

// FlowControlStats measures only blocked writes, without per-stream counters.
func (s *Session) FlowControlStats() (count, nanoseconds uint64) {
	return s.flowWaitCount.Load(), s.flowWaitNanoseconds.Load()
}
