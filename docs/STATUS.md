# Current deployment status

The liveness release runs on all five hosts after the 6 October customer-traffic
incident. All six routes use four shared-source KCP carriers with adaptation
**enabled**, client S / backend PA flags, quoted null encryption, AF_PACKET with
one receive worker and MTU1350. France's temporary static fallback was retired
only after the revised controller passed changing-delay stress and the binary
was replaced. 171.22.132.226 is France.

## Exact release and topology

Version `enterprise-2026.10.07-liveness`, source
`323d5dc4b548678ab3e8c6d5f3e6b15ee832588b`, SHA-256:

`04508dd5254663042f161c812e001d03e3c8491a2ce37c2034778cf7e2de05bf`

The exact executable is retained at
`build/incident-20261006/super-paqet-liveness`. Later source checkpoints include a staged write-ownership/opening-deadline fix;
that candidate is not yet deployed.
The original raw Ethernet/IP/TCP shaping and sequence/ACK behavior are preserved.

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

## Fixes and verified rollout

The initial recovery release fixed busy-carrier opening retry budgets, accepted
stream GC lifetime and suppressed later warnings. Continued observation caught
another France stall without a process restart/OOM. Three lanes collapsed their
windows/pacing after bulk traffic while RTT rose. Deterministic tests reproduced
small-control capacity erosion, mux control starvation under full receive buffers,
and an outgoing-stream publication race that could discard immediate replies.
The current release fixes those defects and adds bounded wholly-rejected
AF_PACKET ENOBUFS retries. Details, causality limits and failed checks are in the
[incident report](PRODUCTION-INCIDENT-2026-10-06.md).

Client .14 was canaried first; France, Finland, Germany and client .118 followed
with per-host backups and conditional rollback timers. Replacing the executable
restarted each tunnel and ended that host's established streams. France adaptation
on .14 was then enabled by scoped live reload with its main PID unchanged.
All nine authenticated 1 MiB checks passed after rollout: both client IPs and the
public domain, on ports 9001/9002/9003. No main Xray configuration/service was
edited or restarted. Rollback archives and incident evidence are retained.

## Qualification of this exact executable

The earlier fixtures below start with four shared carriers, null encryption,
MTU1350 and production 4 MiB aggregate / 2 MiB per-stream mux buffers. They
omitted `max_sessions`, allowing automatic pool growth; changing-delay tests
reached fourteen client carriers. They therefore do **not** qualify the deployed
fixed-four pool. The fixture now explicitly defaults `max_sessions` to `sessions`.
Disposable namespace cleanup and unrelated firewall-rule preservation passed.

| Workload | Result | Limit |
|---|---|---|
| Clean uncapped bulk, 16 streams | Receiver 4.634 Gbit/s upload / 4.808 Gbit/s download | Separate five-second runs without startup omission; laptop capacity, not backend WAN capacity. |
| 100,000 held TCP forwards | 11.93 s ramp, 120 s hold, all 100,000 reverified, zero errors | Mostly idle retention; shared 8 GiB laptop used swap. |
| 256-worker connection churn, RTT 80 → 180 → 80 ms, 100 Mbit/s | 48,714 successful requests, zero unexpected errors, mean 315 ms | One 60-second run; p99 histogram upper bound 4.194 s. |
| 2,000 held forwards beside asymmetric mixed load | All reverified; zero workload errors; bulk 35.67 Mbit/s | 10/100 Mbit/s, 160 ms RTT, 0.5% loss, jitter and 1% reorder. |
| 1 Mbit/s, 100 ms RTT, 2% loss | Cold 1 MiB integrity passed in 12.32 s; 499 HTTP successes, zero unexpected errors | Four bulk responses did not finish within 15 s; streamed rate 0.693 Mbit/s and deadline cancellations are recorded. |

Root race tests/vet, the full mux race suite (495 seconds), mux vet and repeated
publication/credit/GC regressions passed. [Machine-readable evidence](liveness-qualification-2026-10-07.json)
retains parameters, hashes, resources, deadline cancellations and failed static
comparisons. Static changing-delay churn still failed: the latest run had 90
request timeouts and 37 resets. The old recovery adaptive run had 181 request
failures; the corresponding new adaptive run had zero. Timing comparisons are
individual runs on a shared machine, not universal guarantees.

The 100k fixture peaked at about 1.34/1.68 GiB client/server RSS, with sampled swap
peaks around 338/398 MiB. Generators, kernel sockets and other applications are
additional. It does not establish 100k simultaneously busy Xray customers or
capacity under the backend memory limits. Older incident fixtures omitted explicit
mux buffers and used larger defaults; those results do not qualify production
buffer ceilings.

## Observation still in progress

Independent ten-second collectors continue until approximately **2026-10-07
20:18 UTC**, even if SSH disconnects. `/var/log/super-paqet-watch/summary.json`
retains full-window peaks/counter deltas across raw-log rotation and observer
restarts; twelve rotating incident-profile slots preserve late-day coverage.
`completed` marks normal collector completion. Collection does not itself notify
an operator or repair a failure. Temporary diagnostics return to warning logs
around 19:57 UTC, subject to the unchanged-config guard.

The full-day observation is **unfinished**. A later burst at 22:18–22:20 UTC
recorded 645 failed France openings on client .118 and 74 server control errors
with both processes unchanged; errors then stopped. Captured .118 stacks show
204 openings waiting to submit SYN and 39 failed openings waiting in ordinary
stream close. Those waits are addressed by the staged deadline/abort fix; this
is evidence of the blocked paths, not proof of the original transport stall cause.
 Successful acceptance and local
regressions do not establish freedom from future customer stalls, universal WAN
optimality or multi-gigabit capacity on these backends. Underlying causes of the
captured RTT increase remain unproven. Backend transmit drops were observed and
remain monitored. Preserve incident spools and recovery sets until review.

Earlier backend load/bulk measurements belong to earlier executables and remain
in [production evidence](production-deployment-evidence.json) and the
[deployment report](FINAL-DEPLOYMENT-REPORT.md). Operational instructions are in
[deployment](DEPLOYMENT.md), [live reload](LIVE-RELOAD.md) and [operations](OPERATIONS.md).
