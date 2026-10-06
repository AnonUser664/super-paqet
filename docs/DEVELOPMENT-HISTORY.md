# Development, failure and decision history

This is the recorded engineering history through the review pause on 2026-10-05.
It includes unsuccessful approaches and verification mistakes so the next
iteration does not repeat them. It is based on commits, retained test artifacts
and this session's decisions. It does not invent causes for observations that
remain unexplained. Raw experimental artifacts live in ignored `build/` and
may not survive a fresh clone; committed evidence is linked below.

## Objective and boundaries

Preserve the original fabricated Ethernet/IP/TCP capture/injection mechanism
and reliable KCP delivery. Inner protocols may change; unmodified upstream
interoperability is not required. Firewall-bypass behavior and detectability
matter more than the original source layout. Outer byte-shape checks protect
specific properties; they cannot prove classifier invisibility on every network.

Linux only; KCP only; TCP port forwarding is primary, with UDP support. Remove
SOCKS, allow multiple clients on one server port, and allow one instance to
connect to multiple servers. Aim for adaptive behavior, explicit remaining
limits, low CPU/RAM/latency and automatic owned firewall cleanup.

Accepted initial measurements: 100,000 established forwards and a separate
2+ Gbit/s bulk workload, with hardware/resource limits reported. These are not
100,000 simultaneously busy customers. The user now confirms the deployed
profile works well with **one active user**; thousands of active customers have
not been demonstrated on these hosts. Further deployment/code work is paused
for document review and a final feature decision.

## Phase 1: Linux enterprise foundation

`3aaa214` introduced the unified listeners/peers/forwards engine, TCP/UDP control
messages, limits, firewall ownership/recovery, diagnostics and namespace tests.
The KCP/smux forks replaced expensive repeated work with indexed ACK processing,
bounded on-demand queues/rings, packet batches, shared scheduling and multiplexed
flows. Scratch buffers are acquired after TCP readiness, rather than retained
for every idle connection. Server workers use kernel hash fanout; outgoing
carriers distribute work while retaining established stream ownership.

| Failure or weakness | Important approach / outcome | Evidence |
|---|---|---|
| Original implementation was a small example with incomplete scale/lifecycle support | Unified Linux engine and isolated workload tools; retain raw transport mechanism | `3aaa214`, TRANSPORT.md, fork PATCHES.md |
| Fixed encryption/send queue dropped local output bursts | Grow FIFO on demand within send-window budget; count local pipeline drops | BENCHMARKS-HISTORY.md, KCP PATCHES.md |
| Full stream abandonment was confused with directional EOF; writers could stay blocked | Optional directional FIN plus full reset command, retain ordered data and normal half-close | smux PATCHES.md and half-close/reset tests |
| Idle connections carried unnecessary buffer cost | Lazy TCP scratch allocation, small mux rings, pooled direct slice forwarding | TRANSPORT.md, 100k evidence |
| A single bulk carrier delayed new HTTP requests | Bounded carrier growth and pressure-aware selection; failed carriers excluded from opening retries | `8fc9c55` |
| Flow credits waited behind opposite-direction data | Async coalesced reliable UPD, optional expedited WINS hints, retained reliable fallback | `8fc9c55`, credit tests |
| Small control frames were starved or could starve data if unrestricted | Priority with a bounded 16-frame control burst; data frame size follows transport budget | smux PATCHES.md |
| Pacing work waited for a later normal timer tick | Bring scheduler wake forward; bound pacing credit; leave ACK/window controls exempt | KCP PATCHES.md |

## Phase 2: asymmetric, reordered and changing virtual links

