//go:build linux

// File observe.go: samples carrier and listener state without taking ownership of accepted
// carriers' shared packet sockets.

package engine

import (
	"log/slog"
	"runtime"
	"time"

	kcplib "github.com/xtaci/kcp-go/v5"
	"paqet/internal/socket"
	"paqet/internal/tnet/kcp"
)

// observedPacket identifies a listener-owned worker socket for shared drop telemetry without
// taking ownership.
type observedPacket struct {
	// Listener/worker identities matching socket-scoped diagnostic labels.
	index, worker int
	// Non-owning listener packet reference used for telemetry; it must not be closed by an
	// accepted carrier.
	packet *socket.PacketConn
}

// observedSession copies controller estimates under the tuner lock before formatting
// diagnostics.
type observedSession struct {
	// Non-owning carrier reference retained for one diagnostic snapshot.
	conn *kcp.Conn
	// Copied controller estimates for formatting outside the tuner lock.
	minRTT, rate float64
	// Copied live send window in segments.
	window int
	// Allows bounded window/rate growth while learning a new path.
	startup bool
	// Copied capacity/loss estimates kept separate from live mutable controller state.
	peakRate, lossRatio float64
	// Hysteretic queue classification used to avoid reacting permanently to reorder/jitter
	// noise.
	congested bool
	// Forward-attributed queue growth used to distinguish congestion from reverse ACK pressure.
	queueSignal float64
}

// observedState retains the previous carrier counters/time so periodic logs can report deltas
// instead of misleading totals.
type observedState struct {
	// Previous coherent carrier counters used for interval delivery/retry deltas.
	stats kcplib.TransportStats
	// Prior mux wait time and local output-drop counts used to calculate sample deltas.
	flowWait, pipelineDrops uint64
	// Timestamp for delta/rate expiry calculations.
	at time.Time
}

