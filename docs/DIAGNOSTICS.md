# Diagnostics and reproducible tests

```yaml
log:
  level: debug
  format: json
  interval: 1s
  flow_sample: 1000
metrics: 127.0.0.1:9090
profiling: true
```

The default is JSON at info level, a 10-second summary interval and one sampled
debug flow per 1000. Debug defaults to a one-second interval. Set `flow_sample: 1`
for a small functional reproduction; sampling prevents large connection tests
from becoming log-throughput tests. Keys, configuration objects and payload bytes
are never passed to the diagnostic logger.

Log output uses a bounded asynchronous queue. Slow or failed output increases
`super_paqet_log_dropped_total`; it cannot backpressure packet forwarding.
Shutdown allows up to two seconds to drain logs. Keep this counter visible when
interpreting incomplete traces.

| Event | Meaning |
|---|---|
| `engine.start`, `listener.ready`, `forward.ready` | Runtime capacity and configured endpoints |
| `engine.summary` | Active/accepted/rejected/failed/aborted counts, traffic, goroutines and lost logs |
| `session.connected`, `session.accepted` | Conversation ID and endpoint identity |
| `session.invalidated`, `session.idle_invalidated` | Carrier replacement |
| `flow.open`, `flow.control`, `flow.relay`, `flow.closed` | Sampled lifecycle, correlated by flow ID within the process |
| `transport.sample` | Delivery estimate, RTT/variance, pacing, windows, queues and wait time |
| `engine.stopping`, `engine.stopped` | Shutdown progress, final active count and firewall cleanup result |

For `transport.sample`, compare `kcp_wait_ms` with `mux_wait_ms`: the first
measures waiting for carrier send credit, the second waiting for stream credit.
Values are deltas since the preceding snapshot, summed across writers; they can
exceed the wall interval when multiple streams wait concurrently or a wait spans
snapshot boundaries. `receive_reordered` shows the packet backlog behind a gap.
`pipeline_queued` and `pipeline_drop_delta` separate local pipeline pressure from
link loss. `startup`, `peak_mbit`, `pacing_mbit`, `loss_ratio` and window values
expose adaptation rather than silently changing parameters.

The KCP virtual-clock matrix uses no sockets, sleeps, shared random state or
goroutine timing. It repeats each seeded simulation and asserts identical
delivery/loss/retransmission results as well as ordered byte integrity. Profiles
include changing capacity, blackouts, ACK restriction, duplicate/reordered
packets, burst/random loss, slow/paused readers, tiny queues and wraparound.

```sh
cd third_party/kcp-go
go test -race -run 'TestVirtualLink|TestPacing|TestGapGrace|TestDelayedACK' -v
```

Live tests exercise the real encrypted/raw-packet runtime. Their fault seeds are
fixed, but process scheduling and hardware load still affect measured outcomes.
The namespace-only harness records the running binary hash, resources, emulator
statistics, logs and cleanup. `--profile` records CPU profiles. `--warmup` separates
iperf startup from its reported measurement interval. Scheduled epochs are
recorded with their actual application times.

```sh
make build bench-build
sudo python3 scripts/stress_links.py --duration 20 --profile
sudo python3 scripts/stress_links.py --cases outage rate-step delay-step ipv6 mtu576
```

The expanded runner measures profiles sequentially and stops on workload or
cleanup failure. It expects the local iperf3 build described in BENCHMARKS.md.
Netem queues include emulated propagation, including small ACK packets; their
default capacity uses a 64-byte minimum-frame budget. Explicit tiny packet-count
queues are separate stress cases. Earlier tests sized queues from 1500-byte data
packets, which incorrectly capped ACK throughput on asymmetric links.