| Observed failure / misleading signal | Correction / decision | Evidence |
|---|---|---|
| Deliberate peer ACK scheduling or reverse congestion looked like forward congestion | Optional receive/emission timestamps; subtract peer ACK delay and attribute forward/reverse queue growth; retain full RTT for windows/timers | `47509f3` |
| Jitter/reorder minima caused persistent false queue classification | RTT-variance thresholds and hysteresis before pacing backoff | `47509f3`, seeded jitter/reorder runs |
| Tiny opening/keepalive traffic could erase learned bulk capacity | Separate bulk observation and retain delivery history over multiple RTTs | adaptive controller/tests |
| Low-bandwidth/asymmetric queues were sized as though every packet was 1500 bytes | Size emulator propagation queues for small ACK/control frames too; keep deliberately tiny queues as separate stresses | DIAGNOSTICS.md, netns harness |
| Iperf3 `--bidir` assigned roles by accept order through independently forwarded connections | Retire those historical bidirectional acceptance numbers; run simultaneous one-way clients and require bytes on every receiver | BENCHMARKS-HISTORY.md, BENCHMARKS.md |
| Slow-link bulk intervals could end before a full 1 MiB response completed | Separate byte goodput, completed-request latency and cancellation; do not invent latency for zero completions | mobile profiles in BENCHMARKS.md |
| Debug logging could dominate a scale run | Bounded asynchronous queue, sampled flow lifecycle, dropped-log counters | DIAGNOSTICS.md |
| Pcap `LocalAddr` returned nil and diagnostics panicked | Return a cloned configured IP plus reserved port; add aliasing and live fallback checks | `2d5f7a0` |
| OOB library test attempted to use a KCP conversation before it was established | Establish the conversation first; fix test ordering without calling that a production transport correction | `64fe72a` |
| Service qualification could use a generic binary rebuilt from unrelated dirty source | Pin the tested executable/hash; isolate source; record stage completion and resume required failed stages | `e1df492`, full-check logs |

The retained default candidate passed 109 virtual scenarios and 38 live
profile/seed runs. Local bulk was 4.387 Gbit/s upload / 3.708 download; 1000
Mbit/s at 100 ms RTT delivered roughly 915 Mbit/s in either direction. These
numbers refer to the frozen encrypted/adaptive local candidate, not the current
unencrypted real-WAN recovery configuration.

Remaining measured limitations include p99 bounds up to 2.097–4.194 seconds
when small requests share a saturated 1/100 Mbit/s asymmetric path. No claim
was established for minimum latency on every saturated/lossy link.

## Phase 3: stronger connection-scale verification

The earlier held-socket test sampled only a few connections; counting open
sockets could miss dead forwards. A run at default host receive backlog 1000
had a premature local TCP timeout and loopback receive drops. The revised load
generator verifies a complete response from **every** held socket. A deliberate
reset control reported 99/100 usable with one error, proving the verifier can
reject a bad run.

The successful repeat temporarily raised host `netdev_max_backlog` to 65536,
reduced expensive descriptor/monitor sampling, held 100,000 mostly idle forwards
for 600 seconds, then verified all 100,000. Both changes were made together;
their separate contribution is not isolated. The original backlog was restored.

RSS-only reporting also understated cost because the laptop swapped. The final
record includes concurrent RSS+swap, kernel/target/generator exclusions and
whole-stage CPU accounting. Peak tunnel RSS was about 1.83/1.93 GiB; peak
RSS+swap 1.90/2.30 GiB. Mixed bulk was 1.572 Gbit/s and HTTP 722 requests/s.

Full root/smux race suites, KCP tests, vet, two 60-second fuzzers, systemd
restrictions, crash/restart and owned-rule recovery passed on that frozen
candidate. A ten-minute local soak is finite qualification, not multi-day
production endurance or proof of every default host setting.

Evidence: [BENCHMARKS.md](BENCHMARKS.md),
[step1-qualification.json](step1-qualification.json). Checkpoints:
`6567e2e`, `7ba4664`, `f9ef793`, `231f33e`.

## Phase 4: first five-host deployment

Requested persistent paths were tunnel servers on 116.202.177.233,
65.109.192.172 and 171.22.132.226, clients 89.45.68.14/.118, tunnel port 2052,
null encryption, and forwards 9001/9002/9003 to corresponding TCP 2096 targets.
The user confirmed direct topology. When the direct middle-backend path failed,
the user chose to keep 9002 direct and report it unavailable rather than relay.
Later instructions excluded that backend entirely from further work.

`1c77c55` added explicit null mode and encryption aliases. The deployed binary
was built for Ubuntu's libpcap.so.0.8 / compatible glibc, rather than copying a
laptop binary linked against a different libpcap soname. The /root service
needed `ProtectHome=read-only` to execute the requested installation. Staging
checks and backups preceded replacements. WARP/policy routing and the German
secondary address required explicit physical interface/IP/gateway MAC values.

All services could be active/enabled and answer health checks while forwarding
failed. KCP pings sometimes passed while application opening/data stalled.
Unauthenticated TLS/tiny control probes were inadequate for the actual
VLESS/Reality/Vision service behind 2096. The supplied Reality profile allowed
proper authenticated proxy requests; credentials are absent from committed docs.

