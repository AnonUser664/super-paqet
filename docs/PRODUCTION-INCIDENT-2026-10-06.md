# Customer-traffic incident, 6 October 2026

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
Further qualification and the 24-hour production observation remain in progress.
Private raw receipts/profiles are under `build/incident-20261006`; customer
credentials and stack dumps are not committed.
