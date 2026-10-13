# Branch consolidation and experiment decisions

The user requested one `master` branch and one working checkout, with useful
changes integrated and every tested approach retained before branch removal.
This consolidation changes documentation and local measurement tools. It does
not deploy or alter the production tunnel runtime/configuration.

## Branch disposition

| Former branch / checkpoint | Useful work retained | Runtime disposition |
| --- | --- | --- |
| `exploration/ack-budget-20261013`, `e4fb701` | Exact 100 MB ACK-cost harness, parser checks, clean/impairment captures summarized in receipts, capture readiness/drain/coverage checks | Measurement tools only; no transport changes |
| `exploration/production-20261012`, `e4fd461` | Full experiment journal, 49 additional historical reports/receipts, archived replacement-client config/unit, all source history | Selective merge retains the current runtime; rejected/unqualified writer and estimator changes are excluded |
| `release/production-20261013`, `2e6dd0c` | Already identical to pre-consolidation `master`; final qualification/deployment/cleanup evidence stays in the checkout | Deployed runtime source remains `37904d6`, executable SHA-256 `df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f` |

The selective historical merge records the old exploration head as a merge
parent while explicitly retaining current production source. All older source
commits remain reachable through `master`; no experiment is silently rewritten
as accepted. Historical reports are checkpoint records, not current deployment
instructions. Older deployment examples are archived rather than promoted.

## Tested approaches from the retired exploration branch

The [full 12 October journal](EXPLORATION-2026-10-12.md) preserves the exact
progression, failed attempts, executable hashes, native gates, matrices and
causal limitations. The table below is its decision index, not a replacement
for those receipts.

| Approach | What was tested / observed | Decision and reason |
| --- | --- | --- |
| Cached raw-send callback | Actual socket/race microbenchmarks removed three allocations; clean upload 5.776→5.974 Gbit/s. Ordinary pooled-pressure mean 49.573→120.384 ms and p99 104→212.5 ms failed. Pinned-placement supplementary gates passed. | Excluded from current runtime. Allocation gains and a favorable pinned comparison did not establish a safe whole-tunnel improvement. [Receipts](raw-callback-screen-2026-10-12.json), [pinned comparison](raw-callback-pinned-pressure-2026-10-12.json). |
| Guarded execution tracing | 250 ms traces exposed shared raw writer synchronization during backend download; harness trace duration/size were bounded. | Trace hypothesis and measurements retained. The older branch's special pprof trace-handler change is not imported into this documentation/tool consolidation; isolate and qualify it separately before using it in a new release. |
| Per-carrier private output sockets | TCP/UDP/half-close, sequenced migration, reverse recovery and two-worker reload passed. Clean download 4.399→5.183 Gbit/s; pooled pressure bulk -7.22%, mean +21.96%, p99 +27.06%. Equal offered work increased backend CPU 12.90% and p99 20%. | Rejected as a default. Experimental build-tag selection, typed-nil fix and writer lifetime/cancellation tests remain in historical source. [Screen](private-writer-screen-2026-10-12.json), [matched work](private-writer-matched-2026-10-12.json). |
| Shared-output scaffold plus cached callback | Functional/native checks passed; equal-work finite gates and clean rates passed. Asymmetric p99 worsened 16.90%; a longer callback-only run had four body timeouts. | Not promoted. Passing native checks did not clear the WAN failures. [Complete qualification](shared-output-qualification-2026-10-12.json). |
| Reordering ceiling 750 ms alone | Matched 200 ms controls had no errors; one 750 ms run had four body timeouts. | Rejected. Faster successful bodies do not compensate for timeouts. [Receipt](reorder-ceiling-screen-2026-10-12.json). |
| Pacing-feedback retention, 200 ms ceiling | Deterministic model reproduced learning the sender's own reduced pacing rate. Engine race checks passed; link controls/candidates still timed out and candidate tail worsened. | Hypothesis supported by a model, end-to-end acceptance unmet. [Native](pacing-feedback-native-2026-10-12.json), [link screen](pacing-feedback-screen-2026-10-12.json). |
| Pacing retention plus 750 ms ceiling | Diagnostic/unprofiled impaired-link goodput improved roughly 25.6%; narrow-loss HTTP passed. Later equal-work mean +13.08% and p99 +57.14% failed; capacity-step cases were not launched. | Excluded. [WAN](combined-pacing-wan-2026-10-12.json), [remaining gate](combined-pacing-remaining-2026-10-12.json). |
| ACK RTT eligibility and bootstrap | Retired ACK changed 100 ms RTT to 1,337 ms and RTO 300→11,385 ms. Strict latest-attempt-only policy failed high-delay bootstrap. Live transmitted-state eligibility passed native coverage. | Correctness finding retained and independently implemented on final production base; experimental writer scaffolding not imported. [Native evidence](ack-rtt-native-2026-10-12.json). |
| Pre-cumulative-UNA ACK batch scan | Regression reproduced fresh timing evidence being retired by an earlier ACK's cumulative UNA. Allocation-free scan passed native, migration and integrity checks. Newest-echo executable nevertheless fell 6.163→3.655 Mbit/s with four WAN timeouts. | Final release retains eligibility scan with **arrival-order** echo selection, not the failed newest-echo candidate. [Batch WAN](ack-batch-wan-2026-10-12.json), [final choices](FINAL-PASS-2026-10-13.md). |
| Callback isolation during mixed load | Diagnostic mean/p99 improved when restoring the old callback, but backend CPU rose 10.64%, with varying carrier/capture placement. | No universal callback-caused regression established; both failed limits retained. [Ablation](tx-callback-ablation-2026-10-12.json). |
| Opening/queue diagnostics and observers | Per-carrier delivery/pending/RTO/queue evidence, bounded recovered-opening warnings, kernel qdisc/softnet observations and observer continuity were checked. | Useful portions already integrated into final source/observer. Host drops are not added to overlapping carrier/worker counts and do not prove filtering. [Kernel evidence](kernel-queue-deltas-2026-10-12.json), [diagnostic definitions](PRODUCTION-DIAGNOSTICS.md). |
| Finer benchmark histogram and explicit helper | 125 µs bins, bounded counters, repeated race boundaries/concurrency checks and real TCP/UDP/half-close fixture verification passed. | Already integrated; changes measurement resolution, not historic latency samples. [Receipt](latency-histogram-qualification-2026-10-12.json). |
| .118 connectivity and replacement client | Sparse raw PA/S/ACK, ordinary TCP and ICMP comparisons reproduced broad .118 external failure independently of KCP. Corrected .109 target ports and authenticated application/public nginx probes passed at that checkpoint. | Operational evidence retained. It does not identify a blocking actor/trigger or establish today's six-route status. [Incident](CLIENT118-INCIDENT-2026-10-12.md), [replacement](EMERGENCY-CLIENT109-DEPLOYMENT-2026-10-12.md). |

