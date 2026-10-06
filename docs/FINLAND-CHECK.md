# Finland replacement-IP checks, 2026-10-06

**65.109.249.222 is deployed and working on both clients with the Netherlands
profile.** Port 9002 now uses raw endpoint `65.109.249.222:29999`, with the
application target `65.109.249.222:2096`. This address is a secondary IPv4 on the
same host as the earlier failed 65.109.211.233; the assigned primary address was
preserved. [Evidence](FINLAND-CHECK-EVIDENCE.json).

## Current deployed profile

Finland was upgraded from the older enterprise executable to the same current
release as Germany, Netherlands and both clients, SHA-256:

`47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`

The selected settings are client S / backend PA flags, quoted null cipher,
MTU1350, fast KCP, four shared carriers per client, AF_PACKET and one backend
receive worker. Client windows are 131/522 and backend windows 1044/1044;
timestamps, credit hints and adaptive buffers remain disabled. Clients use fixed
source port 29996, leaving Germany's 29998 and Netherlands' 29997 unchanged.
Backend source is explicitly 65.109.249.222 on eth0, using the verified physical
gateway 172.31.1.1 / d2:74:7f:6e:37:e3. All existing Germany/Netherlands endpoint
objects were retained during the client live reloads.

No application runtime source change was required. The existing current binary,
configuration validation, live reload and owned firewall rules are deployed with
persistent enabled systemd service. Binary/config remain under `/root/super-paqet`.

## Acceptance

| Check | Result |
|---|---|
| Exact checked echoes, 64 bytes / 16 KiB / 256 KiB | 6/6 across both clients. |
| Full authenticated 10 MiB Reality/Vision downloads | 4/4, HTTP 200 and exact bytes, 2.14–2.43 s, no retries. |
| Connection churn, 64 workers/client | 1024/1024 checked 64 KiB echoes, zero failures. |
| Continuous two-minute echo soak | All sixteen retained streams passed without reconnect/retry. |
| Post-cleanup port 9002 | 4/4 authenticated 1 MiB downloads across both clients. |
| Germany/Netherlands regression | 4/4 full 1 MiB downloads, one per route per client. |
| External laptop → domain:9002 and each client IP:9002 | 3/3 full authenticated 1 MiB downloads after final cleanup. |
| Final service/config audit | Active/enabled, current running hash, valid 0600 config, no degraded reload on all three modified hosts. |

Client `89.45.68.14`: 307.4 MiB checked in each direction during the continuous soak; 21.46 Mbit/s per direction under this finite workload. Churn p99 1.833 s. These are not maximum-throughput measurements or qualification of thousands of simultaneously busy Reality customers.

Client `89.45.68.118`: 429.8 MiB checked in each direction during the continuous soak; 30.00 Mbit/s per direction under this finite workload. Churn p99 1.906 s. These are not maximum-throughput measurements or qualification of thousands of simultaneously busy Reality customers.

## Address persistence, cleanup and rollback

The new IPv4 was assigned in the kernel but was absent from the checked boot
network configuration files. An owned service drop-in restores that secondary
address before the tunnel binds:

```ini
[Service]
ExecStartPre=/usr/sbin/ip -4 address replace 65.109.249.222/32 dev eth0
```

The drop-in is installed at
`/etc/systemd/system/super-paqet.service.d/20-finland-address.conf`, and its exact
snapshot is [recorded](deployed/65.109.249.222-address.conf). The idempotent command
was verified in a transient unit with the production service's capability,
filesystem and address-family constraints. No reboot was performed, and no
working carrier was restarted to add the drop-in. The primary address and
existing routes remain unchanged. Future address changes must update both the
application configuration and this explicitly configured startup drop-in.

Original binaries/configs/units are backed up under `/root/super-paqet/rollback/20261006T091909Z-finland-nl/` on the modified hosts. Rollback timers stayed armed through qualification and were disarmed afterward. Temporary echo services, loopback forwards, test files and processes were removed. Live config hashes match the [recorded snapshots](deployed/README.md).

