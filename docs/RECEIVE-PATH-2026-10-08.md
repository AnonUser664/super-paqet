# Receive processing and admission experiments — 8 October 2026

This investigation selects a receive-only optimization and rejects two admission
policies. The candidate is **not deployed**. Production still runs migration.4;
the controller, configuration, packet flags and customer services are unchanged
by this investigation. The WAN follow-up is in progress; final acceptance across
the tested link profiles is not yet claimed.

## Selected change and its reason

A receiver CPU profile attributed about 22% of sampled CPU to `parse_data`,
including heap maintenance, interface boxing and payload copies. Previously,
ordered input entered the reorder heap and immediately left it. A persistent gap
also caused root pop/reinsert operations on subsequent input and reads.

Unique input at `rcv_nxt` now enters the receive FIFO directly when the window has
room. Reordered input and input blocked by a full receive window retain the heap
and its duplicate-membership map. A shared drain helper checks the root sequence
before removing it. A gap or full FIFO leaves the heap untouched.

The payload still enters owned pool storage by copying: the caller may reuse its
input buffer immediately. This preserves queue limits, sequence advancement,
fragment assembly, duplicate handling, ACK/window output and ordered delivery.
There is no wire extension, borrowed-buffer lifetime, timer or configuration
change. This reduces CPU work under the existing adaptive controller rather than
changing its congestion, pacing or retransmission decisions.

An independent oracle retains the original heap-only input/read algorithms.
Six seeded traces compare **36,000 operations**, including sequence wrap,
duplicates, gaps, fragments, window changes, undersized read buffers and deliberate
caller-scratch reuse. Every operation compares logical queue/heap membership,
retained payloads, reader results and receive state. Fixed-clock feedback is
byte-identical. The existing 18-profile, three-seed, paced/unpaced virtual-clock
matrix and fast-ACK differential controls also passed under the race detector.

## Every experiment and decision

Private receipts, profiles and captures are retained under
`build/receive-tuning-20261008/`; commands and compact measurements will be
published with the completed follow-up. Do not reuse an output directory: the
runner refuses to overwrite an experiment. Earlier controller and recovery work
is recorded in the incident, migration and recovery-grace reports linked below.

| Checkpoint / receipt directory | Hypothesis and method | Observation and decision |
|---|---|---|
| `eb4127e`; `healthy` (16 workloads) | Ordered receive bypass and peek-before-drain should remove heap allocations. Profile and compare clean bulk/churn/mixed traffic. | Differential correctness and microbench gains passed. Whole-tunnel rates and mixed tail latency varied with admission, capture fanout and CPU placement. Keep the receive implementation for further qualification; do not infer universal performance from this first cohort. |
| `694d7da`; `healthy-2` (24 workloads) | Publish mux membership in an atomic gauge; use the live count instead of a count sampled about every 250 ms. Retain the strong busy-carrier preference. | Correctness controls passed, but recently busy carriers were sometimes excluded during the entire opening burst. Some bulk runs effectively used one capture worker. Upload medians were 5.172 versus 4.784 Gbit/s; mixed p99 was also worse. Reject this combined candidate. |
| `de1d150`; `final` (56 workloads) | Make live population primary and use bulk pressure only as a tie breaker: score `2 * population + busy`. This should use every carrier during bursts. | Connection ramps improved and integrity passed, but median HTTP p99 at the same offered bulk load rose 9.5 to 16 ms. Preliminary one-worker download median fell 3.228 to 2.798 Gbit/s, with substantial run variation. More even connection counts did not establish better latency or throughput. Reject the policy and atomic mux gauge. |
| `1c0cb79`; `receive-only` (12 workloads) | Restore original admission/mux runtime; compare receive-only with five omitted iperf startup seconds. | Clean bulk medians improved in this cohort. One bounded-load control failed to attain the requested 1 Gbit/s per direction, so its HTTP result is not a matched offered-load comparison. Preserve it rather than selecting only favorable pairs. |
| `pinned` (12 workloads) | Give equal iperf stream counts to four independent one-slot peers to remove admission placement as a confounder. | All lanes carried work; CPU/fanout variation remained. Bounded p99 values were 5/10 ms for control and 6/8 ms for receive-only; mean latency did not consistently improve. Keep this as a diagnostic fixture, not a new runtime balancing policy. |
| Canonical receive.4; `selected` (26 workloads) | Run 13 clean, impaired, encrypted and capacity cases against the rebuilt control. | All integrity and cleanup gates passed. Individual reorder, mobile, rate-step and encrypted runs had performance outliers; one pair per case is insufficient to dismiss them. Repeat those cases with fixed CPU placement before accepting them. |
| `232e108`; `affinity` (12 workloads) | Isolate role CPU placement on a hybrid laptop, retaining pinned lanes and identical protocol settings. Three alternating pairs per case. | Median clean upload 3.887 to 4.648 Gbit/s; download 3.315 to 3.381. At equal 1 Gbit/s each-way bulk, HTTP mean 857 to 820 microseconds and p99 12 to 6 ms. Accept the receive optimization for these controlled workloads. This does not prove CPU placement explains every earlier difference. |
| `capacity` (2 workloads) | Hold 100,000 forwards for 120 seconds and reverify each before closing. | Both versions established, held, reverified and closed every connection with zero errors. Laptop swap was active; this establishes mostly idle connection capacity, not 100,000 simultaneous busy flows. |
| `wan-repeat` (in progress) | Three alternating pairs for reorder, mobile, rate-step and AES-GCM with fixed role CPUs and adaptation on. | Pending completion. Retain each result immediately, including failures. |

