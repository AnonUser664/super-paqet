# Current deployment status

Latest local checkpoint: source `3d4387f`, executable SHA-256
`cd693a374a9f4ae3425a43138655920184d15b632b5fe0f9f20e48c99ffd3466`.
The [per-carrier output-pressure qualification](output-pressure-carrier-qualification-2026-10-12.json)
records two pooled-pressure pairs against the exact production executable:
combined bulk median **213.24 → 347.07 Mbit/s**, small-request mean
**1923.91 → 118.65 ms**, and p99 **3671 → 204.5 ms**, with zero errors.
Two clean pairs against the preceding local controller retain median upload
**5.54 → 5.66 Gbit/s** and download **4.21 → 4.23 Gbit/s**.
Asymmetric/reordered and symmetric-loss comparisons, repeated/reverse carrier
recovery, and two-worker live reload pass for the candidate. Its asymmetric
control has body timeouts, retained in the receipt; conditional body-latency
quantiles are consequently not an uncensored latency comparison.
Root race/vet, targeted library races and the full native library suite pass.
Direct production WAN comparisons pass on both shapes. The exact artifact also
opens and verifies 100,000 connections after a 120-second hold, with zero errors;
client/server peak RSS is about 1.65 GiB each, with swap in this laptop experiment.
240 ms RTT connection churn has unchanged mean/p99; two constrained-ACK pairs
retain 98.24% of production median upload. A bandwidth-change pair improves rate
and mean but raises bulk p99 from **2.319 to 3.263 seconds**. A reverse-order repeat
completes without errors but leaves two-pair bulk p99 at **3.127 versus 2.531
seconds** (+23.55%); this adverse result blocks a nonregression claim.
This candidate is **local and not deployed or release-qualified**.

A [local queue-relief pacing ramp](pacing-recovery-models-2026-10-12.json) passes
deterministic temporal models and full root race/vet checks. It limits upward gain
only after recent pressure; its exact-binary performance qualification is pending.

The warning-level visibility follow-up exports each carrier's existing soft-loss
counter on `/metrics` and includes it in finite observer incident detection.
The [observer/race/vet checks](output-loss-observer-checks-2026-10-12.json) pass;
legacy absence, migrated labels, counter resets and process replacement are
covered. No packet-path counter/state or adaptive algorithm changes are added.

The [latest read-only production refresh](production-followup-2026-10-12.json)
through approximately 21:42 UTC on 11 October finds healthy services, unchanged
tunnel/config/Xray identities, zero automatic restarts, no selected journal
warnings or opening-error/retry counter increases since 20:30 UTC. Finland's
worker-0 transmit-drop count rises by 2,714; shared-worker totals cannot identify
the affected carrier. The candidate uses precisely attributed batch losses
rather than propagating that worker's pressure to other clients.

All five hosts run **enterprise-2026.10.09-latency.1** under enabled
`super-paqet.service`. Runtime source `276c43a70873670a7e1665283620aba38594b1ab`,
SHA-256 `256cfc65daaa6596ff2b31e2863296a3182975faa71b1a746295cc6172231958`.
The release includes the selectively reviewed integration and direct-read latency
fix. The binary and tunnel unit/drop-ins retain the accepted deployment. On 11 October,
client configs added TCP ports 8001/8002/8003 to backend 8880 by live reload;
[six HTTP route checks](forward-8880-deployment-2026-10-11.json) pass with unchanged
process PIDs. Existing port-900x definitions remain. Transport configuration: four
independent automatic source slots per country, negotiated connection preservation,
**10s stall / 15s retry / 5s probe**, two backend capture workers, S outbound / PA
return, quoted null encryption, adaptation enabled, warning logs and profiling off.
Logical carriers activate lazily inside the four-slot pool.

All six routes passed two authenticated 1 MiB Reality downloads after rollout.
Actual-host config validation, exact running/disk hashes, unchanged backend Xray
process identities, active/enabled services and zero automatic restarts were
verified. Temporary rollback timers were disarmed and owned staged/probe files
removed; complete rollback archives remain. See the
[9 October rollout](LATENCY-DEPLOYMENT-2026-10-09.md),
[receipts](latency-deployment-evidence-2026-10-09.json) and
[accepted configuration snapshots](deployed/README.md).
This finite check does not establish sustained full-capacity customer traffic or
one-day stability.

