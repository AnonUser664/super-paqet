package kcp

// PostProcessingDrops counts local pipeline overflow, distinct from network loss.
func (s *UDPSession) PostProcessingDrops() uint64 { return s.postProcessingDrops.Load() }

// TransportStats is a coherent per-session snapshot for adaptive window control.
type TransportStats struct {
	AckedSegments                                               uint64
	ReceivedBytes, PendingBytes                                 uint64
	TransitSamples                                              uint64
	ForwardQueue, ReverseQueue                                  uint32
	OutputPackets, OutputBytes, ACKPackets, ACKSegments         uint64
	SpuriousRetransmissions                                     uint64
	ReorderDelay                                                uint32
	AckedBytes, SentSegments, RetransmittedSegments             uint64
	SRTT, SRTTVar                                               int32
	RTO                                                         uint32
	Pending, SendWindow, ReceiveWindow, RemoteWindow, MSS       int
	SendQueued, ReceiveQueued, ReceiveReordered, PipelineQueued int
	WriteWaitCount, WriteWaitNanoseconds                        uint64
	PacingBytesPerSecond                                        uint64
	WriteBudgetBytes                                            int
}

func (s *UDPSession) TransportStats() TransportStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.kcp
	pipeline := 0
	if s.postQueue != nil {
		pipeline = s.postQueue.len()
	}
	return TransportStats{AckedBytes: k.ackedBytes, SentSegments: k.sentSegments, RetransmittedSegments: k.retransmittedSegments, AckedSegments: k.ackedSegments,
		ReceivedBytes:  k.receivedBytes,
		PendingBytes:   k.enqueuedBytes - k.ackedBytes,
		TransitSamples: k.transitSamples, ForwardQueue: k.forwardQueue, ReverseQueue: k.reverseQueue,
		OutputPackets: k.outputPackets, OutputBytes: k.outputBytes, ACKPackets: k.ackPackets, ACKSegments: k.ackSegments,
		SRTT: k.rx_srtt, SRTTVar: k.rx_rttvar, RTO: k.rx_rto, Pending: k.WaitSnd(), SendWindow: int(k.snd_wnd), ReceiveWindow: int(k.rcv_wnd), RemoteWindow: int(k.rmt_wnd), MSS: int(k.mss),
		SendQueued: k.snd_queue.Len(), ReceiveQueued: k.rcv_queue.Len(), ReceiveReordered: k.rcv_buf.Len(), PipelineQueued: pipeline,
		WriteWaitCount: s.writeWaitCount.Load(), WriteWaitNanoseconds: s.writeWaitNanoseconds.Load(), PacingBytesPerSecond: k.pacingRate, SpuriousRetransmissions: k.spuriousRetransmissions, ReorderDelay: k.observedReorderDelay, WriteBudgetBytes: s.WriteBudget()}
}
