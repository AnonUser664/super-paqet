// File telemetry.go: exposes coherent per-session window/delivery/RTT state to the external
// adaptive controller.

package kcp

// PostProcessingDrops counts local pipeline overflow, distinct from network loss.
func (s *UDPSession) PostProcessingDrops() uint64 { return s.postProcessingDrops.Load() }

// TransportStats is a coherent per-session snapshot for adaptive window control.
type TransportStats struct {
	// Peer feedback scheduling cost in milliseconds, not attributed path queue.
	PeerACKDelay uint32
	// Cumulative successful segment acknowledgments used to estimate packet delivery rate.
	AckedSegments uint64
	// Accepted data and currently outstanding byte storage, not raw wire bitrate.
	ReceivedBytes, PendingBytes uint64
	// Count supporting directional queue attribution; zero means no such samples.
	TransitSamples uint64
	// Relative estimated queue delays in milliseconds.
	ForwardQueue, ReverseQueue uint32
	// Output/control attempt counters; retries and dropped injection attempts can be included.
	OutputPackets, OutputBytes, ACKPackets, ACKSegments uint64
	// Observed retries later classified as possible reordering.
	SpuriousRetransmissions uint64
	// Observed reorder timing used to choose bounded gap grace.
	ReorderDelay uint32
	// Coherent delivery and retry totals for controller deltas.
	AckedBytes, SentSegments, RetransmittedSegments uint64
	// Smoothed round-trip delay and variation in milliseconds.
	SRTT, SRTTVar int32
	// Current estimated retry timeout in milliseconds, independent of outer fabricated
	// timestamps.
	RTO uint32
	// Pending count, segment-window settings and usable payload bytes in this snapshot.
	Pending, SendWindow, ReceiveWindow, RemoteWindow, MSS int
	// Queue occupancy by reliable send, ordered receive, reordered receive and output pipeline
	// stage.
	SendQueued, ReceiveQueued, ReceiveReordered, PipelineQueued int
	// Cumulative send-capacity waits and their blocked time.
	WriteWaitCount, WriteWaitNanoseconds uint64
	// Current data rate limit; zero disables pacing rather than meaning no traffic.
	PacingBytesPerSecond uint64
	// Current maximum data-frame budget exposed to the mux.
	WriteBudgetBytes int
}

// TransportStats takes one coherent locked snapshot so controllers do not combine counters
// from different protocol instants.
func (s *UDPSession) TransportStats() TransportStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.kcp
	pipeline := 0
	if s.postQueue != nil {
		pipeline = s.postQueue.len()
	}
	return TransportStats{AckedBytes: k.ackedBytes, SentSegments: k.sentSegments, RetransmittedSegments: k.retransmittedSegments, AckedSegments: k.ackedSegments, PeerACKDelay: k.peerACKDelay,
		ReceivedBytes:  k.receivedBytes,
		PendingBytes:   k.enqueuedBytes - k.ackedBytes,
		TransitSamples: k.transitSamples, ForwardQueue: k.forwardQueue, ReverseQueue: k.reverseQueue,
		OutputPackets: k.outputPackets, OutputBytes: k.outputBytes, ACKPackets: k.ackPackets, ACKSegments: k.ackSegments,
		SRTT: k.rx_srtt, SRTTVar: k.rx_rttvar, RTO: k.rx_rto, Pending: k.WaitSnd(), SendWindow: int(k.snd_wnd), ReceiveWindow: int(k.rcv_wnd), RemoteWindow: int(k.rmt_wnd), MSS: int(k.mss),
		SendQueued: k.snd_queue.Len(), ReceiveQueued: k.rcv_queue.Len(), ReceiveReordered: k.rcv_buf.Len(), PipelineQueued: pipeline,
		WriteWaitCount: s.writeWaitCount.Load(), WriteWaitNanoseconds: s.writeWaitNanoseconds.Load(), PacingBytesPerSecond: k.pacingRate, SpuriousRetransmissions: k.spuriousRetransmissions, ReorderDelay: k.observedReorderDelay, WriteBudgetBytes: s.WriteBudget()}
}
