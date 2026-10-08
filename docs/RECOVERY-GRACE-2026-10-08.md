# Bounded recovery grace qualification — 8 October 2026

Production remains `enterprise-2026.10.07-migration.4`. This report describes a
locally tested candidate; no customer host was restarted or reconfigured for this
work. The complete day-long production observation window is still in progress.

## Problem and change

The production review found two original France sessions disappearing before a
successful source replacement. The old warning logs could not prove their closure
cause. Ordinary mux keepalive sampling can expire a silent carrier before several
15-second-spaced, five-second-budget candidate probes succeed. Physical migration
cannot preserve a session that has already closed.

The candidate retains the first closure cause and adds warning-level closure and
preservation fallback reasons. Optional `kcp.smux_recovery_grace` grants a fixed
extra watchdog budget only after migration negotiation, with live streams.
Default zero retains ordinary timeout behavior; accepted values are integer
seconds from zero to 120. A value of 60 was qualified on both ends. It adds no wire
command or outer packet change, stream timer, extra goroutine or replay buffer.
Permanent errors, application deadlines/cancellation and explicit close retain
precedence. Existing receive-buffer backpressure keeps its ordinary behavior.

Failed probes cannot renew the deadline. Actual inbound activity clears it;
another delivery outage can receive a fresh bounded budget. Expiry is checked by
the existing ping cadence, subject to scheduler jitter. Enabling grace retains
existing queues and backend TCP sockets longer during a dead path. Changing the
setting replaces the affected mux endpoint on live reload, so enabling it on an
occupied deployment requires a planned interruption.

Requirements, defaults and reload impact are in [configuration](CONFIGURATION.md)
and [live reload](LIVE-RELOAD.md). New warning fields and live-carrier metrics are
in [diagnostics](DIAGNOSTICS.md). Architecture and ownership remain documented in
[architecture](ARCHITECTURE.md).

## Method and limits

All faults use owned client/router/backend Linux namespaces. Router ingress drops
occur before AF_PACKET capture, rather than backend INPUT rules that cannot model
this failure correctly. Every run checks namespace/firewall cleanup. Faults use
80 ms RTT, four independent sources, two backend capture workers, S outbound/PA
return, quoted null encryption, 10-second stalls, 15-second retries, five-second
probes, sequenced 64 KiB echo payloads and an independent healthy peer.

Performance runs retain four sessions, two backend workers, adaptive transport,
manual 30 ms reliability updates, 4096 segment windows, MTU 1350 and the deployed
mux/ACK settings. They use 32 iperf streams, 64 clean HTTP churn workers, or 16 WAN
workers. The first pass compares the exact deployed binary. Subsequent comparisons
rebuild its source and the candidate with the same Go/compiler/native libraries,
limit each tunnel to four Go CPUs, alternate execution order, and fix source ports.
Fixed source tuples remove one input variable. Capture-worker distribution
still varies in some fresh namespaces and is measured rather than assumed. No host sysctl or production route is changed.

These are finite local regression checks. They do not certify censorship behavior,
server NIC capacity, busy-customer throughput or uninterrupted one-day operation.
Short HTTP latency values are histogram upper bounds, not exact percentiles.
Canceled requests at the workload deadline are reported separately from failures.

## Qualification results

All **eight fault controls and 60 performance/capacity runs** completed with
successful fixture assertions and cleanup. Grace.1 was the initial candidate;
**grace.2** (`4abe410` runtime source) is the accepted local candidate. Its exact
SHA-256 is `ce15098d39e0fdb1a7feb3e1b27be91a876c42218dca42517206736a1a5db994`.
The rebuilt migration.4 control is
`cfa07b502050309385eb4f9f6106e416acb7c30dd536c7da5cd6d425a9a3d2de`.
All measured receipts, commands, worker counts and repetitions are in
[the compact evidence](recovery-grace-qualification-2026-10-08.json).
Private raw logs, captures and profiles remain under
`build/recovery-grace-20261008/` outside Git.

