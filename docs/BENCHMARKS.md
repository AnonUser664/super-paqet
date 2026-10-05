# Earlier local step 1 benchmark evidence

This document describes a historical frozen local candidate. Current deployed
settings and undeployed fixes are in [STATUS.md](STATUS.md) and
[DEPLOYMENT.md](DEPLOYMENT.md). The user has tested the live version with one
active user; the local 100k results below are not real-host busy-customer proof.

Local step 1 qualification passed: the complete 26-profile matrix, twelve
additional seeded runs, the strengthened connection soak, service recovery,
full checks and fuzzers all passed on the candidate identified below. Verified
configuration, resources, cleanup and exit statuses are in
[step1-qualification.json](step1-qualification.json). Earlier revisions are retained in
[BENCHMARKS-HISTORY.md](BENCHMARKS-HISTORY.md); historical iperf3 `--bidir`
measurements are invalid and are excluded from current evidence.

Tested binary: `build/super-paqet-pcap-address-fix`, SHA-256
`e2ae9b8cce7dc864dae9d21c5f770bb353ab787c218f2faf1d794d8efe7003f6`.
Runtime source checkpoint: `2d5f7a0`; `64fe72a` changes a library test and the
qualification runner only. Full source checks use the isolated checkout at
`build/step1-qualification-source`, excluding other uncommitted workspace edits.

## Changes retained after testing

| Observed problem | Retained correction |
|---|---|
| Reverse-path congestion and deliberate ACK delay throttled an otherwise healthy forward path | Optional encrypted ACK receive/emission timestamps distinguish scheduling delay and directional queue growth; full RTT still budgets windows and retransmission timers |
| Jitter/reorder transit minima caused persistent false congestion | RTT-variance threshold and hysteresis reject noise before pacing backs off |
| Stream credit sat behind bulk data and blocked the opposite direction | Coalesced reliable credit updates plus optional KCP WINS credit hints; reliable fallback remains mandatory |
| New HTTP opens shared an already pressured carrier | Bounded adaptive carrier growth and failed-carrier exclusion isolate new work without closing established streams |
| Pacing deferred work until a later periodic update | Earlier scheduler wake and a bounded pacing credit budget; ACK/window controls remain exempt |
| Debug logging could dominate large connection runs | Bounded asynchronous output, sampled lifecycle events, per-carrier summaries and explicit dropped-log counters |
| Pcap diagnostics panicked because LocalAddr was nil | Return a cloned configured IP and reserved source port; IPv4/IPv6 aliasing tests and live fallback recovery checks |

The Ethernet/IP/TCP encoding and raw capture/injection mechanism remain the
outer transport. The timestamp and credit extensions are inside encrypted KCP;
they do not add a separate application transport. This preserves the mechanism,
while measured traffic timing and size distributions remain deployment-specific.

## Host and measurement method

Intel i5-13420H, eight physical cores / 12 logical CPUs, 7.4 GiB RAM, 15 GiB
swap; Linux 7.2.4-arch1-2, Go 1.27.0-X:nodwarf5, libpcap 1.10.7, iperf3 3.20.
Both tunnel endpoints, targets and load generators share this laptop. Linux
namespaces and a bridge with netem simulate links without an extra IP hop.
Measurements run sequentially, with AES-128-GCM and debug transport summaries.

Rates are received application payload, excluding tunnel overhead. The main
matrix uses seed 42 and 20-second workload intervals. Duplex uses two concurrent
one-way iperf tests on separate target ports, and requires every measured
receiver stream to deliver bytes. HTTP p99 values are logarithmic histogram
upper bounds. Deliberate workload-end cancellations are reported separately
from unexpected errors. Finite seeded live tests still vary with scheduling;
the KCP virtual-clock tests assert identical results on repeated simulations.

## Current live results

| Link | Upload / download, Mbit/s | Simultaneous duplex, Mbit/s |
|---|---:|---:|
| Clean veth, eight initial carriers | 4386.89 / 3707.62 | 2360.77 + 2127.25 |
| 1000 Mbit/s each direction, 100 ms RTT, one flow | 915.40 / 915.46 | 909.11 + 902.77 |
| 100 Mbit/s each direction, 600 ms RTT | 90.13 / 90.13 | 78.17 + 68.21 |
| 20/100 Mbit/s, 80 ms RTT, 0.5% loss | 16.38 / 81.79 | 15.63 + 54.26 |
| 1/100 Mbit/s, 50 ms RTT | 0.75 / 79.74 | 0.52 + 71.09 |

