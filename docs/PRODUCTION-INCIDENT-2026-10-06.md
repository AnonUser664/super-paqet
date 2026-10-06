# Customer-traffic incident, 6 October 2026

Updated after the `enterprise-2026.10.07-liveness` rollout. The initial recovery
and temporary fallback below are chronological checkpoints; the current binary
includes all listed fixes and all countries again use adaptation. The full-day
observation remains unfinished. Exact release and active topology are in
[STATUS.md](STATUS.md).

The operator reported traffic failure after customers joined, followed by
recovery when users moved away. Initial inspection at 19:53 UTC found all five
tunnel services active. Backend PIDs remained unchanged since deployment; both
clients had clean stop/start events around 19:28 UTC. No recorded tunnel panic,
cgroup OOM kill, admission-limit rejection or degraded config was found in the
inspected records. This evidence does not explain who restarted the clients or
prove the absence of an earlier transient stall.

Client 89.45.68.118 had 2,081 active forwards and 1,276 accumulated errors. Its
warnings included France opening timeouts at 19:16 UTC and carrier/opening errors
after the client restart. Germany/Finland had later control EOFs. Finland's
listener had 57,957 accumulated transmit-queue drops; those totals alone do not
identify the failing interval. Memory and descriptor usage were below limits.
Counters subsequently remained flat during the initial live observation window.

## Confirmed code defects and scoped fixes

1. A new opening on a busy carrier received the entire overall deadline. A
   stalled ordered lane therefore prevented retrying healthy sibling carriers.
   The new receipt budget reserves time for untried carriers. Busy carriers and
   their established streams are retained. Status 2 still grants the full target
   dial budget; application bytes are sent only after successful opening.
   A deterministic two-carrier regression failed before the fix and passed
   afterward, including verification of the already established stream.
2. Accepted mux wrappers had a finalizer that closed their backing streams.
   Promoted methods may retain the backing stream without keeping the wrapper
   reachable, allowing GC to close live I/O. Repeated opening tests exposed the
   race and a forced-GC regression reproduced it. Accepted streams now use
   explicit session/caller ownership, matching outgoing streams. This is a
   confirmed lifecycle defect, not proof of the production outage's root cause.
3. Warn-level output logged only the first five failures for the entire process
   lifetime. Later incidents could be invisible apart from metrics. Warnings
   now retain subsequent causes at a bounded rate of one per ten seconds.
   Counters retain every failure; opening recovery attempts have a new metric.

No KCP framing, raw TCP flags, target routing, encryption or Xray settings are
changed by these fixes. Replacing the binary requires a tunnel restart and ends
that host's established application streams.

## Observation and qualification

A separate `super-paqet-watch.service` began on all five hosts around 19:55 UTC.
It samples every ten seconds and expires after 24 hours, independently of SSH.
Data lives in root-only `/var/log/super-paqet-watch`, with bounded rotating logs
and optional incident-triggered profiles. Temporary debug logging at a
10-second interval and loopback profiling were applied live, with a 24-hour
restore timer. The timer restores the previous exact config only if no later
configuration edits occurred; otherwise it preserves those edits for review.
The observer collects evidence and does not autonomously repair services or
send operator alerts.

The first live five-second client CPU profile sampled approximately 1.08 CPU
cores. Packet I/O syscalls and scheduler synchronization dominated; it did not
establish a deadlock or prove spare capacity during the reported incident.

Root race tests, static analysis and repeated new-opening regressions pass.
An isolated 100 Mbit/s, approximately 80 ms RTT link with 1% loss, 5 ms jitter and
1% reordering passed integrity, HTTP and bulk checks with zero reported errors.
The mux race suite also passed, including its large-transfer regressions
(477 seconds). A 10/100 Mbit/s asymmetric, approximately 160 ms RTT link with
0.5% loss, jitter and reordering passed integrity/HTTP/bulk checks. The combined
idle-target case initially failed because the lightweight target sent 16 MiB
regardless of the requested 1 MiB size; this benchmark mismatch was fixed and
covered with an exact-body regression. Combined qualification and the 24-hour
production observation remain in progress.
Private raw receipts/profiles are under `build/incident-20261006`; customer
credentials and stack dumps are not committed.

