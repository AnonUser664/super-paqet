# Current deployment status

The ownership.2 release runs on all five hosts. All six routes retain four
shared-source KCP carriers (`sessions: 4`, `max_sessions: 4`), adaptation enabled,
client S / backend PA flags, quoted null encryption, AF_PACKET with one receive
worker and MTU1350. France is 171.22.132.226.

## Exact release and topology

Version `enterprise-2026.10.07-ownership.2`, linked runtime source
`fe0151e57d7962783007a7c885962e2f1083641f`, SHA-256:

`37743f5acfd9ec8f83c40e4d5a951a3062bf8197f4c9278ca2e7f3fc3dd6c585`

The exact executable is retained at
`build/incident-20261006/super-paqet-ownership`. Subsequent documentation and
observer commits do not change this executable. The raw Ethernet/IP/TCP packet
shape and sequence/ACK behavior remain preserved.

| Client port, on both .14 and .118 | Peer | Raw endpoint | Application target |
|---|---|---|---|
| 9001 | germany | 91.107.251.85:29999 | 116.202.177.233:2096 |
| 9002 | finland | 65.109.249.222:29999 | 65.109.249.222:2096 |
| 9003 | france | 171.22.132.226:29999 | 171.22.132.226:2096 |

Clients are 89.45.68.14 and 89.45.68.118. Germany retains its second local
116.202.177.233 listener. Source ports stay 29998/29996/29997 respectively.
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

## Qualification of the deployed executable

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

## Observation still in progress

Independent ten-second collectors continue until approximately **2026-10-07
20:18 UTC**. Durable summaries retain peaks and within-PID counter deltas across
raw-log rotation and observer restarts; twelve profile slots preserve late-day
capture coverage. Incidents additionally capture bounded qdisc, socket, netstat
and softnet metadata. Observer updates preserve the original end time and do not
restart the tunnel. `completed` marks normal observer completion. Collection
does not itself alert an operator or repair a failure. Temporary diagnostics
return to warning logs around 19:57 UTC, subject to the unchanged-config guard.

The full-day observation is **unfinished**. The underlying transport stall cause
remains unproven. Finland transmit queue drops were observed on the earlier
release and remain monitored; no host qdisc policy was changed. Ordered KCP
carrier head-of-line blocking and the ordinary established-stream 30-second
control-close timeout remain. Preserve incident spools and recovery sets.

See the [incident report](PRODUCTION-INCIDENT-2026-10-06.md),
[deployment guide](DEPLOYMENT.md), [live reload](LIVE-RELOAD.md) and
[operations runbook](OPERATIONS.md). Earlier measurements qualify their named
executables and workloads, not every production load or future failure mode.
