# Opening and local queue investigation — 9 October 2026

This is a **local candidate**, not a production deployment or completed
qualification. Production remains latency.1. Its
[17.6-hour review](PRODUCTION-REVIEW-2026-10-09.md) identified ten opening errors
coincident with Finland receive backpressure and 45,876 local transmit queue drops.

The existing cached admission hint handles a lane already full at selection.
It cannot free an opening that was selected earlier and received ACK2 before
another stream filled that carrier's ordered mux receiver. Blindly retrying that
acknowledged request could dial the target again. Increasing buffers would only
move the same boundary and could increase memory and latency.

The candidate introduces PTCP3/PUDP3 inner requests with a 16-byte random identity.
One listener-generation registry retains one target dial across retry attempts.
The winning stream sends a one-byte commit before target relay reads begin.
Failed/uncommitted carrier streams therefore cannot consume target banners or
close a target being reused by another attempt. A committed identity remains a
replay tombstone for the original server opening budget. No established relay is
stored in this registry. A failed commit is never retried because the write might
already have reached the server. Target rejection is never treated as a physical
path failure. Legacy PTCP2/PUDP2 handling remains for older clients; new clients
require upgraded backends first.

Only unfinished acknowledged openings check their receipt read at most every
250 ms. A full receiver with an eligible sibling causes a same-identity retry;
a healthy slow target retains its whole dial budget. Existing carrier selection,
outer raw TCP encoding, S/PA flags, KCP/mux data ordering and established relays
remain. Pre-receipt EOF/closed-pipe failures also retry an eligible sibling.
Capacity retries do not add tuple suspicion or trigger source migration merely
because a consumer is slow. Final failure messages include receipt stage,
conversation and local receive-buffer state.

Receipt storage is capped at twice the configured connection admission ceiling.
One engine timer and expiry heap retire pending targets and replay tombstones;
there is no retained timer or goroutine per committed relay. Registry occupancy,
pending targets, capacity retries and reused targets have explicit metrics.
This adds handshake bytes and transient per-opening memory/locking, so churn,
CPU/RAM and throughput qualification are mandatory before any rollout.

Initial whole-application race tests pass. Five race-instrumented repetitions of
the focused tests verify a receiver filling **after ACK2**, one healthy-sibling
retry, exactly one target dial, identical winning relay bytes, original held
stream payload continuity, no tuple-suspect flag and owned handler cleanup.
Registry tests also cover conflicting identities, one-time claim, delayed replay,
capacity, expiry, failed dial replay and late dial completion after retirement.
Receipts are in this worktree's `build/opening-fix-20261009/`.

Queue investigation remains open. The namespace fixture can now install an
isolated root fq with explicit per-socket flow_limit/maxrate, excluding unrelated
netem schedules. This provides an actual Linux ENOBUFS reproduction without
changing a host or production qdisc. No queue fix or benchmark improvement is
claimed at this checkpoint.

## Second checkpoint: queue-pressure candidate

The first baseline fixture attempt stopped at argument validation because the
new worktree lacked the local iperf helper. No workload ran; its failure receipt
remains. Copying the unchanged helper into this worktree allowed the reproduction.
The exact deployed binary (`256cfc65…`) produced 23,354 backend transmit drops
with fq flow_limit 100 and maxrate 50 Mbit/s per flow. This is an isolated socket
queue limit, not a claimed 50 Mbit/s aggregate WAN.

A local candidate shrinks only wholly rejected sendmmsg prefixes on ENOBUFS:
64 → 32 → 16 → 8, with the existing three waits and no healthy-path delay.
Partial sends return immediately. Persistent pressure discards/counts only the
last rejected prefix; KCP's existing batch cursor retains the untouched tail.
No accepted datagram is replayed and no queue error tears down a carrier.

The first candidate run (`c46cda9a…`, opening plus queue change) produced
12,228 drops and 120.30 Mbit/s download versus the baseline's 94.85 Mbit/s.
Client/server mean CPU was 0.147/0.204 cores versus 0.172/0.190. Cold 16 MiB
transfer was 3.486 s versus 3.276 s: retain this adverse startup result. These
single runs are exploratory, not acceptance or a production capacity claim.
The pressure branch can spend more aggregate time sending a rejected batch's
tail, so mixed latency and repeated comparisons remain required. No pacing,
outer encoder, flag cycle, socket ownership or production qdisc is changed.

Listener-generation cleanup now cancels reservation and removes its unfinished
targets immediately, rebuilding the expiry heap only on retirement. Other
listeners and relay-owned sockets remain independent. Race tests cover
concurrent claims (one winner among 32), reload retirement, a healthy 600 ms
target dial with no retry, and a target banner surviving late receive blockage.
Three repetitions of the expanded opening race cases and the socket race suite
pass. Whole-suite and namespace performance qualification remain pending.
