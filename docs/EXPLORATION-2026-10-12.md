# Production-led exploration after opening.10

This is the historical 12 October experiment journal, retained during branch
consolidation. Statements about running versions and pending checks describe
their recorded checkpoints. See [the consolidated decisions](CONSOLIDATION-2026-10-13.md)
and [current status](STATUS.md) for the final selection and deployment.

Production remains opening.10, source `2d0e8d1`, SHA-256 `22b2f1ab…`.
This branch is a local candidate; source/measurement commits are not deployments.

## Production review

The read-only review at 09:38 UTC covers approximately 95 minutes since the new
observer started. All five services are active/enabled, healthy, on the same
binary/config/Xray identities, with zero automatic restarts and no new recorded
opening failures/recovery events. Warning records before observer startup belong
to the rollout window; they are retained rather than called fresh incidents.
Finland has 18 packet-drop-growth incident samples. Within matched labels,
carrier consumed-loss growth and worker loss growth both total 1,930 attempts;
these describe overlapping evidence and must not be added together. Its `eth0`
fq qdisc reports `flow_limit 100` and growing `flows_plimit` drops. That is evidence
of per-flow queue pressure, not proof of hardware saturation or filtering.
The [review receipt](production-review-2026-10-12-opening10.json) retains scope.

## Cached transmit callback hypothesis

Profiles from the previous checkpoint found syscall CPU dominant. The actual
Unix sendmmsg microbenchmark also measured three allocations/72 bytes for each
single-packet raw send. The existing writer mutex already serializes batch
headers, payload references and callback completion. Keeping one callback and
its count/result/error storage on that physical socket removes escaping
per-invocation captures without introducing a worker, queue or changing retry
timing. Additional storage is 40 bytes per physical raw socket, not per customer
connection. The constructor initializes the callback; partial test/adaptor
construction has a one-time guarded fallback. All results reset before/after
RawConn.Write, including readiness/deadline failure. Payload pointers still clear
after synchronous submission, and source migration keeps existing route ownership.

Actual socket tests cover healthy/permanent-error payload integrity, a real
queue-full readiness timeout followed by reuse, and four concurrent writers
sharing one physical socket. Five race repetitions pass. Single-packet medians
change 971.8 to 888.3 ns; 32-packet medians 21,901 to 22,000 ns. Both move to
zero measured bytes/allocations. These unpinned microbenchmarks do not establish
a tunnel throughput gain or explain the directional rate gap. Full native and
whole-tunnel gates remain before accepting this candidate. See the
[micro receipts](raw-callback-micro-2026-10-12.json).

No queue policy, host sysctl, outer header/flag cycle, session count, adaptation
algorithm or production service has been changed. Earlier failed pacing/ramp
experiments remain excluded.

## Queue classification investigation and fixture prerequisite

