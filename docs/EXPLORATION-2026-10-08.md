# Coordinated exploration after checkpoint 2be348d

Master checkpoint **2be348d** is pushed. This work continues independently on
`exploration/production-tuning-20261008` in
`/home/shayan/Codes/super-paqet-exploration-20261008`. Nothing here is deployed or
merged back into master. Other agents should use separate worktrees and the
shared test reservation in [AGENTS.md](../AGENTS.md).

## Baseline and failed gate

The [checkpoint investigation](RECEIVE-PATH-2026-10-08.md) records the original
receive-only improvement, rejected balancing policies, and fifteen-hour
production review. Receive.4 is the performance control. Receive.5 adds a
separate cached receive-blockage field and samples it on each new selection.
The backpressure reproduction passes, but that implementation **has not passed
the performance gate** and must not be represented as a qualified rollout.

All 24 initial whole-tunnel workloads passed payload and cleanup checks. Two
additional equal-load comparisons completed after the worktree split. At the
same actual 1 Gbit/s bulk in each direction, three-pair median HTTP p99 was
**7 ms for receive.4 versus 11 ms for receive.5**; mean was 796 versus 872
microseconds and request throughput 5018 versus 4585/s. Individual candidate
p99 values ranged 7–17 ms, control 6–7 ms. This failed gate is retained, not
discarded as noise. Two-pair median bulk upload was 4.800 versus 4.729 Gbit/s;
download was 3.377 versus 3.374. Churn improved slightly. Neither favorable
bulk nor correctness results excuse the mixed-latency regression.

In the new, directionally correct ACK-return case (100 Mbit/s upload, 1 Mbit/s
return, 40 ms RTT), median payload upload was 80.480 versus 78.986 Mbit/s.
All four held sequenced streams survived the receive.5 simultaneous-carrier
fault check. These do not change the failed healthy mixed-load gate.

## Packed-pressure alternative

Checkpoint **d295dba** removes the new slot field and restores the original
selection loop. The existing atomic pressure word carries three priorities:

- Bit 63: ordinary recent bulk pressure, as before.
- Bit 62, with bit 63 also set: local mux blockage or remote zero-window pause.
- Remaining bits: original cached stream count.

Thus every blocked score is above every ordinary busy score, while all-blocked
pools retain stream-count ordering. Transport health still takes precedence;
equal-score rotation and bounded growth remain unchanged. Normal unblocked
scores and selection loads match the original behavior. One atomic publication
keeps count and pressure coherent; there is no extra per-slot storage or new
admission atomic read. Existing metric `peer_carrier_opening_blocked` derives
its value from the word. Sampler cadence, migration, packet format, configuration
and established stream ownership are unchanged.

This is a hypothesis-driven simplification, **not proof that the separate field
caused the entire earlier regression**. Compiler layout, scheduler placement and
hardware state can still affect measurements. The packed candidate was
compared with receive.4 at identical settings, with three alternating pairs,
fixed role CPUs, equal pinned lanes and first-pair CPU profiles. It also missed
the first mixed-load acceptance gate. Full application race/vet and ten repetitions of the
focused real-backpressure/opening/pool race tests already pass.

| Packed-pressure cohort, three-pair medians | Receive.4 | Receive.6 |
|---|---:|---:|
| Upload payload, Gbit/s | 4.449 | 4.418 |
| Download payload, Gbit/s | 3.130 | 3.350 |
| Equal-load HTTP requests/s | 4269 | 3583 |
| Equal-load HTTP mean, microseconds | 936 | 1116 |
| Equal-load HTTP p99 histogram bound, ms | 9 | 9 |
| HTTP churn requests/s | 11,601 | 12,814 |
| HTTP churn p99 histogram bound, ms | 13 | 12 |

All workloads completed with zero errors and successful cleanup, but lower
mixed request throughput/higher mean fail acceptance. Keep receive.6 experimental;
do not promote it based on favorable bulk/churn rows. [Complete branch receipts](exploration-qualification-2026-10-08.json)
retain 49 workload attempts: 48 successful workloads and the missing-dependency
fixture failure, plus the receive.5 fault check and receive.6 code checks.

Profiles attribute about 39–42% of client CPU and 47–51% of backend CPU to
syscalls in the first mixed pair. The sampler/admission change is not a measured
CPU hot path. Client transport wait also increased in the candidate sample.
This identifies pipeline/scheduling symptoms, not a proven cause. The pinned
fixture has four **one-slot peers**: it supplies no alternative slot for admission.
A slower mixed result there cannot simply be explained as choosing the wrong
sibling. Further work must separate adaptive window/startup history, packet
scheduling, generator contention and machine-state variation before claiming a
cure. No global CPU governor, queue or transport parameter was tuned.
The completed runs released the shared lock and left no owned namespaces.

Candidate `enterprise-2026.10.08-receive.6`, runtime source
`d295dba` (full source identity embedded in the executable), SHA-256
`f1374e06115aed45e7350afe5f46e73a8b706dac558feaf412daa0c5deae913e`.
This is a native local executable, not a Debian deployment artifact.

## Coordination and fixture failures

`scripts/with_test_lock.py` acquires the machine-wide lock before authentication,
checks known running tests/load generators and leftover test namespaces, then
holds the reservation through its command and cleanup. Busy checks exit 75
without starting or stopping another workload. Cross-process checks verified
that an existing lock rejects a command and that an unlocked simulated generator
is detected; the probe generated no traffic. Manual/unknown generators still
require coordination. Do not nest wrappers inside an already reserved matrix.

The first follow-up in this worktree failed because ignored
`build/iperf-local/bin/iperf3` had not been copied. Children exited 127 and their
empty logs caused a JSON parse error. Owned namespace/firewall cleanup passed;
there was no valid performance measurement. The failed row remains in the ledger.
The qualified dependency was copied, and the fixture now checks executable
presence before namespace creation and reports child exit failures before JSON
parsing. A negative preflight check confirmed rejection before any fixture was
created. The retry uses a new directory and retains the original attempt.

The earlier user interruption affected a local test, not production. Nineteen
WAN receipts survived; an incomplete mobile attempt was recorded and only its
three empty owned namespaces were removed. Missing comparisons resumed in fresh
directories. Do not attribute that interruption to packet loss or application
failure.

Raw results stay under this worktree's `build/receive-tuning-20261008/`; the
preceding 24-workload cohort remains in the original worktree's ignored build
directory and is referenced by the copied ledger. Master and the customer
deployment remain unchanged during branch exploration. Combined-agent merging
and final qualification are subsequent stages.
