# Deployment and real-link recovery: 2026-10-05

The current deployment is a partial recovery. All four small authenticated
Reality paths passed repeated checks, including the public domain from the
laptop. Sustained transfers still stall on three paths; do not describe the
deployment as fully fixed or production-qualified.

| Client port on both clients | Tunnel peer | Destination | Current evidence |
|---|---|---|---|
| 9001 | 116.202.177.233:29999 | 116.202.177.233:2096 | 10/10 small authenticated requests; one of two 1 MiB transfers completed |
| 9003 | 171.22.132.226:29999 | 171.22.132.226:2096 | 10/10 small authenticated requests; both 1 MiB transfers stalled |
| 9002 | 65.109.192.172:2052 | 65.109.192.172:2096 | Previously unavailable; excluded backend and client peer left unchanged |

Clients are 89.45.68.14 and 89.45.68.118. The latest recovery contacted only
the first/third backends and those two clients. Listeners remain on
`0.0.0.0:9001/9002/9003`; no relay fallback was introduced.

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
those three paths. Encryption-disabled mode was restored afterward. Continued
upstream comparison and packet-level diagnosis are required.

## Installed runtime and recovery configuration

On each deployed host:

- Binary: `/root/super-paqet/super-paqet`, mode 0755.
- Config: `/root/super-paqet/conf.yaml`, mode 0600.
- Unit: `/etc/systemd/system/super-paqet.service`, enabled for boot.
- Initial backup: `/root/super-paqet/rollback/20261005T092106Z/`.
- Four-host recovery backup: `/root/super-paqet/rollback/20261005T152826Z/`.

The binary remains the qualified runtime plus explicit no-encryption support
at source commit `1c77c55`. SHA-256:
`ecb8e002173f6f80cd3f403d3ee3080190f0e49889fa2ba9f883b893f96318f3`.
It is Linux/amd64 and linked to target-compatible `libpcap.so.0.8`. No
experimental packet/sequence or opening-timeout code was promoted. Separate
uncommitted runtime files in the main workspace are excluded.

Current first/third endpoints use `enc: 'null'`, no key, explicit physical
interface/IP/gateway MAC, pcap, `fast` KCP, MTU 1350, one carrier per peer,
fixed client source ports 29998/29997 and disabled adaptation, credit hints,
ACK timestamps and adaptive buffers. Windows are client 128/512 and server
1024/1024; smux buffers are 4 MiB/2 MiB. These are recovery settings, not a
qualification of the original high-throughput adaptive goals.

The service restarts on failure, uses LimitNOFILE 524288, and runs firewall
cleanup on exit. Its capability/filesystem restrictions include
`ProtectHome=read-only` so the requested `/root` executable is accessible.
Metrics/liveness bind only to `127.0.0.1:29090`.

```sh
systemctl status super-paqet.service
journalctl -u super-paqet.service -f
curl -fsS http://127.0.0.1:29090/healthz
```

Health success establishes process liveness, not application forwarding.

## Evidence

Ignored `build/` artifacts include:

- `exact-enterprise-reality-results.json` (AES conservative 29999).
- `exact-enterprise-2052-reality-results.json` (port-only comparison).
- `exact-enterprise-null-reality-results.json` (null conservative 29999).
- `exact-enterprise-default-reality-results.json` (adaptive/default failure).
- `port-fix-staged.json` and `port-fix-rollout.json` (validated configs/backups).
- `port-fix-reality-results.json` (20 persistent small-request passes).
- `port-fix-public-reality-results.json` (public domain, both ports pass).
- `port-fix-transfer-results.json` (sustained-transfer failure).
- `port-fix-backend-transfer-controls.json` (direct origin controls).
- `port-fix-aes-transfer-results.json` (AES did not resolve transfer failures).

No credential values are included in committed evidence or this document.

## Roll back the four-host recovery

Run on a selected recovered host if rollback is requested:

```sh
systemctl stop super-paqet.service
TASK_BACKUP=/root/super-paqet/rollback/20261005T152826Z
cp "$TASK_BACKUP/conf.yaml" /root/super-paqet/conf.yaml
systemctl start super-paqet.service
```

The earlier backup also includes the original binary/unit and previous service
state. Recovery changed configuration only; the deployment binary stayed fixed.