// observe snapshots controller/listener state under ownership locks and reports sampled queue
// and delivery changes.
func (e *Engine) observe() {
	ticker := time.NewTicker(e.current().Log.duration)
	defer ticker.Stop()
	previous := make(map[*kcp.Conn]observedState)
	previousDrops := make(map[*socket.PacketConn]uint64)
	for {
		select {
		case <-e.ctx.Done():
			e.log().Info("engine.stopping", "active", e.stats.Active.Load(), "errors", e.stats.Errors.Load(), "aborted", e.stats.Aborted.Load(), "log_dropped", e.diagnostics.dropped.Load())
			return
		case <-e.observeChanged:
			ticker.Reset(e.current().Log.duration)
		case now := <-ticker.C:
			e.log().Info("engine.summary", "active", e.stats.Active.Load(), "accepted", e.stats.Accepted.Load(), "rejected", e.stats.Rejected.Load(), "errors", e.stats.Errors.Load(), "aborted", e.stats.Aborted.Load(), "sent_bytes", e.stats.Sent.Load(), "received_bytes", e.stats.Received.Load(), "goroutines", runtime.NumGoroutine(), "log_dropped", e.diagnostics.dropped.Load())
			if !e.log().Enabled(e.ctx, slog.LevelDebug) {
				continue
			}

			e.tuneMu.Lock()
			connections := make([]observedSession, 0, len(e.tuners))
			for conn, c := range e.tuners {
				connections = append(connections, observedSession{conn, c.minRTT, c.rate, c.window, c.startup, c.peakRate, c.lossRatio, c.congested, c.queueSignal})
			}
			packets := append([]observedPacket(nil), e.packetObservers...)
			e.tuneMu.Unlock()
			livePackets := make(map[*socket.PacketConn]bool, len(packets))
			for _, observed := range packets {
				livePackets[observed.packet] = true
				packet := observed.packet
				total := packet.TXDrops()
				if total != previousDrops[packet] {
					e.log().Debug("packet.tx_queue", "listener", observed.index, "worker", observed.worker, "local", packet.LocalAddr().String(), "drops_total", total, "drops_delta", total-previousDrops[packet])
					previousDrops[packet] = total
				}
			}
			for packet := range previousDrops {
				if !livePackets[packet] {
					delete(previousDrops, packet)
				}
			}
			live := make(map[*kcp.Conn]struct{}, len(connections))
			for _, v := range connections {
				conn := v.conn
				if conn.Session.IsClosed() {
					continue
				}
				live[conn] = struct{}{}
				s := conn.UDPSession.TransportStats()
				waitCount, flowWait := conn.Session.FlowControlStats()
				creditPending := conn.Session.PendingCredits()
				hintsSent, hintsReceived := conn.Session.CreditHintStats()
				minWindow, maxWindow := conn.Session.ReceiveWindowStats()
				drops := conn.UDPSession.PostProcessingDrops()
				old := previous[conn]
				var txDrops any
				if conn.PacketConn != nil {
					txDrops = conn.PacketConn.TXDrops()
				}
				seconds := now.Sub(old.at).Seconds()
				if old.at.IsZero() {
					seconds = e.current().Log.duration.Seconds()
				}
				previous[conn] = observedState{s, flowWait, drops, now}
				e.log().Debug("transport.sample", "conv", conn.UDPSession.GetConv(), "remote", conn.RemoteAddr().String(), "pacing_mbit", float64(s.PacingBytesPerSecond)*8/1e6, "startup", v.startup, "peak_mbit", v.peakRate*8/1e6, "loss_ratio", v.lossRatio,
					"delivery_mbit", float64(s.AckedBytes-old.stats.AckedBytes)*8/seconds/1e6, "estimate_mbit", v.rate*8/1e6,
					"output_pps", float64(s.OutputPackets-old.stats.OutputPackets)/seconds, "output_kcp_mbit", float64(s.OutputBytes-old.stats.OutputBytes)*8/seconds/1e6,
					"ack_pps", float64(s.ACKPackets-old.stats.ACKPackets)/seconds, "ack_segments_delta", s.ACKSegments-old.stats.ACKSegments,
					"credit_pending", creditPending, "credit_hints_sent", hintsSent, "credit_hints_received", hintsReceived,
					"write_budget_bytes", s.WriteBudgetBytes,
					"rtt_ms", s.SRTT, "rttvar_ms", s.SRTTVar, "min_rtt_ms", v.minRTT, "rto_ms", s.RTO,
					"forward_queue_ms", s.ForwardQueue, "reverse_queue_ms", s.ReverseQueue, "transit_samples", s.TransitSamples, "congested", v.congested,
					"queue_signal_ms", v.queueSignal, "peer_ack_delay_ms", s.PeerACKDelay,
					"send_window", s.SendWindow, "remote_window", s.RemoteWindow, "pending", s.Pending, "send_queued", s.SendQueued,
					"receive_queued", s.ReceiveQueued, "receive_reordered", s.ReceiveReordered, "pipeline_queued", s.PipelineQueued,
					"retransmit_delta", s.RetransmittedSegments-old.stats.RetransmittedSegments, "sent_delta", s.SentSegments-old.stats.SentSegments,
					"kcp_wait_ms", float64(s.WriteWaitNanoseconds-old.stats.WriteWaitNanoseconds)/1e6, "kcp_wait_count", s.WriteWaitCount-old.stats.WriteWaitCount,
					"mux_wait_ms", float64(flowWait-old.flowWait)/1e6, "mux_wait_count_total", waitCount,
					"stream_window_min", minWindow, "stream_window_max", maxWindow, "streams", conn.Session.NumStreams(), "tx_queue_drops", txDrops, "pipeline_drop_delta", drops-old.pipelineDrops)
			}
			for conn := range previous {
				if _, ok := live[conn]; !ok {
					delete(previous, conn)
				}
			}
		}
	}
}
