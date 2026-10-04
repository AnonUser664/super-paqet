# Benchmark evidence

Current tuning and corrected duplex qualification are in progress. The tables
below record earlier revisions. Historical iperf3 `--bidir` results are invalid
for proxied connections because direction assignment depended on accept order;
new tests use two simultaneous one-way tests on separate target ports.

Test host: Intel i5-13420H, 12 logical CPUs, 7.4 GiB RAM, Linux
7.2.4-arch1-2, Go 1.27.0-X:nodwarf5. Both tunnel processes and workload processes
run on this laptop in isolated network namespaces connected by veth links.
These results do not measure Internet paths or arbitrary firewall products.

## Historical first acceptance

The final tested binary SHA-256 is
`be964d2f7f00b4688c76480d590a82a6f8c4b41a6ee33513253648a1649a6aaf`.

| Workload | Received result | Artifact directory |
|---|---:|---|
| AES-GCM iperf3, eight sessions, 12s upload | 7.854 Gbit/s | `build/qualification-clean-final` |
| Same test, download | 4.566 Gbit/s | `build/qualification-clean-final` |
| Same test, bidirectional | 2.408 + 2.633 Gbit/s | `build/qualification-clean-final` |
| 100,000 forwards held 120s with concurrent bulk and HTTP | 3.329 Gbit/s and 2,786 requests/s; zero load errors | `build/qualification-mixed-graceful` |

The 100,000-connection ramp took 4.216 seconds. Peak client/server RSS was
1.794/2.178 GiB; the target peaked at 0.401 GiB. Each tunnel process held about
100,000 descriptors. HTTP median/p99 latency bucket upper bounds were 4.096/
16.384 ms. Most held connections were idle; eight bulk workers and eight HTTP
workers generated the concurrent active load. Kernel socket memory is outside
RSS, and the laptop also runs the target and load generator.

The full application race suite, full smux race suite (468s), full KCP suite
(131s), and vet checks passed. The isolated systemd test exercised the actual
capability/filesystem restrictions, SIGKILL/restart, integrity and firewall
cleanup. Earlier functional capture/restart runs also verified UDP through
65507 bytes, TCP directional EOF, multiple peers/clients and encrypted ping.

Two defects found during WAN qualification were fixed: local KCP encryption/send
queue overflow, and full stream closure being mistaken for directional EOF.
The send FIFO now grows on demand within the send-window budget. The optional
encrypted smux reset command releases blocked writers after target abort while
preserving already delivered ordered data. Graceful EOF in both directions does
not add an unnecessary reset packet.

## Final virtual WAN matrix

All these runs used the same final binary as the local acceptance above. Each
profile first passed a complete integrity transfer, then ran 20s per workload.
The bridge emulator adds no IP hop or header rewrite. Every run completed with
zero load errors, clean owned-rule teardown and unrelated-rule preservation.

| Profile | Received application goodput | HTTP p99 upper bound |
|---|---:|---:|
| 100 Mbit/s, 20ms RTT, 1% random loss | 79.94 Mbit/s | 65.54ms |
| 100 Mbit/s, 40ms RTT, burst loss 0.5/20/80/0.1% | 67.26 Mbit/s | 131.07ms |
| 100 Mbit/s, 50ms base RTT, 5ms one-way jitter, 1% reorder, 0.5% loss | 35.09 Mbit/s | 262.14ms |
| 1 Mbit/s, 100ms RTT, 5% loss | 0.849 Mbit/s | 524.29ms |
| 20/100 Mbit/s asymmetric caps, 80ms RTT, 0.5% loss | 13.43 upload / 18.56 download Mbit/s | — |
| 1 Gbit/s, 100ms RTT, one flow, no injected loss | 348.71 Mbit/s upload | — |

The mobile bulk interval received bytes but completed no 1MiB requests across its
four workers before cancellation; its bulk latency is undefined. Its preceding
complete 1MiB integrity check passed. The asymmetric bidirectional run received
8.07 + 8.33 Mbit/s. These lower utilization results are open tuning issues, not
evidence of best possible performance on every link.

The [compact evidence export](qualification.json) retains configuration, binary
hashes, workload results, process resources and cleanup for all eight final runs.
Raw captures, profiles, logs and emulator counters remain under the listed
ignored artifact directories. Earlier milestones below used earlier revisions.

## Recorded milestones

| Workload | Observed result | Artifact directory |
|---|---:|---|
| Original proxy, HTTP bulk, four sessions | 0.50 Gbit/s | `build/bench-baseline` |
| Optimized encrypted TCP, indexed KCP ACKs | 2.89 Gbit/s | `build/bench-kcp-indexed` |
| Adaptive send window, encrypted TCP | 2.82 Gbit/s | `build/bench-adaptive-clean` |
| 100,000 established TCP forwards, 20-second hold | zero load errors; sampled sockets remained usable | `build/bench-hold-100k` |
| 100 Mbit/s, 20 ms RTT, 1% loss | 84.7 Mbit/s payload goodput; zero load errors | `build/bench-loss-100m-fixed` |
| 1 Mbit/s, 100 ms RTT, 5% loss | 0.764 Mbit/s payload goodput; zero load errors | `build/bench-lowband-loss` |
| AES-GCM iperf3 upload / download | 5.916 / 4.279 Gbit/s received | `build/bench-iperf` |
| AES-GCM, batched ACKs, iperf3 upload / download | 8.316 / 3.727 Gbit/s received | `build/bench-iperf-batched-acks` |
| Batched ACKs, bidirectional iperf3 | 2.461 + 2.584 Gbit/s received | `build/bench-iperf-batched-acks` |
| Functional and forced-server-restart checks | TCP/UDP/half-close/ping passed; owned rules recovered | `build/bench-crash-recovery-v2` |

