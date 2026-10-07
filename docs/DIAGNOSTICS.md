# Diagnostics and reproducible tests

The deployed enterprise release includes pcap ENOBUFS recovery and
`packet.tx_queue` events. See [STATUS.md](STATUS.md) for the exact release and
current path availability.

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
| `opening.syn_timeout` | Candidate: SYN submission exceeded this carrier's receipt budget; identity, attempts and remaining opening time |
| `opening.transport_timeout` | New-stream receipt timeout; carrier identity, attempt, remaining deadline and surviving streams |
| `flow.open`, `flow.control`, `flow.relay`, `flow.closed` | Sampled lifecycle, correlated by flow ID within the process |
| `packet.tx_queue` | New transmit queue drops on a shared listener socket, with cumulative and interval counts |
| `transport.sample` | Delivery estimate, RTT/variance, pacing, windows, queues and wait time |
| `peer.pool_grew`, `peer.pool_growth_failed` | Adaptive carrier growth, ceiling and expansion failures |
| `engine.stopping`, `engine.stopped` | Shutdown progress, final active count and firewall cleanup result |

Wholly rejected AF_PACKET ENOBUFS batches receive at most three retries, requesting
50 microsecond sleeps. Partial sends are never replayed. Persistent AF_PACKET and
recognized pcap ENOBUFS remain counted datagram loss handled by KCP. Metrics
`super_paqet_peer_tx_queue_retries_total` and
`super_paqet_listener_tx_queue_retries_total` count retry attempts, including
attempts that fail. Debug `packet.tx_queue` includes retry totals/deltas. Shared
client sockets can appear under several session labels; summing those labels
can overcount distinct socket drops or retries. `super_paqet_*_tx_queue_drops` covers AF_PACKET and pcap.
Debug `packet.tx_queue` events report shared listener drops only when the count
changes. The `transport.sample.tx_queue_drops` field reports the owning client
socket; accepted server carriers share listener sockets and report null there.
Permanent injection/device errors still abort the affected transport.

For `transport.sample`, compare `kcp_wait_ms` with `mux_wait_ms`: the first
measures waiting for carrier send credit, the second waiting for stream credit.
Values are deltas since the preceding snapshot, summed across writers; they can
exceed the wall interval when multiple streams wait concurrently or a wait spans
snapshot boundaries. `receive_reordered` shows the packet backlog behind a gap.
`pipeline_queued` and `pipeline_drop_delta` separate local pipeline pressure from
link loss. `startup`, `peak_mbit`, `pacing_mbit`, `loss_ratio` and window values
expose adaptation rather than silently changing parameters. `forward_queue_ms`
and `reverse_queue_ms` are smoothed transit estimates relative to recent minima,
not absolute one-way propagation latency; `transit_samples: 0` means RTT fallback.
`congested` explains pacing backoff. `output_pps`/`output_kcp_mbit` count inner
KCP output; encryption and Ethernet/IP/TCP overhead are additional. `ack_pps`
counts standalone ACK/window-control packets, including credit hints.
`credit_pending`, hint counters and `write_budget_bytes` show credit bypass and
batching. Receive window minima/maxima are skipped (zero) above 64 streams.

Debug flow IDs are unique within one process. Match `(conv, stream_id)` across
client/server logs. Lifecycle events are sampled; debug opening/target/relay
failures retain identifiers even when their lifecycle was not selected. Use
`--flow-sample 1` for small live reproductions.

The KCP virtual-clock matrix uses no sockets, sleeps, shared random state or
goroutine timing. The main profile/seed/pacing combinations run twice and assert identical
delivery/loss/retransmission results as well as ordered byte integrity; an
additional scenario verifies sequence and clock wraparound. Profiles
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
cleanup failure. It expects the local iperf3 installation described in [BENCHMARKS.md](BENCHMARKS.md).
Netem queues include emulated propagation, including small ACK packets; their
default capacity uses a 64-byte minimum-frame budget. Explicit tiny packet-count
queues are separate stress cases. Earlier tests sized queues from 1500-byte data
packets, which incorrectly capped ACK throughput on asymmetric links.

Duplex tests run simultaneous one-way iperf clients on separate target ports.
iperf3 `--bidir` chooses roles by accept order and can misassign independently
forwarded connections; prior measurements using it are historical diagnostics.
Every measured duplex receiver stream must carry bytes. `--duplex-http` adds
HTTP connection churn; `canceled_requests` distinguishes workload deadline
cancellation from unexpected forwarding errors. Full steady bulk rates and mixed
request latency are separate acceptance workloads.

ACK receive/emission timestamps also report actual peer ACK scheduling delay.
`peer_ack_delay_ms` is removed from RTT growth used for congestion classification;
`queue_signal_ms` attributes the remaining growth using the forward/reverse
transit estimates. Full RTT still budgets in-flight data and retransmission
timers. Jitter/reorder minima alone cannot trigger indefinite pacing backoff.
Regression floors in the live runner reject catastrophic throughput collapse.

