# Live source-port migration qualification — 7 October 2026

Release `enterprise-2026.10.07-migration.3` is locally qualified and **not deployed**.
Runtime source: `f0af3646cd56841ea73d71142ee83ecd3a2f5277`.
Executable: `build/migration-qualification/super-paqet-migration-3`.
SHA-256: `7c3a71192603d55c636a7a43f7bb17c2d793bf33cf5398711e0594069cbb9735`.
Build timestamp: `2026-10-07T17:04:18Z`.
The [compact receipts](migration-qualification-2026-10-07.json) identify every
command, result hash, measured resource sample and cleanup check. Previous
qualified binary: `6bc3832e4287645b0cfd9565a54d4eafd76dfa05b2d6196f71e81eece894842c` (`carrier.4`).

## Behavior and purpose

A stalled source tuple can now recover without replacing its live KCP and mux
objects. The client adopts a verified fresh source socket; the server moves the
same conversation to that candidate's receive worker and return address. Stream
IDs, unacknowledged bytes, sequence/reorder state and target TCP sockets remain.
There is no per-TCP replay store, payload striping or target redial on migration.
This is physical transport movement, not transferring streams into another mux.

Recovery eligibility remains directional ACK-based, with 15-second stall and
retry defaults and five-second probes. Remote zero-window flow control suppresses
recovery. Candidate PPONG and a capability check precede local movement; resource,
configuration, slot identity and resumed progress are checked again at commit.
Failed probes and stale candidates leave existing slots intact. Siblings and
other peers keep serving throughout.

The backend capability is 32 random bytes, bound to its listener generation and
original client IP. Inner token/check/move/reply controls carry strictly increasing
epochs. A repeated commit on the same tuple is idempotent; higher epochs can
supersede unconfirmed earlier moves; delayed lower epochs cannot revert the path.
After adoption, unavailable commit replies are retried three times, each bounded
by the probe budget, while the original streams remain alive. Normal health
checks handle a continuing outage; they do not blindly revert an ambiguous move.

Older backends, unavailable capabilities and already closed logical sessions
fall back to ordinary verified replacement. This closes affected streams and
requires application reconnection. A whole-IP outage is not repaired by source
rotation; application deadlines, mux liveness expiration and backend restart
cannot be reversed. There is no guarantee every customer survives a 15-second
outage if their application timeout is shorter.

## Configuration and deployment impact

```yaml
shared_source: false
sessions: 4
max_sessions: 4
path_recovery:
  enabled: true
  preserve_connections: true
  stalled_after: 15s
  retry_interval: 15s
  probe_timeout: 5s
```

`preserve_connections` defaults to false. It requires enabled recovery, independent
client source ownership and FEC disabled. Backend listeners use `shared_source:
true` and the migration runtime. Their listening ports stay unchanged. Negotiation
runs after ordinary setup without adding a required opening round trip. Changing
the option on live reload replaces just that peer pool. Identical reload/log edits
retain its effective ports. Updated [five proposed configs](proposed-source-ports/README.md)
passed preparation-only validation in synthetic host namespaces; actual-host
preflight remains necessary. Production hosts and Xray were untouched.

## Outer packets and diagnostic evidence

The existing Ethernet/IPv4/IPv6/TCP encoder is unchanged. A fresh client socket
uses its normal source-port/seed/counter initialization. Backend shared encoding
keeps its seed and counter; original conversation flag ownership transfers to
the new tuple before candidate ownership disappears. Fixed S outbound / PA return,
TCP options and fabricated sequence/ACK/timestamp formulas passed packet checks.
No conntrack or application reordering layer was added; KCP handles duplicates
and retransmission with the retained state.

This does not prove filtering resistance. Under `enc: 'null'`, tokens/control
kinds and the retained KCP conversation ID are observable. A filter can associate
tuples using payload or traffic patterns even though outer construction is
retained. New controls are capability-based, not encryption or on-path protection.

Warn logging includes source-change evidence and `connections_preserved`.
Debug logging shows capability support, backend adoption, confirmation/retries
and the first post-move ACK progress. For a preserved carrier, the first minute
of observation emits **at most one** `path.migration_early_stall` warning when
normal health checks qualify a stall or a previously occupied carrier closes.
It records old/new ports, conversation, elapsed milliseconds, delivered/received
byte deltas, pending bytes, receive window and closed state. The reason says
**cause unconfirmed**; it does not label packet loss as censorship. Healthy,
idle, briefly delayed and zero-window carriers do not produce stall warnings.
Transport stalls use the configured 15-second threshold; an occupied carrier
closing is visible at the next health check. The watch releases a closed carrier promptly and adds no packet hook or
per-customer log state. See [diagnostics](DIAGNOSTICS.md).