| Link stress | Bulk goodput, Mbit/s | Separate HTTP p99 upper bound |
|---|---:|---:|
| 100 Mbit/s, 20 ms RTT, 1% random loss | 77.07 | 131.07 ms |
| 100 Mbit/s, 80 ms RTT, 5% loss | 44.78 | 1048.58 ms |
| 20 Mbit/s, 100 ms RTT, 20% loss | 7.82 | 2097.15 ms |
| 100 Mbit/s, 50 ms base RTT, 5 ms jitter, 5% reorder | 53.34 | 131.07 ms |
| 100 Mbit/s, 50 ms base RTT, 10 ms jitter, 0.5% loss | 50.34 | 262.14 ms |
| 100 Mbit/s, 200 ms base RTT, 25 ms jitter, 10% reorder, 5% loss | 8.90 | 2097.15 ms |
| 1 Mbit/s, 100 ms RTT, 5% loss | 0.73 | 524.29 ms |

HTTP and bulk rows above are separate workloads. The mobile bulk interval
received bytes but completed no 1 MiB responses across four workers before
cancellation; bulk response latency is undefined. A complete integrity transfer
passed before the timed workload.

| Concurrent bulk plus HTTP connection churn | Duplex goodput, Mbit/s | HTTP requests/s | HTTP p99 upper bound |
|---|---:|---:|---:|
| 1/100 Mbit/s, 50 ms RTT | 0.62 + 74.82 | 4.75 | 4194.30 ms |
| 20/100 Mbit/s, 80 ms RTT, 0.5% loss | 14.32 + 62.50 | 10.14 | 1048.58 ms |

Both mixed tests had zero unexpected request errors. These results show the
remaining cost of sharing a saturated constrained path; they do not establish
a low-latency service guarantee on every link. Clean connection churn completed
6,552 requests/s with a 16.384 ms p99 upper bound.

All 26 main profiles passed their integrity/error and conservative performance
regression checks. Additional coverage includes burst loss, tiny queues, MTU
576, IPv6/MTU 1280, capacity and delay steps, a three-second blackout, forced
restart, multiple peers/clients, TCP directional EOF, UDP through 65,507 bytes,
and the pcap fallback. Every main run removed its owned firewall rules and
preserved the unrelated rule planted by the harness.

## Additional seed coverage

All six repeated profiles passed at seeds 7 and 313. Together with the main
seed-42 matrix, the evidence contains 38 live profile/seed runs. Reordered-link
bulk goodput was 53.16–54.52 Mbit/s across all three seeds; combined high-delay,
jitter, reorder and 5% loss yielded 8.53–11.07 Mbit/s. Outage runs delivered
59.36–66.03 Mbit/s over intervals that included a three-second blackout, with
zero unexpected load errors. Mixed 1/100 Mbit/s tests delivered 71.37–74.82
Mbit/s in the larger direction while HTTP churn p99 ranged from 2.097 to
4.194 seconds. That latency remains a qualification limit under saturation.

The deterministic KCP matrix covers 109 virtual scenarios, including loss,
reorder/duplicates, delayed/restricted ACKs, outages, slow/paused readers,
capacity changes and sequence/clock wrap. The main profile/seed/pacing
combinations each run twice and assert identical delivery/loss/retransmission
results. Full root and smux race suites, full KCP tests and vet checks passed.
The 60-second control and raw-frame fuzzers passed 6,397,038 and 3,502,354
executions. Transient systemd service restrictions, forced SIGKILL/restart,
post-restart integrity and firewall recovery also passed on the same binary.

## Connection scale and endurance

The strengthened 600-second mixed soak established 100,000 forwards in 9.216
seconds with zero ramp errors. It then verified a complete response on every
held socket: 100,000/100,000 remained usable, with zero verification or mixed
workload errors. Eight HTTP and eight bulk workers ran beside the mostly idle
held connections for 598 seconds. Both endpoints shut down with zero active
flows; expected workload-end cancellations were logged separately.

| Concurrent workload | Result |
|---|---:|
| Bulk received payload | 1.572 Gbit/s |
| HTTP | 722 requests/s |
| HTTP median / p99 histogram upper bounds | 16.384 / 32.768 ms |
| Whole-stage average occupied client / server cores | approximately 1.68 / 1.57 |
| Peak sampled client / server RSS | 1.829 / 1.930 GiB |
| Peak sampled client / server RSS plus swap | 1.902 / 2.305 GiB |
| Peak client / server descriptors | 100,058 / 100,027 |