Milestones used different implementation revisions, session counts and workloads.
The HTTP bulk response was reduced from 16 MiB to 1 MiB after early experiments
to let slow-link requests complete. Do not infer a speedup ratio by comparing
different protocols, directions, response sizes, crypto settings or revisions.
Experimental failures remain under `build/` to explain fixes, not as acceptance
evidence. Final qualification runs are appended separately.

At 100,000 idle forwards, measured peak tunnel RSS was 1.15 GiB client and
1.57 GiB server. Each process held about 100,000 socket descriptors. The ramp
took 6.9 seconds. The target and load generator have their own memory costs;
the hold test uses a minimal responder to avoid net/http buffers dominating
the laptop's memory. Active request/throughput tests use net/http or iperf3.

## Reproduce

```sh
make build bench-build
sudo python3 scripts/netns_bench.py --enterprise --binary build/super-paqet \
  --duration 20 --workers 32 --sessions 8 --output build/clean
sudo python3 scripts/netns_bench.py --enterprise --binary build/super-paqet \
  --duration 20 --rate-mbit 100 --delay-ms 10 --loss 1 --output build/impaired
sudo python3 scripts/netns_bench.py --enterprise --binary build/super-paqet \
  --hold 100000 --mixed --duration 120 --sessions 8 --workers 64 --output build/mixed
python3 scripts/summarize_bench.py build/clean build/impaired build/mixed
```

`--delay-ms` is one-way delay. `--queue-packets` sets a finite shaper queue.
`--capture` saves a 128-packet sample and live firewall counters. Captures showed
the expected PSH+ACK headers, checksums, options and window values while the
server's INPUT drop rule counted the original packets. This verifies local
capture-before-firewall behavior; it does not prove traffic-classifier equivalence.

`--profile` records CPU profiles. `--restart` kills the server, restarts it, and
checks recovery. `--functional` adds a second client and a second server address
in one server process, tests UDP sizes including 0 and 65507, verifies TCP
half-close, and checks encrypted ping. Cleanup checks preserve an unrelated
firewall rule and reject leaked owned chains.

Run the WAN profiles sequentially:

```sh
sudo python3 scripts/qualify_wan.py --duration 20 --output build/wan-qualification
sudo python3 scripts/systemd_netns_test.py
```

The WAN runner includes random loss, Gilbert-Elliott burst loss, jitter/reordering,
1 Mbit/s mobile-like conditions, asymmetric caps and a high-delay single flow.
Use `--cases` to select individual profiles. The underlying harness supports
`--bridge`, `--jitter-ms`, `--reorder`, `--burst-loss`, `--down-rate-mbit`, and
`--seed`. It saves netem statistics, including queue drops. Seeds reproduce the
configured randomness; process scheduling can still change measured outcomes.
iperf uses a one-off server per direction and checks that previous tunnel work
has drained before starting the next direction.

`--direct-iperf` adds TCP and 1400-byte UDP controls over the same virtual link;
`--tcp-buffer-mib` changes only that namespace's TCP autotuning ceilings. Direct
TCP on a 1 Gbit/s/100ms path rose from about 211 to 906 Mbit/s after increasing
those ceilings, while direct UDP reached 961 Mbit/s. This calibration separates
socket-buffer limits from tunnel limits without modifying host sysctls.

For iperf3, build a local copy (3.20 was used here) following the
[official source build instructions](https://software.es.net/iperf/building.html)
with prefix `$(pwd)/build/iperf-local`. Then add `--iperf` to the harness. Its
upload, reverse and bidirectional runs save complete iperf3 JSON. Summary rates
use received bytes, not sender bytes.

## Interpretation and remaining limits

RSS sampling is periodic and includes Go runtime retained memory. Kernel socket
memory is outside RSS. Earlier reports contain cumulative process CPU seconds;
later reports also record per-workload CPU deltas as average occupied cores.
Latency quantiles are upper bounds from logarithmic microsecond buckets.
Workloads canceled at their scheduled end contribute received bytes to goodput,
but are not counted as completed requests or unexpected errors.

A single-flow 1 Gbit/s / 100 ms RTT test initially measured only 240–265 Mbit/s.
Removing local send-queue overflow improved observed results to 338–402 Mbit/s
in later 20–30s tests. Larger static windows and stronger probing did not provide
a consistent improvement. High-delay, jitter/reordering and asymmetric-link
utilization remain qualification gaps; clean multi-flow gigabit numbers do not
justify claiming full capacity on those paths.

Long soaks, broad hardware coverage, real WAN NAT/firewall qualification and
adversarial authenticated-peer resource tests are distinct from local acceptance.
No universal optimality or blanket production-readiness claim follows from this
table.