A verification mistake initially compared enterprise bulk echo failures with
upstream tiny successful requests. That was not a valid regression comparison.
Later tests used identical authenticated workloads/configurations and changed
one factor where stated. Missing packets did not justify attributing every
failure to a specific provider, DPI mechanism or software regression.

## Phase 5: rejected real-link experiments

These experiments were temporary and were not promoted. Several used different
ports/profiles than the later working baseline, so their failure does not prove
the approach can never work. They establish only that the tested variant did
not recover that workload under those settings.

| Approach | Observed result / disposition | Retained artifact family |
|---|---|---|
| Per-peer outer TCP byte sequence tracker | Early echo tests failed/reset/timed out; exclude from persistent binary | `deployment-sequence-echo-probe.json` |
| Seed sequence from incoming ACK | Echo tests still failed; exclude | `deployment-seeded-echo-probe.json` |
| No-encryption nonce/CRC envelope (`none`) | Did not recover the early real-link integrity probe | `deployment-none-*` |
| AES-GCM instead of null | Did not recover that early profile | `deployment-aes-*` / compatibility probes |
| Pcap fallback alone | Did not make the initial port/profile work | `deployment-pcap-*` |
| Legacy AES/fast profile, fixed low source ports, MTU 576 | Early bulk echo probes still failed/partial; later actual Reality controls were necessary | `enterprise-legacy-aes-probe.json`, `enterprise-fixed-source-probe.json`, `enterprise-small-mtu-probe.json` |
| Remove opening ACK / optimistic opening (`PTCP4`) | Failed integrity; did not bypass delivery failure | `optimistic-probe-results.json`, `optimistic-experiment.patch` |
| Disable batching | Failed same early echo recovery | `unbatched-probe-results.json` |
| Disable mux extensions / plain smux mode | Failed that probe | `plain-smux-probe-results.json` |
| Smaller 8 KiB copies | Failed that probe | `copy8k-*` |
| Broader port-only BPF or temporary broader firewall protection | Did not recover that tested path; no blanket rules retained | `port-filter-probe-results.json`, `upstream-firewall-comparison.json` |

Upstream controls were built from `b9fa0bd93bf93b2eff27197742562ed82201f16e`.
Replacing only the KCP fork, only the mux fork, and both forks did not establish
a consistent fork-specific cause. In the initial tiny authenticated controls,
Germany passed from both clients and Netherlands was intermittent/failing.

The user later clarified the original successful setup used AES and PA flags,
and that **only .118 to Germany** had been verified. That bounded the baseline
claim; the other three bulk paths were not known-working upstream controls.

## Phase 6: controlled recovery and sustained-transfer checks

| Comparison / observation | Result / meaning |
|---|---|
| Current installed binary, conservative AES/fast/fixed-source profile on 29999 | Four authenticated small requests passed. |
| Change only tunnel port to 2052 in that profile | Four timed out; demonstrates port sensitivity, not a named filtering cause. |
| Restore 29999; change only encryption to null | Four passed in 0.696–0.889 s. Null encryption can work. |
| Restore enterprise default profile on 29999 | Four failed. Several factors changed; no single adaptive knob was isolated. |
| Persistent conservative null/29999 recovery | Twenty small requests passed; public-domain small requests also passed. |
| First 1 MiB sustained tests | Three paths stalled; .118 to Germany completed. Small requests were insufficient acceptance. |
| Direct backend Reality controls | Both backends completed 1 MiB in about 0.48/0.52 s. |
| AES sustained comparison | Same three paths failed; AES alone did not solve them. Restore null afterward. |
| Unmodified upstream, identical 1 MiB workload | Same three failed; .118 to Germany passed in 1.265 s. Shared failure under those conditions. |
| Paired physical-interface capture on failed paths | Initial KCP retransmits visible at client but missing from backend capture. Cannot name the intervening filter from this alone. |
| Check live neighbor MACs | Matched configured gateways; stale MAC hypothesis not supported. |
| Fresh source ports 30098/30097 | All tested bulk paths failed. Restoring 29998/29997 restored four small requests. |
| IPv6 controls | Four timed out; not promoted. |
| MTU 128 | Three of four initial 1 MiB bulk paths passed, including both Netherlands paths. |
| Germany .14 through same host's primary 91.107.251.85 | Small and complete 1 MiB request passed. Added primary listener and changed .14 peer; destination stayed 116.202.177.233:2096. |
| Netherlands MTU 512 | .118 completed repeated 10 MiB; .14 failed. Shared listener restored to 128. |
| Final MTU 128 recovery | All four completed 10 MiB. Netherlands initially took 26/35 s. |
| Netherlands windows ×4, then ×13 | Preserve more byte capacity; 10 MiB improved to 10/14 s, then 6.1/8.3 s. |
| Existing AF_PACKET driver with same small-packet/window profile | Both passed in about 6 s, with lower measured CPU/RSS than pcap. Retained. |
| Final repeated persistent checks | Eight complete 10 MiB transfers, two per path; public domain completed 1 MiB on both ports. |