## Live configuration diagnostics

| Event / metric | Meaning |
|---|---|
| `config.detected` | Stable candidate or explicit SIGHUP request reached preparation. |
| `config.applied` | Committed revision, staged `changed_resources`, in-place `updated_resources`, retired and break-before-make `interrupted_resources`. Structural retired endpoints can interrupt streams even without a bind conflict. |
| `config.read_failed` | Missing/unreadable/nonregular/changing/oversized file; old settings remain. Repeated identical read errors are suppressed. |
| `config.rejected` | Validation or resource staging failed; includes phase, current revision and resource retry interval. Validation excerpts are omitted to protect inline secrets. |
| `config.replacing` | Owned occupied bind requires scoped resource interruption before replacement. |
| `config.rollback_failed` | Old resource could not be reconstructed; `/healthz` becomes 503. |
| `config.cleanup_failed` | Old resource's owned-rule removal failed; journal/cleanup ownership is retained. |
| `super_paqet_config_revision` | Startup is revision 1; each successful candidate/manual application increments it. |
| `super_paqet_config_reload_applied_total` | Successful applications after startup. |
| `super_paqet_config_reload_rejected_total` | Rejected validation/resource attempts and distinct read failures. |
| `super_paqet_config_degraded` | 1 after incomplete rollback, cleared by a subsequent successful full reconciliation. |

At log levels above info, successful reload logs are filtered normally; metrics
remain available. `config validate` gives detailed local validation errors.

## Finite production observation

`scripts/production_watch.py` runs independently of the tunnel using Python's
standard library. Its default is ten-second snapshots for 24 hours, with four
rotating 16 MiB log files under `/var/log/super-paqet-watch`. It records process,
file-descriptor, cgroup, host and loopback metric counters. It generates no
customer traffic and never restarts the tunnel. Metrics include configured peer
addresses; configuration credentials and application payloads are not read.

Process changes, unavailable metrics, growing opening/error counters and packet
queue drops trigger bounded journal snapshots. With loopback profiling enabled,
it rotates twelve capture slots containing five-second CPU profiles, aggregated
stacks and bounded detailed goroutine dumps, with at least five minutes between
captures. This retains coverage of later incidents throughout the day rather
than exhausting a lifetime quota early. Slot metadata identifies the captured
PID, UTC time and causes; failed requests cannot retain stale slot artifacts. Profiles contain stack
metadata and require root-only handling. Read `latest.json` for the last sample;
`summary.json` retains full-window resource peaks and within-PID counter deltas
across raw-log rotation and observer restarts, and marks normal completion.
Detailed run history is bounded to 32 processes; global totals survive eviction.
`observer_completed` in `samples.jsonl` also records normal completion. Collection
alone does not notify an operator or automatically fix a fault.

Warning-level connection errors preserve the first five causes and subsequently
one cause per ten seconds, including the cumulative `error_id`. Every error is
still counted. Later incidents therefore remain visible without logging every
failed flow in a burst. `super_paqet_opening_transport_retries_total` counts new
attempts made after an unacknowledged opening timed out; a retry need not succeed.

The deployed liveness release adds per-session
`super_paqet_session_mux_receive_capacity_bytes`,
`super_paqet_session_mux_receive_buffered_bytes` and
`super_paqet_session_mux_receive_blocked`. Debug `transport.sample` includes the
same three values. The blocked gauge denotes payload admission waiting for the
shared application buffer; ordinary packet/socket input waiting is excluded.
The configured buffer can overshoot by one admitted wire frame as before; parsing
control headers while full adds no application payload allocation.

Small-message diagnostics include `transport.sample.small_messages`: true
means the lane uses packet-window control without historical bulk byte pacing.
A new full-size backlog returns it to ordinary bulk pacing before its first ACK.
This classification uses acknowledged and pending sizes, not customer payload
parsing. The benchmark JSON now separates `error_kinds` (request timeout/EOF/reset,
HTTP status, body truncation/length) from expected workload-deadline cancellation;
it never serializes private URLs or body contents.

The ownership candidate adds `super_paqet_session_mux_canceled_writes_total` and
`super_paqet_session_mux_abort_queue_drops_total`, exposed as `mux_canceled_writes`
and `mux_abort_queue_drops` in transport samples. The first counts expired queued
frames actually skipped by the shared sender; an in-flight timeout is not counted
as skipped. The second counts failed new-stream reset notifications rejected by
the bounded queue; local stream ownership is still released immediately. These
are per-session cumulative counters, not application error totals. Their presence
requires the candidate runtime; the older liveness executable lacks them.

Incident captures also retain bounded `tc -s qdisc show`, `ss -s`, kernel netstat
and softnet counters. These help distinguish injection queue drops from NIC,
socket and packet-processing pressure. Capture is read-only and occurs with the
existing five-minute incident cooldown; a missing tool is recorded as diagnostic
failure and does not affect the tunnel. No queue policy is changed.
