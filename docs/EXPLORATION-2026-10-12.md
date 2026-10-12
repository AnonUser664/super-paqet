# Production-led exploration after opening.10

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
