# Complete configuration guide

This reference describes the current committed source and the final deployed
enterprise release, including live reload, validation, shared-source KCP lanes
and small-write flushing. The older `1c77c55` snapshots are history. See
[STATUS.md](STATUS.md), [FINAL-DEPLOYMENT-REPORT.md](FINAL-DEPLOYMENT-REPORT.md) and
[LIVE-RELOAD.md](LIVE-RELOAD.md) for deployment and qualification limits.

The old `role`, `server`, `listen`, `transport`, `forward` and SOCKS configuration
is not accepted by this engine. It uses strict YAML: unknown fields are errors.
An instance can listen, connect to several peers and forward TCP/UDP together.
`run -c PATH` watches that file automatically. Valid edits reconcile only affected
resources; safe KCP reliability edits apply in place. See [LIVE-RELOAD.md](LIVE-RELOAD.md)
for the impact of every category of edit. Restarting a process still ends its
established application streams.

## Start with a complete pair

The documentation IP ranges below are placeholders. Replace the server listener
with an address assigned to its Ethernet interface. All clients can connect to
that listener's same port. The public peer address may differ from the server's
local address behind NAT; that network path still needs actual verification.

Server:

```yaml
listeners:
  - address: 192.0.2.20:29999
    key_env: PAQET_KEY
metrics: 127.0.0.1:9090
log:
  level: info
  format: json
```

Client:

```yaml
peers:
  germany:
    address: 192.0.2.20:29999
    key_env: PAQET_KEY
  second:
    address: 192.0.2.30:29999
    key_env: PAQET_SECOND_KEY
forwards:
  - listen: 127.0.0.1:9001
    peer: germany
    target: 127.0.0.1:2096
  - listen: 127.0.0.1:9003
    peer: second
    target: 127.0.0.1:2096
  - listen: 127.0.0.1:5353
    peer: germany
    target: 1.1.1.1:53
    protocol: udp
metrics: 127.0.0.1:9090
```

Supply each key to its corresponding listener and peer. A target's `127.0.0.1`
means the accepting tunnel server, not the forwarding client. Target hostnames
are sent as names and resolved when the server dials them. Peer/listener
addresses are resolved during configuration loading/discovery; there is no
continuous endpoint DNS refresh.

Bind a forward to `0.0.0.0:9001` to accept connections on all local IPv4
interfaces. Its exposure is independent of the raw tunnel port. Use
`[::]:9001` for an IPv6 bind and verify the host's dual-stack socket behavior.
TCP and UDP can use the same numerical forward port; duplicate listen strings
within the same protocol are rejected.

## Top-level fields

| Field | Type / default | Meaning |
|---|---|---|
| `listeners` | List, empty | Incoming raw-KCP endpoints. At least one listener or peer is required. |
| `peers` | Mapping, empty | Named outgoing endpoints; forward entries select these names. |
| `forwards` | List, empty | Local TCP/UDP binds and remote targets. |
| `limits` | Object | Admission, memory and timeout limits below. |
| `firewall` | Boolean, `true` | Own scoped firewall rules and recover dead-owner journals. |
| `metrics` | String, empty | Optional HTTP metrics/liveness bind; requires an explicit loopback IP. |
| `profiling` | Boolean, `false` | Add local pprof routes to the metrics HTTP server. Needs `metrics` to be useful. |
| `log` | Object | Structured logging and sampling below. |
| `reload` | Object | Automatic content polling/debounce; settings below. |

Every forward supports exactly `listen`, `peer`, `target`, and `protocol`.
`protocol` defaults to `tcp` and accepts `tcp` or `udp`. `target` needs a
nonempty host and port 1–65535. `peer` must name an existing peer. UDP preserves
datagram boundaries, including zero-length datagrams, up to 65,507 payload
bytes. It carries them through a reliable KCP/smux stream; loss recovery and
head-of-line delay therefore apply to UDP traffic too.

## Live reload settings

| Field | Default | Meaning / constraint |
|---|---|---|
| `reload.enabled` | `true` | Automatically watch the `run -c` file. `false` leaves explicit SIGHUP reload available. |
| `reload.interval` | `250ms` | File-content poll interval; `50ms`–`1m`. |
| `reload.debounce` | `500ms` | Require unchanged candidate bytes for this duration; `0s`–`1m`. SIGHUP bypasses the grace. |

