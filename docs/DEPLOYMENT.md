# Deployment history and current release

The current deployment is documented in [FINAL-DEPLOYMENT-REPORT.md](FINAL-DEPLOYMENT-REPORT.md)
and fresh [deployed-final snapshots](deployed-final/README.md). The following
2026-10-05 recovery record is retained as history; it describes the earlier
executable/configs, not today's running version.

# Deployment and real-link recovery: 2026-10-05

At the recorded recovery checkpoint, the four requested surviving paths passed
repeated sustained authenticated Reality tests. The final check completed eight 10 MiB transfers (two per path)
and the public domain completed 1 MiB on both ports. The user subsequently
confirmed successful use with one active user.
Thousands of active customers have not been tested here. This is a functional
recovery; real-WAN enterprise scale and full adaptive settings remain unqualified.

| Client | Port | Tunnel endpoint | TCP destination |
|---|---|---|---|
| 89.45.68.14 | 9001 | 91.107.251.85:29999 (same German host) | 116.202.177.233:2096 |
| 89.45.68.118 | 9001 | 116.202.177.233:29999 | 116.202.177.233:2096 |
| Both clients | 9003 | 171.22.132.226:29999 | 171.22.132.226:2096 |
| Both clients | 9002 | 65.109.192.172:2052 | Excluded backend, previously unavailable; unchanged |

The German service listens on both its primary 91.107.251.85 and secondary
116.202.177.233 address on the same tunnel port. No extra backend or relay was
introduced. Client listeners remain `0.0.0.0:9001/9002/9003`, and Reality
credentials/application destinations did not change.

## What the controlled comparisons establish

The saved upstream configuration used AES, `fast` mode, MTU 1350, destination
port 29999 and a fixed client source port. Unmodified upstream was rebuilt from
`b9fa0bd93bf93b2eff27197742562ed82201f16e`. Authenticated VLESS/Reality/Vision
requests were used instead of treating a raw TCP/TLS connection as a working
proxy. Upstream passed both German paths in the initial small-request control;
the other backend was intermittent across upstream/fork controls.

The installed enterprise binary passed all four small authenticated paths on
29999 using the conservative profile with AES. Changing only the tunnel port
to 2052 caused all four requests to time out. Returning to 29999 and changing
only encryption to `null` passed all four again in 0.696–0.889 seconds.
Restoring the adaptive defaults on 29999 also caused all four to time out.
These comparisons establish a working small-request profile; they do not
identify a specific DPI/filtering mechanism or a single adaptive-setting cause.

The conservative profile was deployed with a fresh backup on each of the four
hosts. Twenty small authenticated requests through the persistent 9001/9003
ports passed (five per path). From the laptop, both `deir.cloudnet1.ir:9001`
and `:9003` also completed authenticated HTTP 200 requests.

A subsequent 1 MiB download exposed sustained-transfer failures on three paths.
Both backend Xray instances completed the same download directly in
0.476/0.517 seconds. Temporarily repeating the tunnel test with AES did not fix
those three paths. Encryption-disabled mode was restored afterward. The unmodified upstream
control reproduced the same three failures; its
one successful 1 MiB transfer was 89.45.68.118 to the German backend, in
1.265 seconds. This failure is shared under the present link conditions.

Paired physical-interface captures show retransmitted initial client KCP
packets with no matching packets in either backend capture. The configured
gateway MACs match current neighbor entries. Fresh source ports 30098/30097
did not restore delivery; restoring 29998/29997 restored all four small
authenticated requests. IPv6 controls also timed out. These observations do
not identify the exact intervening filtering mechanism. These captures bounded that failing profile. Later address/packet-size recovery
is recorded below; a specific filtering-device cause is still unresolved.

## Recovery and tuning outcome

The original upstream path the user had verified was client .118 to Germany.
It remained the one path that completed the initial 1 MiB bulk control in both
versions. Client .14 also completed the bulk request when the tunnel used the
same German host's primary IP, so its peer now uses that address.

Both Netherlands paths completed 1 MiB with MTU 128. A 512-byte profile passed
10 MiB for .118 but failed for .14. The shared Netherlands listener therefore
uses MTU 128. Window limits were scaled to restore the byte capacity lost when
reducing the packet size, then the existing AF_PACKET backend reduced measured
CPU/memory relative to pcap. No wire-sequence prototype was promoted.

Final two-request timings for each 10 MiB authenticated transfer:

| Path | Time range |
|---|---|
| .14 to Germany | 2.66–2.80 seconds |
| .118 to Germany | 2.76–2.95 seconds |
| .14 to Netherlands | 5.46–5.98 seconds |
| .118 to Netherlands | 5.74–6.13 seconds |

