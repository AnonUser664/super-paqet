# Selective exploration integration review — 8 October 2026

Production is unchanged. Review is in `exploration/production-tuning-20261008`;
source worktrees and their completed/interrupted artifacts are preserved.

## Source identity and scope

Exp2 is commit `24b772c75511349fbbf54a1c47888126c6c13b31` in
`/home/shayan/Codes/super-paqet-exp2`, based on `2be348d`.
Its report gives component microbenchmarks and suite claims; these are not
end-to-end proof of a faster production tunnel. We independently qualify the
selected implementation.

Exp1's supplied root is an unchanged master checkout. Its actual unfinished
implementation is in `/home/shayan/Codes/super-paqet-opt-explore-20261008`, an
exp1-owned worktree on `exploration/perf-optimization-20261008`, based on
`2be348d`. Three uncommitted KCP files attempt multi-segment stream reads.
No new tests or experiment report were present. We do not edit that worktree.

## Review findings before qualification

| Proposal | Review decision / reason |
|---|---|
| Exp2 wider checksum accumulation | Candidate; independently check all short lengths, large odd payloads and packet byte shape. |
| Exp2 direct TCP flag construction | Candidate; equivalent bits, but the old temporary literal does not by itself prove a heap allocation. |
| Exp2 output FIFO batch pop / buffer-vector reuse | Candidate after clearing consumed request/vector references; borrowed arrays must not retain recycled packet storage while idle. |
| Exp2 binary endpoint key | Candidate with endpoint identity tests; preserve IPv4/mapped IPv6 normalization, IPv6 zone and arbitrary address fallback. |
| Exp2 skipping payload snapshots without deadline/context | Reject as written. Session close and socket-write failure can still return while carrier output owns a caller's buffer. Existing timeout/cancellation ownership must remain. |
| Exp2 embedded credit payload | Candidate after unlinking stale neighbor references; a dispatched entry must not retain the entire pending credit list. |
| Exp2 larger initial adaptive stream window | Defer. It changes memory/admission behavior and advertised capacity; needs separate evidence for bandwidth and high-connection memory tradeoffs. |
| Exp2 removal of TCP MSG_PEEK | Candidate; retain immediate buffer return on EAGAIN/EOF and cancellation/half-close semantics. |
| Exp2 small UDP datagram combined write | Candidate; validate short writes, empty/maximum boundaries and allocation cost. |
| Exp1 stream read batching | Complete as whole-segment draining. Do not retain `seg.data[copied:]`: that loses original pooled storage capacity and makes recycle fail. Preserve message mode and the existing short-buffer contract. |

Every accepted runtime change still requires deterministic correctness tests,
race checks and matched-binary workload comparisons. This document will retain
rejected experiments and the final decision rather than replacing them with only
favorable results.


## Ownership corrections retained during integration

The raw exp1 patch is preserved privately with SHA-256
`77db922f286907dfa96b4370b70c1f794febe5e64906e2743a36a418146ad7f3`.
Its proposed partial-segment slicing loses the original pooled slice capacity;
`bufferPool.Put` explicitly rejects such a tail. Whole-segment batching avoids
adding offsets/storage fields to every KCP segment and preserves `Recv`'s -2
short-buffer error. Small `UDPSession.Read` calls retain the existing receive
scratch path. Session reads must return the actual drained byte count.

The selected exp2 subset additionally clears local batch/vector references when
packets are consumed/recycled, clears an unlinked credit entry's neighbors, and
uses pooled scratch for combined small UDP records. Passing an array through
`io.Writer` is not a guarantee of stack allocation. Wider checksum checks cover
all lengths 0..256, large carry-heavy/odd payloads through 65536 bytes, eight
alignments and three patterns against an independent byte-pair oracle.

Full root race/vet checks passed. Focused KCP race checks passed in 156.000 s,
including all four plaintext/CFB/Salsa20/AEAD 100 MiB echo tests with FEC,
batch queue concurrency and shared endpoint routing. The full smux race suite
is in progress; no performance or deployment acceptance is inferred yet.