```yaml
reload:
  enabled: true
  interval: 250ms
  debounce: 500ms
```

The watcher supports in-place edits, atomic replacement and symlink rotation.
Use atomic replacement for a complete edit; debounce cannot distinguish a
valid intermediate document from your intended final document. Files must be
regular files of at most 16 MiB. Invalid candidates retain running settings.
Temporary resource failures retry every five seconds. Disabling automatic
reload requires SIGHUP or a restart to enable it again; see the full
[reload and impact guide](LIVE-RELOAD.md).

## Listener and peer fields

Both use the same endpoint object. Some fields only affect outgoing peers.

| Field | Default | Meaning / constraint |
|---|---|---|
| `address` | Required | Concrete, resolvable endpoint IP and nonzero port. A listener uses a local assigned address. |
| `enc` | `aes-128-gcm` after precedence | Endpoint alias for encryption mode; see encryption below. |
| `key` | Empty | Inline shared secret; required for encrypted modes. |
| `key_env` | Empty | Read shared secret from this environment variable. Mutually exclusive with `key`. |
| `adaptive` | `true` | Enable the enterprise send-window/pacing/ACK/reordering/RTO-floor controller; receive adaptation follows this unless independently overridden. |
| `sessions` | `min(8, max(2, GOMAXPROCS))` | Initial outgoing KCP carrier count, 1–256. Not the customer TCP connection count. |
| `max_sessions` | `min(256, max(sessions, 2 × GOMAXPROCS))` | Maximum outgoing carrier pool. Must be at least `sessions`. Defaults to `sessions` for fixed source ports or `adaptive: false`. |
| `source_ports` | Optional list, empty | Peer-only ordered distinct ports, 1–65535, at most 256. Carrier i reserves entry i. Requires port 0 in the network address. Initial and maximum session counts cannot exceed the list. With a list, initial defaults to `min(list length, 8, max(2, GOMAXPROCS))`, maximum to list length. |
| `shared_source` | Boolean, `false` | Both listener and peer opt into independent KCP conversations on one peer source tuple. Permits multiple `sessions` with a fixed source port. Uses one physical receive socket/encoder/reservation per peer pool, distinct KCP/mux queues and schedules per lane. Requires FEC disabled and no `source_ports` list. Changing it replaces that endpoint. |
| `path_recovery` | Disabled | Optional verified source-tuple recovery for outgoing peers; details below. |
| `packet_workers` | Listener `min(4, GOMAXPROCS)` for `packet`; otherwise 1 | Incoming packet workers, 1–64. A peer cannot use more than one; use multiple `sessions` for outgoing parallelism. Pcap requires 1. |
| `network` | Discovery plus defaults | Physical interface, source address, next-hop MAC, driver and outer flags. |
| `kcp` | Defaults below | Reliability, windows, packet size, encryption aliases and mux ceilings. |

### Verified source-tuple recovery

```yaml
shared_source: false
sessions: 4
max_sessions: 4
path_recovery:
  enabled: true
  stalled_after: 15s
  retry_interval: 15s
  probe_timeout: 5s
# Put port 0 in network.ipv4.addr/network.ipv6.addr for automatic per-carrier ports.
```

Recovery is peer-only and disabled by default. The default stall/retry budgets
are **15 seconds**; explicit older `30s` values remain respected. Source rotation
addresses tuple-specific failures, while recreating a conversation on the same
blocked tuple cannot. It does not establish where packets are dropped.

With `shared_source: false`, each carrier owns a distinct reserved client source
port, raw socket, KCP/mux session and firewall journal. A one-second sampler
checks each carrier independently. Either of these conditions permits recovery:

- At least three transport opening attempt failures since its last acknowledged
  outbound progress or successful opening, plus `stalled_after` without that
  progress. Inbound-only keepalives cannot conceal a broken outbound half-path.
- Established streams with pending outbound data and no ACK advancement for
  `stalled_after`. Empty queues, no active streams and a remote zero receive
  window do not qualify through this branch. An inbound-only half-path cannot
  reset this directional delivery clock.