Whole-stage CPU divides recorded process CPU by the 619.984-second harness
stage; it includes ramp and verification, rather than isolating steady traffic.
RSS plus swap is the maximum concurrent sum per process, not the sum of two
independent maxima. Kernel socket memory, the target, generator and other host
applications are additional. The laptop experienced substantial memory pressure
and swapping, so RSS alone understates this scale run's footprint.

The scale run explicitly raised host `netdev_max_backlog` from 1,000 to 65,536.
An earlier run at 1,000 recorded one premature local TCP timeout alongside
loopback receive drops. That exposed the weakness of checking only three held
sockets. The new generator verifies all sockets, and a deliberate isolated
reset test confirmed that it reports 99/100 verified with one error. The repeat
also reduced descriptor-monitoring overhead; these changes were made together,
so the two runs do not isolate the contribution of backlog tuning alone.
The original backlog was restored before service and WAN qualification, with
`restored: true` recorded in the scale artifacts. No host firewall/routes were
changed. A 100,000-socket deployment needs host networking/memory qualification;
this result does not promise the same outcome with every host's default limits.

## CPU profiles and hardware limits

Clean upload occupied 3.598 client and 3.304 server cores on average. Clean
duplex occupied 3.524 and 3.532 cores. Both endpoints and generators contend for
the same 12 logical CPUs; these are local measurements, not NIC line-rate
claims. CPU profiles for the clean upload attribute 52.36% of client samples
and 39.84% of server samples to kernel syscall time. AES-GCM accounts for
6.59% and 7.31%; client checksumming is 7.67% cumulatively. The remaining
bottleneck includes kernel raw-packet work, rather than just encryption.

Resource samples include Go runtime retained memory. RSS excludes kernel socket
buffers; targets and generators have separate memory costs. Per-workload CPU
counts use process CPU deltas divided by wall time. Debug logging is bounded
and sampled. Selected clean/asymmetric/reorder/jitter/harsh logs showed no
diagnostic drops or local KCP output-pipeline drops; expected workload-end
resets are recorded as debug relay failures rather than hidden.

## Reproduce and inspect

```sh
make build bench-build
sudo python3 scripts/stress_links.py --binary build/super-paqet \
  --duration 20 --profile --output build/step1-matrix
sudo python3 scripts/qualification_tail.py --binary build/super-paqet \
  --matrix build/step1-matrix/matrix.json --output build/step1-tail \
  --hold-seconds 600 --host-backlog 65536
```

The tail runs vet, root and smux race suites, the full KCP suite, 100,000 held
connections with mixed traffic and complete post-soak verification of every
connection, transient systemd crash recovery, two 60-second
fuzzers, and six challenging live profiles at seeds 7 and 313. It records actual
exit statuses and stops on failure. The exporter rejects incomplete coverage,
wrong binary hashes, zero-byte duplex streams, incomplete held-socket
verification, workload errors, dirty firewall teardown and unrestored host
settings. The explicit `--host-backlog` option temporarily changes the global
Linux receive backlog for the scale run; default runs do not change it.
The harness saves the original/applied values and restores the original before
service, fuzzing and WAN tests. Do not run competing host-backlog changes during
that isolated experiment. Raw logs, profiles, captures and netem counters remain under
`build/step1-final-v5` and `build/step1-final-v5-tail-final`.
The isolated full-check log is retained under `build/step1-final-v5-tail-retry`.

Build a local iperf3 in `build/iperf-local` when it is unavailable. The harness
uses `build/iperf-local/bin/iperf3`. `--delay-ms` is one-way delay; queue budgets
include propagation for small ACK packets. `--capture` checks the fabricated
PSH+ACK framing and capture-before-firewall behavior in this Linux environment.
[DIAGNOSTICS.md](DIAGNOSTICS.md) explains log fields and individual reproductions;
[TRANSPORT.md](TRANSPORT.md) records the packet contract and why it matters.

## Scope of qualification

100,000 mostly idle connections and multi-gigabit bulk throughput are separate
acceptance workloads. A ten-minute mixed soak is a local endurance check, not a
multi-day deployment soak. Virtual links cannot establish behavior through every
NAT/firewall or traffic classifier. Outer packet-format tests protect the
original mechanism; inner encrypted extensions and traffic timing can still
change observable distributions. The original local qualification preceded deployment. A subsequent recovery
version is now running; newer fixes and the final deployment are paused for
user document review and the final required feature decision. These historical
results do not automatically qualify the newer code or current recovery profile.