Before proposing one transmitter socket per carrier, review of Linux 6.1
[fq classification](https://github.com/torvalds/linux/blob/v6.1/net/sched/sch_fq.c)
shows that TCP_CLOSE sockets fall back to a masked packet hash. Therefore it is
incorrect to assume every raw worker is necessarily one fq flow or that merely
adding sockets fixes queue concentration. This is a source-level hypothesis
check, not a production-kernel trace; actual hash/collision behavior still needs
measurement. No per-carrier TX socket implementation has been attempted.

The first whole-tunnel launch stopped during CLI preflight because the new
worktree lacked its local iperf executable. It created no namespace or traffic.
The failed receipt remains at `whole-tunnel/`; the corrected run uses the exact
archived helper/iperf artifacts with a new `whole-tunnel-v2/` directory.

## Whole-tunnel first screen: clean retained, pooled pressure gate failed

Exact local candidate `dfb8785`, SHA-256 `3c38f842…`, passes root race/vet.
Two clean pairs measure upload medians 5.7758 → 5.9740 Gbit/s and download
4.3950 → 4.3851 Gbit/s. The first control upload is lower than its second;
retention passes but a universal 3.4% gain is not established. CPU budgets are
equal four threads per endpoint on pinned physical P-core pairs.

All four pooled pressure runs finish without errors and with clean teardown,
but the median latency gate fails: mean 49.573 → 120.384 ms and p99
104 → 212.5 ms. Combined bulk differs 285.174 → 346.762 Mbit/s. The final
control's HTTP mean/p99 is 0.644/2 ms; its mid-run stream placement is
1/16/10/10 across the four carriers, versus 9/9/15/8 and 7/11/12/9 for the
candidate. The control's first carrier already has one stream at readiness;
HTTP can use that nearly idle lane while bulk uses the others. This is a
placement observation, not proof of why the cached callback changed the result.
The failed gate remains; this candidate is not production-qualified.

The next comparison fixes placement with the existing four one-slot peer
fixture and HTTP on lane zero beside its pinned bulk streams. Offered bulk is
200 Mbit/s in each direction (aggregate per direction, divided over 16 streams),
with identical fq per-flow limit/maxrate and endpoint CPU placement. This
isolates raw-writer overhead; it does not retroactively clear the ordinary pooled
latency failure or qualify the pool-balancing policy.

The [first whole-tunnel screen](raw-callback-screen-2026-10-12.json) retains
all commands, artifact hashes, resource reports, cleanup and the failed pooled
latency gate. Root race/vet pass. The initial production Reality probe helper
could not execute under `/run`; it was cleaned up before traffic generation and
a new owned attempt uses the previously proven `/root/super-paqet` location.
This fixture failure is separate from a failed tunnel route.

The corrected production probe completed all six authenticated 1 MiB Reality
downloads. Owned standalone probe files/processes were removed; tunnel and
Xray services were not restarted or reconfigured. These checks complement
observer health/counters and do not establish full-capacity traffic or a
completed 24-hour result.

The public whole-tunnel receipt now summarizes iperf and HTTP results rather
than retaining thousands of empty histogram bins/per-stream intervals in the
current document. Complete original receipts remain in the private output
directories and the earlier checkpoint history. No failed gate was removed.

## Matched pressure and short execution tracing

The four pinned pressure runs all finish with zero errors/warmup errors and
clean owned teardown. Median combined bulk changes 346.570 → 346.671 Mbit/s
(+0.029%), HTTP mean 136.374 → 138.543 ms (+1.59%) and p99
231.5 → 217.5 ms (-6.05%). All finite matched-placement gates pass. The
[receipts](raw-callback-pinned-pressure-2026-10-12.json) are supplementary;
they do not erase the ordinary-pool latency failure or establish a general
admission improvement.

Inspection found that the custom metrics mux served CPU profiling specially
but delegated `/debug/pprof/trace` to the ordinary profile index, which cannot
serve an execution trace. The candidate now routes it explicitly to the trace
handler behind the unchanged explicit profiling guard. Production remains
profiling-off and unchanged. The isolated harness accepts only 0..2 seconds
of trace with enterprise iperf profiling and enough omitted warmup, bounds
response size at 64 MiB, and keeps traces separate from acceptance claims.

Two diagnostic executables support the same tracing: the control overlays the
exact original raw writer, and the candidate retains the cached callback.
Short client/server traces during upload/download will identify blocking
locations without assuming that the previous CPU profiles proved mutex costs.
No independent transmit sockets, duplicated descriptors, queue coalescer or
traffic-class isolation has been implemented.

## Trace result and private-transmit prototype

In 250 ms traces, the control and cached-callback backend download accumulate
330.663/331.328 ms of synchronization delay in `rawPacket.writeBatchWithDrops`
across sender goroutines. Client upload has no matching writer-lock delay. This
is evidence of physical writer serialization, not proof that removing it explains
every direction difference. Send syscall durations are still dominant.

The next local prototype gives each accepted Linux raw KCP carrier its own
protocol-zero AF_PACKET output socket: it has no receive subscription, keeps
the existing interface/ETH_P_ALL send metadata, and shares the original encoder
so timestamp/sequence seed and per-client flag ownership are retained. Capture
workers and tuples remain unchanged. It has no additional output worker/timer
or packet queue. Receive arrays are now separately allocated only on capture
sockets, avoiding approximately 128 KiB of unused receive storage per writer.

Writer lifetime is separate from the shared listener: physical moves retire
only the old writer; output queue shutdown gives the current writer a measured
RTO-sized final-drain deadline and closes it on post-processing completion.
Rate-limiter waits use the existing close signal without an extra goroutine.
Listener shutdown closes all its child descriptors, preventing stale-generation
output after replacement. Constructor failures retain the original shared path
and increment a visible failure counter; session metrics show whether the
private writer is installed. Packet retries and precise per-carrier loss are
retained; shared worker totals also accumulate child losses without double-counting
them as independent incidents.

The first two compilation attempts caught an incomplete read/write interface
split and a missing exported soft-loss method. Their logs are retained. After
correction, root race/vet and three library migration/writer-ownership race
repetitions pass. The prototype is not deployed or performance-qualified; raw
wire integrity, whole-tunnel rates, actual socket cleanup and multiworker reload
remain required.

## Scope correction, whole-tunnel screen and production interruption

The first functional run exercised 16 MiB TCP integrity, UDP datagrams through
65507 bytes and half-close, but its metrics exposed a scope error: movable
`dialOnly` client dispatchers were also receiving a private output descriptor.
Source `300af79` restricts ownership to accepted backend carriers. Client source
slots keep their original sockets; the corrected local artifact SHA-256 is
`9f1b19fecd3043092175c8dbd5ce7381e9c319f4538b0e05ae3f53b7b176ea70`.
Both compilation failures and the earlier wrong-scope executable remain retained.

The corrected artifact passes repeated established-stream recovery, reverse-path
recovery and three two-capture-worker live reload cycles. Recovery uses sequenced
32 KiB payloads and a 10-second stall budget; reload holds 32 TCP streams per
original peer with four carriers and also exercises UDP. All commands and owned
cleanup checks pass. Full TCP/UDP boundary qualification and the complete native
library suite remain separately tracked rather than inferred from a successful
performance run.

The [private-writer screen](private-writer-screen-2026-10-12.json) retains two
clean and two pooled-pressure pairs against the exact production executable.
Clean download medians increase 4.3993 → 5.1831 Gbit/s (+17.82%); upload changes
5.8215 → 5.9897 Gbit/s (+2.89%). The first control upload is again lower than the
second, so an unconditional upload gain is not established. Saturated server CPU
use rises with download throughput; matched offered-load CPU costs remain needed.

Ordinary pooled pressure **fails** its declared gates: combined bulk medians
283.054 → 262.623 Mbit/s (-7.22%), mean request latency
52.366 → 63.867 ms (+21.96%) and p99 85 → 108 ms (+27.06%). All runs have zero
request/warmup errors and clean teardown. Control-0 and candidate-1 place HTTP on
nearly idle carriers, with mid-run stream counts 1/18/8/8 and 1/9/20/9; the other
runs share bulk lanes. This placement variability complicates attribution; it
does not invalidate or erase the failed gate. No carrier admission policy was
changed by this prototype. The next comparison uses both an ordinary pool at
equal offered traffic and pinned lanes under equal fq pressure.

Production investigation then interrupted local work. Client .118 suffered broad
external IPv4 failure reproduced with independent PA/SYN/ACK, ordinary TCP and
ICMP probes; see the [incident record](CLIENT118-INCIDENT-2026-10-12.md).
After the user also reported .14 failing, both old tunnel services were stopped
and the exact **opening.10** production artifact was deployed on .109. The
private-writer prototype was not used. Reality targets were corrected to 443
after the user confirmed a destination-service change; Shadowsocks uses 8880.
All six authenticated tunnel routes and the three public nginx Shadowsocks paths
passed. The [deployment](EMERGENCY-CLIENT109-DEPLOYMENT-2026-10-12.md) is separate
from this candidate's qualification.

The [first .109 health review](emergency-client109-health-2026-10-12.json) covers
19 minutes after the corrected deployment, with 1,918 peak active forwards,
84,600 KiB peak RSS, unchanged executable/config/PID and zero new tunnel errors,
opening retries, recovery events or log drops. Application/client aborts are not
automatically path failures. This is a finite real-traffic observation, not a
sustained capacity or future-filtering guarantee.

A new native test exhausts a one-byte-per-second limiter, observes a real
pending output reservation, closes the carrier and requires prompt writer
retirement while the shared receiver stays usable. Ten race repetitions pass.
The entire KCP module race suite and equal-offered-load matrix are queued under
the shared reservation; no overlapping benchmark has been started.

The full `go test -race ./...` KCP run reaches the default 600-second package
deadline. Its dump shows `TestOOB` had run for only eight seconds; preceding
library cases include 1 GiB and 6 GiB byte-verification loops. No race report is
present in that log. This is an incomplete full-suite check, not a pass or proof
that the OOB test hangs. The exact failed log and exit status remain under
`build/private-writer-20261012/native-full-20261012T1440/`. A larger-budget rerun
is needed. The queued matched performance matrix then acquires the reservation;
the tests do not overlap.

## Equal offered traffic: reject private writers as a default

With a two-core backend budget and 1 Gbit/s offered in each direction, all four
ordinary-pool runs deliver approximately 2.00 Gbit/s combined without errors.
Median client/backend CPU changes 0.839/0.558 → 0.9065/0.630 CPU cores
(+8.05%/+12.90%). Mean request latency changes 927.54 → 951.32 microseconds
(+2.56%); p99 changes 5 → 6 ms (+20%). The backend CPU and p99 gates fail.
These are two pairs, not a universal estimate, but do not support a default
architecture change. The pinned-pressure half of the matrix remains running.

Ordinary source builds now return no private writer from the optional raw factory.
The retained prototype requires the explicit `spq_private_writers` build tag;
it remains unaccepted and must not be deployed merely because clean-link download
improved. This separates a local experiment from normal release construction.
The measured executable remains the earlier explicitly identified private-output.2
artifact; the build-selection change does not retroactively alter its receipts.
The new default executable still needs its own build/native/end-to-end gates.

Review of the build-tag selection fixture found a factory API edge case: returning
a nil `*rawPacket` directly as `SessionWriter` creates a non-nil interface on
construction failure. The adapter now returns plain `nil, err` explicitly.
Existing listener fallback already checked the error; this makes the public
optional-provider contract safe for other callers as well. The tagged closed-
listener fixture verifies that contract without needing raw socket privileges.

## Default executable: native checks complete; WAN qualification in progress

The larger-budget complete KCP race suite passes in 677.297 seconds, including
the 1 GiB/6 GiB integrity, OOB, migration and deterministic virtual-link cases.
KCP vet passes. The earlier ten-minute timeout is retained as a distinct attempt.
The [completed native receipt](private-writer-native-complete-2026-10-12.json)
does not rewrite that failed gate.

The ordinary executable is source `b21cd0b`, version
`enterprise-2026.10.12-shared-output.1`, SHA-256
`751e4111bad6b14ee36a02baa562c6b06f7e341b16bdb3ec03ad14b21039e1f1`.
Root race/vet and tagged socket race/vet pass; the default build-selection fixture
and explicit experimental closed-factory fixture both pass. See its
[native provenance](shared-output-native-2026-10-12.json).

Its [functional checks](shared-output-functional-2026-10-12.json) verify three
16 MiB TCP payloads, seven UDP datagrams through 65507 bytes and 262144-byte
half-close. Owned process/rule cleanup succeeds. The short embedded throughput
runs are not called sustained-performance acceptance. Session metrics in the
new benchmark confirm all private-output flags are zero in the ordinary build.

Four ordinary-pool, equal-offered-load runs deliver approximately 2.00 Gbit/s
combined with no errors. Final medians: client CPU 0.877 → 0.8575 (-2.22%),
backend CPU 0.567 → 0.5775 (+1.85%), mean request latency
875.07 → 926.55 microseconds (+5.88%), p99 4.5 → 4.5 ms. These finite gates
pass. The first pair alone looked adverse; the reversed pair is retained rather
than choosing whichever observation supports a preferred conclusion. There is
no established universal CPU/latency gain.

The first WAN launch fails CLI preflight because `--warmup` requires iperf;
the HTTP/body workloads need `--http-warmup`. No namespace or link is created.
The exact failed command remains at
`build/shared-output-20261012/qualification/asymmetric-control-0-runner.log`.
The corrected `wan-v2/` matrix uses a fresh directory and five seconds of HTTP
warmup. Asymmetric, reordering, upload ACK-bottleneck and uncapped bulk comparisons
remain separate, with reversed pairs and exact artifact hashes. A fresh successful
run does not remove the first failed fixture attempt.

ACK-generation review finds existing cumulative compaction in `KCP.flush`: ACKs
already covered by `rcv_nxt` are omitted except the last feedback item. Therefore
simple cumulative ACK pruning is not a missing feature. Selective ACKs above a
receive gap remain necessary, and their cost on constrained/reordered reverse
links needs measurement. The per-item space check before the filtering condition
is a possible micro-optimization to test later; no ACK wire change was made.

Production remains opening.10 on .109. The 15:38 UTC read-only review covers
68 minutes, 4,355 peak active forwards and 137,484 KiB peak RSS, with unchanged
PID/config/executable and zero new tunnel errors, opening retries, recoveries
or log drops. This is real customer traffic; it is not proof of universal link
performance or of resistance to future upstream blocking.

## Completed shared-output screens; asymmetric qualification blocked

The [combined exact-command receipt](shared-output-qualification-2026-10-12.json)
retains all four bounded mixed-load runs, the invalid first WAN CLI attempt,
twelve corrected WAN runs, four uncapped bulk runs and the stopped long ablation.
Clean null-encryption pinned-lane medians are 5.728 → 5.961 Gbit/s upload and
4.374 → 4.392 Gbit/s download. Reordering and upload ACK-bottleneck finite gates
pass. Saturation CPU is not compared as equal-work CPU acceptance.

The 50/10 Mbit/s asymmetric link (40 ms delay each way, 0.5% loss and 5%
reordering) has an adverse bulk p99: 11.8535 → 13.8565 seconds (+16.90%).
Mean body time and throughput improve slightly, but that does not clear the tail
gate. Only 16–23 bodies complete in each short run, so longer investigation is
required rather than dismissing the failure or claiming causation.

The supplementary 120-second body-only ablation retains its first baseline run
(86 successes, zero errors, 29.015-second p99), and callback-only run (82
successes, **four body_timeout errors**, 26.945-second successful-body p99).
The unchanged 30-second request deadline catches the failures. Its client log
shows one carrier replaced after stalled transport; application requests had
already timed out and closed, hence `no_live_carrier` at that later recovery.
No process crashed; owned processes, firewall rules and namespaces were cleaned.
The matrix stopped before testing the ordinary executable. This is an unresolved
failed gate, not proof that caching the syscall callback caused a stall.

A fresh diagnostic matrix repeats production / ordinary / production with
sampled structured debug events and an explicit five-second per-endpoint metrics
timeline. These observer costs are opt-in (`--metrics-interval`) and distinguish
this diagnosis from the earlier acceptance runs. Intermediate state survives
carrier replacement. Observer children stop under normal owned teardown and the
same global machine reservation. Neither request deadlines nor error gates were
relaxed. Investigation remains incomplete.

The machine-wide workload guard now recognizes archived `iperf-local`,
`fixes-iperf-local` and `fixes-spq-bench` names too. A guarded test uses harmless
owned sleep processes with custom argv[0] to exercise real /proc detection of all
five iperf/bench aliases and retires only those children. It passes; no network
load or nested reservation is involved.

## Production follow-up at 17:14 UTC

The [read-only receipt](emergency-client109-health-2026-10-12-1714.json) covers
984 ten-second samples since the corrected replacement deployment. Same
opening.10 executable/config/PID, zero restarts, zero new tunnel errors or rejected
forwards, peak 11,492 active forwards and 282,068 KiB RSS. No swap/OOM is observed
in the latest sample. Aborted forwards include application closures and are not
classified as outages merely from that cumulative counter.

Finland session 1 records 28 opening retries and one successful physical tuple
recovery at 15:44 UTC, source 53492 → 50467, with established connections
preserved. The cause of that tuple's interruption is not established by warning
logs alone. The observer's capture marker contains no profiling artifacts
because production profiling is disabled; the persisted ten-second samples are
being inspected. No production mutation or diagnostic traffic was introduced.

The [Finland incident samples](finland-recovery-2026-10-12-1544.json) retain
18 existing samples around the event. The conversation ID remains 136939470
across the source-port change, corroborating logical-carrier preservation;
slot stream counts remain in the hundreds. No local TX/capture drop increment is
visible. Those observations do not prove where packets were blocked. Existing
metrics lack accepted-carrier ACKed/received byte progress and per-session RTO,
so a local observability edit now exports those existing snapshot fields plus
pending bytes, retransmission totals, RTT variation and queue occupancy. It
reuses the metrics handler snapshot and adds no per-packet work. It is not yet
deployed or included in the measured shared-output.1 executable.

The [completed diagnostic replication](asymmetric-diagnostic-2026-10-12.json)
returns: first production control 89 successes / zero errors / 17.753 s p99;
ordinary candidate 86 / zero / 23.540 s; second production control 85 / **two
body timeouts** / 28.897 s. All observers scrape without error and all six owned
processes exit zero, with clean firewall/namespace teardown. This demonstrates
an existing production-baseline weakness under this synthetic link. It does
not clear the earlier candidate tail gate or prove universal equivalence.
Across the backend's retained one-second diagnostic deltas, the three runs show
25,520 / 27,357 / 27,763 retries and 9,584 / 10,661 / 12,552 retries later
classified as spurious. Maximum RTO is 2.690 / 3.480 / 4.084 seconds. There are
no per-carrier local output drops or recovery warnings in these three runs.
Those sums include cold verification/startup and must not be mistaken for a
pure steady measured interval. The next isolated hypothesis is whether the
200 ms reordering ceiling races late originals under burst queues; raising a
ceiling is an experiment requiring real-loss latency and clean/mixed gates,
not an accepted production parameter change.

The observability edit passes five race repetitions of a real accepted-carrier
mux payload/ACK export check, the full root race suite (engine 6.679 s), engine
vet, diagnostic CLI preflight and archived workload detection tests. See
[validation](observability-native-2026-10-12.json). These checks qualify the
observability code, not the unresolved WAN performance candidate.

## Isolated reordering-ceiling experiment (not a production change)

Source remains committed `701dc74`; the working tree's protocol code is unchanged.
A Go build overlay replaces only `MaxReorderGraceMS = 200` with `750`. The
controller still chooses a bounded allowance from RTT variation/observed late
ACKs; this does not force a 750 ms delay on every link. Timer retries remain
independent. A larger ceiling could reduce redundant retransmissions under
long burst queues, but might worsen real-loss recovery; both outcomes require
measurement. Packet encoding, shared flag-cycle state and outer S/PA behavior
are unchanged.

The first build launch incorrectly selected the repository root instead of its
`./cmd` entry point and failed before producing a binary or namespace. Its log
and failed receipt remain in `build/reorder-ceiling-20261012/`. Corrected builds
use a fresh `build/reorder-ceiling-20261012-v2/` directory and correct exported
version linker fields. Matched instrumented control SHA-256 is
`140ccd02e77d0b4cf9b924bc118f952d898865a1a6410ab887fd97d009eb70fc`;
overlay candidate SHA-256 is
`018732d63264dc299dfeb537c050c598e4001a3875442dc60b9f513ad81c44b1`.
Ten race repetitions of the library's gap, finite-bound, late-ACK and timer-
independence cases pass on that overlay. The body screen is still in progress.

The [17:28 UTC read-only backend review](backend-health-2026-10-12-1728.json)
verifies all three expected hashes and active services with zero restarts.
Germany/France have no selected warning records since 14:30. Finland has one
keepalive-timeout warning at 15:45:04 for conversation 4244184183 on the new
source port, whereas preserved customer conversation 136939470 continues in
the retained client samples. That timing/tuple is consistent with the temporary probe's retirement, an
inference rather than a role recorded in the warning. The record does not
show that the preserved customer conversation failed. Finland SSH needed its existing jump fallback;
SSH accessibility and tunnel health must not be conflated. No backend was
modified or load-tested during this read-only review.

The [completed ceiling screen](reorder-ceiling-screen-2026-10-12.json) is
**rejected for deployment**: both matched 200 ms controls have zero errors; the
750 ms runs have four / zero body timeouts. The successful-body throughput and
latency improve and retry totals fall, but they cannot compensate for failures.
All timeouts in the failing candidate run belong to the same four-stream
carrier. Its pacing/delivery estimate drops while siblings continue, which
points to a separate controller feedback hypothesis. Raising the ceiling alone
is not an adequate fix. It remains solely a preserved build overlay.

A second overlay experiments with bandwidth estimation during queue draining:
when delivered bytes track a deliberately reduced pacing budget (within a 10%
framing/loss allowance), do not treat that self-imposed rate as a fresh physical
capacity reduction. Delivery well below the pacing budget still reduces the
estimate after recent-peak expiry. The paired ceiling-200 and ceiling-750 builds
separate this controller edit from the earlier tolerance edit. Neither is
accepted or deployed. An injected controller trace must fail on the original
code, retain discovered capacity during self-limited draining with the edit,
and still converge its actual pacing budget after a sustained real bottleneck
reduction. Full engine race checks precede link testing.

The [pacing feedback native receipt](pacing-feedback-native-2026-10-12.json)
retains the intended original failure: its learned estimate falls from
1,000,000 to 210,790.94 bytes/s while the modeled delivery follows the
controller's own draining budget. Both modified engine race suites pass
(6.663 / 6.657 s), including retention and a subsequent real-capacity drop.
The pacing-only ceiling-200 executable SHA-256 is
`5c677bff0bd2b4da03de01a9865c3503cd5c20a8f8cde9cc825d7eaf8607f142`;
the combined ceiling-750/pacing prototype is
`da3f673dc7d8fec3da4517bb0d8fdaf02901ba7286bf40fe705a6c29ba4adb8b`.
A fresh two-reversed-pair body screen now measures only the pacing change with
the existing 200 ms ceiling. The combined prototype is built/native-checked
but not link-qualified. Passing a modeled controller trace is not evidence
that either executable is an end-to-end improvement.

The first two pacing-only body runs return zero / three timeouts; their
goodput remains near 6.05–6.09 Mbit/s. They do not establish a safe end-to-end
fix despite the causal model passing. The unchanged 200 ms tolerance still
allows considerable retry traffic. The matrix's second control is completing;
all attempts remain separate. Next is the already-native-checked combined
pacing/ceiling hypothesis. No controller or tolerance edit has been applied
to tracked production source or deployed hosts.

The [completed pacing-only screen](pacing-feedback-screen-2026-10-12.json)
retains both controls (four / six body timeouts) and both candidates (zero /
three). The candidate is **not accepted**. Its median goodput remains near
6.068 Mbit/s versus control 6.186 Mbit/s; median successful-body p99 is
25.632 versus 17.5745 seconds. Timeout bodies are censored from that successful
latency histogram, so controls' shorter displayed p99 must not be interpreted
as universal superiority or pooled with earlier baseline runs. Zero-error
acceptance is unmet. All four attempts clean their owned processes/rules and
namespaces. The combined 750 ms/pacing overlay is built/native-checked but its
prepared local link matrix has not been launched. Source and production retain
the existing adaptive behavior while investigation continues.

## Combined adaptive hypothesis passes its diagnostic body screen

The [combined screen](combined-pacing-screen-2026-10-12.json) completes two
reversed pairs with zero body/warmup errors in all four runs. Matched ceiling-200
control / pacing-plus-ceiling-750 medians: 6.355 → 7.980 Mbit/s, mean body time
9.617 → 7.948 s, p99 19.326 → 16.2175 s. This is a useful diagnostic improvement
(about +25.6% throughput), not final acceptance: both variants include debug
logging/timeline observers, and the controller-only/ceiling-only failures remain.
A fresh unprofiled comparison against the exact deployed opening.10 artifact is
running on asymmetric HTTP/body and narrow-bandwidth 5%-loss HTTP. Real capacity
steps, clean bulk, equal-work mixed CPU/latency and recovery remain required if
those gates pass. No production setting was changed.

## ACK RTT eligibility: independent protocol correctness experiment

Review finds `KCP.Input` updated RTT from ACK timestamps even when their sequence
was already retired or never transmitted. The [deterministic checks](ack-rtt-native-2026-10-12.json)
reproduce a retired duplicate changing a valid 100 ms SRTT to 1337 ms and a
300 ms RTO to 11385 ms. This demonstrates estimator pollution; it does not prove
that it caused the observed real-world interruptions.

A strict latest-attempt-only draft is rejected: on a 1-second path whose first
packet retries before its original ACK arrives, it learns no initial RTT. The
corrected overlay accepts an old-attempt timestamp while its transmitted segment
is still live, and excludes retired/already-ACKed/unsent segments from new RTT
evidence. ACK/UNA delivery processing stays unchanged. Ten race repetitions of
original failure, rejected strict policy and corrected policy run, followed by
three repetitions of reorder/pacing/migration regressions. Corrected checks pass.
This is a separate overlay, not part of the measured combined executable. The
optional ACK transit-timestamp estimator remains unchanged; it is disabled in
current production. Root/native/end-to-end checks and raw throughput remain
necessary before this independent fix is promoted.

At [18:10 UTC](emergency-client109-health-2026-10-12-1810.json), production
retains the same PID/hash/config and zero new tunnel errors/rejections/restarts.
Opening retries increase to 733, across Germany/France/Finland lanes, despite
only one completed recovery and no currently suspect/blocked lanes. This signal
requires incident-window review; process liveness alone is not called clean
path acceptance. The observer continues retaining its bounded samples/journal.

The [unprofiled WAN comparison](combined-pacing-wan-2026-10-12.json) against
production opening.10 passes all eight attempts: asymmetric body throughput
6.135 → 7.707 Mbit/s (+25.62%), mean 10.302 → 8.270 s, p99 12.441 → 10.064 s.
Small-request mean/p99 84.722 → 84.620 ms / 268 → 265 ms. The 2 Mbit/s,
5%-loss case has p99 486 → 477 ms with zero errors.

Its [next qualification gate](combined-pacing-remaining-2026-10-12.json)
retains clean bulk 5.728/4.408 → 5.994/4.427 Gbit/s and bounded combined rate
about 2.000 Gbit/s. Client/server CPU changes +1.34%/+2.95% pass. Request mean
805.294 → 910.636 microseconds (+13.08%) and p99 3.5 → 5.5 ms (+57.14%) fail.
The matrix stops before capacity-step runs. Thus the combined edit remains
**unaccepted**, despite passing WAN and clean rate gates. Placement/low-RTT
controller activity need diagnosis; the failed quantiles are not dismissed.

The [complete ACK RTT native check](ack-rtt-complete-native-2026-10-12.json)
passes the owning KCP race suite in 701.238 s and vet, including large integrity,
OOB, migration, loss/reorder and clock/sequence-wrap coverage. Its standalone
overlay also passes root race/vet and builds SHA-256
`57e9e36212a0e2bda1902289abdadc38a1a2fbebfce01ffe3ee497d4f714521b`.
The first root attempt accidentally enumerates experimental `.go` snapshots
as separate build packages. Private module boundaries isolate those folders;
the original failure and corrected root attempt remain separate. The tracked
core now contains only the validated ACK eligibility edit, not the rejected
pacing/ceiling hypotheses. Link and raw throughput gates are still incomplete.

The user confirms no Nginx/Xray changes at 17:47 UTC. Existing backend samples
show traffic dipping concurrently across all three countries, no local TX/capture
drop increments or sampled mux blockage, unchanged tunnel PIDs and Xray process
start times preceding the burst. Germany/Finland have control-stream EOFs then;
these can be effects of abandoned attempts and do not identify the root cause.
No actor/drop location is established. A common cause is plausible, not proven.

To improve the next production incident record, the local candidate adds
failed-phase counters and an aggregate `opening.retry_burst` warning before the
observer's debug-only gate. The warning is bounded to one per existing interval
and scales its noise threshold with admissions; no target addresses or bearer
capabilities are logged. Opening/retry/recovery decisions remain unchanged.
Actual silent-receipt recovery and deterministic cohort/noise/reset fixtures
verify classification. These logging additions are not in the earlier measured
combined or standalone ACK binaries and have not been deployed.

## Committed ACK eligibility and warning-evidence executable

Source `6fdf3d7` builds `enterprise-2026.10.12-ack-evidence.1`, SHA-256
`eca7bd3aad4ec9406d1074f276a3eb9c12ce364d655d1fb7b4a2880079301cf9`.
It retains the existing 200 ms ceiling and pacing policy; the unaccepted
combined experiment is absent. The [exact-artifact functional receipt](ack-evidence-functional-2026-10-12.json)
passes manual-production-profile TCP/UDP integrity and half-close, repeated
32 KiB sequenced established-stream migration and reverse-direction migration
with the 10 s deployed stall timeout and two backend capture workers. Owned
processes, rules and namespaces are removed; unrelated sentinel rules survive.
This is local qualification, not a deployment. Clean/mixed/capacity-step and
WAN performance checks remain in progress.

The local observer now retains all four opening-phase counters in bounded
durable summaries and incident triggers. Older runtimes may omit those fields;
new fields/process resets are quiet, and raw-log rotation does not erase phase
deltas. Its existing optional-metric/reset regression now covers these fields.
The observer update has not been installed on any host.

The [completed clean/mixed gate](ack-evidence-remaining-2026-10-12.json)
retains upload/download medians 5.740/4.367 → 5.979/4.406 Gbit/s. Equal-work
mixed rate stays about 2.000 Gbit/s and CPU changes +4.08%/+2.82% pass.
Request mean 916.086 → 949.202 microseconds (+3.61%) passes; p99 5 → 6 ms
(+20%) fails. All eight commands complete without workload errors and clean
up their owned resources. Capacity-step qualification is not launched after
that failure. This candidate remains unaccepted; the finite tail gate is not
dismissed as scheduling noise. A separate WAN diagnosis starts, and a callback
ablation is prepared to change only cached versus per-batch callback submission,
retaining ACK eligibility, warning counters and the existing controller.

The [19:26 UTC production review](emergency-client109-health-2026-10-12-1926.json)
retains the same client PID, executable and configuration, zero restarts and
zero new terminal errors/rejections/log drops since the corrected deployment.
There are 948,782 new admissions and 749 opening retries, with the same one
preserving recovery. Peak active count remains 11,492. The retry increments
remain an investigation signal; no terminal error is not an outage-free claim.
The [queue baseline](kernel-queue-baseline-2026-10-12.json) finds cumulative
root drops on France/Finland and no current backlog. It cannot date the 17:47
event. New sample-level kernel evidence is prepared; no qdisc policy is changed.

The [completed WAN diagnosis](ack-evidence-wan-2026-10-12.json) fails the
zero-error gate: asymmetric body timeouts are control 0/1 and candidate 6/2.
The candidate's median body rate is 4.970 versus control 6.185 Mbit/s.
All small-request workloads complete without errors. Successful-only body
latency distributions are censored by timeouts; they do not establish improvement.
The mobile-loss case is not launched after this gate fails. The production
weakness is reproduced again, and the candidate has no acceptance clearance.

The [observer deployment receipt](observer-queue-deployment-2026-10-12.json)
verifies queue/stat/softnet fields on client 94.139.180.109 and all three backends.
Fifteen native observer tests pass, including missing-tc/vanished-process handling
and optional phase-counter resets. Existing observer script paths are replaced
atomically with unique backups; only `super-paqet-watch` restarts. Tunnel PID,
binary, config and restart count are unchanged on every host; watch units,
archives, journal record counts and fixed deadlines are preserved. The first
SFTP attempt uses nonwritable exclusive mode and stops before replacement or any
restart. Its empty owned stage/receipt remain; the fresh `wx` retry succeeds.
No tunnel binary or kernel queue policy is deployed in this update.

## Further ACK batching and callback isolation

The [callback ablation](tx-callback-ablation-2026-10-12.json) holds the ACK
eligibility/warning/controller source constant and restores only per-batch raw
callback submission. Its native socket race check passes. Four diagnostic mixed
runs retain five-second metric timelines and sampled debug flow placement; they
are instrumented and cannot clear an unprofiled release gate. Equal-work rate
stays about 2.000 Gbit/s. Restoring the old callback changes mean 895.860 →
836.588 microseconds, p99 5.5 → 5 ms, client CPU +4.28% and server CPU +10.64%.
The server CPU gate fails. HTTP/source-carrier and capture-worker distributions
vary across attempts, including substantial capture imbalance. This evidence
does not establish a universal callback-caused latency regression or justify
replacing the default callback. Both hypotheses and failed limits remain retained.

A [deterministic batch regression](ack-batch-regression-2026-10-12.json)
finds a gap in the independent ACK correctness fix: a leading retired ACK can
advance cumulative UNA, freeing a fresh later ACK's entry before its per-header
eligibility check. Ten race repetitions leave the old 100 ms SRTT unchanged
instead of incorporating a new 190 ms sample (111 ms smoothed). A bounded,
allocation-free scan against arrival-time send state runs before delivery
processing, choosing the newest eligible echoed timestamp with wrap-aware
ordering. It passes ten repetitions of ACK, gap-grace and deterministic link
checks. Added tracked fixtures cover reversed echoes, timestamp wrap, extension
payloads, malformed trailing headers and FEC exclusion. Reliable ACK/UNA
delivery and wire flags/shape remain unchanged. This is a **local candidate**;
full owning-module checks are running, and earlier executable performance gates
do not transfer to this source. It is not attributed to the 17:47 incident.

The [19:11–19:42 UTC kernel comparison](kernel-queue-deltas-2026-10-12.json)
records two client root drops over 22.75 million packets, no Germany/France
root drop increments and 24 Finland root drops over 13.08 million packets.
Softnet drop deltas are zero; time-budget reschedules increase, principally on
the client. These counters include other host traffic and do not date the
earlier interruption or attribute loss to the tunnel. No queue policy is tuned
from cumulative/drop-rate evidence alone. All four services retain their PIDs
and zero systemd restarts during the comparison.

The batch follow-up [full native receipt](ack-batch-native-2026-10-12.json)
passes the complete KCP owning-module race suite in 691.104 s, including
1 GiB/6 GiB integrity, 1,024-client concurrency, socket ownership, migration,
FEC and deterministic link coverage. Owning/root vet and root race checks pass.
Five isolated scan measurements have medians 3.506 ns for one ACK and 140.7 ns
for 48 ACKs, with zero allocations. These are scan costs, not whole-runtime
throughput measurements.

Clean source `057bb47` builds `enterprise-2026.10.12-ack-batch.1`, SHA-256
`05c99b8d35603a81b80b89118e625bb8c99103e6c67cc322dcb0174432538c7e`.
Its [functional receipt](ack-batch-functional-2026-10-12.json) passes payload,
UDP and half-close integrity, repeated established-stream source recovery and
reverse-direction recovery. Four 32 KiB sequenced held streams survive movement
with 10 s stall detection and two backend capture workers. Owned resources are
removed and unrelated sentinel rules survive. It retains the existing pacing
policy, 200 ms ceiling, shared output and S/PA/null envelope. Clean/mixed/WAN
performance checks are running; this executable remains a local candidate and
has not been deployed.

The [completed batch candidate clean/mixed receipt](ack-batch-remaining-2026-10-12.json)
passes clean upload 5.716 → 5.991 Gbit/s (+4.82%) and download 4.382 →
4.398 Gbit/s (+0.37%). Mixed rate stays about 2.000 Gbit/s, CPU changes
client +0.51%/server +6.24%, and mean request latency 933.307 → 943.982
microseconds (+1.14%); those limits pass. P99 5 → 6 ms (+20%) fails again.
All eight commands have zero workload errors and clean up their resources.
Capacity-step testing is not launched. This result does not clear acceptance
of the callback/scaffold/core changes, even with a negligible isolated scan cost.

The [completed batch candidate WAN diagnosis](ack-batch-wan-2026-10-12.json)
also fails: body throughput 6.163 → 3.655 Mbit/s (-40.70%), successful mean
9.368 → 13.563 s (+44.77%), control errors 0/0 and candidate errors 0/4.
Small requests have zero errors and similar 84 ms means. Lower successful-only
body p99 is censored by the candidate's timeouts and is not called improvement.
Mobile-loss testing is not launched. ACK batching correctness is demonstrated
independently; these paired measurements do not isolate which source changes
cause the runtime regression. The executable is **unaccepted and undeployed**.

The [benchmark resolution receipt](latency-histogram-qualification-2026-10-12.json)
adds 125-microsecond bins below 32 ms, retaining the existing wide ranges and
quantile rank. It uses two KiB of additional bounded counters; no individual
samples are retained. Ten race repetitions cover actual HTTP load fixtures,
fine/wide boundaries, long tails and concurrent updates; vet passes. The new
helper SHA-256 is `0b2acc786e32c791f58610561fb0b9518932359dbe09bea1244db93919a08769`.
An explicit `--benchmark-helper` fixture selection passes real TCP/UDP/half-close
integrity with this helper, exports the resolution metadata, records its exact
path/hash and preserves unrelated rules. Existing archived helpers, failed gates
and old measurements remain intact. This changes measurement precision, not
the running tunnel or retrospective acceptance. A finer-resolution matched
mixed-load comparison remains unperformed.

At the [20:00 UTC production review](emergency-client109-health-2026-10-12-2000.json),
the same client binary/PID/config retains zero restarts and no new terminal
errors/rejections/log drops. Admissions reach 1,073,832; opening retries remain
749, unchanged since 19:26, and the same single preserving recovery is recorded.
The observer resumes its journal cursor and durable summary after the refresh.
This does not establish that the earlier 17:47 interruption is solved.

Next unresolved experiments are a source-layer ablation based on the production
transport plus only ACK/diagnostic edits (the callback-only ablation retains
private-writer scaffolding), a matched ordinary-pool mixed test with the finer
helper and measured placement, and renewed adaptive capacity/pacing diagnostics
against the corrected RTT evidence. Earlier rejected feedback/750 ms edits are
not enabled. Busy UDP, encrypted WAN and latest-executable 100,000 busy forwards
remain unqualified. Production incident attribution still needs a new captured
event; the new kernel evidence supplies timing previously missing.