Target rejections/dial failures are not transport evidence. Suspect carriers are
avoided for new opens when a nonsuspect sibling exists; an all-suspect pool keeps
a fallback so the original path can resume. No timers, scan or packet callback
are added per customer connection. Idle healthy sessions do not rotate.

A live carrier advertising a zero receive window suppresses both recovery
branches and source-port suspicion, including when opening deadlines expire.
Failed-opening evidence is retained: after the window reopens, ACK progress
clears it, or a continuing stall can qualify. A closed conversation has no live
receiver window and can again qualify through opening failures.

A candidate reserves a fresh available source in 32768–65535 and installs only
its own firewall rules. It must complete PPING/PPONG through KCP/mux within
`probe_timeout` (1s–1m); local setup/write is not proof. Probes run outside the
configuration lock, at most one per carrier and four concurrently per process.
`retry_interval` (10s–10m) is a per-carrier minimum between attempt start times.
`stalled_after` accepts 15s–10m. These budgets are not a strict outage SLA.

Failed probes retain the existing slot and streams. Before a successful commit,
configuration/resource/slot identity, cancellation and old-carrier health are
rechecked. If the old carrier resumes or configuration changes, the candidate
is discarded. Otherwise only that slot is switched; its stalled streams close
and applications reconnect. Healthy sibling streams, other peers and listener
bindings retain their ownership. There is no cross-session byte striping or
transparent TCP migration. The published pool still contains at most
`max_sessions`; up to four unpublished probe carriers temporarily add sockets.

Existing shared-source configurations remain supported. Their whole-peer
opening-failure/no-progress detector and replacement scope remain unchanged:
any sibling's acknowledged/received progress protects the pool, one probe per
peer, and a verified switch retires that peer's entire pool.

Flags, packet fields, encryption, backend port and reliability settings remain
preserved; only the client source port changes. Effective ports appear in
`super_paqet_peer_source_port` and `path.recovered`. Identical config reloads and
log edits retain recovered ports. Recovery does not edit YAML. A restart uses
`source_ports`/a fixed configured port if supplied, or chooses fresh automatic
ports for port-zero configuration. An explicit `source_ports` list specifies
**initial** reservations; enabled recovery can select ports outside that list.
Do not enable rotation with remote ACLs restricted to initial source ports.
Recovery settings/source-layout changes replace only the affected peer pool.

`GOMAXPROCS` means the effective Go runtime CPU parallelism, rather than a
promise to occupy every logical CPU continuously. Listener client-carrier
admission is controlled by `limits.sessions`; `sessions`/`max_sessions` are
outgoing pool settings. A fixed source port requires **both counts to be 1**:

```yaml
peers:
  fixed:
    address: 192.0.2.20:29999
    enc: 'null'
    sessions: 1
    max_sessions: 1
    network:
      interface: eth0
      ipv4:
        addr: 192.0.2.10:29998
        router_mac: aa:bb:cc:dd:ee:ff
```

The engine reserves local kernel TCP listeners as port guards, then blocks
kernel input for those tunnel ports. These guards do not carry tunnel data or
establish an outer TCP connection. Automatically selected source ports are
32768–65535. A separately configured fixed port can be outside that range.

## Encryption and exact precedence

The selected mode is, in order:

1. `kcp.block`, if nonempty.
2. Endpoint `enc`, if nonempty.
3. `kcp.enc`, if nonempty.
4. Enterprise default `aes-128-gcm`.

Conflicting aliases are not rejected: the earlier one wins. Use one spelling
per endpoint to avoid ambiguous review. `kcp.key` is rejected; keys belong at
endpoint `key` or `key_env`. An empty/missing environment key fails encrypted
configuration. Key variables must exist in the **service's** environment,
not merely in an interactive shell.

Supported modes are `aes`, `aes-128`, `aes-128-gcm`, `aes-192`, `salsa20`,
`blowfish`, `twofish`, `cast5`, `3des`, `tea`, `xtea`, `xor`, `sm4`, `none`,
and `null`. Support does not mean all modes offer equal protection. AES-GCM is
the authenticated default. Legacy `aes` uses the library's legacy cipher path.
Keys are derived using PBKDF2-SHA256, salt `paqet`, 100,000 iterations, 32 bytes;
the mode selects the needed key length.

