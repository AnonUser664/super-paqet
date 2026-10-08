The live reload implementation is now deployed in enterprise-2026.10.06.
See [the final report](FINAL-DEPLOYMENT-REPORT.md) for exact hashes and limits.

# Live configuration reload and validation

The new source implements live reload for `super-paqet run -c PATH` and a
preparation-only validation command. These features are deployed to the four active hosts in the final enterprise
release. Recorded deployment configs are in [deployed/](deployed/README.md).
The existing raw Ethernet/IP/TCP fabrication, capture/injection and inner
KCP/mux framing contracts are retained.

## Validate before applying

```sh
super-paqet config validate -c conf.yaml
super-paqet config validate -c conf.yaml --json
```

Success exits 0 and reports endpoint/forward counts; invalid or missing files
exit nonzero with a detailed local error. `--json` writes one JSON object to
stdout; process errors go to stderr. Successful output never includes prepared
keys or the full config. Existing `run --check -c conf.yaml` remains supported.

Both validation commands use the same strict parser/defaults/discovery as
startup and reload. They check unknown fields, peer references, duplicate
forward/listener strings, resource bounds, reload budgets, network contracts,
cipher/key requirements and KCP/mux constraints. They do not bind ports, create
raw tunnel sockets or install firewall rules. Discovery can emit the existing
ordinary UDP neighbor probe when network overrides are absent. A valid result
does not prove that a port is available or that a remote path/target works.

## Automatic and explicit reload

```yaml
reload:
  enabled: true       # default
  interval: 250ms    # 50ms..1m
  debounce: 500ms    # 0s..1m
```

The watcher reads regular files up to 16 MiB and compares their bytes, supporting
in-place writes, atomic rename and rotated symlinks. It detects same-size edits
with preserved mtimes. It rejects files changing during a read and retries the
read. Stable candidate bytes wait for the debounce period before preparation;
default detection is usually 500–1000ms after a completed write, plus validation
and resource work. Preparation/discovery and iptables latency are additional.

Prefer validating a complete candidate and atomically replacing the live path.
A syntactically valid intermediate file can be applied if it remains stable
longer than debounce; the reader cannot infer the editor's intended final edit.
There is no distributed synchronization between two tunnel instances.

An explicit reload bypasses debounce:

```sh
kill -HUP PID
systemctl reload super-paqet
```

The deployed service unit includes `ExecReload=/bin/kill -HUP $MAINPID`. SIGHUP remains available with
`reload.enabled: false`; after disabling polling, use SIGHUP to apply an edit
that enables it again. An unchanged-file SIGHUP prepares it again, including
`key_env` values in the running process environment and network discovery.
Editing an external environment file does not change an already running
process's environment.

SIGHUP/`systemctl reload` acknowledge the request, not successful application.
Check `config.applied`, `config.rejected` and `super_paqet_config_revision` for
completion. A rejected config is never treated as an applied revision.

## Impact of each edit

| Edit | Application and impact |
|---|---|
| Add peer, listener or TCP/UDP forward | Stage resources first; publish together and activate. Existing unrelated binds/carriers/streams remain. |
| Change TCP forward target or selected peer | Reuse the TCP accept socket. New accepts use the new route; established streams retain their captured target/pool while that pool exists. |
| Change UDP forward target or selected peer | Reuse the UDP bind. New source address/port conversations use the new route; existing sources keep their target until expiry/closure. |
| Remove/move TCP forward | Close its old accept socket. Established streams continue when their peer remains unchanged. |
| Remove/move UDP forward or change UDP to TCP | Close that bind and its UDP source flows; other forwards continue. |
| Remove peer | Close its carriers/streams and release its guards/rules. No obsolete pool is retained indefinitely. |
| Reorder listener/forward lists | Retain resources identified by address/protocol; ordering alone does not reconnect traffic. |
| KCP `mode`, manual `nodelay`, `interval`, `resend`, `nocongestion`, `wdelay`, `acknodelay` | Apply safe mutex-protected setters to established and future carriers of that endpoint; retain conversation, mux and stream state. Presets still override manual values. |
| KCP `write_batch_ms`, `ack_delay_max_ms`, `small_write_flush` | Apply setters in place for that endpoint, preserving streams. The enabled adaptive controller continues its existing adjustments. |
| KCP send/receive windows, MTU, FEC, cipher/key, ACK timestamps, mux credits/buffers/keepalive | Replace only that peer/listener. Existing streams carried by it are interrupted. Wire/mux contracts and queued data make conservative replacement appropriate. |
| KCP `smux_recovery_grace` | Replace the affected peer/listener because its negotiated mux watchdog contract changes. Unrelated peers retain their streams. Default zero preserves ordinary expiry. |
| Endpoint `adaptive`, sessions/max_sessions, packet workers, source address, interface/MAC, flags, packet driver/budgets, remote address | Replace that endpoint and interrupt its carriers; unrelated endpoints remain. Fixed source/bind reuse can require break-before-make. |
| `limits.connections`, `limits.sessions` | Update admission without evicting established work. Lower limits reject new work until usage permits it. Listener worker admission ceilings update in place. Descriptor capacity is raised within the existing hard limit. |
| `limits.memory_mib` | Update the Go soft memory limit immediately; GC pressure can change. Zero disables that soft limit; kernel/socket memory remains additional. |
| Opening/dial/UDP timeout limits | Future operations and subsequent UDP deadline/expiry updates use current durations. An already created context/deadline keeps its earlier value until replaced by its next operation. |
| Logging level/format/sampling/interval | Publish a new formatting handler on the same bounded asynchronous queue; reset observer cadence. Already queued records keep their original formatting. |
| Metrics bind | Stage/replace only local HTTP resources. Data-plane streams remain. |
| Profiling flag | Change pprof availability on the same HTTP bind immediately. |
| Firewall policy | Replace rule-owning peers/listeners to install/remove their own journals/rules. This can affect every carrier when the global policy changes. |
| Reload settings | Take effect after the candidate commits. Disabled polling requires explicit SIGHUP for subsequent edits. |