Earlier exp1/exp2 integration, admission/receive experiments, multiworker
qualification and paced-latency paths remain in
[the 8 October integration review](INTEGRATION-REVIEW-2026-10-08.md),
[exploration journal](EXPLORATION-2026-10-08.md) and
[multiworker evidence](MULTIWORKER-2026-10-08.md). This cleanup does not remove
their prior accepted or rejected findings.

## Final selection and remaining findings

The [13 October final pass](FINAL-PASS-2026-10-13.md) records the clean-source
ablation, both feedback variants, real-backend comparisons, exact-artifact
native/impairment/recovery/reload gates, 20,000 held forwards and staged rollout.
Production retains adaptive control, arrival-order valid RTT evidence, 200 ms
learned reorder ceiling, four distinct client source slots and S/PA framing.

[ACK-cost measurements](ACK-BUDGET-2026-10-13.md) show average reverse bandwidth
was not exhausted in the tested 50/10 Mbit/s case. Clean 80 ms asymmetry delivered
7.99/7.96 Mbit/s for two verified 100 MB bodies, versus 5.399 Mbit/s with loss and
reordering. The complete confirmation capture contains 8.71 MB of repeated KCP
payload. Its trigger is not diagnosed; no reliability/pacing change is implied.
The subsequent verified upload measures 40.701 Mbit/s against its 50 Mbit/s cap
and captures 2.83 MB of repeated KCP payload. Both clean directions use roughly
80% of their own caps; unequal bandwidth alone is not an established cause of
failure, and this percentage is not proof of optimality. See the
[updated direction summary](asymmetric-directions-summary-2026-10-13.json).
The first buffered capture's trailing-block limitation and incomplete-coverage finding
are retained, alongside the corrected immediate-mode measurement.

The latest read-only production review also records France's 04:17 UTC preserving
carrier recovery and Finland's intermittent local TX pressure. These are not
process crashes; the France trigger remains unproven. The previously recorded
Finland Reality backend-loopback dependency failure remains separate from tunnel
qualification. Current configuration/deployment status is in [STATUS.md](STATUS.md).

Range/bitmap ACK encoding, peak ACK burst requirements, deeper attribution of
loss-free repeats, adaptive bidirectional capacity steps, busy UDP, encrypted WAN
and latest-artifact 100,000 busy forwards are not certified by these measurements.
Old passing or failing screens do not transfer acceptance to a new executable.

## Preserved private evidence and cleanup

Before removing any worktree, its ignored build artifacts and local receipts are
relocated under the sole checkout's `build/archives/consolidation-20261013/`.
The private archive inventory records original paths, archived paths, sizes and
SHA-256 values; a pre-cleanup Git bundle preserves refs/source independently.
Historical absolute worktree paths in immutable receipts identify original
execution placement, not current executable locations. Use the inventory when
reopening those receipts. Compiled Python caches are redundant and removed.

All retired worktrees must have clean tracked state and no running process using
them. Useful changes and historical source ancestry reach `master` before local
or remote branches are removed. Existing production observers, logs, rollback
files and services are untouched. Measurement parser checks and documentation
integrity are checked under the shared reservation; this is repository cleanup,
not deployment or a new performance qualification.

Cleanup is complete: only local/remote `master` and the main checkout remain.
The three retired worktrees' 4,242 artifact entries (1,802,730,872 file bytes)
are archived, not discarded. Seven parser checks, 46 imported JSON receipts and
the consolidated document links pass validation. Go runtime/module bytes still
match deployed source `37904d6`. Two permissions-related archive attempts are
recorded privately; root-private fixtures were preserved using privileged
archival rather than weakening their file permissions.
[Completion evidence](consolidation-evidence-2026-10-13.json) records the archive
counts and source identities. The production observer review can now be reopened
at `build/archives/consolidation-20261013/final-20261013/build/final-pass-20261013/log-review-20261013T063108Z/REPORT.md`.
