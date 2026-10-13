# Final production tradeoff review — 13 October 2026

This review prioritizes reliable operation on the deployed WAN over a one
millisecond local percentile difference. It does not waive data integrity,
resource bounds, recovery continuity or correct cleanup. Candidate builds and
production are distinguished below; a source commit alone is not a deployment.

## Scope and release decision

Review and finite qualification are complete; final.1 is deployed. Earlier
sections below retain the investigation and previous primary opening.10,
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

## Selected implementation, pending final qualification

Retain arrival-order ACK timing, the existing adaptive pacing controller and
the 200 ms learned reordering ceiling. The aggressive alternative remains an
experiment: its Germany gain does not justify its weaker Finland result, and
the conservative correction is competitive on France. The final trailing
control and exact-build correctness/load checks remain required. Diagnostics
and observer improvements do not change carrier selection or packet layout.

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

## Longer comparison and final ablation

All 24 longer cases completed with zero transfer or warm-up errors and unchanged
primary identities. The [longer receipt](final-real-link-long-2026-10-13.json)
records CPU deltas and current memory, including missing peak-memory support on
older systemd. Germany controls varied 311.759 to 452.180 Mbit/s; France controls
373.994 to 337.281; Finland controls 308.746 to 340.501. The conservative candidate
measured 428.723 / 367.377 / 283.202 Mbit/s respectively. Its Finland rate is
12.75% below the bracketing-control median and body p99 532 versus median 457.5 ms;
this adverse production comparison is retained, not dismissed as a local gate.
The aggressive combined candidate was weaker on Finland (259.176 Mbit/s).

A final focused ablation combines arrival-order timing and pacing-feedback
retention with the existing 200 ms reordering ceiling. This separates the
feedback hypothesis from the wider grace. Selection remains provisional until
that comparison completes. Its engine race suite passes; the first remote
launch lacked a terminal and reached getpass EOF before connecting. That failed
attempt and its successful build are preserved. The corrected launch uses a PTY.

Source `37904d6` passes full root race/vet, observer guards, ten opening-phase
race repeats, three KCP timing/virtual-link race repeats and the full KCP suite.
The conservative executable is `enterprise-2026.10.13-final.1`, SHA-256
`df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`.
It remains a local candidate; native correctness does not qualify a production
rollout by itself. [Diagnostic definitions](PRODUCTION-DIAGNOSTICS.md) explain
new warning events, counters and interpretation limits.

## Settled tradeoff

The [focused twelve-case comparison](final-real-link-focus-2026-10-13.json)
completes without transfer or warm-up errors. Bracketing control median bulk is
443.766 / 350.376 / 336.501 Mbit/s for Germany / France / Finland. Arrival-order
alone measures 428.763 / 371.115 / 360.051; feedback with the 200 ms ceiling
431.075 / 363.148 / 331.583. The earlier Finland penalty does not repeat for the
conservative correction; its latest rate is 7% above the control median. This
variation prevents a causal speedup claim. The feedback change has no clear
aggregate advantage over the narrower correction.

Retain arrival-order eligibility, the existing adaptive controller and 200 ms
learned reordering ceiling. Both feedback variants and the larger grace remain
excluded. The selected runtime source and exact binary remain `37904d6` /
`df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`.
Full root race/vet, repeated phase/timing tests and full KCP native qualification
are complete. Exact-artifact impairment, integrity, recovery, reload and
real-host connection-load qualification are now running or pending. Production
still runs opening.10; source checkpoint pushes do not imply deployment.

## Exact selected-artifact local gates complete

[Qualification receipt](final-artifact-qualification-2026-10-13.json) records
full native passes and the exact binary's clean repeats, asymmetric loss/reorder,
narrow lossy HTTP, mixed HTTP/bulk, repeated/reverse established-stream recovery
and four-carrier/two-worker live reload. All eight candidate cases pass; owned
firewall cleanup and the unrelated sentinel are preserved. Recovery checks
verify complete sequenced 32 KiB payloads with the deployed 10-second stall
budget. Reload covers TCP/UDP and directional-close behavior.