The original 24-hour observers completed at **2026-10-09 22:05:41 UTC**,
retaining about 8,560 samples per host beneath
`/var/log/super-paqet-watch/latency1-20261008T220541Z/`. The 10 October refresh
verifies the same running hashes/PIDs, active services, healthy endpoints and zero
automatic restarts. It includes additional opening errors and a Finland migration;
it is not an incident-free stability result.

All five observers were renewed through **2026-10-11 15:52:54 UTC** with separate
archives beneath `/var/log/super-paqet-watch/latency1-review-20261010T155254Z/`.
Successful metrics samples, active journal followers and unchanged tunnel/config/
binary/Xray identities were verified. Client .14 had no warnings in the selected
journal interval, a valid empty record rather than an observer failure.

That window has now completed. The [11 October review](PRODUCTION-REVIEW-2026-10-11.md)
finds substantial failed-opening bursts, France multi-carrier keepalive incidents,
and separate destination-port refusals. Tunnel PIDs/configuration/binary hashes
remain unchanged with zero automatic restarts. Xray identities changed outside
this investigation. New finite observers on all five hosts run through
**2026-10-12 16:38:30 UTC**, retaining separate archives and verified fresh samples.

The [opening investigation](OPENING-FIX-2026-10-09.md) remains a local candidate
on `fix/production-backpressure-20261009`. Its safe same-identity retry passes
race, delayed-backpressure and ownership tests; fixed one-carrier peers retain the
original handshake. Earlier candidate hold/migration/reload controls pass, and
new clean bulk/legacy-client compatibility controls pass. Two ordinary pooled
pressure pairs fail throughput/latency limits; a longer warmed reordering run also
reports eight response-body timeouts. These failed gates remain in the record.
At that earlier checkpoint, candidate source `a348c5c`, SHA-256
`076cbd29ca7c6eec46bf1e1c6001d3194eea9a5f875ee45b18c22bbd10f506a3`, is
**unqualified for production rollout**. Queue experiments were rejected and are
absent from this runtime. No candidate binary/configuration has been promoted to
the five hosts; adaptive established delivery is now a separate investigation.

Source `8e7140e` adds untimestamped RTT-variation handling and controller sample
epoch diagnostics. Its rebuilt local executable SHA-256 is
`70239e4684f9d276d0a653bfd11517e13c07b55ca42f930ebcdd1b883ae1d8c0`.
Race/vet/model regressions pass. Six warmed reordering comparisons finish without
workload errors; independent rate-cap, bottleneck and mixed-load qualification
remains incomplete. The earlier failed gates remain explicit; this executable
is also **not accepted for deployment**.

The independently capped WAN comparison remains diagnostic. Its first
asymmetric pair reports response-body timeouts in both versions; the second
candidate repetition also reports timeouts. These failures do not clear the
earlier gates. On 11 October at 17:06 UTC the operator reported concurrent
laptop use, with unspecified start time and workload. Local performance
comparisons from this session require controlled repeats before acceptance;
preserved failures still require investigation. Production is unchanged.

The twenty-run strict WAN matrix is complete, with four workload failures and
clean owned teardown in all twenty attempts. Quiet-window debug runs expose
thousands of possible spurious retries on a busy carrier. A local follow-up
adds those existing counters to debug samples, prevents timer-only ACKs from
inventing gap-delay evidence, and experiments with a finite 200 ms adaptive
reordering ceiling. Root race/vet, the full native KCP suite, and library race coverage excluding only
the two large echo workloads pass. Two matched asymmetric download pairs improve
median throughput by 18.44%; a loss-only pair shows similar delivery without
errors. The pooled-pressure gate still fails throughput/latency limits. A further
local low-RTT congestion-pacing candidate passes corrected model/race/vet checks,
but fails its standalone pooled latency comparison. The subsequent per-carrier
output-feedback candidate and its current coverage are summarized above.
The first full library race attempt hit its ten-minute deadline and remains a
failed receipt. Test fixtures now explicitly release their accepted carriers.
No new candidate has been deployed; see the
[continued investigation](OPENING-FIX-2026-10-09.md).

