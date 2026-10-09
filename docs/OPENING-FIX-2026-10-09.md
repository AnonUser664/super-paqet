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
