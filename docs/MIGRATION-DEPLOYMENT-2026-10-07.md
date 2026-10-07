# Migration deployment — 7 October 2026

All five hosts run `enterprise-2026.10.07-migration.4` under enabled `super-paqet.service`.
Runtime source: `90e98b1186646c3c56660616309e5fe5573e4208`. SHA-256:
`633750998daa210ca8499ad5081bcb51a775b5c6e713758d709313606f37bd0f`. The running `/proc/PID/exe` image, on-disk binary and
configuration hash were checked on every host. The [deployment receipts](migration-deployment-evidence-2026-10-07.json)
retain actual-host CLI validation, twelve accepted Reality transfers, source
ports, resource samples, cleanup and complete rollback locations.

## Active settings

Both clients use country peers `germany`, `finland`, `france`, with
`sessions: 4`, `max_sessions: 4`, `shared_source: false`, automatic source ports,
`preserve_connections: true`, `stalled_after: 10s`, `retry_interval: 15s` and
`probe_timeout: 5s`. The stall override changes detection, not KCP retransmission
or application/mux timeouts. Source ports remain separate for each of the four
slots; unused logical carriers are created lazily to avoid unnecessary work.

Each backend listener uses two capture workers (previously one), matching the
two-vCPU hosts. Germany retains both listening addresses. Client sockets retain
one capture worker per independent source; their four carriers provide parallel
receive processing per peer. Go scheduling remains unrestricted by extra CPU
quotas or affinity. All endpoints retain S outbound / PA return, quoted null
encryption, manual 30 ms KCP updates, window ceilings 4096, MTU 1350,
small-write flushing 256, endpoint adaptation enabled and the previously
qualified buffer/ACK-extension settings. Logging is warn and profiling is off.

Customer ports and destinations remain:

| Client port | Peer | Raw endpoint | Target |
|---|---|---|---|
| 9001 | germany | 91.107.251.85:29999 | 116.202.177.233:2096 |
| 9002 | finland | 65.109.249.222:29999 | 65.109.249.222:2096 |
| 9003 | france | 171.22.132.226:29999 | 171.22.132.226:2096 |

## Qualification and rollout

The earlier migration.3 build correctly rejected 10 seconds because its config
minimum was 15. Migration.4 changes that one validation bound to 10 seconds;
the default remains 15 seconds. A boundary test checks eligibility just before
and at ten seconds, independently verifying the fifteen-second retry cooldown.
The application race suite and vet passed. The KCP fork tree is unchanged from
[migration.3 qualification](MIGRATION-2026-10-07.md), so its full fork race and
bulk/WAN receipts remain prior-version evidence; they were not relabeled as
measurements of this executable. No retransmission/config-value sweep was done.

The exact migration.4 executable passed three new namespace fixtures at an
80 ms nominal RTT: all four source tuples blocked simultaneously, repeated moves
with unique sequenced 256 KiB payloads, and immediate reblocking of the new
source. All four held connections survived each fixture with zero affected
errors. First source changes completed 10.9–12.0 seconds after injection.
The reblocked source produced exactly one early-stall warning and another move.
Owned namespaces/firewall rules were removed; unrelated sentinel rules survived.

Before activation, all six routes passed one authenticated Reality download of
1 MiB. Staged binary/config pairs then passed `config validate --json` on each
actual host. The three backends were updated first, followed by the clients.
Every host retained a full binary/config/unit rollback archive and a temporary
40-minute rollback timer during qualification. Tunnel restarts disrupted existing
streams during cutover; migration cannot preserve state across process restarts.
Xray process IDs/start identities remained unchanged, and no Xray config was
edited.

After deployment, each route passed two authenticated 1 MiB downloads with HTTP
200 and exact byte counts. More transfers exercised idle slots on the quieter
client before checking its complete pool. An initial deployment metric assertion
expected all idle logical carriers to exist immediately; it was a qualification
assumption, not a tunnel failure. Lazy carriers required traffic before exporting
conversation/source metrics. The final checks verified all twelve distinct ports
on each client:

| Host | Verified source ports or capture workers |
|---|---|
| 116.202.177.233 | 2 listener(s), workers [2, 2] |
| 171.22.132.226 | 1 listener(s), workers [2] |
| 65.109.249.222 | 1 listener(s), workers [2] |
| 89.45.68.14 | germany: 57460, 50988, 47955, 50319; finland: 32965, 64717, 50284, 49130; france: 41951, 57817, 50756, 48981 |
| 89.45.68.118 | germany: 60239, 56714, 52102, 44585; finland: 38262, 45938, 34149, 51757; france: 46450, 43946, 44306, 62425 |

These source ports are observed values, not pinned configuration; recovery or
restart can select others. Both clients now have four independently reserved
source slots per country. A new source keeps the original KCP/mux/target state
when negotiated migration succeeds. The [diagnostics guide](DIAGNOSTICS.md)
explains the bounded early-stall signal; it reports evidence, not a filtering
verdict.

The post-deployment audit found all services enabled/active, zero automatic
restarts, zero application errors/reload rejections/dropped logs and no warning
entries for the new PIDs in the bounded journal read. The busier client had 2,625
established forwarded connections at that sample, with about 123 MiB service
memory. This is observed live activity, not proof of thousands of simultaneously
busy users or an Internet throughput ceiling.

## Acceptance and cleanup

All five temporary rollback timers/services were verified inactive after
acceptance. Stopping a not-yet-loaded transient systemd service returned nonzero;
finalization checks actual inactive state rather than assuming that combined
stop-command status means a deployment failure. Complete archives remain under
`/root/super-paqet/rollback/20261007T182238Z-migration4/`, including `restore.py`.
Prior incident archives/diagnostic spools remain available for investigation.

Staged executables/configs and owned temporary client Xray probe binaries were
removed. The probe only ran standalone client processes and never changed the
backend Xray instances. Existing systemd units, Finland's address-restoration
drop-in, topology and unrelated firewall policy remain as before. The exact
[deployed snapshots](deployed/README.md) describe current settings; the original
[proposal files](proposed-source-ports/README.md) differ only in their provenance
comment. No production filtering fault was injected, no reboot was tested and
no one-day stability result is claimed by this finite rollout.