| Mode | Behavior |
|---|---|
| `null` | No cipher and no nonce/CRC crypto envelope. No key required. |
| `none` | No payload encryption, but retains the library's nonce/CRC envelope. No key required. |
| Encrypted modes | Matching mode/key required at both ends; crypto overhead reduces usable KCP payload. |

Explicit no-encryption syntax is **`enc: 'null'`**. Plain YAML `null` is a null
value, not the string mode name, and can fall back to the encrypted default.
`none` and `null` are not wire-equivalent. Application Reality/TLS encryption
is independent of tunnel encryption.

A listener's encrypted clients share its key and are trusted to request remote
destinations. There is currently no per-customer identity/ACL/destination
allowlist configuration. Null mode provides no tunnel-level authentication;
application authentication does not authenticate the tunnel control messages.
These limitations are relevant to the final feature review.

## Network fields and discovery

| Field | Default | Meaning |
|---|---|---|
| `network.backend` | `packet` | `packet`: Linux AF_PACKET batch I/O/fanout. `pcap`: capture/injection fallback. |
| `network.interface` | Discovered | Ethernet interface name, at most 15 characters, with a 6-byte MAC. |
| `network.ipv4.addr` | Discovered | Local `IP:port`; peer port 0 requests automatic reservation. |
| `network.ipv4.router_mac` | Discovered | Ethernet next-hop MAC, not the distant server's MAC on a routed path. |
| `network.ipv6.addr` | Discovered for IPv6 endpoint | Local `[IPv6]:port`. |
| `network.ipv6.router_mac` | Discovered | IPv6 next-hop MAC from NDP; can differ from the IPv4 next hop. |
| `network.pcap.sockbuf` | Client 4 MiB / server 8 MiB | Packet socket/capture buffer request in bytes, 1024–104857600. Used by both drivers. |
| `network.tcp.local_flag` | `[PA]` | Local outer flag cycle, up to 64 combinations. |
| `network.tcp.remote_flag` | `[PA]` | Requested outer flag cycle for the remote side, up to 64 combinations. |
| `network.guid` | Empty | Legacy capture field; not required for this Linux deployment. |

Flags use uppercase `F S R P A U E C N` for FIN/SYN/RST/PSH/ACK/URG/ECE/CWR/NS.
Each string combines flags; list entries form a cycle. Changing flags changes
observable raw packet behavior and needs deployment testing. It does not turn
the carrier into normal TCP. Keep `[PA]` when reproducing the known baseline.

The current Netherlands deployment uses client `[S]` / requested return `[PA]`.
It passed the [recorded path qualification](NETHERLANDS-DIAGNOSIS.md); this is a
path-specific profile, not a new generic default. SYN also selects the encoder’s
original SYN sequence/options branch. Flags are static choices: the application
does not rotate profiles automatically in response to loss. Structural flag edits
replace the affected endpoint; coordinate both ends and verify full transfers.

Discovery uses `ip route get` for peers and the first applicable default route
for listeners. It fills missing interface, source and gateway MAC. It may emit
one ordinary UDP neighbor-discovery probe to port 9; application tunnel traffic
still uses raw TCP. Discovery is a startup operation, not continuous route/MAC
tracking. There is no guarantee it selects the desired physical path through
WARP, policy routing, secondary addresses or several gateways.

When necessary, override all three values explicitly:

```yaml
network:
  backend: packet
  interface: eth0
  ipv4:
    addr: 192.0.2.10:0
    router_mac: aa:bb:cc:dd:ee:ff
  tcp:
    local_flag: [PA]
    remote_flag: [PA]
```

IPv6 example:

```yaml
network:
  interface: eth0
  ipv6:
    addr: '[2001:db8::10]:0'
    router_mac: aa:bb:cc:dd:ee:ff
```

A listener's local network port must equal its `address` port. Configuring both
families requires matching local ports. An unspecified source address is not
accepted. Loopback and layer-3 TUN devices lack the required Ethernet MAC.
Discovery success and syntax validation do not establish NAT/firewall delivery.

## All KCP and multiplexing fields