The admission experiments are absent from the selected implementation. Runtime
engine/mux files match `eb4127e`. The later buffer-pool edit changes a test only.

Current admission prefers healthy over suspect carriers, then compares cached
pressure. Its busy bit strongly favors a carrier without recent bulk pressure;
the remaining score is the sampled stream count. The rotating scan spreads
equal scores. Pressure normally refreshes every 250 ms; adaptive startup may
sample sooner. Busy hysteresis lasts a bounded 2–10 seconds. Pool growth retains
the configured maximum. This selects a carrier for a new stream and does not
migrate established streams for load balancing. Failure migration is separate.

## Measurement controls and adaptive scope

Each comparison runs serially in disposable Linux namespaces. Both binaries use
four independent sources, S outbound/PA return, null encryption unless testing
AES-128-GCM, MTU 1350, 4096-segment ceilings, 30 ms initial reliability updates,
4 MiB mux/2 MiB stream buffers and four client/two backend Go CPUs. Recovery grace
60 is enabled on **both** sides of the comparison; it was qualified separately
and is not represented as a receive-optimization gain. The control is rebuilt
from `a8c1cb9` using the same compiler/native libraries, not the exact older
production executable.

**Adaptation is on.** `mode: manual` supplies initial reliability settings and
ceilings; `adaptive: true` still enables delivery/window/pacing/ACK/reorder/RTO
control. `adaptive_buffers`, ACK timestamps and credit hints remain disabled to
match the deployed profile. Static transport comparisons require explicitly
setting `adaptive: false`, which these receive comparisons do not do.

The CPU-controlled cohort assigns client CPUs 0,1,2,3, backend CPUs 4,6 and
synthetic workload CPUs 8,9,10,11. On this i5-13420H, client threads occupy two
physical P cores, backend threads two other P cores, and generators the E cores.
Only fixture children receive `taskset`; no host governor, affinity, qdisc or
sysctl changes are made. Two-worker capture distribution is recorded, not
assumed. One-worker cases remove capture fanout variation.

Pinned lanes create four separate one-slot peers with equal iperf worker counts.
They intentionally isolate receive performance from admission. They do not
replace qualification of the normal four-slot peer pool. Both fixtures remain
in the evidence. Equal-load comparisons request **aggregate** 1 Gbit/s in each
direction, divided across parallel streams. Actual receiver rates are retained.
Five omitted iperf startup seconds separate steady-state capacity from startup.
HTTP percentiles are histogram upper bounds; deadline cancellations are reported
separately from errors. Sparse completed bulk-request percentiles are not latency
acceptance measurements.

## Receive microbench and resource evidence

The final three-repeat microbench medians are:

| Operation | Heap-only oracle | Selected implementation |
|---|---:|---:|
| Ordered 1326-byte parse/read | 163.2 ns, 184 B, 3 allocations | 63.63 ns, 24 B, 1 allocation |
| Duplicate behind a persistent gap | 89.25 ns, 160 B, 2 allocations | 5.198 ns, zero allocations |
| Full receive queue | 91.82 ns, 160 B, 2 allocations | 4.924 ns, zero allocations |
| Reverse-ordered 64-segment batch | 27.167 microseconds, 318 allocations | 15.808 microseconds, 190 allocations |

These isolate receive queue work, not total packet processing latency. The
remaining ordered allocation is owned storage management; the optimization does
not remove the payload copy.

