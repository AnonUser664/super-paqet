# Current deployment status

All five hosts run **enterprise-2026.10.07-migration.4** under enabled
`super-paqet.service`. Runtime source `90e98b1186646c3c56660616309e5fe5573e4208`,
SHA-256 `633750998daa210ca8499ad5081bcb51a775b5c6e713758d709313606f37bd0f`.
Both clients use four independently reserved automatic source ports per country,
negotiated connection preservation, **10s stall / 15s retry / 5s probe** budgets.
Each backend listener uses **two capture workers**. All retain S outbound / PA
return, quoted null encryption, adaptation enabled, warn logging and profiling off.

All six routes passed two authenticated 1 MiB Reality downloads after rollout.
Actual-host config validation, exact running-image hashes, twelve distinct ports
per client, stable Xray process identities and zero automatic restarts were
verified. Temporary rollback timers were disarmed and staged/probe files removed;
complete rollback archives remain. See the
[rollout report](MIGRATION-DEPLOYMENT-2026-10-07.md),
[receipts](migration-deployment-evidence-2026-10-07.json) and
[accepted snapshots](deployed/README.md).
This finite live check does not establish sustained busy-customer capacity or
one-day stability. Earlier observations below describe their identified versions.

All five hosts now have independent, enabled ten-second observers and private
rotating metric/journal archives through **2026-10-08 19:14:08 UTC**. Production
stays at warning logs with profiling off. Installing and cleaning the observers
left all tunnel configs/PIDs and Xray process identities unchanged. See the
[current observation window and tomorrow's review](PRODUCTION-OBSERVATION-2026-10-07.md).

The [8 October production review](PRODUCTION-REVIEW-2026-10-08.md) covers the
first three hours of this window: all twelve authenticated transfer checks passed,
with no process restarts or OOM events. France suffered a roughly minute-long
carrier incident; four migrations retained sessions and two used replacement
after the originals disappeared. Production settings remain unchanged. The full
day is still in progress; this is not an incident-free stability pass.

## Recovery-grace candidate

The next candidate is locally qualified and **not deployed**. It adds first-cause
warning diagnostics and optional, bounded mux recovery grace after migration
negotiation. Default zero retains ordinary keepalive expiry. Production configs
and executables remain unchanged. See the
[qualification and regression controls](RECOVERY-GRACE-2026-10-08.md) and
[receipts](recovery-grace-qualification-2026-10-08.json).

A read-only production check at **2026-10-08 00:55 UTC** found all five original
processes active, zero restarts and identical running binary hashes. France had
another nonpreserving recovery at 23:11 UTC after two failed probes. At 00:20 UTC,
seven France probes were later discarded without source replacement; the live
pending/suspect metrics were zero at the check. Warning-only logs do not expose
the exact discard cause. This is not an incident-free stability result.

## Qualification and deployment before migration.4

The earlier migration candidate `enterprise-2026.10.07-migration.3` is locally
qualified and **not deployed**. It adds opt-in preservation of established
KCP/mux/TCP state during a verified source-port move and bounded early-stall
diagnostics. Both clients and backends need the new runtime for preservation.
Its [report](MIGRATION-2026-10-07.md) and
[receipts](migration-qualification-2026-10-07.json) retain 12 fault scenarios,
17 primary workloads, four profiling repeats and five config validations.
Clean median receiver bulk was 5.812/4.284 Gbit/s upload/download versus
carrier.4 at 5.992/4.332. The first mixed-load pair was slower; repeated profiling
runs varied substantially in both versions. These are finite local tests, not a
production filtering or busy-customer capacity guarantee.

The previous independent-source release `enterprise-2026.10.07-carrier.4` is
locally qualified and **not deployed**. It uses four distinct client source ports, carrier-scoped
verified recovery and 15-second stall/retry defaults. The exact executable,
behavior, performance ranges, failure tests and limits are in the
[candidate report](CARRIER-RECOVERY-2026-10-07.md) and
[receipts](carrier-recovery-qualification-2026-10-07.json).
Proposed five-host configurations are in
[proposed-source-ports/](proposed-source-ports/README.md); the historical versions
and configurations below preceded the accepted migration.4 rollout.

Before migration.4, both clients ran recovery.1 and the three backends retained ownership.2.
Verified source-tuple recovery is enabled on every outgoing client peer. The
server wire protocol is unchanged, so backend and Xray restarts were unnecessary. All six routes retain four
shared-source KCP carriers (`sessions: 4`, `max_sessions: 4`), adaptation enabled,
client S / backend PA flags, quoted null encryption, AF_PACKET with one receive
worker and MTU1350. France is 171.22.132.226.

## Pre-migration.4 release and topology

Client version `enterprise-2026.10.07-recovery.1`, runtime source
`efa095aacb2417246c4ae9aa568f03c119d19cf1`, SHA-256:

`5718f1c4e87f3e91bb9ca2399d3debdf5c7e4b3f8773e40cc8ebc2b94f757465`

Backend version `enterprise-2026.10.07-ownership.2`, linked runtime source
`fe0151e57d7962783007a7c885962e2f1083641f`, SHA-256:

`37743f5acfd9ec8f83c40e4d5a951a3062bf8197f4c9278ca2e7f3fc3dd6c585`

The client executable is retained at
`build/incident-20261006/super-paqet-recovery`; the backend executable is
`build/incident-20261006/super-paqet-ownership`. Subsequent documentation and
observer commits do not change this executable. The raw Ethernet/IP/TCP packet
shape and sequence/ACK behavior remain preserved.

| Client port, on both .14 and .118 | Peer | Raw endpoint | Application target |
|---|---|---|---|
| 9001 | germany | 91.107.251.85:29999 | 116.202.177.233:2096 |
| 9002 | finland | 65.109.249.222:29999 | 65.109.249.222:2096 |
| 9003 | france | 171.22.132.226:29999 | 171.22.132.226:2096 |

Clients are 89.45.68.14 and 89.45.68.118. Germany retains its second local
116.202.177.233 listener. Client .14 uses source ports 29998/29996/29997.
Client .118 uses 29998/29994/29995 after the 7 October source-tuple recovery;
see [incident evidence](SOURCE-TUPLE-INCIDENT-2026-10-07.md).
The working raw tunnel port is 29999, superseding the initial 2052 request.
Germany administration uses SSH through France, with Finland as a fallback.

All available processors are usable: two vCPUs/~4 GiB per backend, four
vCPUs/~8 GiB per client. No CPU quota or affinity restriction is installed.
LimitNOFILE is 524288 and TasksMax 65536; soft Go memory limits are 1536/4096 MiB
by role. Admission ceilings are safeguards, not measured capacity guarantees.
Services are enabled systemd units with failure restart, live config reload,
validation and owned firewall cleanup. [Recorded configurations](deployed/README.md)
have warn logging and profiling off. One-day diagnostics temporarily enable
sampled debug logs and loopback profiling; their guarded timer returns to those
baseline settings while preserving the revised adaptive profile.

## Fixes and rollout

Continued observation caught a France opening failure burst without a process
restart: 645 client .118 opening errors and 74 server control errors at
22:18–22:20 UTC on 6 October. The captured client stack showed 204 SYN-submission
waiters and 39 failed-opening close waiters. Independently, deterministic tests
reproduced caller scratch-buffer reuse after a timed-out mux write and expired
queued frames still reaching the carrier.

The release owns queued payload bytes until caller and sender release them,
skips expired queued frames and refunds only unsent credit. SYN submission now
shares the opening's reserved receipt deadline; failed new streams locally abort
without another 30-second control-close wait. A busy carrier and its established
forwards survive a failed new opening. Successful PTCPF setup retains normal
close semantics, bounded by the unpublished carrier's setup context. Previously
verified controller/mux/GC/raw-queue fixes remain included. These are confirmed
code defects and fixes, not proof of the original transport stall's cause.

Client .14 was canaried with three authenticated checks before the remaining
hosts were rolled with backups and conditional rollback timers. All nine
post-rollout authenticated 1 MiB checks passed across both client IPs and the
public domain. All main Xray PIDs stayed unchanged. Binary replacement restarted
tunnels and ended established streams on the replaced host; existing clients
also recorded temporary opening failures during server replacement. Client .14's
261 errors belong to that rollout interval and remain visible, rather than being
cleared. Post-rollout counter growth remains monitored.

## Qualification of the ownership transport base

All checks below explicitly bound the outgoing pool at four and use null
encryption, MTU1350, production 4 MiB/2 MiB mux buffers and the deployed manual
adaptive reliability parameters. Namespace tests use the default PA/PA outer
flags; public acceptance verifies the deployed S/PA paths. Local cleanup and
unrelated-rule preservation passed.

| Workload | Result | Scope |
|---|---|---|
| Clean uncapped bulk, 16 streams | Receiver 4.479 Gbit/s upload / 4.836 Gbit/s download | Separate five-second local runs; baseline liveness binary 4.486/4.901 on identical fixed-four parameters. |
| 256-worker churn, RTT 80 → 180 → 80 ms, 100 Mbit/s | 55,512 successful requests, zero workload/client/server errors; 121 carrier retries | 60 seconds; mean 276 ms, p99 histogram upper bound 460 ms. Baseline also passed, so this does not establish a normal-link speed improvement. |
| 2,000 held forwards with asymmetric mixed traffic | All reverified, zero workload/client/server errors; 42.90 Mbit/s bulk | 10/100 Mbit/s, 160 ms RTT, 0.5% loss, jitter, 1% reorder. |
| 10,000 held forwards with HTTP and bulk | All reverified, zero workload/client/server errors; 3.888 Gbit/s bulk | 30-second hold, shared clean laptop link; peak tunnel RSS about 274/304 MiB, no fixture swap. |
| 100,000 held forwards | 5.64 s ramp, 30 s hold, all reverified, zero workload/client/server errors | Mostly idle retention; laptop swapping occurred. |
| TCP/UDP functional integrity | Multi-target 16 MiB checks, seven UDP sizes through 65,507 bytes, TCP half-close and ping passed | Disposable local links, plus HTTP/bulk workload. |

Root full race/vet, full mux race (490.171 seconds)/vet and repeated deterministic
ownership/cancellation/credit regressions passed. [Exact receipts](ownership-qualification-2026-10-07.json)
retain hashes, parameters, resources, internal retries, workload deadline
cancellations, cleanup and deployment verification. Single samples on a shared
laptop are not statistical guarantees or backend WAN capacity measurements.
The 100k test peaked near 1.71/1.77 GiB client/server RSS; separate sampled swap
peaks were about 216/591 MiB. Generators, kernel sockets and other applications
are additional. It does not prove 100k simultaneously busy Xray customers.

Earlier liveness fixtures started with four carriers but omitted `max_sessions`
and permitted growth; churn reached fourteen client carriers. Their real results
remain in [historical liveness receipts](liveness-qualification-2026-10-07.json),
with the corrected scope. They do not qualify a fixed-four pool. The benchmark
now defaults the maximum to the explicit initial count.

## Historical observation before migration.4

Before migration.4, independent ten-second collectors were scheduled until approximately
**2026-10-07 20:18 UTC**. They have been superseded by the fresh window above;
their evidence remains available. Durable summaries retain peaks and within-PID counter deltas across
raw-log rotation and observer restarts; twelve profile slots preserve late-day
capture coverage. Incidents additionally capture bounded qdisc, socket, netstat
and softnet metadata. Observer updates preserve the original end time and do not
restart the tunnel. `completed` marks normal observer completion. Collection
does not itself alert an operator or repair a failure. Temporary diagnostics
previously had a guarded warning-log restore timer; that obsolete timer is now
disarmed, and migration.4 already uses warning logs with profiling off.

The full-day observation is **unfinished**. Paired captures and same-binary
fresh-source tests confirmed selective tuple loss during the .118 France episode.
The dropping device/trigger and causes of other historical episodes remain
unproven. [Verified recovery evidence](SOURCE-TUPLE-INCIDENT-2026-10-07.md) records
the scoped repair and tests. Finland transmit queue drops were observed on the earlier
release and remain monitored; no host qdisc policy was changed. Ordered KCP
carrier head-of-line blocking and the ordinary established-stream 30-second
control-close timeout remain. Preserve incident spools and recovery sets.

See the [incident report](PRODUCTION-INCIDENT-2026-10-06.md),
[deployment guide](DEPLOYMENT.md), [live reload](LIVE-RELOAD.md) and
[operations runbook](OPERATIONS.md). Earlier measurements qualify their named
executables and workloads, not every production load or future failure mode.


## Client source-tuple recovery qualification

The recovery.1 executable passed persistent source-tuple loss, whole-peer outage
and one-conversation loss with four carriers. Independent-peer identities and
requests survived; failed probes preserved the old pool. A selective tuple fault
recovered about 38 seconds after injection. Root race/vet and reload/progress/
cancellation regressions passed. With recovery enabled, clean null bulk measured
4.534/4.831 Gbit/s and changing-delay churn completed 10,535 requests with zero
workload errors. [Exact receipts](path-recovery-qualification-2026-10-07.json)
include successful and insufficient/failed external-origin comparisons.
