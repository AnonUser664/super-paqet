# Production review: 11 October 2026

All five hosts still run latency.1, SHA-256
`256cfc65daaa6596ff2b31e2863296a3182975faa71b1a746295cc6172231958`.
Their tunnel PIDs/configuration hashes are unchanged from the previous review;
services are active/enabled, health endpoints respond and automatic restarts
remain zero. This is **not** an incident-free observation or a candidate rollout.

The completed observer window contains approximately 8,600 samples per host.
Its full-window counters show substantially more failures than the sparse warning
journal: warnings are rate limited, so counting warning lines understates impact.

| Host | Peak active forwards | Peak tunnel RSS, KiB | Error increase | Opening retries | Successful/rejected recovery probes |
| --- | ---: | ---: | ---: | ---: | ---: |
| Germany | 2,819 | 135,624 | 1,486 | 0 | 0 / 0 |
| France | 7,617 | 157,352 | 1,320 | 0 | 0 / 0 |
| Finland | 1,914 | 99,564 | 349 | 0 | 0 / 0 |
| Client .14 | 2,400 | 147,236 | 9,239 | 32,799 | 3 / 12 |
| Client .118 | 5,632 | 237,152 | 29,528 | 113,138 | 24 / 28 |

These are process-local counters, including retries and different failure stages;
they cannot be summed into a count of distinct failed customer connections.
The [compact receipts](production-review-2026-10-11.json) preserve window limits,
counter deltas, capture/drop counters and identities. Client .118's final wall
timestamp extends beyond the nominal observer end; retain that discrepancy when
correlating hosts rather than assuming perfectly synchronized clocks.

## Separate failure classes

France has another multi-carrier incident involving both clients. Around
15:41:28 UTC, both clients probe fresh source tuples; some probes time out.
France subsequently closes those probe conversations around 15:42:28–30.
Another group of probes starts around 15:45:45–46; some fail and corresponding
backend keepalive closures occur around 15:46:46. Sparse samples show continuing
traffic on some conversations, not a tunnel process crash. They do not identify
a filtering device, prove censorship, or prove that changing the source tuple
will always restore a path. Grace can retain existing streams for a bounded
recovery opportunity; it cannot deliver packets while every tested tuple fails.

At 15:49:45–55 UTC, all three backends explicitly report `connect: connection
refused` when dialing their destination on port 2096. Client logs correctly
report remote target rejection. Germany also has an earlier refusal at
12:48 UTC. The backend Xray process identities have changed since yesterday.
This investigation did not restart Xray or modify its configuration. The operator
confirmed multiple Xray and nginx restarts during this period; nginx forwards
customer traffic to client ports 900x. Destination refusals and some customer
interruptions therefore have a confirmed external maintenance explanation.
The exact per-restart intervals are unavailable. Do not label the entire window
as a tunnel regression or an unconfounded production load test, and do not assume
that application restarts explain every earlier raw-carrier probe timeout.

Selected 15:38–52 incident samples show no tunnel cgroup OOM kills, memory-limit
events, task-limit events or CPU throttling. France mux receive buffers are not
globally full in those samples. Ten-second snapshots cannot exclude short
queue/CPU/backpressure excursions between samples. The prior slow-reader
opening failure remains separately reproduced; it must not be asserted as the
cause of every new failure.

## Continued observation and local work

The observers were renewed on all five hosts, with fresh samples and unchanged
tunnel/config/binary/Xray identities verified across the renewal. New archives:
`/var/log/super-paqet-watch/latency1-review-20261011T163830Z/`, scheduled through
**2026-10-12 16:38:30 UTC**. Only the finite diagnostic observer changed;
production binaries, tunnel configurations and Xray were not changed. Finland
SSH required a verified-host-key jump route after direct SSH failed. Failed
collection attempts remain in the private evidence directory.

The local RTT-uncertainty candidate was rebuilt from source `8e7140e`, SHA-256
`70239e4684f9d276d0a653bfd11517e13c07b55ca42f930ebcdd1b883ae1d8c0`.
Race/vet and modeled jitter/queue-response tests passed at the earlier checkpoint.
Paired real namespace reordering, bottleneck and pressure qualification continues.
Neither the earlier body timeouts nor the pooled-pressure regressions are
cleared by a single successful repeat. No production acceptance is claimed.
