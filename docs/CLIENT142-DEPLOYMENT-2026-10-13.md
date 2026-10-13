# Additional client deployment — 13 October 2026

94.139.180.142 now runs the existing **enterprise-2026.10.13-final.1** executable,
runtime source `37904d6`, SHA-256
`df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`.
`super-paqet.service` and `super-paqet-watch.service` are active and enabled;
final verification found zero automatic restarts and healthy loopback metrics.
The four existing hosts were not reconfigured or restarted. No candidate from
another exploration branch was included.

| Country | New client forward | Backend target |
|---|---|---|
| Germany | 9001 / 8001 | 116.202.177.233:443 / :8880 |
| Finland | 9002 / 8002 | 65.109.249.222:443 / :8880 |
| France | 9003 / 8003 | 171.22.132.226:443 / :8880 |

Germany's physical tunnel endpoint remains 91.107.251.85:29999. The other two
use their country IPs on 29999. Binary and config are beneath `/root/super-paqet/`.
The [YAML](deployed/94.139.180.142.yaml) and
[unit](deployed/94.139.180.142.service) match the installed files.

The live .109 profile was cloned with host-specific interface `ens160`, source
IP and gateway MAC. Settings retain four adaptive sessions per country with
independent automatic source ports, S outbound / PA return, null encryption,
MTU1350, manual KCP base 0/30/2/nc=1, 4096-segment ceilings, 10s stall / 15s retry /
5s probe, connection preservation, warning JSON logs and profiling off.
GOMAXPROCS=4 uses the four-vCPU host; one capture worker and a 4096 MiB memory
budget retain the qualified resource policy on its approximately 7.77 GiB RAM.
Carriers activate lazily. All four Germany/France slots and two Finland slots
had distinct source ports at final inspection; four are configured for Finland.

Germany and France Reality and Shadowsocks and Finland Shadowsocks each passed
**two authenticated HTTPS transfers of exactly 1 MiB** after final activation.
Finland Reality failed twice on the new client and on backend loopback using the
same credentials, bypassing the tunnel. The user explicitly accepted that
failure as expected. No Xray configuration was changed to conceal it. This
installation check does not establish new sustained-load capacity.

Two initial deployment-controller attempts stopped only the new client's tunnel
and observer. The first queried metrics before startup completed; activation now
waits up to 45 seconds for readiness. The second treated an independently
restarted Nginx process identity as a rollout failure. The corrected final check
records external application changes separately; **Nginx restarts do not require
a tunnel restart**. The tunnel exited normally in both stopped attempts. Final
activation found unchanged Nginx identities over its checks and no tunnel errors,
opening retries, degraded configuration or dropped logs. Failed and interrupted
receipts remain available privately; their hashes are in the public
[deployment evidence](client142-deployment-evidence-2026-10-13.json).

Warning logs remain available through `journalctl -u super-paqet`. The bounded
observer writes ten-second metrics, full-window summaries and warning journals to
`/var/log/super-paqet-watch/client142-20261013T143745Z/`, with a fixed deadline
**20 October 2026, 14:37:45 UTC**. Fresh samples and journal recording were verified.
The archive retains startup/shutdown samples from the earlier guarded attempts;
its process-specific counters distinguish those from the final run. Samples
rotate within 1 GiB and journals within 64 MiB. No completed seven-day stability
claim is made.

Owned probe executables and empty staging directories were removed. Rollback
scripts and prior observer archives remain for incident review. No Nginx/Xray
service or configuration was modified. Old .14/.118 clients remain stopped.