Two concurrent Netherlands 10 MiB transfers in the resource comparison used
approximately 0.27 CPU cores averaged on the backend and 0.31–0.32 on each
client over the sampling interval. Lifetime peak RSS since the service restart
was 96.5 MiB on that backend and 45.8–46.0 MiB on the clients. Sampling includes
setup/SSH time; these are observed process totals, not a capacity claim or
measurements of 100,000 connections. Final throughput is tens of Mbit/s on these
paths, not multi-gigabit production qualification.

## Installed runtime and configuration

Binary remains `/root/super-paqet/super-paqet`, mode 0755; configuration remains
`/root/super-paqet/conf.yaml`, mode 0600. The enabled persistent unit is
`/etc/systemd/system/super-paqet.service`. At this checkpoint the installed
binary is still source `1c77c55`, SHA-256
`ecb8e002173f6f80cd3f403d3ee3080190f0e49889fa2ba9f883b893f96318f3`.
It is Linux/amd64 and linked to target-compatible libpcap.so.0.8.

All surviving endpoints use `enc: 'null'`, no key, explicit physical
interface/IP/gateway MAC, `fast` KCP, one carrier per peer, fixed client
source ports 29998/29997, and disabled adaptation/credit hints/ACK timestamps/
adaptive buffers. German endpoints use pcap, MTU 1350, client windows 128/512
and server windows 1024/1024. Netherlands endpoints use AF_PACKET with one
worker, MTU 128, client windows 1664/6656 and server windows 13312/13312.
Smux buffers remain 4 MiB/2 MiB. These are tested recovery settings.

Backups include the initial `20261005T092106Z`, complete four-host recovery
`20261005T162157Z`, and later three-host backend tuning `20261005T164636Z`,
under `/root/super-paqet/rollback/`. Each deployment stages and checks configs
before replacement. Later backups preserve earlier intermediate settings.

The service restarts on failure, uses LimitNOFILE 524288, and runs firewall
cleanup on exit. `ProtectHome=read-only` permits the requested /root executable.
Metrics/liveness bind to `127.0.0.1:29090`.

```sh
systemctl status super-paqet.service
journalctl -u super-paqet.service -f
curl -fsS http://127.0.0.1:29090/healthz
```

## Committed code fixes awaiting deployment

The source-only sequence experiment is committed separately at `4c7aa6a`,
branch `experiment/wan-sequence-tracking`, and remains excluded from deployment.
It tested per-peer byte sequences and pcap receive feedback, and passed three
initial 1 MiB paths at MTU 1350. Ordinary race suites and vet passed, but that
alone does not qualify the changed outer sequence behavior.

The subsequent mixed-client null/pcap test at 100 Mbit/s and 80 ms RTT failed
in both the existing runtime and the experiment. Detailed logging identified
`send: No buffer space available` being propagated as a fatal transport error,
which aborted the server stream and left the client waiting for its tail.
The narrow transmit-pressure and absent-chain fixes are now committed at
`20227d3` after the capped-link regression passed, but have not been redeployed.
The patch preserves the packet encoding and lets KCP retransmit a dropped datagram;
permanent injection errors still propagate. Startup recovery also exposed a
stale-journal issue when an owned nftables chain was already absent.

## Evidence and limits

Sanitized comparisons are in
[deployment-recovery-evidence.json](deployment-recovery-evidence.json).
Ignored build artifacts include the `exact-*-reality-results.json` controls,
`recovered-paths-final-results.json`, `recovered-paths-public-results.json`,
`recovered-paths-final-manifest.json`, and resource/tuning results. No
credential values are included in the committed evidence.

The exact intervening filtering mechanism is not identified. Port/address/
packet-size sensitivity was demonstrated empirically. Local earlier scale
qualification does not qualify this recovery profile or arbitrary real networks.
Wire/timeout edits in the main workspace remain uncommitted and excluded from
all deployed binaries. Temporary probe cleanup/final service audit is pending
the next user decision; no fresh final remote audit is claimed.

## Review pause and code patch evidence

The `20227d3` queue-pressure fix passed the test that previously failed: both
16 MiB transfers verified exact bytes, UDP/half-close passed, HTTP/bulk had zero
unexpected errors, all processes exited zero and owned rules were removed.
[queue-pressure-evidence.json](queue-pressure-evidence.json) records the profile,
resources, cleanup and collected check status. This is a local regression,
not a new remote customer-scale qualification.

The release staging script was stopped at its password prompt before connecting
to the hosts; no code-fix staging or rollout manifest exists. Current source,
unit/config paths and deployed executable hash above remain the last recorded
state. The final remote temporary-file/rule audit remains deferred. User review
of these docs and the final feature decision comes before further deployment.

Exact recovery YAML/unit snapshots are linked in [deployed/README.md](deployed/README.md).
