# Final deployment qualification — working notes

This is an unfinished qualification log, not a production acceptance statement.
The release work is isolated in `build/final-production-source`, branch
`release/final-production`. The main workspace's six earlier experiments remain
untouched. The excluded backend `65.109.192.172` has not been contacted.

## Implemented checkpoints

- `2326421` / `836a88d`: ordered peer `source_ports` lists, reservations and
  reload conflicts; distinct ports allow independently captured carriers.
- `7d95927`: optional `shared_source` at listener and peer. Several KCP
  conversations share a single verified source tuple, raw socket and encoder.
  Their muxes, reliability queues and schedules remain independent. Source-port
  firewall rules and guard belong to the pool. Lane closure retains siblings.
  Shared mode rejects FEC because legacy parity packets lack a conversation ID.
  Ordinary address/reset/FEC behavior remains the default. Flag state uses
  per-tuple live lane references and is deleted after final server lane closure.
- `664247b`: a send-ring index of outstanding sequence numbers eliminates linear
  fast-ACK scans through acknowledged tombstones. Links are sequence numbers,
  never pointers into a resizing ring. Gap counters, timestamp gating,
  retransmission decisions and wire encoding are unchanged. Deterministic
  differential traces cover cumulative/selective ACKs, appends, ring resizing,
  sequence wrap and unsent paced segments. Sparse-gap benchmark: approximately
  16,000 ns/op before, 92 ns/op after. Each retained send-ring entry adds eight
  bytes of link metadata; no per-ACK allocation is introduced.
- `5161925` / `deee0b1`: static reliability now still samples slot pressure and
  can grow its configured pool. Adaptive RTT fallback allows the known
  ACK/update scheduling budget when peer timestamps are disabled. This avoids
  treating local batching as network congestion. Live reliability edits update
  that budget. A fixture identifier shadowed its type in the first regression
  test; the follow-up commit corrected it and root race tests/vet passed.

## Local measurements collected

- Root race tests/vet passed. Full smux race suite passed (488.317 s), initial
  full KCP suite passed (136.231 s), shared-conversation full KCP suite passed
  (140.977 s), indexed full KCP suite passed (136.089 s). Targeted encrypted
  shared-lane, pacing/ACK and differential race tests passed.
- Ten WAN profiles passed on the first release: random loss, reorder, harsh
  loss/jitter, mobile, satellite, asymmetric, ACK-limited, outage, pcap
  restart/functional, MTU576. Seven profiles passed on indexed shared-source
  unencrypted transport: random, reorder, harsh, asymmetric, ACK-limited,
  outage and pcap restart/functional.
- Four shared lanes per peer / 256 active TCP streams passed the 18-checkpoint
  reload test on 20/70 ms asymmetric delay, 20/50 Mbit/s directional caps,
  0.3% loss and 5% reorder. Unaffected streams had zero errors. Half-closed
  relays, descriptors, firewall cleanup and unrelated rules passed. The same
  test passed on the latest cadence/pressure executable.
- Unencrypted shared-source bulk: 3.933 Gbit/s upload, 2.970 Gbit/s download;
  simultaneous directions 1.829 and 1.849 Gbit/s. These are laptop namespace
  measurements, not deployment throughput promises.
- Release `664247b`, executable SHA-256
  `5f57e2083faab4031f80634e0da8d2b76eb767df598402c6e993ba05c9aef46f`:
  100,000 established forwards, ramp 6.574 s, 120 s soak, all 100,000 fully
  verified, zero errors; concurrent bulk 2.240 Gbit/s and HTTP 718 req/s.
  HTTP p99 histogram upper bound 65.536 ms. Tunnel peak resident memory was
  approximately 1.84/1.97 GiB client/server; RSS plus swap approximately
  1.92/2.32 GiB. Host backlog was temporarily 65,536 and restored to 1,000.
  This exact scale result predates the subsequent controller/pressure changes.

## Deployment measurements and failures

All releases were staged and checked on the four hosts before replacement:
Ubuntu-compatible libpcap linkage, binary hash, configuration validation,
service validation, enabled/running state and health. Per-host backups retain
binary, config and unit. The unit adds ExecReload/SIGHUP and TasksMax 65,536;
CPU quota remains unlimited. Actual hosts have two vCPUs and about 4 GiB RAM
per backend, four vCPUs and about 8 GiB RAM per client.