| Fault control | Result |
|---|---|
| Exact deployed migration.4; fresh tuples blocked until second 70 | All four held streams lost; healthy peer had 337 successes and zero failures. |
| Grace.1, 60-second grace, same fault | All four original conversations/streams survived 16 failed probes; 218 successful post-fault exchanges. |
| Grace.2, grace disabled, same fault | All four held streams lost, confirming default expiry is retained. |
| Grace.2, 60-second grace, same fault | Backend grace was observed; all four conversations/streams survived 16 failed probes without integrity errors. |
| Grace.2, fresh tuples blocked until second 150 | Four original backend carriers expired at workload seconds 120.4–123.3 with `recovery_grace_expired`; all four held streams lost as required; healthy peer had 594 successes and zero failures. |
| Reverse-direction loss | One preserving move; all four held streams survived. |
| Repeated tuple loss | Two preserving moves; all four held streams survived. |
| Newly adopted tuple blocked again | Two preserving moves; established streams survived; exactly one early-stall warning as required. |

The default delayed-probe recovery finished around workload second 79, after
roughly 74 seconds of blocked original delivery. Grace does not shorten the probe
retry interval; its benefit is retaining the logical session until verification
succeeds. Grace ends on real input and is not renewed by each failed probe.
Wire checks retained fixed S/PA flags and existing sequence/ACK/options/timestamp
rules. Identical reloads retained adopted ports and live siblings; retired rules
were removed without touching unrelated guards.

Healthy performance below uses grace.2 with grace enabled and the rebuilt
migration.4 control. Bulk numbers are receiver payload rates. The one-worker
set has three repetitions per version; the both-worker row is the first matching
worker-distribution pair. Other rows have two repetitions per version.

| Workload | Migration.4 control | Grace.2 |
|---|---:|---:|
| Clean bulk, all four sources on one worker, median upload/download | 3.251 / 2.996 Gbit/s | 3.220 / 2.949 Gbit/s |
| Clean bulk, both workers carrying substantial traffic, matching pair upload/download | 5.018 / 4.190 Gbit/s | 5.248 / 4.206 Gbit/s |
| Clean HTTP churn, median requests/s | 11,177 | 11,067 |
| Clean HTTP churn, median p50/p99 histogram bounds | 6 / 12.5 ms | 6 / 12.5 ms |
| 80 ms RTT, 50/10 Mbit/s, 0.5% loss, 5% reorder: HTTP requests/s | 168.06 | 169.82 |
| Same impaired link: median bulk payload | 5.835 Mbit/s | 5.679 Mbit/s |
| 240 ms RTT, 20 Mbit/s: HTTP churn requests/s | 30.70 | 30.67 |
| Same high-delay link: median p50/p99 bounds | 482 / 1377 ms | 482 / 1360 ms |
| 10,000 held, verified, then closed connections | Zero errors | Zero errors |
| 10,000 connections: median client/backend peak RSS | 209.4 / 210.5 MiB | 206.3 / 213.2 MiB |

Clean churn was 0.98% lower; one-worker upload/download were 0.97%/1.54% lower;
impaired-link bulk was 2.67% lower. These finite comparisons fall within the
working five-percent throughput regression guard. Churn CPU medians were
2.149/2.162 client/backend cores versus 2.124/2.131. One-worker bulk CPU medians
were essentially unchanged. Median held-connection RSS was within two percent
on each endpoint. Every workload reported zero application errors; expected
end-of-workload cancellation remains separately recorded.

The second both-worker-set baseline run placed almost all captured traffic on
one worker despite fixed source ports. Its upload was 3.393 Gbit/s, versus the
candidate's 6.431 Gbit/s with work distributed across both workers. The aggregate
medians of that two-run set (4.205 versus 5.839 Gbit/s) therefore **do not establish
a throughput improvement caused by grace**. Worker telemetry is necessary to
interpret the numbers. The three one-worker repetitions and first both-worker
pair provide the more directly comparable checks.

## Failed gates and profiling

The first exact-binary pass had substantial bulk variation. Rebuilding both
versions with the same compiler/native libraries did not eliminate it: the first
candidate's unconstrained-source bulk medians were lower by roughly a quarter.
That gate was not accepted. Capture counts showed that one candidate run put
nearly every packet on a single worker, while several control runs used both.
This is evidence of a confounder, not proof that the feature caused or cured the
entire difference.

