# Current status: working deployment, live reload added locally

Updated 2026-10-05. The user confirms the current deployment works well with
**one active user**. Thousands of simultaneously active customers have not been
tested on these backends. The requested live reload and validation CLI are implemented locally. Further
performance tuning and redeployment remain paused for review; deployed services
have not been changed.

## Version boundaries

| State | Source / artifact | What is established |
|---|---|---|
| Earlier local qualification | Runtime `2d5f7a0`, isolated checks `64fe72a`; `build/super-paqet-pcap-address-fix` | 38 live virtual-link runs, 109 virtual scenarios, mostly-idle 100k soak, local multi-gigabit bulk, service/fuzz/checks. |
| Currently deployed base | `1c77c55`; `build/super-paqet-deploy-ubuntu` | Four surviving real-link forwards pass authenticated repeated 10 MiB checks; user reports one-user success. Conservative recovery overrides. |
| Committed fixes awaiting decision | Main `20227d3`; isolated equivalent `d4205c1`; `build/super-paqet-pcap-backpressure-release` | Pcap transmit ENOBUFS recovery, drop diagnostics, absent-chain firewall recovery. Focused/root races/vet, full KCP and actual pressure-functional checks passed. Not deployed. |
| Live config source | Main runtime `545e9b1`; isolated equivalent `f9547a8` on `feature/live-config` | Automatic reload, safe KCP reliability setters, scoped resources/rollback, validation CLI, tests and documentation. Clean 256-stream and asymmetric/loss/reorder 64-stream reload tests pass, including idle-half-close cleanup. Undeployed; see [LIVE-RELOAD.md](LIVE-RELOAD.md). |
| Sequence experiment | `4c7aa6a`, branch `experiment/wan-sequence-tracking` | Per-peer byte sequence / pcap receive feedback candidate; initial three-path 1 MiB successes, not final qualification. Not deployed. |
| Dirty workspace | Six source files below | Preserved separate timeout/wire experiments; not part of the deployed base or clean queue-pressure candidate. |

Deployed binary SHA-256:
`ecb8e002173f6f80cd3f403d3ee3080190f0e49889fa2ba9f883b893f96318f3`.
Earlier local-qualified binary SHA-256:
`e2ae9b8cce7dc864dae9d21c5f770bb353ab787c218f2faf1d794d8efe7003f6`.
The pressure-functional test used a pre-commit build SHA-256
`8348eb9b1b56aabb27f2c3de4c2d90c5f8ee9b8e2f19940a9fff25b7da30f1c3`;
the clean release rebuild has different VCS build metadata. It has not been
installed or tested through the production paths. Do not treat all named
artifacts as one qualified executable.

## Current deployed paths

- Clients: 89.45.68.14 and 89.45.68.118; forwards 9001 and 9003 work in recorded checks.
- Germany: one host listening on both 116.202.177.233 and primary 91.107.251.85,
  tunnel port 29999. Client .14 uses the primary address; .118 uses the original.
- Netherlands: 171.22.132.226:29999, small packet profile with scaled windows/AF_PACKET.
- Targets remain the corresponding backend's TCP 2096, carrying Xray Reality.
- Null encryption and PA outer flags; no new relay topology.
- 65.109.192.172 and direct 9002 are excluded from further work, unchanged and
  previously unavailable.

Eight final 10 MiB authenticated transfers passed, two per path. German times
were 2.66–2.95 s; Netherlands 5.46–6.13 s. Public-domain 1 MiB checks passed on
both ports. These are small finite workload checks, not real-host capacity.
Recorded snapshot configs/unit are in [deployed/](deployed/README.md).

## Local qualification and its limits

The earlier candidate passed the complete 26-profile main matrix plus twelve
additional seeded runs. Clean veth goodput was 4.387 Gbit/s upload and 3.708
Gbit/s download; duplex 2.361 + 2.127. A 1000 Mbit/s / 100 ms RTT single flow
was about 915 Mbit/s per direction. [BENCHMARKS.md](BENCHMARKS.md) and
[step1-qualification.json](step1-qualification.json) retain exact evidence.

The 600-second mixed soak established 100,000 mostly idle forwards in 9.216 s
and verified a complete response from every held socket afterward. Mixed bulk
was 1.572 Gbit/s; HTTP 722 requests/s with 32.768 ms p99 histogram upper bound.
Peak tunnel RSS was about 1.83/1.93 GiB and RSS+swap 1.90/2.30 GiB. Host receive
backlog was explicitly raised from 1000 to 65536 for this experiment and restored.
This is not 100,000 busy customers or a result for the current recovery profile.

Under saturated 1/100 Mbit/s mixed workloads, p99 remained 2.097–4.194 s. Finite
local qualification establishes neither arbitrary firewall compatibility,
unchanged detectability, universal optimality, nor multi-day production endurance.

## Confirmed new code defect and correction

A static null/pcap multi-client test at 100 Mbit/s and 80 ms RTT reproduced a
body-tail stall in the base and sequence experiment. Debug logging identified
fatal `send: No buffer space available`. `20227d3` instead treats recognized
Linux ENOBUFS injection failures as dropped datagrams, allowing KCP recovery,
while preserving permanent errors and packet encoding. Pcap drops are counted;
listener debug events report changes. An absent-chain recovery check also fixes
stale nftables journals preventing startup.

The corrected pressure run passed two 16 MiB integrity transfers, UDP through
65507 bytes, half-close, ping, HTTP/bulk work, all process exits and owned-rule
cleanup despite thousands of actual queue drops. Full root races/vet and KCP
suite passed. The full smux race process was launched and later was no longer
running, but its final output was not collected before interruption; the new
patch's documented evidence does not claim an uncollected pass.

The attempted code redeployment stopped at its password prompt. No code-fix
staging/rollout manifest was produced. The working base remains deployed.
Final remote temporary-file/rule cleanup audit was also deferred; this document
is an artifact-based state report, not a fresh SSH inspection.

## Preserved uncommitted work

`internal/engine/peer.go`, `internal/engine/relay.go`,
`internal/socket/codec_linux.go`, `internal/socket/codec_linux_test.go`,
`internal/socket/raw_linux.go`, `internal/socket/send_handle.go` contain separate
opening/half-close timing and sequence experiments. They were preserved when
the qualified queue-pressure patch was committed. A generic build from the
current workspace includes them. Use a clean selected checkout for review or
qualification; do not deploy that generic build assuming it equals the base.

## Reading and next decision

The [architecture guide](ARCHITECTURE.md) explains the current package layout,
object ownership and runtime/data paths.

1. [DEVELOPMENT-HISTORY.md](DEVELOPMENT-HISTORY.md): failures, experiments,
   corrections, mistaken comparisons and unresolved causes.
2. [CONFIGURATION.md](CONFIGURATION.md): complete schema, defaults, manual knobs,
   adaptation boundaries, retransmission and validation gaps.
3. [DEPLOYMENT.md](DEPLOYMENT.md): actual topology, settings, results and backups.
4. [OPERATIONS.md](OPERATIONS.md): build/version checks, service layouts,
   monitoring, future replacement/rollback and test reproduction.
5. [TRANSPORT.md](TRANSPORT.md), [DIAGNOSTICS.md](DIAGNOSTICS.md),
   [BENCHMARKS.md](BENCHMARKS.md): mechanism, interpretation and earlier evidence.

The requested live configuration feature is ready for user review.
Active-customer workload/resource acceptance, restored adaptation, retransmit
policy, authorization controls and any final deployment remain outstanding.