## Further backpressure regression (now deployed)

A deterministic duplex test filled a mux session's application receive budget
with unrelated undrained streams, then attempted a reverse transfer larger than
the initial stream credit. The receiver stopped parsing window-update frames
when its shared data buffer was full. Reverse delivery timed out despite the
reverse application's ability to read. This reproduces a genuine mux liveness
failure without WAN loss or production load.

The fix parses fixed-size frame headers and control feedback independently
of data-token availability. Payload admission still waits for the same existing
receive budget. Thirty repeated credit/bounded-payload regressions passed, and a
2,000-held-connection asymmetric mixed HTTP/bulk check reverified every held
connection with zero errors. Shared buffer capacity, buffered bytes and payload
admission blockage are now exposed in metrics and debug samples so a production
incident can be correlated with this condition.

This candidate does not remove KCP's ordered-carrier head-of-line behavior: a
data frame awaiting capacity still blocks later frames in that carrier. It does
not expand the configured receive budget. Definitive attribution of the operator's
outage is still pending. This fix is included in the later liveness rollout.

## Captured recurrence and France fallback

At 20:37–20:41 UTC the recovery release experienced another live France-path
stall. Client .118 errors reached 7,369 and opening retries 3,023; France errors
reached 3,479, primarily control EOF/timeout. No main PID changed, no admission
limit was reached, and no OOM/panic appeared. The path recovered through new
carrier generations before the later manual stack capture. Automatic ten-second
samples and failure-time profiles were retained privately.

Three older France carriers reduced send windows to roughly 4–31 packets and
paced rates to roughly 0.02–0.06 Mbit/s. Their remote receive windows stayed
available. Their learned RTT floors were approximately 80 ms; current RTT had
risen to approximately 180 ms. A fourth carrier retained a larger window.
This establishes adaptive-controller collapse as part of the failure pattern;
it does not establish why the path RTT changed or exclude an underlying link
problem. The receive-budget fix alone is not proven to explain this recurrence.

A deterministic controller fixture reproduces a distinct defect: after a bulk
sample, queued tiny control packets can erase learned bulk capacity because
packet occupancy is mistaken for bulk saturation. The candidate ignores those
byte-rate observations only when acknowledged packets and the byte backlog are
small. Its paired regression retains convergence when real bulk payload is
queued. The completed candidate is included in the later liveness rollout.

France temporarily used `adaptive: false` on its listener and both client peers as a
production fallback. Only France carriers were recreated by live reload;
other routes and all main process PIDs were retained. The guarded diagnostic
restoration was updated to preserve that fallback when returning to warning-level logs.
All nine authenticated 1 MiB checks passed afterward. Germany and Finland retain
their previous adaptive settings. No Xray service/configuration was changed.

## Bounded transmit-queue retry (now deployed)

Finland's root `fq` qdisc reported per-flow-limit drops, and the packet driver's
transmit-drop counter grew during observation. The candidate retries a wholly
rejected `ENOBUFS` batch at most three times, requesting a 50 microsecond sleep
between attempts. Actual scheduler delay may exceed that requested sleep.
Partial/successful sends are returned immediately and never replayed; persistent
failure remains counted loss handled by KCP. This does not change host qdiscs.
Normal successful packet sends have no added timer. Deterministic syscall-result
regressions, root race tests/vet and an isolated 100 Mbit/s, 80 ms RTT integrity,
HTTP and bulk smoke passed. This fix is included in the liveness release; it is not established
as the cause of the France incident.

The observer now captures aggregated stacks before its bounded detailed dump;
this retains all stack categories when thousands of streams exceed the detailed
byte ceiling. The one-day observation is still unfinished.

## Refined controller and outgoing publication fixes

