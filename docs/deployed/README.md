# Recorded production configuration

Five baseline snapshots were fetched and validated after the incident liveness
rollout. The live release is `enterprise-2026.10.07-liveness`, source
`323d5dc4b548678ab3e8c6d5f3e6b15ee832588b`, binary SHA-256:

`04508dd5254663042f161c812e001d03e3c8491a2ce37c2034778cf7e2de05bf`

Every endpoint uses the same manual KCP base (0/30/2/nc=1), 4096-segment window
ceilings, MTU1350, batched writes with small-write threshold 256, and adaptation
**enabled**. France's temporary static fallback was retired after deterministic
controller fixes and exact-buffer qualification. Send windows, pacing, ACK timing,
reorder allowance and RTO floors adapt; carrier count is fixed at four. Receive
buffer adaptation and peer ACK/credit extensions remain explicitly off. All use
AF_PACKET/one receive worker. Host addresses/interfaces/MACs and role memory
limits are necessarily distinct.

Peer names are `germany`, `finland`, `france`. 171.22.132.226 is France; historical
Netherlands reports refer to that same IP. Every route uses S outbound / PA
return, quoted null encryption and a shared fixed source tuple per country.
Source ports are 29998/29996/29997 respectively. These baseline snapshots have **warn** logging and profiling off; current
one-day diagnostics temporarily enable sampled debug logs and loopback profiling.
Their guarded restore preserves the revised adaptive settings. Metrics are loopback-only. These profiles are deployment-specific and
contain no encryption keys.

`super-paqet.service` is the common base unit. Finland additionally installs
`65.109.249.222-address.conf` as
`/etc/systemd/system/super-paqet.service.d/20-finland-address.conf` to restore its
assigned secondary IPv4 before binding. The startup command was verified; no
reboot qualification was performed.

Two complete recovery archives per host remain under
`/root/super-paqet/rollback/retained/`: current and previous accepted release.
The current incident rollback backups and diagnostic spools are retained for
review; they have not been pruned during active observation. See
[status](../STATUS.md), [deployment](../DEPLOYMENT.md) and
[qualification limits](../production-deployment-evidence.json).
