# Deployment

Five hosts run the current enterprise release under enabled systemd services.
The excluded 65.109.192.172 backend is not contacted. Netherlands and the new
Finland address are working with the qualified directional SYN profile;
see [Netherlands](NETHERLANDS-DIAGNOSIS.md) and [Finland](FINLAND-CHECK.md).

## Topology

| Hosts | Role | Tunnel or forwarding ports |
|---|---|---|
| 116.202.177.233, primary alias 91.107.251.85 | Germany backend | Both local addresses listen on 29999; target 2096. |
| 171.22.132.226 | Netherlands backend | Tunnel 29999; target 2096. |
| 65.109.249.222 | Finland backend, current binary | Tunnel 29999; target 2096. |
| 89.45.68.14 and 89.45.68.118 | Multi-peer clients | Public TCP 9001/9002/9003 through the respective backend; all pass. |

Both clients use Germany's 91.107.251.85 endpoint. Source ports are 29998 for
Germany and 29997 for Netherlands. There is no relay through another backend.
Port 9002 uses Finland's 65.109.249.222 endpoint, with client source 29996.
All three routes passed authenticated post-cleanup checks on both clients.
The old 65.109.192.172 backend remains excluded. Finland's earlier 65.109.211.233
address is still assigned as its primary IP but is not a tunnel endpoint.

## Installed files

| File | Path | Mode |
|---|---|---|
| Binary | `/root/super-paqet/super-paqet` | 0755 |
| Configuration | `/root/super-paqet/conf.yaml` | 0600 |
| Service | `/etc/systemd/system/super-paqet.service` | 0644 |

The [recorded configs and unit](deployed/README.md) are deployment-specific;
use the [configuration guide](CONFIGURATION.md) for defaults and field meanings.
The release binary requires target-compatible libpcap.so.0.8/glibc. Its version
and hash are in [STATUS.md](STATUS.md).

## Selected settings

Germany has eight fixed shared carriers per client, pcap, MTU1350 and manual
30 ms updates with immediate writes. Netherlands and Finland each have four shared carriers per
client, AF_PACKET with one backend receive worker, MTU1350 and fast-mode batching.
Their flags are client S / backend PA, with windows 131/522 on clients and
1044/1044 on the backend. Germany flags remain PA. All use null encryption,
with timing/credit extensions and adaptive buffers disabled. Fast mode overrides stored manual fields.

Backends have two vCPUs/~4 GiB; clients four vCPUs/~8 GiB. Soft Go memory budgets
are 1,536 and 4,096 MiB respectively. The service uses LimitNOFILE 524288,
TasksMax 65536, automatic restart on failure, SIGHUP reload and owned firewall
cleanup on exit. `ProtectHome=read-only` permits the requested /root layout.
No CPU quota or affinity restriction was added.

## Validation, monitoring and rollback

```sh
/root/super-paqet/super-paqet config validate --json -c /root/super-paqet/conf.yaml
systemctl status super-paqet.service
journalctl -u super-paqet.service -f
curl -fsS http://127.0.0.1:29090/healthz
```

Validate a staged config before atomic replacement. Automatic polling or
`systemctl reload super-paqet` reconciles it; check `config.applied` and revision
metrics. See [LIVE-RELOAD.md](LIVE-RELOAD.md) for affected-stream behavior.

Remote backups remain beneath `/root/super-paqet/rollback/`; the final binary
rollout checkpoint is `20261006T002600Z-final`. A full binary rollback requires
stopping the service, restoring binary/config/unit, daemon-reloading, restarting
and verifying authenticated application traffic. Detailed procedures are in
[OPERATIONS.md](OPERATIONS.md). Repository cleanup did not modify deployed hosts.

Finland also installs the owned `20-finland-address.conf` service drop-in to
restore its secondary IPv4 before binding after restart/boot. Its idempotent
startup command was verified without reboot; update it when changing that address.
[Snapshot and validation boundary](FINLAND-CHECK.md).