In the fixed-CPU bulk cohort, upload client/backend CPU medians were
2.563/1.148 versus 2.630/1.124 cores while processing more bytes. Download CPU was
1.190/1.052 versus 1.123/1.054 cores. Peak RSS medians rose from 153.09/155.90 to
156.80/156.92 MiB. The equal-load HTTP/bulk cohort used 1.568/1.036 versus
1.538/0.989 cores and 61.02/69.32 versus 56.97/60.59 MiB peak RSS. CPU accounting
excludes synthetic generators; one core means one second of CPU per elapsed
second.

The 100,000-connection ramp took 7.581 versus 6.087 seconds. Peak client/backend
RSS was 1712.58/1687.20 versus 1809.05/1715.55 MiB. RSS plus swap peaks were
1774.66/1753.36 versus 1811.18/1722.15 MiB; they occur at different times and must
not be summed with independently observed RSS peaks. Kernel sockets and workload
processes consume additional memory. This laptop has about 7.4 GiB RAM; its swap
pressure limits conclusions about production memory sizing.

## Faults, compatibility and code checks

Four sequenced 64 KiB established-stream fault controls passed: repeated failures,
simultaneous carrier failures, delayed fresh-source probes and immediate failure
of an adopted source. They verify payload integrity, retained logical sessions,
healthy siblings, bounded source ownership, reload behavior and the existing
packet-header contract. Four additional fixtures passed packet/pcap functional
checks, UDP, half-close, multiple endpoints/clients, restart, IPv6 and FEC 10/3.
These functional fixtures ran beside code checks: their rates are **not controlled
performance comparisons**. FEC uses its supported independent-listener role.

Application full race tests and vet passed. KCP full normal tests passed in
137.494 seconds; focused receive/virtual/ACK race controls passed in 35.062
seconds. The completed full KCP race retry passed in **740.933 seconds**, with no
tests skipped; KCP vet also passed. Mux full normal tests passed in 65.987 seconds
and all mux benchmarks completed. Its runtime is unchanged; the previously
qualified full mux race suite was not rerun for this receive-only change.

The first full KCP race attempt failed for two documented reasons. Its pool test
required `sync.Pool` to return the same address, although a pool may discard an
entry at any time and the race runtime deliberately does so. The corrected test
checks accepted ownership and restored length/capacity, checking the marker only
if reuse occurs. No pool runtime changed; 100 repeated race runs passed. The
default ten-minute timeout also expired during a suite containing multi-GiB
encrypted/FEC transfers. A 25-minute-budget full retry completed; this timeout
alone is not evidence of a tunnel deadlock.

## Artifact identity and reproduction

Candidate `enterprise-2026.10.08-receive.4`, runtime source
`1c0cb7996a45cc0c8d11ca75fbce47ce9ccedc82`, SHA-256
`64cb857db6acaaf9fb38342fa89b5c6186c514e665343f1f931e573335fd1371`.
Control SHA-256
`bf89fa1bea8fe715e5d7124fc800391d5a4a3557052bcd658d46c17c947839c4`.
These native Arch executables link libpcap.so.1; deployment to Debian requires a
compatible rebuild. They are not the executable currently serving customers.

```sh
sudo python3 scripts/compare_receive_paths.py \
  --baseline build/receive-tuning-20261008/baseline \
  --candidate build/receive-tuning-20261008/candidate-4 \
  --output build/receive-reproduction \
  --cases single-worker-bulk bounded-duplex-one-worker \
  --repetitions 3 --warmup 5 --pinned-lanes \
  --client-cpus 0,1,2,3 --server-cpus 4,6 --workload-cpus 8,9,10,11
```

Use available CPU IDs appropriate to the machine and record their physical-core
mapping. Root is needed for owned namespaces/firewall fixtures. Build the workload
helper from `cmd/bench` and install the dependencies described in
[benchmark documentation](BENCHMARKS.md).

## Follow-up boundaries

No finite laptop matrix proves universal optimality, server NIC capacity,
censorship behavior or sustained busy-customer capacity. Unmatched worker
placement, differing offered load and rejected prototypes must remain visible.
Production observation is reviewed separately; the current receive implementation
does not fix an externally interrupted route or extend a deployed mux watchdog.

Prior investigations: [customer incident and adaptive fixes](PRODUCTION-INCIDENT-2026-10-06.md),
[source-tuple recovery](SOURCE-TUPLE-INCIDENT-2026-10-07.md),
[migration](MIGRATION-2026-10-07.md),
[bounded recovery grace](RECOVERY-GRACE-2026-10-08.md) and
[production review](PRODUCTION-REVIEW-2026-10-08.md).