## Matched local performance

Both binaries used four independent client sessions, two backend capture workers,
GOMAXPROCS 4/2, unencrypted S/PA, manual 30 ms updates, MTU 1350, windows 4096,
small-write threshold 256 and the same adaptive-window behavior. Adaptive buffers,
ACK timestamps and mux credits stayed disabled; no config-value tuning was used.
Clean bulk used 16 streams, eight seconds per direction, three alternating pairs.
The 1 Gbit/s cap used ten seconds and CPU profiles. Rates below are receiver goodput;
CPU is the sum of the two tunnel processes in core equivalents, excluding generators.

| Workload | Version | Gbit/s median | Range | CPU cores median | Cores/Gbit/s median |
|---|---|---:|---|---:|---:|
| Clean upload | baseline | 5.992 | 3.653–7.505 | 4.335 | 0.764 |
| Clean upload | migration | 5.812 | 5.436–5.999 | 4.443 | 0.758 |
| Clean download | baseline | 4.332 | 2.755–5.183 | 3.241 | 0.785 |
| Clean download | migration | 4.284 | 4.016–4.335 | 3.394 | 0.792 |
| 1 Gbit/s cap upload | baseline | 0.896 | single run | 1.243 | 1.387 |
| 1 Gbit/s cap upload | migration | 0.878 | single run | 1.203 | 1.370 |
| 1 Gbit/s cap download | baseline | 0.935 | single run | 1.323 | 1.416 |
| 1 Gbit/s cap download | migration | 0.934 | single run | 1.335 | 1.430 |

The ranges retain every matched run; differences include dispatcher costs and
local scheduling variability. These local measurements
are not a capacity promise for the backend Internet links. At the cap, CPU
profiles remained dominated by system calls, scheduling, checksumming and KCP;
the control registry runs outside the packet path.

WAN profiles used 20-second connection-churn runs, seed 42 and a middle bridge.
`asym-loss`: 160 ms nominal RTT, ±5 ms one-way jitter, 10/100 Mbit/s directional
caps, 0.5% loss and 1% reordering, 128 workers. `delay-reorder`: 400 ms nominal
RTT, ±20 ms jitter, 50/5 Mbit/s caps, 1% loss and 10% reordering, 64 workers.
They are single matched pairs; loss and scheduling variability prevents a general
claim of latency improvement. P99 is the generator's histogram upper bound.

| WAN profile | Version | Requests/s | Mean ms | P99 upper ms | Errors |
|---|---|---:|---:|---:|---:|
| asym-loss | baseline | 203.29 | 621.11 | 4194.30 | 0 |
| asym-loss | migration | 186.04 | 676.41 | 4194.30 | 0 |
| delay-reorder | baseline | 44.65 | 1376.32 | 4194.30 | 0 |
| delay-reorder | migration | 42.65 | 1458.01 | 8388.61 | 0 |

Ten thousand held forwards were all checked while separate HTTP and bulk workers
ran together. The held population is largely idle and is not 10,000 busy customers.
RSS includes the tunnel processes, excluding target/generator/kernel memory.

| Version | Held verified | Mixed bulk Gbit/s | HTTP requests/s | HTTP mean ms | Client / backend peak RSS MiB |
|---|---:|---:|---:|---:|---:|
| baseline | 10000 | 3.361 | 626.16 | 12.77 | 276.0 / 296.7 |
| migration | 10000 | 2.737 | 603.96 | 13.24 | 272.4 / 297.8 |

The first mixed pair showed an 18.6% bulk decrease with migration enabled. Two
additional pairs used identical debug/CPU-profile settings in both versions,
with order migration→baseline and then baseline→migration. They expose substantial
run variation rather than a consistent remaining slowdown or speedup:

| Profiling repeat, in execution order | Bulk Gbit/s | HTTP requests/s | HTTP mean ms | HTTP / bulk errors |
|---|---:|---:|---:|---:|
| migration-1 | 4.074 | 759.15 | 10.54 | 0 / 0 |
| baseline-1 | 2.763 | 516.36 | 15.49 | 0 / 0 |
| baseline-2 | 2.739 | 518.15 | 15.44 | 0 / 0 |
| migration-2 | 2.761 | 567.38 | 14.09 | 0 / 0 |

