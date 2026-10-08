# Recorded production configuration

All five hosts run `enterprise-2026.10.09-latency.1`, runtime source
`276c43a70873670a7e1665283620aba38594b1ab`, SHA-256:
`256cfc65daaa6596ff2b31e2863296a3182975faa71b1a746295cc6172231958`.
These YAML files remain byte-for-byte identical to the accepted live configs;
their migration.4 comments describe configuration provenance. Exact config hashes
and unchanged tunnel unit/drop-in checks are recorded in the
[latest deployment receipts](../latency-deployment-evidence-2026-10-09.json).

Both clients have country peers `germany`, `finland`, `france`, with four slots
per country, separate automatically reserved source ports, negotiated live-session
preservation and **10s stall / 15s retry / 5s probe** budgets. Logical KCP/mux
carriers open lazily as traffic uses their slots. All twelve active ports were
exercised on busy client .118; quiet .14 had two logical carriers active per country
during the latest acceptance. Recovery/restart can rotate ports. Identical reload retains effective reservations.

Backend listeners use two capture workers, matching their two-vCPU hosts.
Germany retains both 116.202.177.233 and 91.107.251.85 listeners; clients use the
latter. France is 171.22.132.226; Finland is 65.109.249.222. Customer forwarding
ports remain 9001/9002/9003 and targets remain the respective server's 2096.
All endpoints retain S outbound / PA return and quoted null encryption.

Manual KCP base 0/30/2/nc=1, 4096-segment ceilings, MTU1350, small-write threshold
256 and adaptation enabled remain synchronized. Adaptive receive buffers,
ACK timestamps and mux credits remain off. IP/interface/MAC values and role
memory limits remain deployment-specific. Logging is warn; profiling is off;
metrics listen only on loopback. Server Xray processes/configurations were not
changed during rollout; recorded process identities stayed unchanged.

`super-paqet.service` remains the shared enabled unit. Finland retains
`65.109.249.222-address.conf` as
`/etc/systemd/system/super-paqet.service.d/20-finland-address.conf` to restore its
assigned secondary IPv4 before binding. No reboot test was performed.

Each host retains a full pre-latency-upgrade binary/config/unit archive and restore
script at `/root/super-paqet/rollback/20261008T214503Z-latency1/`.
The rollout's temporary automatic-rollback timers are inactive. Staged files and
owned standalone client probe binaries were removed; prior incident archives and
observer spools remain available. See the
[deployment report](../LATENCY-DEPLOYMENT-2026-10-09.md) for accepted live checks,
cleanup and qualification limits.

`super-paqet-watch.service` is the independent enabled observer unit renewed on
all five hosts. It retains private metrics and journal copies until **9 October
22:05:41 UTC** under `/var/log/super-paqet-watch/latency1-20261008T220541Z/` without
changing the tunnel configuration or process identity. See the
[latest rollout report](../LATENCY-DEPLOYMENT-2026-10-09.md) for retention, paths,
cleanup and acceptance limits. This unit's fixed deadline is specific to this
window; renew it explicitly for another observation period.