The earlier failure evidence below remains useful: the Netherlands profile failed on 65.109.211.233 but passes on the new assigned address. This supports destination/path-selective behavior; it does not identify an intervening policy or prove permanent filtering of the old address.

# Earlier failed address: 65.109.211.233

The qualified Netherlands profile **did not work** on `65.109.211.233` from either
client in this bounded check. Four authenticated 1 MiB Reality requests failed
at the eight-second TLS deadline. Six independent echo checks (64 bytes, 16 KiB
and 256 KiB per client) also failed. No 10 MiB success or capacity qualification
is claimed. [Evidence](FINLAND-CHECK-EVIDENCE.json).

## Configuration correction

The replacement host still tried to bind `65.109.192.172:2052`, which was no
longer assigned. Its systemd service repeatedly exited with `cannot assign
requested address`. Both clients also retained the old raw endpoint and target.

Only those IP references were corrected: backend listen/source becomes
`65.109.211.233:2052`; client backend2 becomes that endpoint, and port 9002 targets
`65.109.211.233:2096`. Exact originals were backed up under each modified host's
`/root/super-paqet/rollback/*-finland-ip/`. The corrected service is active and
enabled; the existing 9002 profile still failed four 1 MiB requests. Germany and
Netherlands endpoint settings were preserved.

Finland's production executable remains the older enterprise build, SHA-256
`ecb8e002173f6f80cd3f403d3ee3080190f0e49889fa2ba9f883b893f96318f3`.
The old excluded IP was not contacted.

## Matched Netherlands-profile test

Because that executable lacks shared-source carriers, an isolated systemd
listener used the exact current `47c61ac2…` binary on port 29999. The Netherlands
profile was copied with its existing S outbound / PA return flags, null cipher,
MTU1350, four shared carriers, fast KCP, client windows 131/522 and server
1044/1044, and timing/credit/adaptive-buffer extensions disabled. Backend source,
interface and gateway MAC were changed to the verified new host values. Client
test sources were 29996; test forwards were loopback-only on 28082/28084. This
was a genuine matched-profile control, not an old/current shared-source mismatch.
The temporary listener's health endpoint passed before testing.

Paired physical-interface captures (`ens160` / `eth0`, full 1600-byte snapshot)
showed:

| Direction | Sent at client | Matching backend arrival |
|---|---|---|
| .14 → Finland | 252 | 0 |
| .118 → Finland | 322 | 0 |

The backend capture saw no frames in the selected tunnel tuples. Every capture
reported zero kernel drops. The separate checked echoes rule out attributing
this failure solely to VLESS/Reality credentials or proxy egress.

## Reachability controls and limits

Both clients established ordinary TCP connections to Finland ports 22 and 2096,
and received the SSH banner. Each received one of three ICMP replies (~90/95 ms);
that short probe cannot distinguish ICMP rate limiting from packet loss. A
backend-local Reality request returned HTTP 200 and exactly 1 MiB in 0.188 s.

This excludes a complete IP outage from the clients and supports selective
failure of this raw TCP profile/tuple before the backend capture. It does not
identify a filtering device, prove all ports/flags fail, or establish that the
new IP is permanently blocked. A broader port/flag study was not performed in
this check.

Temporary listeners, binaries, echo services, captures and loopback forwards
were removed from the hosts; exact saved client configuration bytes were
restored. Only the requested replacement-IP corrections remain. The current
Netherlands SYN profile was **not** promoted to Finland's production 9002 route.
The superseded configuration is retained in Git history and remote rollback
backups; current snapshots below use 65.109.249.222.

Final regression requests through ports 9001 and 9003 returned HTTP 200 and
exactly 1 MiB from both clients (4/4). All three modified hosts were active/enabled;
owned test services and rollback timers were inactive, temporary files absent,
and live config hashes matched the recorded snapshots.
