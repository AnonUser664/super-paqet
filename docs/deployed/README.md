# Recorded production configuration

Five live snapshots were fetched and validated after final warning-level
finalization on 2026-10-06. The live incident release is `enterprise-2026.10.06-recovery`, source
`7b6a7fe642c8175b67fe82af990fdf3e07188709`, binary SHA-256:

`d3ddafe5c2fe056b24b32ef6fb0658e81c81d7c123db19bc494afaf46c42cfdd`

Every endpoint uses the same manual KCP base (0/30/2/nc=1), 4096-segment window
ceilings, MTU1350, batched writes with small-write threshold 256, and adaptation enabled for Germany/Finland. France adaptation is disabled
after the captured 20:37–20:41 UTC stall; see the incident report. On adaptive endpoints, send windows, pacing, ACK timing, reorder allowance and RTO
floors adapt; carrier count is fixed at four. Receive-buffer adaptation and peer
ACK/credit extensions remain explicitly off. All use AF_PACKET/one receive worker.
Host addresses/interfaces/MACs and role memory limits are necessarily distinct.

Peer names are `germany`, `finland`, `france`. 171.22.132.226 is France; historical
Netherlands reports refer to that same IP. Every route uses S outbound / PA
return, quoted null encryption and a shared fixed source tuple per country.
Source ports are 29998/29996/29997 respectively. These baseline snapshots have **warn** logging and profiling off; current
one-day diagnostics temporarily enable sampled debug logs and loopback profiling.
Their guarded restore preserves the France fallback. Metrics are loopback-only. These profiles are deployment-specific and
contain no encryption keys.

`super-paqet.service` is the common base unit. Finland additionally installs
`65.109.249.222-address.conf` as
`/etc/systemd/system/super-paqet.service.d/20-finland-address.conf` to restore its
assigned secondary IPv4 before binding. The startup command was verified; no
reboot qualification was performed.

Two complete recovery archives per host remain under
`/root/super-paqet/rollback/retained/`: current and previous accepted release.
Owned temporary probes and historical backups were cleaned. See
[status](../STATUS.md), [deployment](../DEPLOYMENT.md) and
[qualification limits](../production-deployment-evidence.json).