These defaults are **enterprise endpoint defaults**, not the older role-based
paqet defaults. Recovery configurations intentionally override many of them.

| Field under `kcp` | Default | Meaning / units |
|---|---|---|
| `mode` | `fast3` | `normal`, `fast`, `fast2`, `fast3`, `manual`. Presets override the manual knobs below. |
| `nodelay` | Manual integer 0 | Manual only: 0 normal timeout growth, 1 faster growth/floor behavior. |
| `interval` | Manual integer 0; KCP clamps to 10 ms | Manual only: update interval, intended 10–5000 ms. Not the RTO or a fixed retry interval. |
| `resend` | Manual integer 0; presets 2 | Manual fast-retransmit evidence threshold, not number of retries. |
| `nocongestion` | Manual integer 0; presets 1 | 0 native KCP congestion window enabled; 1 disabled. Enterprise adaptation is a separate controller. |
| `wdelay` | Manual `false`; preset-specific | Allow batching writes until KCP update. |
| `acknodelay` | Manual `false`; preset-specific | Immediate ACK behavior; adaptive bounded ACK scheduling also applies when enabled. |
| `mtu` | `min(1350, interface MTU − 80)` | Maximum KCP transport datagram size before outer IP/TCP headers. 50–1500, and must leave 80 bytes within the NIC MTU. Crypto/FEC overhead also consumes this size. |
| `sndwnd` | 32768 | Send-window ceiling in **segments**, 1–32768. Actual send window adapts when enabled. |
| `rcvwnd` | 32768 | Receive-window setting in segments, 1–32768. Not TCP's outer window. |
| `dshard` | 0 | FEC data shards; zero with parity zero means disabled. |
| `pshard` | 0 | FEC parity shards; use a supported matched pair at both ends. Adds wire bandwidth and CPU. |
| `block` | Selected encryption mode | Highest-priority encryption alias. |
| `enc` | Empty | Lower-priority encryption alias described above. |
| `key` | Rejected | Use endpoint `key`/`key_env`. |
| `smuxbuf` | 33554432 | Aggregate smux receive budget per carrier, bytes. |
| `streambuf` | 16777216 | Per-stream advertised receive-window ceiling, bytes. |
| `smuxkalive` | 2 | Keepalive interval, **integer seconds**. |
| `smuxktimeout` | 30 | Idle/no-inbound-traffic keepalive timeout, integer seconds. |
| `small_write_flush` | 0 | Immediate-flush exception for logical KCP writes at or below this byte threshold, 0–65535. Keeps the selected preset's bulk batching and ACK policy. Zero disables it. May be reloaded in place. |
| `write_batch_ms` | 20 | Paced data-frame batching-time ceiling, effective 1–1000 ms. |
| `ack_delay_max_ms` | 20 | Adaptive ACK delay ceiling, effective 1–20 ms. |
| `ack_timestamps` | `true` | Optional inner ACK timestamps for peer scheduling/directional queue estimates. |
| `credit_hints` | `true` | Optional expedited inner WINS stream credit; reliable smux UPD fallback remains. |
| `adaptive_buffers` | Follows endpoint `adaptive` | Independent boolean override for stream receive-window adaptation. |

Buffer ceilings are allocated on demand, not per idle application connection.
`streambuf` must not exceed `smuxbuf`; both must be at least 1024 bytes, and the
mux implementation requires each to fit a signed 32-bit size. Keepalive timeout
must be at least the interval. Some lower-level checks run when the carrier is
created; `--check` does not exercise every runtime/FEC/mux validation path.
FEC shard and manual numeric parameters do not currently have complete
front-end validation. A value parsing successfully is not proof it is safe.

Neither arbitrary-path MTU discovery nor automatic FEC shard selection is
implemented. The default MTU follows the **local NIC**, not the whole path.
The deployed Netherlands MTU 128 is an empirical recovery override, not a
universal recommendation. Approximate payload capacity is segment count ×
usable MSS. Reducing packet size without scaling windows reduces byte capacity;
scaling packet windows also increases metadata and packet-processing cost.

## Retransmission behavior and manual overrides