The full selected smux race suite passed in **545.290 s**, and its vet check
passed. An isolated copy of exp2's original snapshot-elision code ran the new
`TestNoDeadlineWriteOwnsPayloadDuringClose` and failed deterministically:
`early return retained caller bytes: "recycled"` (0.002 s). This is a reproduced
correctness failure, not merely a speculative objection. The selected runtime
continues snapshotting queued payloads and passes the same test.

The completed exp1 read implementation has no new segment field or ring
`PushFront` method. Focused race checks passed in 28.688 s, including whole
segments, small session reads, full-window reopening, preserved fragmented
message boundaries, the original 36,000-operation receive oracle, deterministic
virtual links and duplicated traffic. Its full KCP suite and end-to-end
performance comparisons are still pending at this checkpoint.


The completed KCP suite passed in **140.709 s**; combined root race/vet and
fork vet checks passed. The receive-batch microbenchmark, which excludes the
session mutex and network reads, was slower (median 2592 → 2821 ns per 32
segments, +8.8%, identical allocations). It is retained as an adverse result:
we must measure the intended session/read benefit end-to-end rather than claiming
that fewer calls automatically improve every layer. Small combined UDP records
use **0 B/op, 0 allocs/op** after pool warmup; the relay benchmark also reports
zero allocations. Final runtime acceptance is still pending matched binaries.


## Completed exp1 decision: exclude from selected runtime

Sixteen isolated workloads compared selected exp2 (`c1aee36`, SHA-256
`2962659362143ade1aec169f06991000b2f49e06f791d5adeb2a70db50831ee2`)
with the finished exp1 addition (`820ca5f`, SHA-256
`0b0de506f3bdd3e1583c346c2b846c4a39a1969e74e204a833d88c368f0d087a`).
Two alternating pairs used 20 measured seconds, 60 seconds of iperf/HTTP warmup,
fixed role CPUs, four pinned carriers, identical adaptive settings and debug
logging. All payloads verified, all workloads had zero errors and all cleanup
passed. The interruption did not stop this matrix; its complete receipt survives.

| Median result | Selected exp2 | Exp2 + finished exp1 |
|---|---:|---:|
| Clean upload, Gbit/s | 4.546 | 4.399 |
| Clean download, Gbit/s | 3.291 | 3.346 |
| HTTP churn, requests/s | 13,057 | 13,008 |
| Saturated mixed HTTP, requests/s | 3,888 | 3,910 |
| Saturated mixed HTTP p99 bound, ms | 9.5 | 10.5 |
| Paced mixed HTTP mean, us | 1,140 | 1,309 |
| Paced mixed HTTP p99 bound, ms | 8.5 | 8.5 |

Both mixed cases held about 1 Gbit/s in each bulk direction. Clock observations
are retained and still show some role variation; these small finite cohorts do
not prove a universal regression. However, they do not demonstrate the required
overall benefit. Conservatively exclude stream-read batching from the selected
runtime. The completed implementation/tests remain in
`archive/completed-exp1-stream-batching-20261008`; the original exp1 worktree and
its uncommitted edits remain untouched. No `PushFront` or partial pooled slice
is imported. [Experiment receipts](integration-experiments-2026-10-08.json)
include adverse microbenchmarks, the reproduced snapshot failure and full suite
results. The selected exp2 subset still needs its final baseline comparison.


## Final selected-candidate gates

The independent final comparison uses receive.4 as control (the qualified
receive-only source) and selected exp2 with packed cached pressure as candidate.
Both use identical adaptive transport settings, four independent sources,
S outbound / PA return, fixed role CPUs and the same steady HTTP helper.
The helper hash is retained in each new receipt. Correctness/cleanup must pass
for every workload. Clean bulk and churn must show no material repeated loss;
mixed tests must retain offered bulk and demonstrate acceptable latency at both
saturation and a fixed per-worker pause. Clock variation remains visible.