Measured tradeoffs are retained: median clean upload 6.411 versus 6.866 Gbit/s,
download 4.942 versus 4.933; asymmetric useful delivery 4.628 versus 6.225 Mbit/s
with no errors; narrow-loss HTTP p99 578 versus 567 ms. Mixed mean is 60.618
versus 58.190 ms and p99 110 versus 249 ms. The valid-timing correction is
selected for correctness and its balanced real-link comparisons, not because
all local rates improved. The asymmetric rate gap remains a future optimization
opportunity; no claim of maximum performance on every link is made.

A real-host 20,000 held-connection plus three-country mixed-load check is now
running on isolated sidecars. Primary binaries/configs remain unchanged before
that check and rollout is still pending.

The first scale setup was interrupted before its fourth host staged and before
any target, tunnel or generator started: its validation path still named the
comparison control instead of the final binary. Interrupted SSH cleanup then
hung awaiting a request acknowledgement (the library's output timeout does not
bound that handshake). The uniquely identified owned staging process was
terminated, with the wrapper retaining its reservation until exit. No customer
unit was stopped. The corrected harness bounds both SSH acknowledgement and
output, uses the final validation binary and exact-unit journal queries, and
retains the interrupted-stage receipt separately. This is a harness failure,
not a successful or failed connection-load measurement.

## Real-host scale gate complete

[The selected binary's scale receipt](final-real-scale-2026-10-13.json) opens
20,000 forwards in 29.70 seconds, holds them for 60 seconds and verifies every
connection afterward, with zero errors. Three-country concurrent HTTP (32
workers each) and bulk (four workers each) complete with zero errors. Tunnel
peak RSS is 497.34 MiB client and 157.10 / 167.86 / 152.38 MiB backends. These
are held-and-verified connections beside finite mixed traffic, not 20,000 fully
saturated customers. Primary identities are preserved on all four hosts and
owned sidecar cleanup completes without errors. Baseline authenticated Reality
and Shadowsocks route verification precedes the protected binary-only rollout.

## Application baseline and dependency exclusion

Before any binary promotion, Germany/France Reality and all three Shadowsocks
routes each passed two authenticated 1 MiB downloads. Finland Reality failed on
the deployed opening.10 binary. Its forward correctly targets 65.109.249.222:443
and the backend listens there. Wikipedia SNI repeats fail, as do direct backend
loopback probes using both live configuration-derived credentials and the
previously supplied client profile. Those loopback tests contain no tunnel.
This establishes an independent service/probe dependency failure, not its exact
cause or the status of every customer credential. No Xray/Nginx configuration or
service was changed to mask it.

The binary rollout checks the five working authenticated routes for regression
and retains every Finland Reality failed row under an explicit, preexisting
exclusion. It must not be described as six healthy authenticated application
paths. The tunnel itself passed payload/scale checks on all three countries.
Final acceptance is scoped to the tunneling runtime; Finland Reality still
requires an operator/service follow-up.

## Final deployment complete

All four active hosts run the selected exact binary with healthy enabled
services, zero automatic restarts and matching disk/running hashes. Config and
unit hashes and Xray/Nginx identities remain unchanged. Backend-first replacement
then independently scheduled Finland/client restarts prevent administrative
connectivity loss from blocking recovery. Established customer connections
reconnected during the update. Rollback protection was disarmed only after
five working authenticated routes passed twice each and all final identities
verified. Finland Reality failed again as it had before and on backend loopback;
that exclusion remains explicit in [the rollout receipt](final-deployment-evidence-2026-10-13.json).

The final decision is closed: retain the corrected arrival-order RTT sampler,
existing adaptive pacing, 200 ms learned reorder ceiling and current four-lane
configuration. Correctness, finite real-load checks and incident visibility
justify this release; neither universally highest throughput nor a repaired
Finland Reality service is claimed. Production observer refresh, owned scratch
cleanup and final source integration/push complete the operational handoff.

The [finite observers](final-observer-evidence-2026-10-13.json) are refreshed
on all four hosts with the new phase counters, retaining the existing archive,
deadline, summary history and journal cursor. Updated script hashes, new samples,
qdisc/softnet/host scheduling evidence and unchanged tunnel identities verify.
Client collection continues until 19 October 14:30 UTC; backend collection until
19 October 08:01 UTC. Observer restart does not restart the application.