Equivalent prepared defaults, fresh cipher object identities, key-env indirection
resolving to the same key, or moving the same cipher alias between its supported
locations do not by themselves replace a resource. Actual keys/cipher contracts
remain part of equality; key rotation is a structural endpoint change.

Endpoint removal also cancels its pending opening work and releases half-closed
relays whose opposite TCP writer remains idle. Ordinary directional EOF keeps
that writer open; full carrier closure/cancellation ends it. This uses existing
lifecycle channels without additional per-flow timers or goroutines.

No application bytes are migrated between old and new KCP conversations during
configuration replacement. Opt-in source-port recovery can retain the **same**
conversation and mux through a physical move; it does not make key/MTU/backend
changes or endpoint removal transparent. Changing
`path_recovery.preserve_connections` replaces only that peer pool.
Changing a server listener alone can leave the remote client's old carrier stale
until its configured keepalive/opening recovery expires. Coordinate structural
changes across both ends and verify new target flows. Reliability settings can
be changed independently when their wire contract is unchanged.

## Transaction and failure handling

A transaction first stages new transport resources and local binds without
admitting traffic. Each peer/listener has independent port guards and owned
firewall journals. Atomic runtime publication pairs every route's target and
pool, preventing mixed-generation target/peer selection under concurrent accepts.

Invalid YAML/preparation failures retain the running configuration. They are
logged once per candidate and wait for changed bytes or SIGHUP. Missing/read
errors are retried by polling and identical repeated errors are suppressed.
Daemon validation logs omit YAML excerpts; the validation CLI gives local detail.

A new bind occupied by an unrelated application rejects the candidate without
closing existing resources. Resource failures retry the same candidate every
five seconds or immediately on SIGHUP. Staged socket/guard/rule work is unwound.

Replacing an owned occupied bind requires closing the affected old resources
before rebinding. If replacement fails, the engine reconstructs their previous
settings and republishes the old route view. Streams already interrupted by
that operation cannot be restored. If reconstruction fails, `config.rollback_failed`
identifies it, `/healthz` returns 503, and `super_paqet_config_degraded` becomes 1.
Unrelated resources continue. A later successful full reconciliation clears it.

Failed rule cleanup keeps its private journal and in-process retry ownership;
subsequent transactions and shutdown retry it. Dead-owner journal recovery
covers abnormal process exit. Shared host tables/rules are never flushed.

## Diagnostics and qualification

See [DIAGNOSTICS.md](DIAGNOSTICS.md) for events and counters. Resource identities
in `config.applied` distinguish staged replacements, in-place updates and
retirement. `interrupted_resources` identifies break-before-make; structural
retired resources can also interrupt their streams without a bind conflict.

The reproducible workload is
[scripts/live_reload_netns_test.py](../scripts/live_reload_netns_test.py):

```sh
sudo python scripts/live_reload_netns_test.py \
  --binary build/super-paqet --streams 128 --cycles 12
sudo python scripts/live_reload_netns_test.py \
  --binary build/super-paqet --streams 32 --cycles 6 \
  --delay-ms 20 --reverse-delay-ms 70 \
  --rate-mbit 20 --reverse-rate-mbit 50 --loss 0.3 --reorder 5
```

It keeps tagged payload exchanges on both original peers while editing config,
adds a third peer/listener and TCP/UDP routes, checks invalid edits and temporary
busy binds, SIGHUP, logging/limits, live retransmission changes, scoped structural
replacements, AES/null/driver changes, repeated removal, metrics movement and
same-process operation. Every unaffected stream must retain integrity and make
progress within its existing eight-second application read deadline. Recovery
stalls on loss/reordering are recorded separately from connection failures;
this test does not promise unchanged subsecond latency under every WAN profile.
Owned rules/guards/descriptors and unrelated rules are checked on teardown.

Unit/race checks additionally inject fixed-port replacement and rollback failures,
exercise concurrent route readers and carrier registration, invalid polling
budgets, oversized/FIFO files, same-mtime edits and rotated symlinks. Validation
CLI tests verify errors/nonzero outcomes, JSON results and preparation without
binding an occupied forward port. The current release results are recorded in
[final-deployment-evidence.json](final-deployment-evidence.json).

These are functional reload qualifications. They do not qualify 100,000 busy
customers, guarantee lossless structural replacement, or redeploy the feature.

The exact deployed executable subsequently passed 256-stream asymmetric/loss/
reorder continuity through 18 edit checkpoints. Refer to
[BENCHMARKS.md](BENCHMARKS.md) for the current qualification boundary.