The completed exp1 candidate is already excluded rather than retested until a
favorable result appears. Final WAN, migration, live-reload and held-flow checks
will test the selected executable after healthy-link gates. Root/fork suite
results do not replace those end-to-end checks. No customer deployment, CPU power
policy, host-wide queue tuning or Xray modification is included in this review.


One selected-candidate paced result had p99 28 ms versus control 14 ms;
last-20-s role clocks were 2.30/2.28 GHz versus 2.78/2.81 GHz. The next pair
measured mean 1381 versus 1411 us and p99 8 versus 7 ms. Preserve both. Two
additional unchanged paced pairs are defined before launch: inspect whether
role clocks match within 5%, require zero errors and offered bulk retention,
and assess mean/rate within 5% and p99 within a 2 ms histogram allowance for
matched-clock pairs. All unmatched receipts remain visible; no retrospective
configuration tuning or claim that clock variation proves code equivalence.

The second primary pair was interrupted during one bulk attempt. Its first
8 receipts survived; empty owned namespaces `spq-c-32941`/`spq-s-32941` were
recovered and deleted under the reservation, then the missing 8 comparisons
completed in `selected-resumed`. The partial original directory is excluded.
The interruption-cleanup script's unrelated-rule heuristic was insufficient;
that field is not used as independent preservation evidence. Completed normal
fixtures check the exact unrelated UDP/31111 rule. Further tests use a private
local transient systemd job plus the same nonblocking reservation, so terminal
interruption cannot invalidate a workload. No production service is touched.


The completed primary selected-candidate two-pair medians are upload
4.330 → 4.431 Gbit/s, download 3.189 → 3.341 Gbit/s, churn
12,649 → 12,649 requests/s, and saturated mixed rate 3671 → 3784 requests/s.
Mixed p99 bounds are 12 → 7 ms with about 1 Gbit/s offered in each bulk
direction. All 16 completed primary workloads report zero errors and exact-rule
cleanup success. The paced primary aggregate is worse because it includes the
low-clock candidate outlier; it remains visible and is not a pass claim.
[Primary qualification receipts](integration-qualification-2026-10-08.json)
retain this pending checkpoint. Confirmations and WAN/functional tests are
still running; no master merge or deployment acceptance has occurred.


The 24 short WAN runs all passed byte verification and cleanup. Two-pair
mobile p99 bounds were 529.5 → 676.5 ms while HTTP rate changed
27.732 → 26.865 requests/s and bulk 1.530 → 1.486 Mbit/s. The short
four-worker test supplies only a few hundred HTTP samples per run; preserve the
adverse tail result and extend the unchanged profile to two alternating pairs
of 60-second HTTP/bulk workloads before deciding. This is extra measurement,
not a changed loss/queue/transport configuration. High-delay and asymmetric
p99 changes were about +4% and +2%, respectively; reorder bulk improved in this
cohort. No claim of universal link superiority follows from these samples.

One-pair multiworker rates were 5.110 → 5.241 Gbit/s upload and
3.965 → 4.373 download. AES-GCM rates were 3.366 → 3.601 upload and
2.981 → 2.974 download. These single-pair figures are supplementary capacity
checks, not repeated throughput guarantees. Both 10,000-flow holds reported
zero errors; tunnel peak RSS was about 206.6/201.6 MiB client/server for the control
and 204.2/196.9 MiB for the candidate (the receipts retain KiB units). No swap
occurred. Held flows are predominantly idle, not 10,000 busy customers.


The first longer mobile attempt failed on **receive.4 control**, after its
60-second HTTP phase completed 1653 requests, zero errors, p99 585 ms. The
subsequent 60-second four-worker 1 MiB-body phase hit three `body_timeout`
errors at the benchmark client's fixed 30-second per-request deadline. Cleanup
passed. This is retained as a failed workload and not erased by a later pass.
It does not establish a candidate regression (the candidate had not run).
A dedicated `mobile-http` case extends only the small-request measurement,
retaining the same delay/loss/rate/queue/seed; short bulk throughput results and
the longer bulk deadline limitation remain documented separately.