The primary-address change does not add a relay or another German server.
Current configs remain null encryption and PA flags. The packet-size/address/
port sensitivities are empirical; the exact filtering mechanism remains open.
The user's one-active-user success is additional deployment feedback, not a
thousands-customer stress result.

Evidence: [DEPLOYMENT.md](DEPLOYMENT.md),
[deployment-recovery-evidence.json](deployment-recovery-evidence.json).
Documentation checkpoints: `ae4ffe0`, `c3c0227`.

## Phase 7: code candidates after the working baseline

The isolated sequence experiment found missing pcap receive feedback in an
earlier tracker. It connected that feedback, counted outgoing payload/SYN/FIN
sequence space, and tested wraparound, peer separation and owner cleanup.
It passed three 1 MiB paths at MTU 1350. Checkpoint `4c7aa6a` remains on
`experiment/wan-sequence-tracking`; it is not deployed or merged into the
qualified main runtime. Changes in outer sequence semantics/detectability and
complete real-link qualification remain review concerns.

A mixed-client null/pcap test at 100 Mbit/s and 80 ms RTT failed in both the
installed baseline and that experiment. Initially this was recorded as a
body-tail stall, not a proven sequence regression. Detailed debug logs then
identified the actual failure: **pcap transmit ENOBUFS was propagated as a
fatal error**, aborting the server stream while the client awaited remaining
body bytes. AF_PACKET already treated its equivalent as packet loss.

The narrow fix classifies recognized Linux queue-full injection failures as
lost datagrams, lets KCP retransmit, counts pcap TX drops in existing metrics,
and adds sampled listener drop logs. Permanent errors still propagate.
Deterministic tests inject typed/wrapped ENOBUFS, libpcap text, later successful
packets, permanent errors and close behavior.

Failures during qualification were corrected before deployment:

- Go's errno string and libc's string differed in capitalization; the injected
  libpcap-text test caught it. Matching is case-insensitive only for the known
  send/ENOBUFS message; arbitrary errors are not ignored.
- An initial diagnostic accessed a nil owning PacketConn on accepted server
  carriers and crashed. Accepted carriers share a listener socket; logging now
  samples those sockets separately and guards per-carrier ownership.
- Sampling the startup closers slice could race registration. Listener observers
  are registered/snapshotted under the existing tuner mutex instead.
- Crash/deleted-namespace journals encountered a missing nftables target: `-C`
  returned exit 2. Check the owned chain with `-S` before referring to its jump;
  missing chains are already clean. Other failures remain errors.

The final pressure run passed both 16 MiB integrity transfers, 65,507-byte UDP,
262,144-byte half-close, ping, HTTP/bulk work and owned-rule cleanup with all
processes exiting zero. New logs showed over 18,000 queue drops in one early
interval while the transfer recovered. Root races/vet and full KCP tests passed;
the full smux race process was launched, but its final output
was not collected before interruption, so this new patch's review evidence
does not claim that uncollected result.

These fixes are committed on master at `20227d3` (isolated branch source
`d4205c1`). Their redeployment was stopped before credentials were entered;
there are no new code-fix staging/rollout manifests. They are **not running on
the backends**. No further deployment is authorized until document review and
the user's next decision.

## Tooling and evidence limitations

The work also encountered target-incompatible libpcap sonames, /root access
restrictions in systemd, /run mounted noexec, busy executable replacement,
temporary gzip/output-name collisions, interrupted commands, dropped/idle SSH
sessions and slow serial SFTP writes. Compatible target libraries, /root probe
paths, unique staged filenames, reconnects, bounded transient units, pipelined
uploads and atomic replacements addressed these. The German SSH outage was
confirmed by the user and later reopened; it was separate from tunnel testing.

Early diagnostics and qualification were slower than necessary. Repeated
speculation, differing workloads and insufficient progress visibility delayed
causal comparisons. The useful corrections were frozen binaries, exact
workloads, one-factor comparisons where possible, correlated logs, complete
body/socket verification, and explicit limits on what each observation proves.

