# Independent KCP carrier recovery — 7 October 2026

The candidate is implemented and locally qualified. **It has not been deployed.**
Deployment to the five production hosts waits for the user's review. This work
used disposable local namespaces; production tunnels, Xray, service files and
running configurations were not changed.

## Exact candidate

- Version: `enterprise-2026.10.07-carrier.4`
- Linked runtime source: `9cb1c6ab69bb0106cefadb0b6d5c1771a97be9b2`
- Executable SHA-256: `6bc3832e4287645b0cfd9565a54d4eafd76dfa05b2d6196f71e81eece894842c`
- Executable: `build/carrier4-qualification/super-paqet-carrier-tuned`
- Dynamic requirements: libpcap.so.0.8, maximum imported GLIBC version 2.34.
- [Compact qualification receipts](carrier-recovery-qualification-2026-10-07.json)
  retain commands, parameters, individual samples, counters and teardown checks.
- [Five proposed configurations](proposed-source-ports/README.md) are review
  artifacts. `deployed/` still describes the actual running shared-source release.

## Recovery behavior

Each outgoing country peer has four fixed KCP/mux slots on **four independently
reserved client source ports**. Proposed clients use `shared_source: false` and
local IP port zero. Initial reservations and replacements select available ports
in 32768–65535; TCP guards keep live ports from colliding with another reservation.
There is still one backend destination port, 29999. S outbound / PA return,
null encryption, the fabricated Ethernet/IP/TCP envelope, target/customer ports
and country peer names remain preserved.

A one-second sampler checks only carrier count, not customer count. A slot
qualifies after either three transport-opening attempt failures since its last
ACKed outbound progress/successful opening and 15 seconds without that progress,
or 15 seconds of pending outbound data with active streams and no ACK advancement.
Inbound-only traffic cannot conceal the outbound stall. Target rejection/dial
failure does not count as transport failure. Empty queues and idle carriers do
not trigger periodic port changes. A live zero receive window suppresses both
recovery and suspicion, including expired openings; evidence is retained until
ACK progress or window reopening resolves the condition.

New openings prefer nonsuspect sibling slots. If all slots are suspect, fallback
is retained so an original path can resume. A candidate uses a new source port
and its own socket/firewall ownership. It must complete a PPING/PPONG exchange
over normal KCP/mux within five seconds. A successful local write is insufficient.
There is at most one candidate per slot and four concurrently per process. The
**15-second cooldown is between attempt starts**, independently per slot; it is
not an additional 15-second wait after a failed five-second probe.

The old slot remains available while probing. Failed probes remove candidate
resources and retain the original slot. Before adoption the engine rechecks
configuration generation, slot identity, cancellation and old-slot progress.
A resumed old carrier or concurrent configuration change discards the candidate.
Adoption replaces only the failed slot, retaining sibling carriers, peer context,
local forwarding listeners and other peers. The replaced slot's established
TCP streams close and applications reconnect; there is no transparent stream
migration. Backend state on an unreachable retired tuple may persist until the
existing session timeout; a client cannot send its close through a blocked path. Old rules are removed individually, and the old guard is retained
until cleanup succeeds. Config reload keeps adopted ports; recovery never edits
YAML. Process restart with port zero chooses fresh reservations.

The public pool remains fixed at four, with bounded temporary probe resources.
No kernel connection tracking, per-customer failure scan or cross-carrier
reordering layer was added. Existing KCP ordering remains per carrier. Owned
NOTRACK, RST suppression and local TCP isolation rules remain scoped per tuple.
Explicit old `30s` configuration values still work; the defaults and proposed
profiles use **15s stall / 15s retry / 5s probe**. Eligibility, sampler cadence,
setup/probe latency and backpressure mean 15 seconds is not a hard outage SLA.
A whole-IP outage cannot be repaired by a source-port change.

New fixed peer/session metrics report suspicion, pending recovery and slot
opening failures. Logs identify peer, session index and old/new source ports.
The slot failure counter resets on replacement; process recovery totals remain
cumulative. A successful probe proves transport reachability, not Xray health.

## Profiling and corrections

An untuned separate-port build regressed bulk with one backend capture worker.
Profiles showed receive overload and dense KCP fast-ACK handling repeatedly
reading the clock per outstanding segment. Backend proposals now use **two
capture workers** for their two vCPUs. Distinct tuples can distribute receive
work across workers; packet hash distribution can still be uneven.

