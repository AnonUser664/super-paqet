# super-paqet

A Linux port-forwarding tunnel using KCP datagrams, with configurable encryption, inside fabricated
raw TCP packets. It preserves paqet's raw Ethernet/IP/TCP envelope and packet
capture/injection mechanism. It does not establish an outer TCP connection.

One process can accept multiple clients, dial several named peers, and forward
TCP and UDP ports. SOCKS5 and the old role-based configuration have been removed.

The transport contract and its reasons are documented in [docs/TRANSPORT.md](docs/TRANSPORT.md).
Measured results and qualification limits are in [docs/BENCHMARKS.md](docs/BENCHMARKS.md).
This is a substantial transport/runtime rewrite; successful local benchmarks do
not establish universal optimality, WAN behavior, or production readiness on
untested hardware and firewall products.

## Review documentation

Start with [current status](docs/STATUS.md), then the
[architecture](docs/ARCHITECTURE.md), [configuration guide](docs/CONFIGURATION.md),
[deployment guide](docs/DEPLOYMENT.md) and [operations runbook](docs/OPERATIONS.md).
The [documentation map](docs/README.md) links current guides and compact evidence.

Live reload, validation and the incident liveness release are deployed. All six
Germany/Finland/France paths use four adaptive sessions per peer and S outbound /
PA return. After customer traffic failures, opening recovery and mux stream
lifetime bugs were fixed; continued observation exposed further controller and
mux liveness defects, now fixed and redeployed. 24-hour production observation remains
in progress. See the [incident report](docs/PRODUCTION-INCIDENT-2026-10-06.md) and
[current status](docs/STATUS.md). Earlier load/bulk measurements are retained in
[production evidence](docs/production-deployment-evidence.json); they qualify the
earlier executable and do not prove this release is production-ready.

## Build and run

Requires Linux, Go 1.27+, libpcap development headers, `ip` from iproute2, and
iptables/ip6tables. The packet backend uses AF_PACKET, sendmmsg/recvmmsg, and
kernel packet fanout. The pcap backend is available as an explicit fallback.

```sh
# Debian/Ubuntu build dependencies:
sudo apt-get install libpcap-dev iproute2 iptables
make build
./build/super-paqet secret
sudo ./build/super-paqet run -c config.yaml
```

The program needs raw-packet and network-administration privileges. Firewall
rules are installed automatically by default. Configuration is strict: unknown
fields are errors. `run --check -c config.yaml` validates configuration and
network discovery without starting listeners or modifying firewall rules.

## Minimal configuration

Server: use an address assigned to its local Ethernet interface.

```yaml
listeners:
  - address: 192.0.2.20:29999
    key_env: PAQET_KEY
metrics: 127.0.0.1:9090
```

Client: the peer address may be the server's public/NAT address. The target is
resolved and dialed from the server.

```yaml
peers:
  primary:
    address: 203.0.113.20:29999
    key_env: PAQET_KEY
forwards:
  - listen: 127.0.0.1:8080
    peer: primary
    target: 127.0.0.1:80
  - listen: 127.0.0.1:5353
    peer: primary
    target: 1.1.1.1:53
    protocol: udp
metrics: 127.0.0.1:9090
```

Provide the same secret to both endpoints. `key` can be used in place of
`key_env`; keep config/environment files private. Authenticated AES-128-GCM is
the new default. Existing encrypted block modes remain configurable under
`kcp.block`. All clients using one listener share its key and are trusted to
request destinations from that server.

A second entry under `peers` can be selected by any forward's `peer` field.
`listeners`, `peers`, and `forwards` can coexist in one configuration. Several
listeners can use different local addresses or keys. Application traffic uses
KCP only; there is no TCP/UDP transport substitution underneath the raw envelope.

All YAML fields, encryption precedence, preset/manual retransmission behavior
and validation limitations are documented in [CONFIGURATION.md](docs/CONFIGURATION.md).

See [client example](example/client.yaml.example) and
[server example](example/server.yaml.example) for additional settings.

## Automatic discovery and overrides

The interface, local source address, and next-hop MAC are discovered using the
Linux route and neighbor tables. Client tunnel ports are reserved randomly in
the original 32768..65535 range, preventing clashes with ordinary TCP sockets.
An interface must have an Ethernet MAC; use a real NIC or veth rather than `lo`
or a layer-3 TUN device.

On hosts with policy routing, several gateways, VPN default routes, or no usable
default route, provide the necessary network overrides:

```yaml
peers:
  primary:
    address: 203.0.113.20:29999
    key_env: PAQET_KEY
    network:
      interface: eth0
      ipv4:
        addr: 192.0.2.10:0
        router_mac: aa:bb:cc:dd:ee:ff
      tcp:
        local_flag: [PA]
        remote_flag: [PA]
```

IPv6 uses `network.ipv6` with a bracketed address and next-hop MAC. Configuring
both families requires matching ports. Listener network ports must match the
listener's address. A fixed client source port requires one carrier unless `shared_source: true`
enables multiple KCP conversations on that tuple.

## Performance and resource control

KCP send windows adapt from delivered bytes and RTT. Stream receive windows grow
with observed drain rate and transport RTT, subject to configured ceilings.
TCP scratch buffers follow queued bytes and are acquired after readiness, so
idle connections retain no copy buffer. smux transfers received slices directly
to TCP sockets. Linux I/O batches packets, and server receive workers distribute
KCP sessions through kernel hash fanout. Worker count is fixed at startup so
existing flows never move to workers without their session state.

Peers start with up to eight carriers (CPU-derived). `sessions` sets the initial
count and `max_sessions` bounds adaptive growth (default twice the CPU count,
at most 256). Cached traffic/queue pressure keeps new short requests off busy
carriers where capacity permits. Set both counts equal for a fixed pool. Fixed
source ports require both counts 1 unless `shared_source: true`; `adaptive: false` defaults to a fixed pool.

