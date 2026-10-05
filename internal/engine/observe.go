//go:build linux

package engine

import (
	"log/slog"
	"runtime"
	"time"

	kcplib "github.com/xtaci/kcp-go/v5"
	"paqet/internal/tnet/kcp"
)

type observedSession struct {
	conn                *kcp.Conn
	minRTT, rate        float64
	window              int
	startup             bool
	peakRate, lossRatio float64
	congested           bool
	queueSignal         float64
}
type observedState struct {
	stats                   kcplib.TransportStats
	flowWait, pipelineDrops uint64
	at                      time.Time
}

func (e *Engine) observe() {
	ticker := time.NewTicker(e.cfg.Log.duration)
	defer ticker.Stop()
	previous := make(map[*kcp.Conn]observedState)
	for {
		select {
		case <-e.ctx.Done():
			e.log().Info("engine.stopping", "active", e.stats.Active.Load(), "errors", e.stats.Errors.Load(), "aborted", e.stats.Aborted.Load(), "log_dropped", e.diagnostics.dropped.Load())
			return
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
			e.tuneMu.Unlock()
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
				seconds := now.Sub(old.at).Seconds()
				if old.at.IsZero() {
					seconds = e.cfg.Log.duration.Seconds()
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
					"stream_window_min", minWindow, "stream_window_max", maxWindow, "streams", conn.Session.NumStreams(), "pipeline_drop_delta", drops-old.pipelineDrops)
			}
			for conn := range previous {
				if _, ok := live[conn]; !ok {
					delete(previous, conn)
				}
			}
		}
	}
}