Fast-ACK handling samples monotonic time once per ACK event and skips an event
with no earlier outstanding segment. Sequence/timestamp gating, thresholds and
wire representation are retained. The dense 4096-segment microbenchmark fell
from median 76,799 to 12,258 ns/event, about 6.3× faster; sparse handling stayed
near 90 ns/event. Differential traces, sequence wrap, index growth/tombstones,
clock-call limits and the unchanged fork's full race suite passed.

Qualification also corrected evidence lost before the first health sample,
stale-slot admission after retirement, ownership of retired-port cleanup and
zero-window backpressure with timed-out openings. The zero-window regression
was first reproduced by a failing deterministic test, then passed after the
fix; the exact corrected executable repeated the local matrix below. An old
one-conversation fixture incorrectly expected no source change under the new
per-slot design; its assertion now requires all unaffected siblings to survive.
Initial baseline launches missing libpcap exited before measurement and are not
throughput results. These development artifacts remain in the private build
qualification folders.

## Measured results

Both endpoints and generators share an Intel i5-13420H laptop: eight physical
cores, twelve logical CPUs and about 7.6 GiB RAM. Tests constrain Go parallelism
to client four / backend two and use two backend capture workers in **both**
compared releases. This models scheduling budgets, not dedicated cloud CPUs.
Baseline is recovery.1 (`5718f1c4…`), run at both endpoints. Its socket/KCP/mux
transport code matches the current ownership.2 backend; compatibility with the
actual ownership.2 executable is also checked separately.

Both profiles use four carriers bounded at four, null encryption, S/PA,
MTU1350, adaptive window control and the deployed manual 30 ms reliability
settings with 4 MiB/2 MiB mux buffers. Adaptive buffer/timestamp/credit extensions
remain off. These comparisons qualify the complete port/worker/clock change,
not an isolated source-port-only speed claim.

| Received payload | Baseline recovery.1 | Candidate carrier.4 |
|---|---:|---:|
| Clean upload, median (range), Gbit/s | **3.531** (3.501–5.063) | **5.881** (5.417–5.922) |
| Clean download, median (range), Gbit/s | **3.236** (3.141–5.192) | **4.183** (4.099–4.262) |
| 1 Gbit/s capped upload, Gbit/s | 0.903 | 0.897 |
| 1 Gbit/s capped download, Gbit/s | 0.935 | 0.935 |

Clean bulk is received application payload from three alternating pairs,
16 iperf streams and eight measured seconds per direction, with 16 MiB preflight
integrity checks. Ranges expose run variation; medians do not promise a speedup
on every run. At the uncapped ceiling some capture drops remain and KCP recovers
transport loss. Workload success is not evidence of a zero-loss maximum.

At the clean ceiling the candidate carries more traffic and uses more absolute CPU (upload: occupied tunnel CPU median 2.82 → 4.45 cores, CPU per delivered Gbit/s 0.801 → 0.753; download: occupied tunnel CPU median 2.66 → 3.46 cores, CPU per delivered Gbit/s 0.822 → 0.816). Per-gigabit costs are sample summaries, not universal efficiency guarantees. At the 1 Gbit/s cap, total occupied CPU was 1.279 → 1.217 cores upload and 1.327 → 1.340 cores download.

The 1 Gbit/s profile uses ten measured seconds per direction. The cap applies
to outer traffic; payload is lower after headers and ACKs. The candidate adds
six client FDs per fully open four-carrier peer (three extra raw sockets and
three extra guards); it does not add KCP sessions relative to the old four-slot
pool. Per-socket capture/storage budgets and kernel TCP sockets lie outside the
Go heap limit. RSS and swap samples for every run are retained in the receipts.

Seeded middle-bridge WAN tests used HTTP connection churn for twenty seconds,
one MiB cold integrity checks, and zero workload errors:

| Shaped link / HTTP churn | Baseline requests/s, mean ms, p50 bound ms | Candidate requests/s, mean ms, p50 bound ms |
|---|---:|---:|
| 160 ms RTT; 10/100 Mbit/s; 0.5% loss, 1% reorder; 128 workers | 193.69; 649.5; 545 | 181.94; 692.4; 615 |
| 400 ms RTT; 50/5 Mbit/s; 1% loss, 10% reorder; 64 workers | 47.45; 1312.6; 1204 | 45.60; 1357.4; 1196 |

The p99 histogram upper bound was 4194.304 ms in all four WAN samples. In this corrected-binary round candidate request rates were 6.1% and 3.9% lower; an earlier same-transport development round was within 1% and 8% higher, respectively. Both rounds are retained, so no consistent WAN speedup is claimed.

