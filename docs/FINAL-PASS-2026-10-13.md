# Final production tradeoff review — 13 October 2026

This review prioritizes reliable operation on the deployed WAN over a one
millisecond local percentile difference. It does not waive data integrity,
resource bounds, recovery continuity or correct cleanup. Candidate builds and
production are distinguished below; a source commit alone is not a deployment.

## Scope and release decision

Qualification is in progress. The primary services still run opening.10,
SHA-256 `22b2f1ab66b625403048cc52743fb811ece853b2c84c771932c4e66de9091f7c`.
The active topology is client 94.139.180.109 and Germany, France and Finland
backends. The old .14/.118 clients remain stopped. Application destinations are
now backend Reality port 443 and Shadowsocks port 8880; earlier port-2096 and
five-host descriptions are historical.

The minimal release branch starts from the deployed transport, without the
experimental cached output callback or per-carrier socket factory. Every
comparison uses four distinct client source ports, S outbound / PA return,
null encryption, MTU 1350 and the deployed adaptive/manual 0/30/2/nc1 profile.
Customer binary/configuration identities are checked around isolated sidecars
on raw port 29998; primary raw port 29999 remains unchanged.

## ACK timing alternatives

A retired duplicate ACK does not represent a new round trip. Previously it
could inflate a 100 ms smoothed RTT to 1337 ms and the retry timeout from 300 to
11385 ms in a deterministic reproduction. The correction examines live
transmission state before cumulative ACK processing retires that state. It
excludes unsent, retired and already acknowledged segments, rejects impossible
future echoes and retains delayed original-attempt evidence for high-delay
bootstrap. Ordinary ACK delivery, packet layout and outer flag behavior remain
unchanged.

The first correction selected the newest eligible echo within a datagram. A
production-transport ablation on a 50/10 Mbit/s link with 40 ms one-way delay,
0.5% loss and 5% reordering reduced median useful delivery from 5.969 to
4.190 Mbit/s. Controls also timed out (2 and 6 failures), so this is neither a
clean successful baseline nor evidence of universal performance. The failed
candidate had 5 and 2 failures. Receipts are retained in
[the source screen](FINAL-SOURCE-SCREEN-2026-10-13.json).

A second correction retains the last eligible echo in ACK arrival order. This
preserves the deployed sampler's ordering while removing invalid timing
samples. Selecting the newest timestamp can bias RTT downward on batched or
reordered acknowledgments; the arrival-order candidate tests that hypothesis.
It is evaluated independently of the pacing change below.

## Adaptive pacing alternative

The feedback candidate avoids repeatedly treating delivery at the sender's own
reduced pacing budget as evidence that physical link capacity fell again. It
still learns sustained delivery well below that budget after recent peaks
expire. It also raises the learned reordering allowance ceiling from 200 to
750 ms; this is a ceiling, not a fixed delay imposed on every retransmission.

On the source screen it delivered 5.865/6.208 Mbit/s with 4/0 failures: better
than the newest-echo correction alone, but not sufficient for acceptance. Older
compound experiments mixed this change with a different output callback; their
results do not qualify this isolated implementation.

## Real-link comparison

All 30 first-round HTTP/bulk cases completed without transfer errors. On the
four-worker 1 MiB bulk test, arrival-order plus pacing measured 126.365 Mbit/s
Germany and 130.910 Mbit/s France, versus repeated-control medians 98.956 and
118.100. Finland measured 100.637 versus 107.242 Mbit/s and had one HTTP p99
spike to 402 ms. Arrival-order alone was more consistent across the three
countries. The ambient link changes substantially between repeated controls;
these finite samples do not establish causality or sustained gigabit capacity.

[Exact first-round results](final-real-link-screen-2026-10-13.json) retain binary
hashes, settings and failures. Primary identities were unchanged on all four
hosts. Sidecars were stopped; three post-stop journal reads timed out and require
bounded follow-up verification. A longer eight-worker comparison repeats the
control around both arrival-order finalists and records CPU and memory.

## Diagnostics and required completion

The candidate adds separate counters for opening SYN, receipt and target-result
waiting, plus premature closes. Warning-level `opening.retry_burst` events
summarize significant recovered retries once per existing observer interval.
They describe evidence rather than diagnosing filtering. Per-carrier metrics
export delivery, pending bytes, retry counts, RTO/variation and queue occupancy
from existing snapshots, without adding packet-processing counters.

Before promotion: settle the real-link tradeoff, qualify the exact selected
artifact, verify many established connections with mixed work, check owned
cleanup, and perform staged rollout with rollback. Current observer archives
remain bounded and are retained. The unexplained 17:47 UTC opening-retry cohort
is not claimed solved by these experiments; the added phase and queue evidence
makes a recurrence more diagnosable.
