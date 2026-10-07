# Clean-link unencrypted bulk measurements

## Current liveness executable

The deployed `enterprise-2026.10.07-liveness` binary, SHA-256
`04508dd5254663042f161c812e001d03e3c8491a2ce37c2034778cf7e2de05bf`, measured
**4.634 Gbit/s upload / 4.808 Gbit/s download** received payload in separate
five-second runs with sixteen streams and no startup omission. Both endpoints
shared the laptop's clean uncapped veth link: four initial shared carriers with
automatic growth allowed, null encryption,
no FEC, MTU1350, production 4 MiB/2 MiB mux buffers and manual adaptive profile.
Mean occupied client/server cores were 1.60/1.21 for upload and 1.29/1.81 for
download; peak tunnel RSS was below 150 MiB with no fixture swap. Integrity and
owned-rule cleanup passed. This is a short local measurement, not backend WAN
capacity or a matched comparison against the longer older runs below. Parameters
and receiver receipts are in [liveness evidence](liveness-qualification-2026-10-07.json).

A corrected fixed-four rerun on 7 October explicitly sets `sessions: 4` and
`max_sessions: 4`: **4.486 Gbit/s upload / 4.901 Gbit/s download** on the same
04508 executable. The staged ownership.2 candidate measured **4.479 / 4.836
Gbit/s**, using identical parameters and production mux buffer ceilings. These
single five-second pairs differ by about 0.2%/1.3%; they do not establish a
statistically significant performance change or backend WAN capacity. Local raw
receipts are `build/production-qualification/liveness-fixed4-clean-bulk-a` and
`ownership2-fixed4-clean-bulk`. Historical fixtures with an omitted
`max_sessions` must not be described as fixed-four tests.

## Earlier executable measurements

Measured 2026-10-06 on the then-deployed executable, SHA-256
`47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`.
Embedded runtime source `abf04f6`, main equivalent `97529dd`. No runtime code was
changed for these measurements.

## Method

Both endpoints and iperf3 run on the i5-13420H laptop in disposable network
namespaces connected by a clean veth pair. No injected loss, delay or bandwidth
cap; null encryption, no FEC, AF_PACKET backend, 1500-byte virtual interface MTU,
eight initial shared-source KCP lanes and default adaptive reliability settings.
These are generic clean-link settings, not the conservative deployed WAN profile.
The small-write threshold defaults to zero. Debug/profiling are disabled.

Each direction has eight TCP streams, five omitted startup seconds and thirty
measured seconds. Duplex uses two simultaneous one-way tests on separate targets,
with eight streams in each direction. Reported rates are **received payload**.
Every run first verifies a complete 16 MiB patterned transfer. There are no 100k
held connections or concurrent HTTP workload in these tests.

The initial launch failed before measurement because the local dynamic loader
could not find Ubuntu's `libpcap.so.0.8` name. Subsequent runs use the existing
compatibility library via `LD_LIBRARY_PATH`, without rebuilding the executable.
That startup failure is retained in the evidence and excluded from throughput.

## Results

| Configuration/run | Upload, Gbit/s | Download, Gbit/s | Simultaneous upload + download, Gbit/s |
|---|---:|---:|---:|
| Default, run A | 3.738 | 3.078 | 1.838 + 1.879 |
| Default, run B | 3.496 | 3.096 | 1.745 + 1.763 |
| Same settings, `small_write_flush: 32` | 3.151 | 3.105 | 1.862 + 1.902 |
| Flush-32 selective upload repeat | 3.523 | — | — |

The two default-setting runs occupied approximately these CPU cores on average
per tunnel process, including their startup interval:

| Workload | Client occupied cores | Server occupied cores |
|---|---:|---:|
| Upload | 1.85–1.88 | 1.71–1.74 |
| Download | 1.79–1.80 | 1.93–1.96 |
| Duplex | 2.23–2.30 | 2.27–2.33 |

Default-run peak RSS was approximately 53–59 MiB per tunnel process. Across the
four runs, tunnel peak RSS stayed below 60 MiB and tunnel swap stayed zero.
Every measured receiver stream delivered bytes; all iperf clients/servers and
tunnel processes exited successfully. The preceding integrity checks passed.
The runs transferred approximately 119.37 GiB of measured receiver payload in
total, excluding omitted startup traffic. TCP retransmissions occurred and are
retained in the evidence; no injected loss is not a promise of zero local loss.

Owned firewall cleanup and unrelated-rule preservation passed for every run;
namespaces were removed. The host backlog remained 1000 throughout. No remote
service or configuration was modified by these local tests.

## Interpretation and remaining gap

That earlier executable's isolated default-setting result is approximately
**3.50–3.74 Gbit/s upload and 3.08–3.10 Gbit/s download** on this laptop. The
previous 2.084 Gbit/s measurement was bulk beside a 100k connection hold and HTTP
load, with swapping; it was not this isolated workload.

Relative to the earlier 7.854/4.566 Gbit/s peak, these received rates are about
**52–55% lower in upload and 32–33% lower in download**. Relative to the later
historical 4.387 Gbit/s upload, the current upload is about 15–20% lower. These
are numerical comparisons, not attribution of a change to one optimization.
The earlier 7.854 result used a different runtime, ordinary separate carriers,
AES-GCM and a different interval; a matched cross-build comparison is still
needed to isolate the performance cost.

The threshold-32 upload samples span 3.151–3.523 Gbit/s, while its full duplex
result was above both default duplex samples. This finite series does not show
a consistent bulk collapse from the option, and does not prove universally zero
cost. The default-setting measurements had the option disabled, so their gap
from the historical peak cannot be caused by enabling it. Germany's deployed
manual immediate-write profile already flushes without this exception; this
comparison uses generic adaptive defaults instead.

Machine-readable rates, receiver-stream byte counts, CPU, memory, parameters,
hashes and cleanup are in [clean-null-current-evidence.json](clean-null-current-evidence.json).
The compact evidence retains parameters, end-of-run measurements and cleanup.
Historical raw logs were removed during repository cleanup.

Reproduce a default run:

```sh
sudo env LD_LIBRARY_PATH="$PWD/build/deploy-libs" python3 scripts/netns_bench.py \
  --enterprise --binary build/final-production/super-paqet-small-flush \
  --block null --shared-source --sessions 8 --workers 8 \
  --duration 30 --warmup 5 --iperf --output build/clean-null-reproduction
```

The compatibility-library environment variable is only needed where the expected
libpcap loader name is unavailable. This fixture measures local capacity, not WAN
gigabit throughput, physical filtering or performance on every machine.
