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