The initial new binary retained the proven single-session settings and passed
all eight authenticated 10 MiB transfers. With four shared lanes and manual
immediate writes, all eight authenticated 10 MiB transfers also passed.

Controlled HTTP echo baseline, 40 samples per route: warm median approximately
121–124 ms, new-connection median approximately 245 ms. Immediate writes with
30 ms maintenance, normal retry floor and delayed ACKs: warm medians about
86–94 ms; new connections about 172–187 ms. The 10 ms maintenance candidate
retained the latency reduction but had less consistent bulk results. These are
controlled forwarded requests, not the user's unmeasured application latency.

On the first shared release, 4,096 forwards per client (8,192 total) remained
established during HTTP, connection churn and bulk loads. Every held connection
passed full verification; workload errors were zero. At 256 concurrent HTTP
workers per client, roughly 2,200–2,800 requests/s per route/client were seen.
Bulk aggregate Germany was about 265 Mbit/s in the sequential-route load;
Netherlands about 60 Mbit/s with MTU128. Tunnel RSS peaked at about 184–375 MiB.
Average stage CPU samples are available; initial maximum CPU samples included
an excessively short first interval and must not be treated as valid maxima.
The sampler was corrected. Idle CPU profiles were initially collected after
load ended; a second set was captured during sustained bulk.

Netherlands busy profile attributed approximately 61% of sampled CPU to the
old gap-ACK/ring scan. Its MTU128 profile also counted about 1.5 million
recoverable transmit queue drops during qualification. These were useful
optimization signals, not application error counts.

Increasing Netherlands MTU with byte-scaled windows passed controlled 10 MiB
checks at 256, 512 and 1350. Single-flow results increased from roughly
14–19 Mbit/s at 128 to 31–35 at 256, 46–50 at 512 and 53–59 at 1350. Larger
MTUs are not accepted merely because these low-concurrency tests passed.

Authenticated concurrency adds a stricter gate: 256 requests per route with
64 workers per client. Manual MTU1350 passed Germany but had six Netherlands
TLS timeouts out of 512 requests. The first adaptive trial made Germany's cold
start worse (19 timeouts on one client), while Netherlands passed. The RTT
fallback/ACK cadence issue was corrected, but real deployment acceptance must
still be repeated. Direct backend Xray controls passed 256/256 requests on
both backends at concurrency 64.

The fast3 candidate then failed three real paths entirely; it is rejected.
Restoring manual mode did not immediately restore those paths. Packet captures
showed valid PA packets leaving client 89.45.68.118, correct IP/TCP checksums,
correct interface and gateway MACs, and no corresponding arrival on Germany's
116.202.177.233 interface or Netherlands. The primary Germany path from .14 to
91.107.251.85 remained live. Temporary AES did not restore the failed paths.
This localizes observed loss before server capture; it does not establish a
specific firewall/provider/DPI cause. Fresh destination port and primary-IP
alias tests are underway. AES is diagnostic only and must be removed before
final acceptance; the requested final cipher remains quoted `null`.

## Harness mistakes and incomplete checks

- Early handwritten Reality/HTTP probe bytes were incorrectly escaped.
  Corrected greetings were verified as bytes 5,1,0. The attempted custom HTTP
  target through Xray was not used as an acceptance baseline. Direct-forward
  HTTP and the existing known-working curl/Reality path replaced it.
- A Python urllib/HTTP-proxy stress path returned HTTP 403 for every request;
  it was excluded from acceptance. The stress harness now uses curl through
  the previously checked local SOCKS Xray test inbound. The tunnel itself has
  no SOCKS listener.
- A legacy-control ACK-limited virtual duplex case with peer timestamps,
  credit hints and adaptive buffers all disabled left one of four upload
  streams with zero measured receiver bytes. Other uploads and downloads
  progressed. This case is a failed gate, not a pass inferred from aggregate
  throughput. A longer no-omit test with adaptive receive buffers is underway.
- Final cleanup, source integration, no-encryption restoration, final real
  authenticated stress, local latest-binary scale qualification and final
  config snapshots remain required before declaring this stage complete.

Detailed private logs/results are retained in `build/final-production`.

## Subsequent recovery and final-candidate evidence