| Mode | `nodelay` | `interval` ms | Fast threshold `resend` | `nocongestion` | `wdelay` | `acknodelay` |
|---|---:|---:|---:|---:|---|---|
| `normal` | 0 | 40 | 2 | 1 | true | false |
| `fast` | 0 | 30 | 2 | 1 | true | false |
| `fast2` | 1 | 20 | 2 | 1 | false | true |
| `fast3` | 1 | 10 | 2 | 1 | false | true |
| `manual` | configured | configured | configured | configured | configured | configured |

In this implementation even the `normal` preset disables native KCP congestion
control. With `adaptive: false`, the enterprise pacing controller is also
inactive; no congestion-control guarantee should be inferred from the name.
The presets are inherited mappings, not proven optimal policies for all links.

For an explicit reproduction of `fast` with a different fast threshold:

```yaml
kcp:
  mode: manual
  nodelay: 0
  interval: 30
  resend: 3
  nocongestion: 1
  wdelay: true
  acknodelay: false
```

This is a parameter demonstration, not a recommended universal profile.
Setting `resend: 3` under `mode: fast` does **not** override that preset's 2.
The original controls remain supported; shorter examples omit them because
presets provide their values.

KCP retries through three paths: evidence-based fast retransmission, early
retransmission for a remaining gap when no new segments are queued, and
per-segment timeout retransmission. `resend: 0` disables the threshold-based
fast path, but the implementation's independent early-gap path and timeout
retries still exist. It does not disable reliability or all retries.

The base timeout estimator is smoothed RTT plus `max(update interval, 4 × RTT
variation)`, bounded by its floor and 60 seconds. It starts at 200 ms. Mode 0
has a base 100 ms floor; no-delay mode has a 30 ms floor. Each timed-out segment
adds the current estimated RTO to its retry delay, or half of it in no-delay
mode. Gap-based retries are guarded against repeatedly firing before the timer.
The internal dead-link marker is set at 20 transmissions; it is not an exposed
configuration knob or a documented hard user-stream retry limit. There is no
currently exposed maximum retransmission count/elapsed-time policy.

When endpoint adaptation is enabled, new bulk carriers learn on a cadence
bounded to 50–250 ms, using the observed RTT. Idle, static and established
controllers retain the 250 ms update cadence. A shared 50 ms ticker dispatches
those updates; controllers and delivery history are allocated per carrier, not
per forwarded customer connection. The controller:

- Adjusts pacing and the send window from delivery, queue signals and loss.
- Uses packet-window control for tiny acknowledged/pending messages without
  historical bulk byte pacing; a new full-size backlog returns to bulk pacing.
  This avoids treating queued control packets as saturated bulk delivery.
- Adjusts ACK delay within the configured ceiling.
- Applies a 0–50 ms allowance before gap-based retransmission to tolerate reorder.
- Adjusts the RTO floor to approximately `max(30 ms, 2 × SRTT + 4 × RTT variation)`.

Timeout retries remain active independently of reorder allowance. The fast
threshold remains static; an adaptive loss-versus-reorder threshold is not yet
implemented. `adaptive: false` disables these controller actions while ordinary
KCP RTT estimation/retries continue. The earlier physical recovery profile used
that override; the final four-session deployment enables endpoint adaptation.
A fixed `sessions: 4` / `max_sessions: 4` keeps carrier count bounded while send
windows, pacing, ACK scheduling, reorder allowance and RTO floors adapt.
Receive-buffer adaptation remains independently configurable.
No current policy is established as optimal for every bottleneck, loss pattern,
reordering level or delay. These are final-review decisions, not completed claims.

## Admission, memory and timers

| Field under `limits` | Default | Meaning |
|---|---|---|
| `connections` | 200000 | Active handled-flow/stream admission limit (brief control streams also count). It is not preallocation or a throughput guarantee. |
| `sessions` | 1024 | Accepted server carrier admission limit; outgoing pools have separate endpoint limits. |
| `memory_mib` | 0 | Optional soft Go runtime memory limit in MiB; zero leaves existing runtime/environment behavior. Excludes kernel socket buffers and other processes. |
| `open_timeout` | `10s` | Overall opening deadline; positive duration. The ownership release includes SYN submission in this budget. |
| `dial_timeout` | `5s` | Remote target dial deadline; positive and less than `open_timeout`. |
| `udp_idle` | `60s` | UDP flow inactivity/read deadlines; positive duration. |

