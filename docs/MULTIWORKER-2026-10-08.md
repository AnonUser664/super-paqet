# Capture-worker coverage — 8 October 2026

The integration candidate has real two-worker coverage. The earlier qualification
included **36 two-worker comparisons out of 54**, and packet counters show both
workers received traffic in every one of those 36 runs. Four recovery and
compatibility scenarios also used two backend capture workers. However, the
latest clean mixed-load latency comparisons and live-reload fixture used one
worker. This follow-up closes those gaps and retains an adverse latency result.

Production and runtime sources are unchanged. Candidate runtime is
`c1aee3696d766c56f4d11a1a34bc9a9f075cb0c2`, native binary SHA-256
`2962659362143ade1aec169f06991000b2f49e06f791d5adeb2a70db50831ee2`.
The deployed fleet remains migration.4 with two backend capture workers.
The [receipts](multiworker-qualification-2026-10-08.json) record commands,
executable identities, actual worker counts, cleanup, failed attempts and limits.

## Existing coverage

| Two-worker workload | Completed comparisons |
|---|---:|
| HTTP connection churn | 4 |
| Asymmetric, high delay, reordering, mobile loss, upload ACK bottleneck and tiny queues | 24 |
| Uncapped clean bulk | 2 |
| 10,000 predominantly idle established connections | 2 |
| Longer mobile small-HTTP requests | 4 |

The previous four recovery cases cover all carriers blocked, repeated migration,
70-second delayed probes and a known preservation-capable older backend. These
exercise shared conversation dispatch while four sequenced streams remain active.
The application and owning transport modules also have race tests for concurrent
reload readers, peer growth, send-queue draining, shared conversations, migration
ownership and stream/session shutdown. Module race tests alone do not establish
that privileged packet fanout was exercised; the namespace receipts provide that
evidence separately.

## Added two-worker live reload

The fixture now accepts `--packet-workers`. Backend listeners use that count;
each independent outgoing client carrier retains one capture reader. It scrapes
per-listener worker counters and refuses to claim complete fanout coverage unless
every requested worker handled packets on at least one listener. Both original
listeners actually exercised both workers in the completed two-worker runs.

Native and race-instrumented executions each passed with **64 established TCP
streams per original peer**, four distinct source-port carriers per peer, TCP/UDP
probes and three edit cycles. The 19 continuity checkpoints cover invalid-config
retention, adding routes/peers, graceful removal of binds, in-place retransmission
edits, replacing affected transport settings, cipher rotation, driver replacement
and restoring packet fanout. Structural edits may close the affected peer's
streams as designed; the unrelated peer retained all 64 streams with zero errors.
Process identities remained stable, client descriptor counts stayed **87 → 87**,
half-closed relays released, owned firewall rules cleaned up and the unrelated
firewall rule survived.

Driver replacement explicitly goes **packet/two workers → pcap/one worker →
packet/two workers**. The first attempt incorrectly retained two workers when
selecting pcap; validation rejected that configuration and retained the old
runtime. The test fixture timed out waiting for a revision that could not apply.
Its failed receipt is preserved. The corrected fixture passed independently.

The race build has SHA-256
`7625c4311dd7fdbf2ee92924071aeb7183517092d5f394b89ae9e84e121e3eb6`.
Its reload execution passed in 43.71 seconds with no reported Go data races and
normal process exits. The initial race launcher failed before compilation because
root Git rejected the user-owned worktree; the retry resolves the source identity
as the worktree owner. No global Git ownership exception was added. Race builds
are correctness evidence, not throughput measurements, and do not detect every
kernel/packet-ring ownership defect.

## Added two-worker mixed-load comparisons

Eight successful workloads compare receive.4 against the integration candidate:
two alternating pairs each for saturated HTTP and HTTP with a 1 ms request gap,
both alongside bidirectional bulk. Each run uses 60 seconds warmup, 20 seconds
measurement, adaptive transport enabled, four pinned carriers, S outbound / PA
return and null encryption. The offered bulk is approximately **1 Gbit/s in each
direction**, with separate client/server/workload CPU placement. This is a
latency-under-load test, not an uncapped throughput test. Both capture workers
handled millions of packets, all measured/warmup HTTP requests had zero errors,
and all cleanup checks passed. Unequal worker counts are expected from tuple
hashing and unequal traffic among four carriers; they are not packet round robin.

Two-pair medians:

| Workload / metric | receive.4 | Integration candidate |
|---|---:|---:|
| Saturated HTTP requests/s | 3,426 | 4,748 |
| Saturated HTTP mean | 1,171 µs | 842 µs |
| Saturated HTTP p99 histogram bound | 11.5 ms | 7 ms |
| Paced HTTP requests/s | 1,837 | 1,733 |
| Paced HTTP mean | 1,057 µs | 1,180 µs |
| Paced HTTP p99 histogram bound | 6 ms | 10 ms |

Both variants retain the offered bulk in both directions. Saturated tunnel CPU
medians are client/server **1.513/1.003 → 1.549/0.989 cores**; paced medians are
**0.944/0.640 → 0.911/0.631 cores**. Saturated peak RSS medians improve from
client/server **71.8/65.0 → 56.8/60.2 MiB**; paced peak RSS rises from
**60.4/55.3 → 63.5/66.0 MiB**. These short-run high-water values are not fleet
memory budgets. Histogram medians average bucket bounds, not individual samples.

Clock observations remain part of the evidence. The first saturated candidate
ran at higher role clocks, so its improvement has a confounder; the second
saturated pair matches role clocks within 5% and also favors the candidate.
The first paced pair matches client/server/workload clocks within 5%, yet mean
latency worsens about 18% and p99 goes **6 → 9 ms**. The second paced candidate
has lower clocks and p99 **11 ms**, versus **6 ms** for its control. The first
pair means the paced regression cannot be dismissed as clock variation alone.

**Paced latency misses the earlier acceptance allowance** of no more than 5%
mean regression and 2 ms additional p99 bound on matched-clock pairs. The cause
has not been isolated. Successful correctness tests and better saturated results
do not clear this performance gate. Hold rollout of this integration candidate
pending investigation; production remains unchanged. Pinned carriers remove
changing peer selection as a variable in these particular comparisons.

## Reproduction and coordination

Before every command, inspect running tests/load generators and use the shared
machine reservation. Namespace commands require root; keep the reservation
outside sudo so it includes authentication and cleanup. Choose a new output
directory for each execution; preserve failures and interrupted attempts.

```sh
python3 -B scripts/with_test_lock.py -- sudo python3 -B scripts/live_reload_netns_test.py \
  --binary build/integration-20261008/candidate-exp2 \
  --output build/your-unique-reload-run --packet-workers 2 \
  --streams 64 --carriers 4 --small-write-flush 256 --cycles 3

python3 -B scripts/with_test_lock.py -- sudo python3 -B scripts/compare_receive_paths.py \
  --baseline build/receive-tuning-20261008/candidate-4 \
  --candidate build/integration-20261008/candidate-exp2 \
  --output build/your-unique-mixed-run --cases bounded-duplex paced-duplex \
  --repetitions 2 --duration 20 --warmup 60 --duplex-http-steady --pinned-lanes --debug \
  --client-cpus 0,1,2,3 --server-cpus 4,6 --workload-cpus 8,9,10,11
```

The receipt records the exact race build and reload commands. Build and run it
under the same reservation, as a separate workload from performance measurements.
This audit exercises two backend workers; it does not qualify every worker count,
every link type or thousands of simultaneously busy production customers.
