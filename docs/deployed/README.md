# Recorded production configuration

Five live snapshots were fetched and validated after final warning-level
finalization on 2026-10-06. All hosts run `enterprise-2026.10.06-startup`, source
`5f403c23e53c58f4917ad2b0c5a480bb96719516`, binary SHA-256:

`e4bbd4915cded878ce3b8038c9f114d58cab8c20e4271c0a73646a892d4c4294`

Every endpoint uses the same manual KCP base (0/30/2/nc=1), 4096-segment window
ceilings, MTU1350, batched writes with small-write threshold 256, and endpoint
adaptation enabled. Send windows, pacing, ACK timing, reorder allowance and RTO
floors adapt; carrier count is fixed at four. Receive-buffer adaptation and peer
ACK/credit extensions remain explicitly off. All use AF_PACKET/one receive worker.
Host addresses/interfaces/MACs and role memory limits are necessarily distinct.

Peer names are `germany`, `finland`, `france`. 171.22.132.226 is France; historical
Netherlands reports refer to that same IP. Every route uses S outbound / PA
return, quoted null encryption and a shared fixed source tuple per country.
Source ports are 29998/29996/29997 respectively. Logging is **warn**, profiling is
off and metrics are loopback-only. These profiles are deployment-specific and
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
