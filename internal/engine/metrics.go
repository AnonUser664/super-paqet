//go:build linux

// File metrics.go: exports counters and bounded snapshots on a local HTTP server; liveness and
// byte attempts are not delivery proof.

package engine

import (
	"fmt"
	"github.com/xtaci/kcp-go/v5"
	"net/http"
	tkcp "paqet/internal/tnet/kcp"
	"runtime"
	"strconv"
)

// metrics exports process and carrier snapshots; expensive stream scans are bounded separately
// by mux telemetry.
func (e *Engine) metrics(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "super_paqet_path_recovery_attempts_total %d\nsuper_paqet_path_recovery_succeeded_total %d\nsuper_paqet_path_recovery_rejected_total %d\n", e.pathRecoveryAttempts.Load(), e.pathRecoverySucceeded.Load(), e.pathRecoveryRejected.Load())
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	if e.diagnostics != nil {
		fmt.Fprintf(w, "super_paqet_log_dropped_total %d\n", e.diagnostics.dropped.Load())
	}
	for _, v := range []struct {
		name  string
		value int64
	}{
		{"active_connections", e.stats.Active.Load()}, {"accepted_total", e.stats.Accepted.Load()}, {"rejected_total", e.stats.Rejected.Load()},
		{"errors_total", e.stats.Errors.Load()}, {"opening_transport_retries_total", e.stats.OpenRetries.Load()}, {"sent_bytes_total", e.stats.Sent.Load()}, {"received_bytes_total", e.stats.Received.Load()},
		{"aborted_connections_total", e.stats.Aborted.Load()},
		{"server_sessions", e.stats.Sessions.Load()}, {"goroutines", int64(runtime.NumGoroutine())},
	} {
		fmt.Fprintf(w, "super_paqet_%s %d\n", v.name, v.value)
	}
	fmt.Fprintf(w, "super_paqet_config_revision %d\nsuper_paqet_config_reload_applied_total %d\nsuper_paqet_config_reload_rejected_total %d\n", e.revision.Load(), e.reloadApplied.Load(), e.reloadRejected.Load())
	degraded := 0
	if e.degraded.Load() {
		degraded = 1
	}
	fmt.Fprintf(w, "super_paqet_config_degraded %d\n", degraded)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Fprintf(w, "super_paqet_heap_bytes %d\nsuper_paqet_heap_sys_bytes %d\nsuper_paqet_gc_total %d\n", m.HeapAlloc, m.HeapSys, m.NumGC)
	stats := kcp.DefaultSnmp.Copy()
	names, values := stats.Header(), stats.ToSlice()
	for i, name := range names {
		fmt.Fprintf(w, "super_paqet_kcp_%s %s\n", name, values[i])
	}
	// Include accepted sessions as well as outgoing peers. Copy pointers under
	// the tuner lock so formatting and stream sampling do not delay control.
	e.tuneMu.Lock()
	connections := make([]*tkcp.Conn, 0, len(e.tuners))
	for c := range e.tuners {
		connections = append(connections, c)
	}
	e.tuneMu.Unlock()
	for _, c := range connections {
		if c.Session.IsClosed() {
			continue
		}
		state := c.UDPSession.TransportStats()
		labels := fmt.Sprintf("conv=\"%d\",remote=%s", c.UDPSession.GetConv(), strconv.Quote(c.RemoteAddr().String()))
		fmt.Fprintf(w, "super_paqet_session_postprocessing_drops_total{%s} %d\n", labels, c.UDPSession.PostProcessingDrops())
		fmt.Fprintf(w, "super_paqet_session_output_packets_total{%s} %d\nsuper_paqet_session_output_kcp_bytes_total{%s} %d\nsuper_paqet_session_control_packets_total{%s} %d\nsuper_paqet_session_ack_segments_total{%s} %d\nsuper_paqet_session_pending_credits{%s} %d\n", labels, state.OutputPackets, labels, state.OutputBytes, labels, state.ACKPackets, labels, state.ACKSegments, labels, c.Session.PendingCredits())
		capacity, buffered, blocked := c.Session.ReceiveBufferStats()
		waiting := 0
		if blocked {
			waiting = 1
		}
		fmt.Fprintf(w, "super_paqet_session_mux_receive_capacity_bytes{%s} %d\nsuper_paqet_session_mux_receive_buffered_bytes{%s} %d\nsuper_paqet_session_mux_receive_blocked{%s} %d\n", labels, capacity, labels, buffered, labels, waiting)
		canceled, resetDrops := c.Session.WriteCancellationStats()
		fmt.Fprintf(w, "super_paqet_session_mux_canceled_writes_total{%s} %d\nsuper_paqet_session_mux_abort_queue_drops_total{%s} %d\n", labels, canceled, labels, resetDrops)
		minWindow, maxWindow := c.Session.ReceiveWindowStats()
		for _, v := range []struct {
			name  string
			value int
		}{
			{"send_window", state.SendWindow}, {"remote_window", state.RemoteWindow},
			{"pending", state.Pending}, {"rtt_ms", int(state.SRTT)},
			{"forward_queue_ms", int(state.ForwardQueue)}, {"reverse_queue_ms", int(state.ReverseQueue)},
		} {
			fmt.Fprintf(w, "super_paqet_session_%s{%s} %d\n", v.name, labels, v.value)
		}
		if maxWindow > 0 {
			fmt.Fprintf(w, "super_paqet_session_receive_window_min{%s} %d\nsuper_paqet_session_receive_window_max{%s} %d\n", labels, minWindow, labels, maxWindow)
		}
	}
	view := e.view.Load()
	if view == nil {
		return
	}
	for name, p := range view.peers {
		p.mu.RLock()
		for i, s := range p.slots {
			labels := fmt.Sprintf("peer=%s,session=%s", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)))
			suspect, probing := 0, 0
			if s.suspect.Load() {
				suspect = 1
			}
			if s.recoveryPending.Load() {
				probing = 1
			}
			fmt.Fprintf(w, "super_paqet_peer_carrier_suspect{%s} %d\nsuper_paqet_peer_carrier_recovery_pending{%s} %d\nsuper_paqet_peer_carrier_transport_failures_total{%s} %d\n", labels, suspect, labels, probing, labels, s.recoveryFailures.Load())
			if c := s.conn.Load(); c != nil && !c.Session.IsClosed() {
				fmt.Fprintf(w, "super_paqet_peer_conversation_id{peer=%s,session=%s} %d\nsuper_paqet_peer_source_port{peer=%s,session=%s} %d\n", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), c.UDPSession.GetConv(), strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), s.network.Port)
				state := c.UDPSession.TransportStats()
				fmt.Fprintf(w, "super_paqet_peer_rto_ms{peer=%s,session=%s} %d\n", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), state.RTO)
				minWindow, maxWindow := c.Session.ReceiveWindowStats()
				if maxWindow > 0 {
					fmt.Fprintf(w, "super_paqet_peer_receive_window_min{peer=%s,session=%s} %d\nsuper_paqet_peer_receive_window_max{peer=%s,session=%s} %d\n", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), minWindow, strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), maxWindow)
				}
				fmt.Fprintf(w, "super_paqet_peer_send_window{peer=%s,session=%s} %d\nsuper_paqet_peer_pending{peer=%s,session=%s} %d\n", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), state.SendWindow, strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), state.Pending)
				packets, drops := c.PacketConn.PacketStats()
				fmt.Fprintf(w, "super_paqet_peer_tx_queue_retries_total{peer=%s,session=%s} %d\n", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), c.PacketConn.TXQueueRetries())
				fmt.Fprintf(w, "super_paqet_peer_tx_queue_drops{peer=%s,session=%s} %d\n", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), c.PacketConn.TXDrops())
				fmt.Fprintf(w, "super_paqet_peer_capture_packets{peer=%s,session=%s} %d\nsuper_paqet_peer_capture_drops{peer=%s,session=%s} %d\n", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), packets, strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), drops)
				fmt.Fprintf(w, "super_paqet_peer_streams{peer=%s,session=%s} %d\nsuper_paqet_peer_rtt_ms{peer=%s,session=%s} %d\n", strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), c.Session.NumStreams(), strconv.Quote(name), strconv.Quote(strconv.Itoa(i)), c.UDPSession.GetSRTT())
			}
		}
		p.mu.RUnlock()
	}
	e.tuneMu.Lock()
	packets := append([]observedPacket(nil), e.packetObservers...)
	e.tuneMu.Unlock()
	for _, observed := range packets {
		i, worker, packet := observed.index, observed.worker, observed.packet
		packets, drops := packet.PacketStats()
		fmt.Fprintf(w, "super_paqet_listener_tx_queue_retries_total{listener=\"%d\",worker=\"%d\"} %d\n", i, worker, packet.TXQueueRetries())
		fmt.Fprintf(w, "super_paqet_listener_tx_queue_drops{listener=\"%d\",worker=\"%d\"} %d\nsuper_paqet_listener_capture_packets{listener=\"%d\",worker=\"%d\"} %d\nsuper_paqet_listener_capture_drops{listener=\"%d\",worker=\"%d\"} %d\n", i, worker, packet.TXDrops(), i, worker, packets, i, worker, drops)
	}
}