The original Netherlands single-carrier fast/MTU128 profile recovered; its 512
authenticated concurrency requests passed. Four shared lanes with the same
fast/MTU128/one-packet-worker profile also passed 512/512. Fresh destination
port, AES and IPv6 did not independently establish recovery and are not selected.
Both Germany clients now use the assigned primary alias 91.107.251.85:29999;
forwarded Germany targets remain 116.202.177.233:2096.

Checkpoint `abf04f6` / main `97529dd` adds configurable `small_write_flush`. It
expedites only logical writes below a byte threshold, preserving preset bulk
batching and ACK cadence; zero retains existing behavior. Deterministic tests
verify default/bulk/vector thresholds and byte order. Full KCP suite passed
(132.998 s); root race/vet passed. Larger interactive thresholds are not selected
on Netherlands: 128 had three TLS timeouts/1024 requests, 32 had eight/1024 on
a repeat. These failures are recorded rather than hidden by retries.

Selected executable SHA-256
`47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`,
embedded source `abf04f6`, version `enterprise-2026.10.06`. Its exact-byte local
100k soak passed: 6.952 s ramp, 120 s hold, every connection verified, zero
errors; concurrent bulk 2.084 Gbit/s and HTTP 1,168 req/s, p99 histogram upper
bound 32.768 ms. RSS plus swap peaked near 1.90/2.32 GiB client/server. This
scale fixture uses adaptive default transport and is not the conservative WAN
profile. Seven final-binary shared/null/fast/small-write-32 profiles passed,
including ACK-limited duplex, random/reorder/harsh/asymmetric/outage/pcap.
The legacy all-telemetry-disabled ACK-limited case remains a failed gate.

The final candidate's 8,192-forward deployment test verified every held socket
with zero errors, alongside 256-worker HTTP/churn and bulk. Peak tunnel RSS
146–204 MiB; sampled peak CPU 0.65–1.77 cores and average stage CPU 0.10–0.46
cores (stage includes idle time). A repeat under final resource budgets is
underway. Both backends have a 1,536 MiB soft Go memory limit, clients 4,096;
kernel/capture memory is additional. The requested null cipher is restored.

Two packet workers on Netherlands failed one client in the larger authenticated
cohort. One worker is retained pending diagnosis; independent KCP/mux lanes
still run on all available Go processors. One-worker final cohort passed
Germany 1024/1024 and Netherlands 1023/1024, with one SSL connection timeout.
Direct client-to-Xray comparison is underway; no production-ready claim is
made for that unresolved tail. All eight final 10 MiB authenticated transfers
and both public-domain 1 MiB checks passed.

The HTTP fixture's two-hour runtime expired during late raw probes; this caused
connection resets and was corrected before final workload tests. Those resets
are not attributed to tunnel corruption. Old idle-only-timeout bulk probes
could trickle indefinitely; they were stopped and replaced by total-deadline,
uniquely tagged probes. Probe routes, files, rules and units still require final
cleanup.

Main now includes the reviewed runtime checkpoints. The six pre-final
unqualified timing/sequence experiments are preserved on
`archive/pre-final-wire-experiments`, in a named stash and byte-for-byte backup
under `build/final-production/preserved-experiments`; they are excluded from the
clean main checkout and selected deployment executable.

### Final acceptance observations (still in progress)

The resource-budget/worker-one repeat verified all 8,192 held forwards with
zero errors and zero HTTP/churn/bulk workload errors. Backend peak RSS was
180.5/228.5 MiB; clients below 151 MiB. Public domain 9001/9003 checks passed
and all eight 10 MiB transfers passed. The full 512-request-per-path cohort
passed 2,047/2,048, with one Netherlands SSL connection timeout on .14.
A direct client-to-Netherlands VLESS control (ordinary TCP to 2096) failed all
1,024 requests, despite TCP connect succeeding; this is not a useful successful
application control for attributing the occasional tunnel timeout. It does
confirm that bypass is needed for the application on that path.

Re-enabling two Netherlands packet workers failed .118 entirely in one cohort;
one worker was restored. This is an unresolved physical-path/fanout qualification
limit, not a proven universal kernel bug. Go/KCP/mux/application work remains
parallel across the available processors. An eight-lane fixed-source candidate
is now being checked to reduce head-of-line sharing further; it is not accepted
until authenticated concurrency and hold checks pass.