The alternate-backend fixture failed an incorrect expectation: it treated every
`--server-binary` as lacking preservation, despite all four streams surviving and
`connections_preserved=true`. Cleanup passed. Add explicit
`--expect-backend-preservation` for known capable alternate executables while
keeping the historical legacy-fallback default. The corrected run must also
assert unchanged logical conversation IDs and retained stream bytes. The original
failed assertion remains in the ledger; this change affects only the test runner.


## Final selection and qualification

Merge the selected exp2 subset and packed cached pressure representation.
Retain owned write snapshots and the original initial adaptive receive window;
exclude the completed exp1 batching experiment. Outer packet encoding, S/PA
flags, source-port migration and application byte ordering are preserved.
There is no new deployment YAML field or transport parameter retuning.

The full longer mobile HTTP comparison measured two-pair medians
28.000 → 28.308 requests/s, mean 142.716 → 141.069 ms and p99
583.5 → 525.5 ms. The original shorter adverse tail and failed bulk control
remain retained. Four-pair paced medians, including the original low-clock
outlier, favor the candidate slightly; one confirmation control has no complete
clock record, and the fully clock-recorded confirmation pair matches within 5%
and improves p99 11 → 7 ms. These bounds are histogram buckets.

Final KCP suite passed in **145.113 s**. Fresh application race and all owning
module vet checks passed; the selected smux full race suite previously passed
in 545.290 s and its runtime did not change afterward. Endpoint-key benchmarks
report zero allocations. Four recovery/compatibility scenarios preserve all
four sequenced 64 KiB streams, retain healthy siblings and clean up exactly;
one uses the older receive.4 backend with explicitly required preservation.
The 70-second delayed-probe scenario exercised negotiated recovery grace.
Live reload passed with 32 active streams per original peer, four carriers and
three edit cycles; its wire and unrelated-rule cleanup checks pass.

[Final receipts](integration-qualification-2026-10-08.json) retain 54 successful
workload comparisons, four successful fault/compatibility scenarios, live reload,
all executable identities and failed/interrupted attempts. Exp1's separate
16-workload rejection is in the experiment ledger. Component improvements and
healthy throughput gains support this selective source merge; they do not
establish superiority on every link or busy-customer capacity. Production
continues running migration.4; these native local artifacts are not a Debian
fleet rollout. Review the preserved failures before any further deployment.


The private test units and observer are now inactive, the machine reservation
is released, and no owned namespace remains. Failed-unit state was cleared;
the successful HTTP unit had already been garbage-collected by systemd. The
clean temporary exp2-control worktree was removed; its commit, exact executable,
all result/profile files and the original exp1/exp2 worktrees remain preserved.
Final production-runtime Go sources match tested commit `c1aee36` exactly;
subsequent edits are test-runner expectation fixes and qualification documents.

## Subsequent two-worker coverage audit

The [capture-worker audit](MULTIWORKER-2026-10-08.md) proves both workers handled
traffic in 36 of the original 54 comparisons. It adds native and race-instrumented
two-worker live reload plus eight repeated mixed-load comparisons. Correctness,
Go race detection and cleanup pass; saturated mixed results favor the candidate.
However, paced mixed p99 rises **6 → 10 ms** across the new two-pair medians,
including a matched-clock first pair that fails the earlier latency allowance.
This new evidence leaves a performance gate unresolved before deployment.
Runtime sources and production remain unchanged. The
[supplemental receipts](multiworker-qualification-2026-10-08.json) preserve both
adverse comparisons and failed fixture/launcher attempts.

The [direct-read follow-up](LATENCY-2026-10-08.md) selects source `8f1ca0f` and
clears that local latency allowance. It removes the queued-byte ioctl rather
than reverting output batching, adapting pooled scratch between the existing
4–64 KiB classes. Forty follow-up workloads include paired clean bulk,
representative WAN, churn and held-flow checks; native/race two-worker live
reload and application race/vet pass. All adverse samples remain visible.
Production remains migration.4; source integration is not backend acceptance.
