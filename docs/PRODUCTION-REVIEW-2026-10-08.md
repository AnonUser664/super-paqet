# Production review — 8 October 2026 (Tehran)

All five tunnel processes are healthy at the closing check, and twelve authenticated
1 MiB Reality downloads passed: two through each client/country route. There were
no tunnel restarts, OOM events, admission rejections or packet-capture drops in the
reviewed window. There **was a France carrier outage**, with successful automatic
recovery and partial connection loss. This is not an incident-free stability pass.

The available observation spans **7 October 19:14–22:16 UTC**, approximately three
hours (**7 October 22:44–8 October 01:46 Tehran**). Closing checks ran at
22:24–22:25 UTC. The observer's full day has not finished: collection continues
until **8 October 19:14:08 UTC / 22:44:08 Tehran**. UTC host clocks and snapshot
timestamps, rather than the calendar date in the workstation, define coverage.

The runtime remains `enterprise-2026.10.07-migration.4`, source
`90e98b1186646c3c56660616309e5fe5573e4208`, executable SHA-256
`633750998daa210ca8499ad5081bcb51a775b5c6e713758d709313606f37bd0f`.
No executable was redeployed. Production configurations are byte-identical to
those before the review. All five tunnel PIDs and actual Xray process identities
are unchanged. Four independent client sources, 10s stall / 15s retry / 5s probe,
adaptation, S outbound / PA return and null encryption remain in use.

## Observed load and resources

These are measured customer-load observations, not capacity benchmarks. CPU is
elapsed cgroup CPU time divided by sample duration; **1.0 means one fully busy
CPU core**. RSS excludes other applications and the separate collector. Traffic
counts bytes accepted by the application relay, both directions, averaged over
successive ten-second intervals. It is not wire bandwidth or a link-capacity test.

| Host | Peak forwarded connections | Peak RSS | Mean tunnel CPU cores | Mean / peak relay Mbit/s |
|---|---:|---:|---:|---:|
| Client 89.45.68.14 | 147 | 47.3 MiB | 0.160 | 0.57 / 8.51 |
| Client 89.45.68.118 | 3900 | 170.0 MiB | 1.478 | 87.11 / 184.24 |
| Germany 116.202.177.233 | 2425 | 103.0 MiB | 0.316 | 48.09 / 124.63 |
| France 171.22.132.226 | 1045 | 66.5 MiB | 0.159 | 12.29 / 38.15 |
| Finland 65.109.249.222 | 1231 | 89.8 MiB | 0.272 | 27.48 / 138.77 |

The busy client used about 37% of its four-core allocation on average. Backend
mean tunnel CPU remained below one third of one core on these two-core hosts.
No cgroup memory/admission limit was reached. Host CPU pressure exists and also
includes Xray, WARP, nginx and virtualization effects; tunnel CPU alone is not
all host demand. These observations do not establish multi-gigabit capacity on
these particular hosts. CPU should not be extrapolated linearly from this mix.

## France incident

Both clients simultaneously stopped making progress on several original France
carriers around **22:01:30 UTC / 01:31:30 Tehran**. Fresh-source probes had mixed
success. Germany and Finland carriers were not rotated during this incident.

| Client | Session | Verified move UTC | Preserved logical session |
|---|---:|---|---|
| .118 | 3 | 22:01:42.249 | Yes |
| .14 | 3 | 22:01:43.802 | Yes |
| .14 | 2 | 22:01:44.185 | Yes |
| .118 | 2 | 22:01:58.580 | Yes |
| .118 | 0 | 22:02:28.541 | No; replacement |
| .118 | 1 | 22:02:32.902 | No; replacement |

Four retained their conversation IDs and resumed traffic. Client .118 sessions
0/1 had disappeared from metrics by 22:02:23, before their replacements passed
verification. France recorded a burst of 183 relay aborts between its 22:02:16
and 22:02:26 snapshots. This supports actual connection loss on expired sessions;
`connections_preserved: false` is not merely a missing diagnostic field.

The most likely expiry mechanism is the mux keepalive watchdog. Its configured
default is 30 seconds, evaluated by a periodic activity-bit check; closure can
occur after roughly 30–60 seconds without inbound activity. Probes were failing
for long enough to reach that interval. **This is an inference:** warning logs do
not identify the original mux closure cause. Application cancellations may also
contribute. A migration cannot restore an already closed mux or backend socket.

