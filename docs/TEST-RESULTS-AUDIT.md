# Test results audit: throughput and Netherlands reachability

Recorded on 2026-10-06. This audit reads retained results; it does not represent
a new benchmark run. It separates workloads and executable versions. Missing
measurements remain missing, and failed experiments remain failed.

## Why 2.08 Gbit/s is not the isolated clean-link result

The exact deployed executable (`47c61ac2…`, runtime source `abf04f6`, main
equivalent `97529dd`) delivered **2.084 Gbit/s while holding 100,000 TCP forwards
and serving concurrent HTTP requests**. That run used eight shared-source KCP
lanes, null encryption, default adaptive settings, and no injected link faults.
The laptop swapped. It used HTTP bulk responses, not isolated iperf3 streams.

Crucially, its `kcp_options` was `{}`: **the new `small_write_flush` setting was
zero, its disabled default**. Thus that result cannot be attributed to enabling
the small-write exception. The implementation still allows a small write to
flush data already queued ahead of it; enabling it could affect batching and
must be measured, rather than assumed free of a throughput cost.

| Executable / workload | Upload or bulk, Gbit/s | Download, Gbit/s | Difference that prevents a causal comparison |
|---|---:|---:|---|
| Historical `be964d2f…`, isolated iperf3 | 7.854 | 4.566 | Eight ordinary carriers, AES-GCM, 12 s |
| Later historical `e2ae9b8c…`, isolated iperf3 | 4.387 | 3.708 | Different runtime and reliability implementation |
| Shared-source `4e826bcc…`, isolated iperf3, null | 3.933 | 2.970 | Eight shared lanes, 15 s plus 4 s omitted startup; temporary backlog 65536 |
| Same shared-source executable, isolated iperf3, AES-GCM | 1.778 | 1.951 | Backlog default instead of 65536 as well as cipher change |
| Historical `be964d2f…`, 100k hold + HTTP + bulk | 3.329 | — | AES-GCM, ordinary carriers, earlier harness/runtime; older verification scope |
| Indexed `5f57e208…`, 100k hold + HTTP + bulk | 2.240 | — | Shared lanes/null, before subsequent controller changes |
| Deployed `47c61ac2…`, 100k hold + HTTP + bulk | 2.084 | — | Shared lanes/null; every held socket verified; swapping observed |

These measurements **do not establish that bulk performance was preserved**.
The shared-source isolated results and mixed-load numbers justify investigating
a possible regression. They do not isolate its cause: shared socket processing,
cipher cost, buffering/backlog, other runtime changes and host contention differ.
The exact latest executable has no retained equivalent isolated packet-backend
clean-link iperf3 measurement in this qualification set. An unchanged-harness
comparison across builds and an on/off small-write comparison remain required.
Historical single-command iperf3 `--bidir` results are excluded from acceptance;
the later harness uses two simultaneous one-way tests instead.

Sources: `build/qualification-clean-final/results.json`,
`build/step1-final-v5/clean-seed-42/results.json`,
`build/final-production/shared-clean{,-null}/results.json`,
`build/qualification-mixed-graceful/results.json`,
`build/final-production/{release-scale,small-flush-scale}/results.json`.

## Latest local qualification

| Test | Recorded outcome | Limit |
|---|---|---|
| Exact deployed binary, 100k forwards | 6.952 s ramp, 120 s hold, 100,000/100,000 fully verified, zero errors | Most held connections idle; both endpoints and generators on laptop |
| Concurrent bulk + HTTP in that hold | 2.084 Gbit/s, 1,168 HTTP requests/s, HTTP p99 upper bound 32.768 ms | Not isolated bulk or 100k active customers |
| Memory in that hold | Client/server peak RSS about 1.83/1.83 GiB; RSS+swap peaks about 1.90/2.32 GiB | Kernel socket memory excluded; swapping occurred |
| Latest shared/null virtual matrix | Seven of seven gates passed | Fast mode with small-write threshold 32 and adaptive extensions enabled |
| Latest live reload test | 256 original active streams, 18 edit checkpoints, zero unaffected-stream errors | 20/70 ms directional delay, 20/50 Mbit/s caps, 0.3% loss, 5% reorder |
| Root race/vet, fork suites | Passed; latest complete KCP suite 132.998 s | Finite code tests, not certification of physical paths |
| Outstanding-ACK index differential tests | Passed seeded reference comparisons, including sequence wrap and ring growth | Sparse-gap microbenchmark about 16,000 → 92 ns/op, not whole-app speedup |
| Shared-lane isolation | Null/AES/AES-GCM concurrent payload, closure, unknown-input and FEC rejection tests passed | Opt-in shared protocol requires both endpoints |
| Small-write behavior | Disabled default, vector size, bulk, immediate small writes and ordering tests passed | No retained latest-build clean iperf on/off performance comparison |
| Legacy telemetry-disabled ACK-limited duplex | **Failed**: one upload receiver stream delivered zero measured bytes; longer retry also failed | Cannot claim universal adaptive performance with these extensions disabled |

Latest seven-profile measurements, from the **same deployed executable**:

| Simulated link | Received bulk or upload/download, Mbit/s | Simultaneous duplex, Mbit/s | Separate HTTP p99 upper bound |
|---|---:|---:|---:|
| 100 Mbit/s, 20 ms RTT, 1% loss | 74.53 bulk | — | 262.14 ms |
| 100 Mbit/s, 50 ms RTT, 5 ms jitter, 5% reorder | 53.44 bulk | — | 131.07 ms |
| 100 Mbit/s, 200 ms RTT, 25 ms jitter, 10% reorder, 5% loss | 8.53 bulk | — | 2097.15 ms |
| 20/100 Mbit/s, 80 ms RTT, 0.5% loss | 15.80 / 84.76 | 15.04 + 59.24 | — |
| 1/100 Mbit/s, 50 ms RTT | 0.77 / 83.19 | 0.64 + 75.85 | — |
| Outage profile, including blackout | 52.53 bulk | — | — |
| Pcap functional/restart profile | 2861.92 bulk | — | 131.07 ms |

