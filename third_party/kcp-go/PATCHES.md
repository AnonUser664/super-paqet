Based on github.com/xtaci/kcp-go/v5 v5.6.72; MIT license retained.

* Linux batch I/O accepts a PacketConn implementing WriteBatch/ReadBatch,
  allowing raw TCP datagrams to use sendmmsg/recvmmsg without UDP encapsulation.
* ACK lookup indexes the send ring by sequence offset, including wraparound.
* New-data flushes visit only newly queued segments. Timer flushes and fast
  retransmission still visit outstanding segments. ACK-only flushes do not move
  queued data into the send window.
* Batch receive coalesces immediate ACKs until the end of an already available
  batch. No timer delay is added to singleton packets. Out-of-order ACKs and
  timer-driven retransmission are retained.
* Coherent per-session delivery/RTT/window counters support adaptive control.
* The encryption/send FIFO grows on demand within the current send-window
  budget instead of dropping bursts at a fixed 2048-packet channel. Empty
  sessions start with 64 metadata entries. Overflow is counted separately from
  network loss. Shutdown rejects new work and drains already queued packets.
* A bounded minimum-RTO setter supports the engine's RTT-aware timer floor;
  ACK-based fast retransmission remains independent.
* Pre-Accept session admission bounds allocation; default zero preserves upstream.
* Upstream tests no longer start an unsolicited public pprof server.

KCP packet format, encryption derivation, segment ordering and FEC encoding are
unchanged. Scheduling and ACK cadence change intentionally and need impairment
regression tests, not just throughput tests.
