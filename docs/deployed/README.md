# Recorded production configuration

All five hosts run `enterprise-2026.10.07-migration.4`, runtime source
`90e98b1186646c3c56660616309e5fe5573e4208`, SHA-256:
`633750998daa210ca8499ad5081bcb51a775b5c6e713758d709313606f37bd0f`.
These YAML files were read back from accepted hosts and their exact hashes are in
[deployment receipts](../migration-deployment-evidence-2026-10-07.json).

Both clients have country peers `germany`, `finland`, `france`, with four slots
per country, separate automatically reserved source ports, negotiated live-session
preservation and **10s stall / 15s retry / 5s probe** budgets. Logical KCP/mux
carriers open lazily as traffic uses their slots; twelve distinct active source
ports per client were verified during acceptance. Recovery/restart can rotate
ports. Identical reload retains effective reservations.

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

Each host retains a full pre-migration binary/config/unit archive and restore
script at `/root/super-paqet/rollback/20261007T182238Z-migration4/`.
The rollout's temporary automatic-rollback timers are inactive. Staged files and
owned standalone client probe binaries were removed; prior incident archives and
observer spools remain available. See the
[deployment report](../MIGRATION-DEPLOYMENT-2026-10-07.md) for accepted live checks,
cleanup and qualification limits.