The pcap profile is a separate clean functional/restart HTTP workload, not an
equivalent isolated iperf baseline. Timed workload-end cancellations are retained
separately; some slow-link tests receive bytes without finishing every response.
All seven include integrity checks and owned-rule cleanup checks.

The first release additionally passed ten profiles including mobile, satellite,
and MTU576. Its satellite 600 ms RTT result was 89.63/86.24 Mbit/s, duplex
85.47+82.40. Those extra scenarios have not all been rerun on the latest binary.
Historical step-1 qualification passed 26 profiles plus twelve extra seeded runs,
109 deterministic virtual KCP scenarios, a 600 s verified 100k soak, and 60 s
control/raw-frame fuzzers. See [BENCHMARKS.md](BENCHMARKS.md) for their exact older
binary boundary and results; they must not be presented as latest-build runs.

## Deployed tests

| Test | Germany | Netherlands (171.22.132.226) |
|---|---|---|
| Eight-lane capacity run, 4096 held forwards/client | All held sockets verified; combined 8192 across both routes, zero errors | Same combined passing run |
| 256-worker forwarded HTTP | 1700–1739 requests/s per client | 2218–2228 requests/s per client |
| 256-worker connection churn | 582–643 requests/s per client | 973–1020 requests/s per client |
| Eight-worker bulk beside the hold, separate route stages | 35.0 / 51.6 Mbit/s per client | 17.1 / 20.1 Mbit/s per client |
| Controlled latency comparison | About 27–36 ms warm-request improvement | Selected safe profile did not retain that improvement |
| Reality burst, 32 workers/client, eight lanes | 512/512 | 512/512 |
| Reality burst, 64 workers/client, eight lanes | 1024/1024 | **1020/1024; four TLS timeouts** |
| Earlier Reality burst, 64 workers/client, four NL lanes | — | 512/512 |
| Earlier repeated Reality 10 MiB downloads | 4/4 | 4/4 |
| Latest repeated Reality 10 MiB downloads, after NL four-lane reversion | 4/4 | **0/4; both clients timed out** |
| Latest public-domain 1 MiB request | Passed | **12 s timeout** |
| Backend-local Reality control, 64 workers | 256/256 | 256/256 |
| Ordinary direct client-to-NL Reality control | — | **0/1024; TLS timeouts despite ordinary TCP connectivity** |

The held-connection load uses direct HTTP forwarding. It is not equivalent to
thousands of busy authenticated Xray customers. Peak tunnel RSS in the eight-lane
load was about 153/155 MiB on the clients and 184/225 MiB on the backends.
Public-route and authenticated failures invalidate an unconditional production
claim, even if process health is green.

Rejected physical-path candidates include larger Netherlands MTUs under burst
load, fast3 settings, two Netherlands receive workers, small-write thresholds
128/32 on Netherlands, and temporary AES/IPv6/new-port recovery attempts. Their
finite successes and failures are retained in the inventory and
[working notes](FINAL-QUALIFICATION-WORKING-NOTES.md); none proves one universal
configuration is optimal.

## Did Netherlands work, and is filtering established?

**Yes, 171.22.132.226 worked in recorded tests from both clients.** Before this
release, the recovered null/fast/MTU128 path completed repeated 10 MiB transfers.
The first redeployment and shared-source deployment each passed all eight 10 MiB
checks across four paths. Four shared Netherlands lanes later passed 512/512
authenticated requests at concurrency 64. The latest failures are therefore a
loss of a previously observed working path, not a path that never worked.

The initial upstream comparison predates that recovery: unmodified paqet's
1 MiB checks failed on both Netherlands paths, while .118 → Germany passed.
Only that Germany path was originally verified by the user. This earlier result
is evidence of shared failure **under those earlier conditions**, not a fresh
upstream control for today's failure.

Earlier paired captures showed valid outbound PA packets at a client, correct
checksums/interface/gateway MAC, and no corresponding arrival at the backend.
That observation points to loss before the server's capture point. Port, IP,
MTU and traffic-cadence sensitivity make filtering plausible. Ordinary TCP
connectivity and passing backend-local Xray tests do not establish that the raw
tunnel traffic can traverse the path.

**The cause is unresolved.** Provider/path filtering is possible; capture or
host delivery problems are possible; app-generated traffic could trigger a
filter. A client-valid packet is not proof that its traffic pattern is accepted
by a firewall. No current synchronized upstream/enterprise comparison under
identical port, source tuple, cipher, MTU and workload has established which
explanation accounts for the latest failure.

## Complete retained structured-result inventory

[test-results-inventory.json](test-results-inventory.json) enumerates all readable
`*results.json` files under `build/`, recorded matrix gate outcomes, and retained
final-stage remote stress/bulk/public results. It includes experimental failures
and incomplete results. Numeric observations and non-secret test labels are
exported; configurations, credentials, raw payloads and arbitrary error text are
excluded. Absence of an error counter does not mean a pass. Some old iperf duplex
outputs use the invalid historical method described above.

The export contains 353 result files, 24 matrix files with 176 gate records, and
43 final-stage remote reports. These overlap and are not counts of distinct
executed tests. Iperf interval samples are omitted from the compact export;
aggregate receiver rates and per-stream receiver bytes remain available.

This inventory covers **available structured artifacts**, not every shell command
ever run or tests whose output was overwritten. Code-suite/fuzzer evidence is
documented separately above and in the historical reports. The index records
the artifact boundaries instead of claiming an unverifiable total test count.