Recovery fields were moved to the end of `Session` and `Config` to preserve all
existing hot-field offsets. This is a conservative layout precaution; the tests
do not isolate a cache-layout effect. The following runs fixed initial source
ports and recorded actual worker distribution. The first delayed-baseline attempt
also failed before startup because the retained Debian executable needed its
`libpcap.so.0.8` search path; rerunning with the retained library passed the negative
control and cleanup. That was a fixture environment failure.

Ten-second bulk profiles still attribute about 39–43% of client sampled CPU to
syscalls and about 23–25% cumulatively to KCP fast-ACK parsing. Recovery/grace
logic is not a bulk hot path. No speculative change to batching, ARQ or packet
shape was introduced from those samples. Further syscall/ACK optimization needs
its own controlled integrity, loss and latency qualification.

The laptop is an Intel i5-13420H with 12 logical CPUs, Linux
`7.2.4-arch1-2`, Go `go1.27.0-X:nodwarf5`. Controlled comparisons set four Go CPUs
per tunnel; they are not pinned physical-core or remote-NIC capacity measurements.

## Code checks

The full application race suite passed, as did application vet. The full mux
normal suite passed after the layout edit; the full mux race suite passed before
that field-only edit, with the new watchdog/first-cause race tests repeated after
it. All mux benchmarks ran before and after the layout change. Deterministic tests
cover negotiation, zero/invalid grace values, exact expiry, nonrenewal by repeated
probes, input resetting grace, receive-buffer backpressure, application departure,
first terminal cause under concurrent close, supported endpoint roles and reload
replacement. No owned namespace or fixture process remained after qualification.

A read-only five-host snapshot at 00:55 UTC confirmed all original production
PIDs, zero restarts, accepted config hashes and identical binary hashes. France
had another nonpreserving recovery after two failed probes at 23:11 UTC. This
reinforces the diagnostic need without proving keepalive was its closure cause.
The observer's complete day still ends at 19:14 UTC; this is not a day-long pass.

## Reproduce the fault controls

Build the candidate and retain a compatible baseline executable. The scripts
require root and installed `ip`, `tc`, `iptables`, `tcpdump`, `iperf3`, Go and Python.
The namespace workload helper is `build/spq-bench` from `cmd/bench`.

```sh
go build -o build/super-paqet ./cmd
go build -o build/spq-bench ./cmd/bench
sudo python3 scripts/path_recovery_bench.py --binary build/super-paqet \
  --case delayed-probes --preserve-connections --sequenced-payloads \
  --held-payload-bytes 65536 --stall-seconds 10 --recovery-grace-seconds 60 \
  --output build/grace-survival
sudo python3 scripts/path_recovery_bench.py --binary build/super-paqet \
  --case delayed-probes --preserve-connections --sequenced-payloads \
  --held-payload-bytes 65536 --stall-seconds 10 --expect-session-loss \
  --output build/grace-zero-control
sudo python3 scripts/path_recovery_bench.py --binary build/super-paqet \
  --case delayed-probes --preserve-connections --sequenced-payloads \
  --held-payload-bytes 65536 --stall-seconds 10 --recovery-grace-seconds 60 \
  --probe-outage-seconds 150 --expect-session-loss --output build/grace-expiry
```

The default delayed-probe case admits fresh tuples at workload second 70; original
four tuples remain blocked. The expiry case delays admission to second 150.
Reverse faults, repeated migration and newly adopted tuple failure use
`--case reverse-established`, `repeat-established` and `early-stall` respectively.
The scripts check surviving stream integrity, healthy-route ownership, the pool
ceiling, unchanged flag/header formulas and retirement of only owned rules.

## Rollout decision

No production rollout has been performed. Grace remains opt-in and the recorded
production configs remain unchanged. The first implementation checkpoint is
`bfa814c`; checkpoint `4abe410` appends recovery fields after existing mux fields
and extends the deterministic fixture. Exact executable receipts accompany the measurements above.