Preserving a learned bulk byte rate alone still paced queued tiny controls using
historical bulk bytes. The revised controller uses packet-window control for a
lane whose acknowledged and pending packets are small, without that byte pacing.
It resumes bulk pacing as soon as a full-size backlog forms, including before
the first new bulk ACK. Deterministic tests cover both capacity preservation for
small traffic and continued convergence for genuine bulk saturation. Customer
payload content is never inspected.

A separate forced-duplex fixture made a peer reply immediately after receiving
SYN, while the local SYN writer had not yet returned. The outgoing stream was
previously registered afterward, so the receive loop discarded that response as
an unknown stream. Streams now enter the receive map before SYN submission.
Failed submissions remove the registration and reclaim early receive credits
without a second potentially blocking control write. The original fixture timed
out; fifty race-enabled publication/cleanup repeats passed after the change.
This is a reproduced defect, not definitive attribution of the France incident.

## Exact release qualification and promotion

Root race tests/vet, the full mux race suite (495 seconds), mux vet and repeated
credit/GC regressions passed. Four shared carriers and production 4 MiB/2 MiB mux
buffers were used for the following checks of the exact liveness binary:

- Clean uncapped null bulk: receiver 4.634 Gbit/s upload / 4.808 Gbit/s download,
  sixteen streams, separate five-second measurements without startup omission.
- 100,000 held forwards: 11.93-second ramp, 120-second hold, every connection
  reverified, zero errors. The shared laptop used swap; this is retention
  evidence, not backend capacity for 100k active Xray customers.
- 256-worker churn over a 100 Mbit/s link with RTT changing 80 → 180 → 80 ms:
  48,714 successful requests, zero unexpected errors, mean 315 ms, p99 histogram
  upper bound 4.194 s. The old recovery binary recorded 181 failures in the
  corresponding fixture. Individual timings depend on shared-machine scheduling.
- Asymmetric 10/100 Mbit/s, 160 ms RTT, loss/jitter/reorder mixed load: all 2,000
  held forwards reverified, zero workload errors, bulk 35.67 Mbit/s.
- Narrow 1 Mbit/s, 100 ms RTT, 2% loss: cold 1 MiB integrity in 12.32 s, 499 HTTP
  responses without unexpected errors. Four concurrent bulk responses did not
  finish in their 15-second interval; partial payload and cancellations remain
  explicit in the receipt.

Static changing-delay churn **still failed**. The most recent static diagnostic
run had 90 request timeouts and 37 resets. Static fallback therefore served as
an operational pause during qualification, not a claim of universal stability.
The revised adaptive binary was canaried on client .14 before the remaining
servers and client .118 were rolled. All nine authenticated 1 MiB checks passed.
France adaptation was re-enabled on the new binary; diagnostic restoration now
preserves it. Main Xray PIDs were checked against pre-incident values and were
unchanged. Binary replacement deliberately restarted tunnel processes; those
restarts end existing streams. Backups/incident artifacts remain retained.

[Machine-readable evidence](liveness-qualification-2026-10-07.json) includes
parameters, hashes, memory/swap, cancellations and failed comparisons. Earlier
incident fixtures omitted explicit mux buffer values and used 32 MiB/16 MiB
instead of production 4 MiB/2 MiB; their successes do not qualify those smaller
production ceilings.

## Monitoring continuity and outstanding limits

Collectors retain full-window peaks and within-PID counter deltas in durable
`summary.json`, across raw-log rotation and observer restarts. Twelve rotating
profile slots retain late-day capture coverage; each has PID/time/cause metadata.
The deadline remains approximately 2026-10-07 20:18 UTC. Guarded diagnostic
restoration remains around 19:57 UTC; updating its configuration target did not
reset its timer. The one-day observation is not complete.

Backend transmit drops remain observed after rollout and continue to be
monitored. No underlying cause of the earlier RTT increase is proven. KCP
ordered-carrier head-of-line blocking remains; static changing-delay churn and
end-to-end timeout limits require further qualification. No claim of arbitrary
saturation capacity or every-link optimality follows from these checks.