RTT labels are base propagation RTT before serialization/queueing. Both WAN
comparisons are single seeded pairs and vary with scheduling and packet arrival
order. No candidate source recovery or capture drops occurred on these profiles.
HTTP latency includes opening and response handling; p99 values are histogram
upper bounds, not precise quantiles. Workload-deadline cancellations are recorded
separately from failures.

10,000 held forwards were all reverified beside 3.831 Gbit/s bulk and 762 HTTP requests/s, with zero workload errors and no tunnel swap. The 100,000-forward test ramped in 8.10 seconds, held for twenty seconds and reverified all 100,000 with zero errors. It used client/backend Go soft budgets of 4096/1536 MiB. Peak tunnel RSS was 1.72/1.57 GiB; separate sampled swap peaks were 131/139 MiB. RSS and swap peaks need not coincide. Kernel socket storage and generators are additional. This qualifies mostly idle retention, not 100,000 simultaneously busy users.

Null/AES IPv4 functional checks passed multi-target 16 MiB integrity, UDP
boundaries at seven sizes through 65,507 bytes, TCP half-close and tunnel ping.
IPv6 TCP integrity/load passed separately. Five proposed YAML files passed the
validation CLI verbatim on synthetic namespaces carrying their configured
interface names/local IPs, with no firewall/listener creation. Actual-host
preflight remains part of the reviewed deployment.

## Failure qualification

Faults are injected before endpoint AF_PACKET capture on a middle bridge with
80 ms base RTT. An independent peer continuously verifies echoed payloads.
Expected requests lost during the injected fault remain recorded as failures;
they are not silently counted as successful recovery.

| Injected fault | Observed outcome |
|---|---|
| One client source tuple | 1 verified replacement(s); source change 16.83 s after injection. |
| One return tuple | 1 verified replacement(s); source change 15.74 s after injection. |
| Established-only outgoing tuple | 1 verified replacement(s); source change 15.73 s after injection. |
| Established-only return tuple | 1 verified replacement(s); source change 15.77 s after injection. |
| All four original client source tuples | 4 verified replacement(s); source change 16.85 s after injection. |
| One KCP conversation | 1 verified replacement(s); source change 16.87 s after injection. |
| Same slot blocked again | Two verified replacements of slot zero; three sibling identities survived. |
| Five-second source loss | No source hop; original path resumed. |
| Whole-peer loss for 65 seconds | 12 failed probes kept original slots; traffic resumed after restoration. |
| Candidate against ownership.2 backend | 1 verified replacement(s); source change 15.76 s after injection. |
| Legacy shared-source layout | 1 verified replacement(s); source change 22.82 s after injection. |

All tests retained the four-slot ceiling, unaffected peer identity and zero
independent-peer workload errors. Established-only tests opened four streams
once, without new-opening failure evidence, and retained the other three streams.
Repeated blocking recovered the same slot twice. Five-second loss restored
without source rotation. Whole-peer failed probes preserved original slots until
reachability returned. Live firewall checks retained current/sibling rules and
removed retired tuple rules; normal teardown removed owned rules and retained an
unrelated sentinel rule. Config reload preserved adopted ports.

The root full race suite, vet and thirty repeated carrier/recovery race tests
passed. Full KCP and SMUX race suites passed at the preceding checkpoint; their
Git trees are unchanged in this candidate and are recorded with those checks.
This is finite local qualification, not proof of universal link performance,
multi-day production stability or 100,000 busy Xray customers. The ongoing
production observation belongs to the existing deployment and remains separate.

## Reproduction and review

The receipt stores every exact argv and generated workload parameter. The
expanded harnesses are `scripts/netns_bench.py` and
`scripts/path_recovery_bench.py`. A minimal recovery reproduction is:

```sh
sudo env LD_LIBRARY_PATH="$PWD/build/deploy-libs" \
  python3 scripts/path_recovery_bench.py \
  --binary build/carrier4-qualification/super-paqet-carrier-tuned \
  --case established --output build/recovery-review
```

Available faults are tuple, reverse-tuple, established, reverse-established,
all-tuples, one-lane, short-loss, repeat-tuple and whole-peer. `--shared-source`
checks the legacy layout and `--server-binary` checks a named older backend.
No production deployment is performed by these fixtures. On approval, deployment
will use this exact qualified executable and the five proposed configurations,
with real-host validation, controlled service replacement and route checks.
