# Emergency replacement client

Historical 12 October deployment record. The 13 October release supersedes this
binary; [current status](STATUS.md) records the active deployment and dependencies.

`94.139.180.109` now runs the existing production executable
**enterprise-2026.10.12-opening.10**, runtime source `2d0e8d1`, SHA-256
`22b2f1ab66b625403048cc52743fb811ece853b2c84c771932c4e66de9091f7c`.
The experimental private-output-socket candidate was not deployed.

At the user's request, `super-paqet` was stopped on both `89.45.68.14` and
`89.45.68.118`, reached through the replacement client. Both acknowledged
`MainPID=0`, `ActiveState=inactive`, `SubState=dead`. Their units remain enabled
because this was a temporary stop; reboot or an explicit start can activate them.
No old-client Xray or nginx service was stopped. Backend tunnel services were
not restarted or reconfigured.

The new client has four CPUs and approximately 7.76 GiB RAM. It retains four
independently reserved source-port slots per country, adaptive transport control,
one capture worker, four Go execution workers, a 4096 MiB application memory
limit, S outbound / PA return, null tunnel encryption, 10s stall / 15s retry /
5s probe, warning logs, loopback metrics and profiling off. Its detected interface
is `ens160` and gateway `94.139.180.1`; configuration uses the observed gateway
MAC. Binary/config live under `/root/super-paqet`; libpcap is provided under its
`lib/` directory. The enabled systemd unit retains the existing capability,
resource-limit and firewall-cleanup policy.

| Client port | Peer | Backend target |
| --- | --- | --- |
| 9001 | germany | 116.202.177.233:443 |
| 9002 | finland | 65.109.249.222:443 |
| 9003 | france | 171.22.132.226:443 |
| 8001 | germany | 116.202.177.233:8880 |
| 8002 | finland | 65.109.249.222:8880 |
| 8003 | france | 171.22.132.226:8880 |

Germany's physical carrier endpoint remains `91.107.251.85:29999`; Finland
and France use their listed IPs on port 29999. The client's new source address
requires no backend listener change.

Initial authenticated probes failed because the inherited configuration still
targeted Reality port 2096. The user then confirmed Reality moved to 443 and
Shadowsocks now runs on 8880. The corrected configuration passed CLI validation
and live reload preserved tunnel PID 246452. All three Reality and all three
Shadowsocks routes subsequently completed authenticated 1 MiB HTTPS downloads
with HTTP 200 and exact response size. The existing nginx/Xray process identities
on the new client were unchanged; no customer-proxy service configuration was
edited. These finite checks establish delivery, not sustained maximum capacity
or protection against an upstream IP-wide block.

The existing nginx entry points 10001/10002/10003 also passed authenticated
Shadowsocks downloads. A final external check from the laptop against
`ss.cloudnet1.ir` on those three ports completed three exact 1 MiB downloads
with HTTP 200. DNS resolved to `94.139.180.109` from both the new client and
the laptop. No nginx change or DNS record update was performed.

The first plain HTTP checks against 8880 closed without an HTTP response;
that is not a Shadowsocks failure criterion. SS2022 validation uses the supplied
credentials in memory and pipes JSON to the isolated Xray process's stdin;
no SS password is placed in files, command arguments or public receipts. See
[Xray's Shadowsocks configuration](https://xtls.github.io/en/config/outbounds/shadowsocks.html)
and [stdin configuration support](https://xtls.github.io/document/command).

The enabled bounded observer collects ten-second metrics and warning journals
until **19 October 2026, 14:30:12 UTC** beneath
`/var/log/super-paqet-watch/emergency109-20261012T142945Z/`. A real sample and
active collector were verified. Temporary probe binaries/configs were removed;
the original configuration and service backup remain under `rollback/`.

The [deployment receipt](emergency-client109-deployment-2026-10-12.json)
preserves the failed first probes, successful correction, resource/identity
checks and old-client stop acknowledgements. Current configuration and the
new-client-specific systemd unit from this checkpoint are retained as
[configuration](archive/production-exploration-2026-10-12/94.139.180.109.yaml)
and [unit](archive/production-exploration-2026-10-12/94.139.180.109.service).
