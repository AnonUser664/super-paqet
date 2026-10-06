# Benchmark evidence

## Current liveness executable

The deployed `enterprise-2026.10.07-liveness` binary is
`04508dd5254663042f161c812e001d03e3c8491a2ce37c2034778cf7e2de05bf`.
Exact-production-buffer local checks measured 4.634/4.808 Gbit/s clean receiver
bulk, reverified 100,000 held forwards after 120 seconds, and passed changing-delay
churn/asymmetric mixed load. The 100k run used swap. Static changing-delay churn
still failed; one-day production observation is unfinished. See
[status](STATUS.md) and [same-binary evidence](liveness-qualification-2026-10-07.json)
for parameters/resources/cancellations and failure counts.

## Earlier executable and configuration evidence

Measurements below describe finite workloads on identified hardware and configurations.
The retained executable's SHA-256 is
`47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`.
The [release evidence](final-deployment-evidence.json) retains the final-stage
checks; [clean-link evidence](clean-null-current-evidence.json) retains the exact
executable's isolated unencrypted measurements.

## Earlier release results

| Workload | Observed result | Scope |
|---|---|---|
| Isolated null bulk | Upload 3.50–3.74 Gbit/s, download 3.08–3.10 Gbit/s | Eight shared carriers, eight streams, 30 measured seconds plus five omitted startup seconds. |
| Simultaneous duplex | 1.75–1.84 + 1.76–1.88 Gbit/s | Concurrent one-way tests; every receiver stream carried bytes. |
| Local 100k mostly idle forwards | All 100,000 verified, 6.952 s ramp, 120 s soak, zero errors | Laptop namespaces; swapping occurred. |
| Bulk and HTTP beside 100k hold | 2.084 Gbit/s; 1,168 HTTP req/s; p99 upper bound 32.768 ms | Mixed load, not an isolated bulk test. |
| Deployed capacity stage | All 8,192 held forwards verified, zero forwarding errors | Earlier eight-carrier cohort, before the current Netherlands outage. |
| Authenticated burst at 32 workers/client | 1,024/1,024 passed | Earlier four-route deployment measurement. |
| Authenticated burst at 64 workers/client | Germany 1,024/1,024; Netherlands 1,020/1,024 | Four Netherlands TLS timeouts remain failures. |
| Latest Netherlands controls | Current 0/4, previously working 0/4, upstream null 0/4 | Authenticated 1 MiB requests; severe loss between captured interfaces. |

Seven final-binary virtual profiles passed: random loss, reordering, harsh
jitter/loss, asymmetric rates, ACK-limited duplex, outage and pcap restart.
Reload continuity passed 256 active streams through 18 edit checkpoints with
20/70 ms directional delay, 20/50 Mbit/s caps, 0.3% loss and 5% reorder.
Root race/vet and fork suites passed at the recorded release checkpoint.

A saturated virtual 1/100 Mbit/s duplex configuration with timing/credit
extensions disabled left an upload stream with zero measured bytes, including a
longer repeat. This is a known failed gate, not universal asymmetric-link support.

## Resources and measurement limits

The laptop has an Intel i5-13420H, 12 logical CPUs and about 7.4 GiB RAM. Both
endpoints and generators share it. Clean bulk averaged roughly 1.7–2.3 occupied
CPU cores per tunnel and less than 60 MiB peak RSS with no tunnel swap.
The 100k hold peaked near 1.83 GiB RSS per tunnel; RSS plus swap reached roughly
1.90/2.32 GiB client/server. Kernel sockets and generators are additional.
Deployed 8k-load tunnel RSS peaked around 153–225 MiB.

Rates are received application payload, excluding tunnel overhead. HTTP p99
values are logarithmic histogram upper bounds. Duplex uses concurrent one-way
iperf clients; historical iperf `--bidir` role-order measurements are not accepted.
Seeded live runs still vary with scheduling; virtual-clock KCP tests check exact
repeatability. Local capacity does not establish real-host WAN throughput,
100k busy customers, multi-day stability or universal production readiness.

The [clean-link report](CLEAN-LINK-CURRENT.md) explains the unresolved gap from
historical peaks. The [Netherlands report](NETHERLANDS-DIAGNOSIS.md) explains the
current path failure. Those path-failure and tuning-pause statements describe that earlier checkpoint;
current reachability and incident work are recorded in STATUS.md.

## Reproduction

```sh
make build bench-build
make vet test
sudo python3 scripts/netns_bench.py --enterprise --binary build/super-paqet \
  --block null --shared-source --sessions 8 --workers 8 \
  --duration 30 --warmup 5 --iperf --output build/clean-bulk
sudo python3 scripts/stress_links.py --binary build/super-paqet \
  --duration 20 --profile --output build/wan-matrix
sudo python3 scripts/live_reload_netns_test.py --binary build/super-paqet \
  --streams 128 --cycles 12
```

Bulk scripts expect iperf3 at `build/iperf-local/bin/iperf3`; that locally installed
3.20 tool is retained. To rebuild it from upstream source, use its standard
configure/make/install workflow with prefix `$PWD/build/iperf-local`.
Tests need Linux root access, iproute2, iptables, libpcap and the built workload
utility. Each run writes new evidence under its chosen ignored build directory.
For the exact retained deployment executable, use the command in the clean-link
report and its compatibility-library environment where needed.

## Netherlands repaired-path check

The resumed [path diagnosis](NETHERLANDS-DIAGNOSIS.md) qualified the selected
S/PA, null, MTU1350 profile on the original tuples: 1024 checked TCP churn echoes
(64 workers per client), sixteen continuous two-minute streams without retry,
eight complete 10 MiB Reality downloads and four post-cleanup 1 MiB downloads.
Germany and public-domain regression checks passed. This is an additional finite
WAN repair gate; it does not resolve the deferred clean-link historical throughput
gap or substitute for a thousands-of-busy-customers qualification.

The same Netherlands profile subsequently passed the equivalent finite
connection-churn and two-minute echo-soak gate on Finland 65.109.249.222, with
four full 10 MiB Reality downloads and final public/regression checks.
[Measurements and older failed-IP control](FINLAND-CHECK.md).
