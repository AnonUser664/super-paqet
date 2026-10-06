Based on github.com/xtaci/smux v1.5.53, MIT license retained.

Local changes:
* Optional directional FIN (`Config.HalfClose`, `Stream.CloseWrite`). Default
  remains the upstream full-close behavior. Both enterprise endpoints enable
  directional FIN. Outer smux framing is unchanged, but semantics differ.
* Start per-stream receive rings at 2 entries; grow on demand. Avoid eight
  preallocated entries for every idle stream.
* Small opening/control messages may use the existing priority class before
  application data. Flow-control accounting increments before enqueueing, so a
  fast reader's consumption update cannot outrun the writer's byte counter.
* Upstream tests no longer start an unsolicited public pprof server.
* Optional receive-window adaptation uses stream drain rate and KCP RTT, with
  bounded growth/reduction and explicit stream/session ceilings. Idle streams
  retain no timer. Window updates use the current advertised threshold, not the
  maximum ceiling, to avoid stalling growth at the initial window.

* Optional HalfClose full-close/reset command (cmdRST=5) distinguishes abandoning
  both directions from CloseWrite's directional FIN. It unblocks peer writers
  waiting on stream credit, preserves already received ordered data, and avoids
  retaining abandoned streams after target errors. Both HalfClose endpoints must
  use this extension; the default upstream mode still sends ordinary FIN.

Tests must cover upstream behavior and half-close with backpressure, ordered
delivery, cancellation and deadlines.

* Optional asynchronous coalesced receive credits let readers drain independently
  of opposite-direction send backpressure. One queued credit per stream, close
  unlink, no per-stream timer/goroutine. Cumulative zero survives 4GiB wrap.
* Optional KCP WINS hints retain reliable UPD fallback; duplicates/stale/future
  consumption cannot rewind credit or advertise unsent bytes.
* Optional global control priority has a bounded 16-frame burst before data;
  data frames follow the current carrier window/rate budget.
* Atomic CloseIfIdle excludes stream registration while retiring a carrier.
* An asynchronous carrier write error closes the session, clears pending work
  and wakes blocked readers. Fault/integrity/race coverage exercises this path.

* Accepted stream wrappers no longer have a GC finalizer. The session owns the
  backing stream and callers explicitly close it. Promoted methods retain the
  backing object without necessarily retaining the wrapper; its finalizer could
  therefore close live reads/writes. A forced-GC regression covers that lifetime.

* Parse mux control headers/UPD/FIN/reset feedback while the application receive
  budget is exhausted. Payload allocation still waits for data tokens before
  reading its body. This prevents reverse credits from depending on unrelated
  application readers. Fixed-frame ordering still applies; this is not an
  unordered transport. Duplex-starvation and payload-bound regressions cover it.
* `ReceiveBufferStats` exposes shared capacity, buffered bytes and whether a
  parsed payload is waiting for tokens, without scanning per-stream buffers.
* Publish outgoing stream receive ownership before SYN write completion. A
  full-duplex transport may deliver early peer data/credits before Write returns;
  those frames must not be discarded as belonging to an unknown stream. Pending
  opens also count as live users for CloseIfIdle. A failed opening reclaims early
  receive data/credits. Deterministic duplex-completion regressions cover both.

* Queued writes own pooled payload snapshots and a two-reference completion
  lease. A returned timed-out write cannot retain the caller's scratch buffer;
  abandoned queued frames are skipped and their pre-reserved stream credit is
  refunded. In-flight writes may still complete and retain their reservation.
  Sender shutdown drains bounded queues; storage/channel reuse waits for both
  producer and sender, including late abandoned results.
* `OpenStreamContext` applies a caller budget to SYN submission without changing
  shared-carrier deadlines. `Abort` releases local ownership immediately with
  bounded best-effort FIN/RST admission; ordinary `Close` retains graceful/default
  semantics. Failed-opening reset queue overflow and skipped writes are observable.