Client .118 made eleven probes, with four verified replacements and seven failed
probes. Client .14 made four probes, with two retained-session moves and two failed
probes; the remaining suspect session disappeared later, without a successful
move log. Failed candidates did not tear down the original streams. Healthy
replacement carriers continued carrying France traffic while the other probes
retried. All six routes passed the closing authenticated checks.

This simultaneous, France-specific delivery loss is consistent with an external
path/tuple disruption. It does **not** prove censorship or a source-port block:
there was no paired packet capture during the event. France's process remained
alive, with no capture/TX queue drops or OOM event. Customer demand was present.

## Packet drops and relay-abort counters

Finland recorded **51 new local transmit-queue drops**, in two short bursts
around 19:47:54 and 21:43:47 UTC. Its captured qdisc reports show `fq` flow-limit
drops. Capture drops stayed zero; there was no Finland carrier recovery. The
existing bounded ENOBUFS retry/KCP retransmission behavior remained active.
There is no evidence here that a larger global queue would improve customer
latency; it could instead increase queueing and affect Xray/WARP traffic.

Client .118's `aborted_connections_total` grew by 574,152 while 658,501 new
forwards were accepted. That aggregate is not a tunnel-outage count. A short,
temporary debug sample recorded 779 relay failures: **347 local TCP broken-pipe
writes and 432 local TCP shutdowns returning ENOTCONN**, all with a successful
opposite copy result. The peer endpoints were loopback connections, consistent
with the local frontend closing its connections. This explains the sampled
counter growth, but does not prove that every abort in the three-hour archive
was harmless. Compare specific customer symptoms with timestamps and actual
transport/opening failures before interpreting this counter.

## Profiling, changes and next useful work

Client .118 received a diagnostic-only live reload at 22:19:48 UTC: debug logging
and loopback profiling enabled briefly, with a guarded restoration timer. The
reload reported no changed, updated or interrupted resources. A ten-second CPU
profile sampled 10.31 CPU seconds, primarily socket syscalls (37.6% flat) and
runtime futex work (14.1% flat). Logging/JSON-related stacks accounted for under
1% of this short profile. No adaptive-controller hotspot or memory leak was
established. These short observations do not prove that any particular code
change would improve throughput or latency.

The original warning/profiling-off configuration was restored after roughly
24 seconds and verified by file hash and profiling endpoint 404. The tunnel PID
stayed unchanged. An older remote `journalctl` rejected the ISO-offset timestamp
used by the diagnostic download step; its `finally` block still restored the
config. The journal was then retrieved with a compatible UTC timestamp, and the
restored state was independently checked. The restoration timer was stopped.
Owned probe executables/configs were removed after acceptance checks; private
profiles remain available for later comparison. Server Xray was not changed.

No performance parameters or keepalive timeouts were changed. Extending the
logical-session lifetime is a useful **controlled experiment** for repeated
failed recovery probes, because today's France incident lost sessions before
verification succeeded. It also retains stalled sockets/memory longer and
changing the current mux contract replaces the affected endpoint on reload.
That tradeoff needs a deterministic prolonged-outage test and measurement before
production adoption. It would improve the opportunity to preserve connections,
not make a filtered path recover faster or guarantee application survival.

Another diagnostic improvement would be recording mux closure causes and the
reason preservation fell back at warning level. Current logs establish the
sequence but leave expiry attribution uncertain. Neither improvement is claimed
as implemented or deployed by this review.

## Evidence and collection integrity

The [compact receipts](production-review-2026-10-08.json) retain per-host coverage,
archive hashes, process/config identity checks, statistics, recovery events,
classification totals and authenticated HTTP results. Full private archives and
profiles are under `build/production-review-20261008/`, outside Git. Their remote
source is the [existing observation window](PRODUCTION-OBSERVATION-2026-10-07.md).

Every retained snapshot was checked for observer/metrics/journal-reader errors;
none was found. Sample gaps stayed below 10.11 seconds. The first France archive
download lost its SSH channel and was rejected as a truncated gzip; a new
connection retrieved a valid complete archive. That administrative connection
loss is separate from the earlier customer carrier incident. Final health,
strict config validation, unchanged identities, warning logging, profiling off,
zero service restarts and continuing collection passed on all five hosts.
