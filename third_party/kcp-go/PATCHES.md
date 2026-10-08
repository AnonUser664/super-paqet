Based on github.com/xtaci/kcp-go/v5 v5.6.72; MIT license retained.

* Linux batch I/O accepts a PacketConn implementing WriteBatch/ReadBatch,
  allowing raw TCP datagrams to use sendmmsg/recvmmsg without UDP encapsulation.
* ACK lookup indexes the send ring by sequence offset, including wraparound.
* Fast-gap evidence walks an allocation-free list of outstanding sequence
  numbers rather than acknowledged send-ring tombstones. Selective/cumulative
  ACKs unlink in constant time; ring growth and sequence wrap retain numeric
  links. The linear algorithm is the deterministic differential-test oracle.
* Ordered receive segments enter the reader FIFO directly when it has room.
  Reorder draining inspects the heap root before removing it, so unresolved gaps
  and full reader queues do not allocate a pop/reinsert pair. Payload ownership,
  duplicate detection, window/sequence advancement, fragmentation and ACK output
  match the original heap-only algorithm in deterministic differential traces.
* Explicit shared-source mode demultiplexes by address and conversation, so
  independent lanes retain one raw source tuple. Outgoing groups reject unknown
  input. Default address/reset behavior and FEC are unchanged; shared mode rejects
  parity-only FEC because those frames cannot identify their conversation.
* New-data flushes visit only newly queued segments. Timer flushes and fast
  retransmission still visit outstanding segments. ACK-only flushes do not move
  queued data into the send window.
* Batch receive coalesces ACKs. An optional 1..20ms engine-controlled deadline
  survives application data flushes; low-RTT paths retain immediate ACKs.
  Out-of-order ACKs and timer-driven retransmission are retained.
* Coherent per-session delivery/RTT/window counters support adaptive control.
* The encryption/send FIFO grows on demand within the current send-window
  budget instead of dropping bursts at a fixed 2048-packet channel. Empty
  sessions start with 64 metadata entries. Overflow is counted separately from
  network loss. Shutdown rejects new work and drains already queued packets.
* A bounded minimum-RTO setter supports the engine's RTT-aware timer floor;
  ACK-based fast retransmission remains independent.
* Pre-Accept session admission bounds allocation; default zero preserves upstream.
* Upstream tests no longer start an unsolicited public pprof server.

* Optional encrypted eight-byte ACK receive/emission timestamps estimate forward/reverse
  transit relative to recent minima without synchronized clocks. Legacy ACKs
  fall back to RTT; wraparound and mixed-enabled interoperability are tested.
* Optional KCP WINS payload (SPQ1 + stream ID/consumed/window) expedites
  cumulative stream credit. Application bytes remain reliably ordered by KCP.
  The mux retains ordinary reliable UPD fallback and validates stale/future hints.
  Hint callbacks run after releasing the carrier lock, including FEC input.
* Data pacing exempts ACK/window control. Deferred paced writes bring the shared
  timer wake forward; superseded callbacks do not create extra update chains.
* Data-frame budget follows send-window bytes and pacing rate, with a full-MSS
  floor and configurable batching time. Receive/enqueued/ACK packet counters
  distinguish tiny control messages from bulk and expose real pending bytes.
* Only initial PUSH data can replace a mismatched carrier generation. Unknown
  control cannot create sessions; late control cannot reset a newer session.

The outer Ethernet/IP/TCP byte contract and encryption derivation are preserved.
The ACK/WINS extensions change encrypted inner control payloads, not application
ordering or FEC coding. Scheduling/ACK cadence intentionally change and require
impairment regression tests. The full upstream suites remain required.


* Selective exploration integration batches bounded output FIFO pops and reuses
  packet buffer-vector descriptors instead of allocating one per output packet.
  Consumed request/vector references are cleared before idle wait. Packet order,
  FEC/crypto stages, queue ceilings and shutdown draining remain intact.
* Valid UDP-shaped endpoint metadata uses a copied binary address/port key,
  including IPv6 zones and conversation ownership. Generic/invalid metadata
  retains a string fallback. This changes dispatch allocation cost, not the raw
  TCP wire endpoint or source-port/flag lifecycle.
* A completed stream-read batching experiment is archived separately after
  failing to demonstrate an overall end-to-end benefit. The selected runtime
  retains the previous receive/read contract and introduces no pooled tails.

These integrated candidates and their end-to-end qualification decisions are
tracked in `docs/INTEGRATION-REVIEW-2026-10-08.md` in the parent application.
They are not a production deployment.