All four repeats verified all 10,000 held forwards, with no workload errors or
swap in either tunnel process. Comparable slow-run backend profiles spent about
58% of CPU samples in syscalls in both versions. Together these runs do not
establish a consistent remaining migration regression, but the initially slower
pair is retained above and no zero-cost or universal latency claim is made.
There are four additional profiling receipts alongside the 17 primary workloads.

## Fault and correctness qualification

The final executable passed 12 root-only namespace fixtures and
17 workload receipts, plus five config validations. Held
connections are opened only once. Forward/reverse failures, four simultaneous
failures, repeated moves, larger payloads and sequenced exchanges preserved all
four held streams on migration-capable ends. The early-reblocked tuple produced
one warning and recovered again. Healthy-peer error lists stayed empty; sibling
conversation IDs, fixed four-slot ceilings, live firewall ownership and log-edit
reload stability were checked.

| Fixture | Verified switches | Held streams preserved | Affected request errors | Early-stall warnings |
|---|---:|---:|---:|---:|
| final3-whole-peer | 2 | — | 48 | 0 |
| final3-established | 1 | 4 | 0 | 0 |
| final3-reverse-established | 1 | 4 | 0 | 0 |
| final3-all-established | 4 | 4 | 0 | 0 |
| final3-repeat-established | 2 | 4 | 0 | 0 |
| final3-early-stall | 2 | 4 | 0 | 1 |
| final3-short-loss | 0 | — | 0 | 0 |
| final3-tuple | 1 | — | 0 | 0 |
| final3-all-tuples | 4 | — | 10 | 0 |
| final3-legacy-backend | 1 | 3 | 1 | 0 |
| final3-replacement-disabled | 1 | 3 | 1 | 0 |
| final3-sequenced-repeat | 2 | 4 | 0 | 0 |

Errors during injected whole-path/all-tuple outages are expected for new openings.
The older-backend and option-disabled tests deliberately close one affected held
stream and retain the other three. They qualify fallback, not preservation.
The sequenced repeated test uses a unique exchange number plus a 256 KiB body,
so a duplicated prior reply cannot masquerade as the next correct reply.

Full root-module race tests, the complete KCP fork race suite, both vet checks,
a FreeBSD fork cross-build, replay/scope/ownership tests and a 30-second protocol
fuzz run (3,195,482 executions) passed. The mux fork was
unchanged; null/AES functional tests verified UDP boundaries through 65,507 bytes,
TCP integrity, multiple peers/targets and directional half-close, with standalone
IPv6 checks. The KCP race suite qualified fork tree `dfe6b59a94c9c5b562cfc6eac53b1d7e3862f23e`.

The earlier `migration.2` executable passed correctness checks but regressed in
10k mixed load: 2.636 Gbit/s versus 3.703 for the baseline; HTTP mean latency was
15.38 ms versus 11.36 ms. A profiling repeat reproduced the regression (3.053
versus 4.064 Gbit/s). The final runtime adds a validated one-conversation outgoing
receive hint to avoid per-packet address formatting and listener-map locking.
It also follows the current listener for batched ACK ownership and waits for
backend confirmation before forcing pending bulk retransmission. The hint checks
conversation, remote tuple and current listener ownership; other input uses the
authoritative map. The matched final measurements above qualify that optimization,
not the earlier slower development executable.

During development, an over-strict capture check mistook zero-payload kernel RSTs
at retired probe ports for KCP frames after a long outage. Capturing on the client
before the bridge, classifying those kernel resets separately and allowing parallel
worker counter delivery order corrected the fixture. Every synthetic KCP frame
still has to match the retained encoder formulas. A baseline fixture also had to
omit the new field entirely because the older executable correctly rejects unknown
YAML fields. These were fixture errors, not unexplained runtime crashes.

All owned processes/namespaces and firewall journals were cleaned up; unrelated
sentinel firewall rules survived. Raw captures and profiles remain private under
`build/migration-qualification/`; only compact receipts are committed. Deployment
awaits the user's requested confirmation. This qualification does not establish
unknown filtering behavior or sustained thousands-busy-customer Internet capacity.
