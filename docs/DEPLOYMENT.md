# Deployment

Four hosts run the current enterprise release under enabled systemd services.
The replacement Finland backend also has an enabled service but retains an
older enterprise binary and a failing tunnel path; [details](FINLAND-CHECK.md). The
excluded 65.109.192.172 backend is not part of active deployment work. Netherlands
forwarding is restored with a qualified directional SYN profile; see
[NETHERLANDS-DIAGNOSIS.md](NETHERLANDS-DIAGNOSIS.md).

## Topology

| Hosts | Role | Tunnel or forwarding ports |
|---|---|---|
| 116.202.177.233, primary alias 91.107.251.85 | Germany backend | Both local addresses listen on 29999; target 2096. |
| 171.22.132.226 | Netherlands backend | Tunnel 29999; target 2096. |
| 65.109.211.233 | Replacement Finland backend, older binary | Tunnel 2052; target 2096; path test failed. |
| 89.45.68.14 and 89.45.68.118 | Multi-peer clients | Public TCP 9001/9002/9003 through the respective backend; 9002 fails. |

Both clients use Germany's 91.107.251.85 endpoint. Source ports are 29998 for
Germany and 29997 for Netherlands. There is no relay through another backend.
Port 9002 now uses the replacement Finland host 65.109.211.233:2052, targeting
65.109.211.233:2096. Corrected IP references do not establish a working path;
[the Finland check](FINLAND-CHECK.md) failed on both clients. The old backend
65.109.192.172 remains excluded and was not contacted.

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
30 ms updates with immediate writes. Netherlands has four shared carriers per
client, AF_PACKET with one backend receive worker, MTU1350 and fast-mode batching.
Netherlands flags are client S / backend PA, with windows 131/522 on clients and
1044/1044 on the backend. Germany flags remain PA. Both use null encryption,
with timing/credit extensions and adaptive
buffers disabled. Fast mode overrides stored manual fields.

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