The [9 October production log review](PRODUCTION-REVIEW-2026-10-09.md) covers about
17.6 hours. Original PIDs, executable/config hashes and Xray identities remain;
there are no automatic restarts, sampled OOM/task-limit events or capture drops.
The record is not incident-free: France had three preserving carrier recoveries
and three rejected probes across the clients; .118 accumulated 3,387 opening
retries and ten errors in Finland warning bursts. A full mux receive buffer
coincided with those errors. Finland had 45,876 driver transmit queue drops.
All live suspect/recovery-pending/opening-blocked gauges were zero. No production
change or new end-to-end probe was made; slow-consumer opening behavior and queue
pressure remain concrete follow-up work.

## Earlier investigation and local qualification

The [8 October production review](PRODUCTION-REVIEW-2026-10-08.md) covers the
first three hours of the earlier window: all twelve authenticated transfer checks passed,
with no process restarts or OOM events. France suffered a roughly minute-long
carrier incident; four migrations retained sessions and two used replacement
after the originals disappeared. Production settings remained unchanged at that checkpoint. That report did not
complete the full day and was not an incident-free stability pass.

## Reviewed integration candidate

The [selective integration review](INTEGRATION-REVIEW-2026-10-08.md) qualifies
packet checksum/dispatch/output batching, pooled small UDP records, fewer TCP
relay syscalls and bounded credit metadata. The cached backpressure hint uses
one existing pressure word. It preserves payload snapshots and excludes exp1
read batching after completing and rejecting that experiment. Native local
candidate source `c1aee36`, SHA-256
`2962659362143ade1aec169f06991000b2f49e06f791d5adeb2a70db50831ee2`.
This was local source qualification; its selected changes are now included in
the latency.1 deployment above.
See [all integration receipts](integration-qualification-2026-10-08.json),
including adverse results, fixture corrections and finite acceptance limits.

The [two-worker coverage audit](MULTIWORKER-2026-10-08.md) confirms actual traffic
on both workers in 36 earlier comparisons and adds eight mixed-load comparisons,
native live reload and race-instrumented live reload. Correctness and cleanup
pass, but paced mixed HTTP p99 rises **6 → 10 ms** across the new two-pair medians;
the first paced pair has matched role clocks and misses the latency allowance.
The [direct-read follow-up](LATENCY-2026-10-08.md) clears this local latency gate:
two-pair integration comparisons measure mean **1020 → 940 µs** and p99
**7.5 → 6 ms**; independent receive.4 comparisons pass the original allowance.
Runtime source `8f1ca0f`, native SHA-256
`8b538459fa7252afbd38a32161bf69dd2f7270ffc537ea519b0718c244f1209e`.
Two-worker uncapped bulk medians stay within 0.3%, while churn, representative
WAN, 10,000 held flows and native/race live reload pass correctness. The short
adverse reordering result remains visible beside its longer favorable repeat.
See [all 40 follow-up workloads](latency-qualification-2026-10-08.json).
This local qualification preceded the accepted latency.1 fleet deployment above.

## Recovery-grace candidate

The newer [receive-processing investigation](RECEIVE-PATH-2026-10-08.md) retains
that candidate's bounded recovery grace and adds a locally tested receive-only
optimization. Both experimental admission policies were reverted. Three
CPU-controlled pairs measured 3.887 to 4.648 Gbit/s median upload and 3.315 to
3.381 Gbit/s download. WAN comparisons were still in progress at that checkpoint.
Selected receive/recovery changes were subsequently reviewed and integrated;
see the integration and latency rollout above.

At that checkpoint the candidate was locally qualified and **not deployed**.
It adds first-cause
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

The [10:22–10:29 UTC production follow-up](PRODUCTION-FOLLOWUP-2026-10-08.md)
extends archive coverage to about fifteen hours. All original process identities
and config/binary hashes remain unchanged; all twelve authenticated downloads
passed. No later path recovery or error growth occurred, but a full client-side
Finland mux receiver caused 576 opening retries. A narrowly scoped admission
hint for that backpressure is now under local qualification. Production is
unchanged; the one-day observer still ends at 19:14 UTC.

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