Some source edits/commands initially used the wrong working directory, an
incorrect receiver method/type or mismatched argument count. Compilation and
regression checks rejected those attempts before rollout; the validated patch
was rebuilt afterward. These failures did not change the deployed executable.

Temporary test cleanup sometimes failed after SSH interruption or a deliberate
crash. Local namespace deletion removed its kernel rules even when the harness
reported an in-namespace leak. Retained dead-owner journals exposed the recovery
bug above. Remote probes have been stopped through their cleanup/runtime limits,
but the final four-host temporary-file/rule audit was deferred with redeployment;
it must not be represented as a fresh completed audit. No unrelated firewall,
WARP or Xray configuration was intentionally changed.

Raw logs/captures may contain endpoint/control data. SSH passwords and Reality
credentials were not committed. Not every intermediate experiment has a complete
result artifact after interruptions; conclusions above are bounded accordingly.

## Decisions outstanding for review

1. Customer workload: thousands of simultaneously busy users, TCP/UDP mix,
   transfer sizes, churn, latency/error budget and real-host CPU/RAM capacity.
2. Recovery profile versus restoring enterprise adaptive settings on real links.
3. Adaptive retransmit threshold/reorder discrimination and explicit static knobs.
4. Path-MTU learning/small-packet cost and qualification of any outer-sequence change.
5. Customer identities, authorization/destination policy, null-mode exposure,
   config validation gaps and the last required feature check.
6. Long soak, restart/failure behavior, monitoring cost and staged rollback criteria.
7. Whether/when to deploy `20227d3`, preserving the currently working base.

## Live configuration and validation feature (local; not deployed)

The user requested automatic file reload with minimal impact on other routes,
then added a dedicated validation CLI. The implementation replaces startup-only
resource ownership with immutable route/config publications and separate peer,
listener, forward and metrics lifetimes. Each rule-owning endpoint has a private
journal, permitting staged rollback and removal without touching unrelated rules.
Safe KCP scheduling/retransmission setters apply to existing/future carriers;
wire/mux changes conservatively replace only affected endpoints. `config validate`
shares strict startup/reload preparation and offers human/JSON results.

Implementation regressions caught before completion:

- A shadowed UDP opening context was canceled before the relay's cancellation
  hook was registered, closing newly opened UDP streams. Separate opening and
  bind-lifecycle contexts fixed it; real UDP probes now pass through reload.
- A closed listener originally checked only engine cancellation; per-listener
  cancellation now ends its accept loop, preventing retries against its old socket.
- Treating every occupied bind as an owned replacement could unnecessarily
  interrupt changed resources when a new unrelated port was busy. Explicit local
  bind overlap checks now reject external conflicts before teardown.
- Future accepts/dials could register old reliability settings while a live
  update committed. Atomic endpoint templates and a shared registration/update
  lock now close that race without locking packet forwarding.
- A short continuity observation demanded progress in about 400ms on a lossy,
  reordered link whose measured KCP RTO exceeded a second. It failed with all
  streams open and zero errors. The harness now records time to verified progress
  within its existing eight-second application read deadline; no reset/corruption
  or application timeout is accepted as continuity.
- An already half-closed relay could retain an idle write leg on generation
  removal. It now selects existing full-stream and generation lifecycle channels,
  closes the TCP socket on abandonment, and keeps normal directional EOF behavior.
  Unit and real namespace regressions cover this lifecycle edge case.

The harness also caught a metrics-port string/integer mismatch in its own wait
code; that run was not accepted. Final results and source/binary boundaries are
in [LIVE-RELOAD.md](LIVE-RELOAD.md) and [live-reload-evidence.json](live-reload-evidence.json).
No backend service/config/binary was modified by this feature work.

## Final release integration and deployment qualification

The subsequent user authorization resumed qualification and deployment. The
[final report](FINAL-DEPLOYMENT-REPORT.md) identifies the deployed source/hash,
finite capacity measurements and remaining Netherlands authenticated tail.
[Working notes](FINAL-QUALIFICATION-WORKING-NOTES.md) record every new rejected
profile, invalid harness comparison, fixture expiry, capture result and corrective
checkpoint. Earlier review-paused/undeployed statements above describe their
historical checkpoints, not current state. The pre-final unqualified timing/
sequence edits were preserved in an archive branch, named stash and exact backup
and excluded from clean main integration.