Defaults: up to four server packet workers, KCP window ceilings 32768 segments, stream receive ceiling 16 MiB, and
aggregate smux receive budget 32 MiB per session. These are ceilings, not memory
allocated for each idle connection. FEC is off by default and can be configured
with matched `kcp.dshard` and `kcp.pshard` settings.

Advanced ceilings/overrides remain optional:

```yaml
kcp:
  write_batch_ms: 20       # paced frame duration ceiling, 1..1000ms
  ack_delay_max_ms: 20     # adaptive ACK delay ceiling, 1..20ms
  ack_timestamps: true     # relative forward/reverse queue estimates
  credit_hints: true       # expedited KCP control; reliable fallback retained
  adaptive_buffers: true  # independent override for receive-window adaptation
```

Data frames adapt to the live send window and pacing rate. Control requests have
bounded priority across streams. These mechanisms preserve application ordering
and bound advertised receive storage with the existing stream/session ceilings.

```yaml
limits:
  connections: 200000
  sessions: 1024
  memory_mib: 1024
  open_timeout: 10s
  dial_timeout: 5s
  udp_idle: 60s
```

`memory_mib` is an optional soft Go memory limit; kernel socket memory is outside
it. File descriptor limits are raised within the existing hard limit. Use
systemd/cgroups for an overall process budget. Establishment rate, idle capacity,
active-flow throughput, and tail latency are separate workloads.

For a specific environment, `sessions`, listener `packet_workers`, buffer/window
ceilings, MTU, encryption, FEC, keepalive, and manual KCP parameters can be
configured. `adaptive: false` disables the enterprise pacing/send-window/ACK/reorder/RTO-floor
controller and defaults receive-window adaptation off. Ordinary KCP RTT-based
retries remain; `kcp.adaptive_buffers` can independently override receive adaptation. `network.backend: pcap` requires one packet worker. Very long or
high-bandwidth/high-delay paths may require higher ceilings; qualification
results should determine those values rather than assuming one setting is best.

## Lifecycle and monitoring

Owned firewall chains are scoped to interface, local address, and tunnel port.
SIGINT/SIGTERM clean up those rules and close tunnel resources. Journals under
`/run/super-paqet` record ownership before mutations. Startup recovers dead
owners in the same network namespace, using PID start time and boot identity.
No cleanup runs inside a process after SIGKILL; the systemd service uses
`ExecStopPost` to perform recovery after an abnormal exit.

```sh
sudo ./build/super-paqet firewall-cleanup
./build/super-paqet ping -c config.yaml --peer primary
./build/super-paqet dump -c config.yaml --listener 0
```

Do not persist application-owned `SPQ_*` chains in an iptables snapshot. Normal
exit and recovery preserve unrelated rules. `firewall: false` leaves firewall
management to external automation.

`metrics` binds only to an explicit loopback address. `/metrics` exposes active
connections, admission rejections, failed opens, aborted relays, byte counters,
KCP retransmissions, local send-pipeline and capture/transmit drops, RTT, windows,
heap, and goroutines. Stream-window samples are omitted for large sessions to
avoid scanning all held connections on each scrape.
`/healthz` reports process liveness. `profiling: true` enables local pprof endpoints;
it is off by default. Existing application TCP streams cannot survive a server
process restart; new flows recover through replacement KCP sessions.

The [systemd unit](deploy/super-paqet.service) expects the binary at
`/usr/local/bin/super-paqet`, configuration at `/etc/super-paqet/config.yaml`, and
an optional private environment file at `/etc/super-paqet/environment`. Install
those files and the unit before enabling the service. The repository does not
automatically start a tunnel on the host network.

## Tests

```sh
make vet test
make build bench-build
# The expanded WAN matrix requires the local iperf3 build described in docs/BENCHMARKS.md.
sudo python3 scripts/stress_links.py --binary build/super-paqet \
  --duration 20 --profile --output build/wan-matrix
sudo python3 scripts/systemd_netns_test.py --binary build/super-paqet
sudo python3 scripts/netns_bench.py --enterprise --binary build/super-paqet \
  --functional --restart --capture --duration 5 --sessions 1 --workers 4
sudo python3 scripts/netns_bench.py --enterprise --binary build/super-paqet \
  --hold 100000 --mixed --duration 600 --sessions 8 --workers 64 --host-backlog 65536
sudo python3 scripts/netns_bench.py --enterprise --binary build/super-paqet \
  --rate-mbit 100 --delay-ms 10 --loss 1 --duration 20
```

The harness creates disposable network namespaces and veth links. The explicit
`--host-backlog` option temporarily raises the global Linux receive backlog for
the scale experiment and records/restores its original value. It is not changed
by default. The held-connection test verifies a complete response from every
socket after the interval, and reports failures rather than counting dead sockets.
Resource reports include RSS and swapped memory. Bandwidth,
delay, queue depth, and loss are configurable. It verifies bytes, checks UDP
boundaries and half-close, samples process resources, records the running binary
hash, and checks firewall cleanup and unrelated-rule preservation. CPU profiles
and optional packet captures remain in the selected ignored `build/` directory.
The iperf3 workload expects `build/iperf-local/bin/iperf3`; see the benchmark notes.

## Dependencies and license

MIT license. Derived from [hanselime/paqet](https://github.com/hanselime/paqet).
Local MIT forks of kcp-go v5.6.72 and smux v1.5.53 are in `third_party`; each has
its license and a patch log. Merging upstream changes requires rerunning both
library suites and the impairment tests. Packet capture/BPF compilation also
uses gopacket/libpcap.