Duration fields accept Go duration strings such as `250ms`, `10s`, `2m`.
The engine requests roughly `2 × connections + 4096` file descriptors within
its current hard limit; the deployed service's limit is 524288. Failure to reach
that capacity is logged. A Go soft limit is not an RSS/cgroup/OOM boundary.
Kernel buffers, sockets, Xray, the OS, targets and generators need their own
budgets. Socket buffers and flow ceilings remain explicit configuration; no
live CPU-budget/RSS feedback controller is implemented.

The deployed ownership release bounds SYN submission with the remaining carrier
receipt budget and locally aborts failed new openings without another control
close wait. Ordinary established-stream close retains its 30-second mux control
timeout; this change does not make every established write/end-to-end operation
subject to `open_timeout`.

There is no five-second post-half-close TCP read deadline. New-opening receipt
recovery is bounded separately from the target dial, as described in
[ARCHITECTURE.md](ARCHITECTURE.md). There is no
configurable TCP idle lifetime or half-close grace deadline in the current
schema. Healthy one-direction EOF permits data in the other direction. Process
restart/crash ends established application streams; new streams can reconnect.

## Logs, metrics and profiling

| Field under `log` | Default | Meaning |
|---|---|---|
| `level` | `info` | Named levels `debug`, `info`, `warn`, `error`. |
| `format` | `json` | `json` or `text`. |
| `interval` | Debug 1s / other 10s | Summary/sample interval, at least 100 ms. |
| `flow_sample` | 1000 | Sample one lifecycle in every N debug flow IDs. Zero selects the default, not logging disabled. |

```yaml
metrics: 127.0.0.1:9090
profiling: true
log:
  level: debug
  format: json
  interval: 1s
  flow_sample: 1000
```

`flow_sample: 1` is useful for a small reproduction, expensive during large
churn. The asynchronous log queue holds 1024 entries and counts dropped/failed
output. It drains for up to two seconds on shutdown. Neither queue size nor
shutdown drain duration is configurable. See [DIAGNOSTICS.md](DIAGNOSTICS.md).

`/healthz` reports process liveness and returns 503 after an incomplete reload
rollback; it does not prove forwarding or target authentication. `/metrics`
exposes process/flow/KCP counters; per-conversation labels can create substantial
monitoring cardinality. Large-session stream-window scans are skipped above 64
streams. Profiling endpoints exist only when enabled and need an HTTP metrics
bind. Packet `dump` prints raw payloads; in null mode that includes plaintext
control metadata. Captures and config/key files require private handling.

## Validate and operate

```sh
./build/super-paqet version
./build/super-paqet config validate -c config.yaml
./build/super-paqet config validate -c config.yaml --json
# Existing equivalent command:
./build/super-paqet run --check -c config.yaml
sudo ./build/super-paqet run -c config.yaml
sudo ./build/super-paqet ping -c config.yaml --peer germany
sudo ./build/super-paqet dump -c config.yaml --listener 0
sudo ./build/super-paqet firewall-cleanup
```

`--check` does not start listeners or install firewall rules. It **can** resolve
DNS, inspect routes/neighbors and emit the short discovery probe. A fixed port
conflict can still appear during startup or reload resource staging. `ping` opens its own temporary peer
connection; a live service reserving the same fixed source port can conflict.
A successful ping proves control delivery, not a large authenticated transfer.

Firewall rules exempt owned raw packets from conntrack, suppress kernel RSTs
and keep kernel TCP from handling tunnel input. Each instance journals owned
`SPQ_*` chains in `/run/super-paqet` before mutation. Cleanup touches only its
recorded chains. PID start time, boot identity and network namespace distinguish
owners. SIGTERM/SIGINT clean up; SIGKILL/power loss requires later recovery.
`firewall: false` requires equivalent external management. There is no hot
firewall/NAT/MTU autodetection or guarantee every provider accepts the envelope.

Use [OPERATIONS.md](OPERATIONS.md) for the two installation layouts, rollback,
monitoring and reproducible checks. Use [deployed/](deployed/README.md) for the
recorded deployment configs; they are deployment-specific snapshots, not defaults.
