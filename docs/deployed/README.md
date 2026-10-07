# Recorded production configuration

Baseline snapshots preserve warning logging/profiling off. Both clients run
`enterprise-2026.10.07-recovery.1`, source
`efa095aacb2417246c4ae9aa568f03c119d19cf1`, SHA-256
`5718f1c4e87f3e91bb9ca2399d3debdf5c7e4b3f8773e40cc8ebc2b94f757465`.
Their outgoing peers enable verified source-tuple recovery (30s stale threshold,
30s probe interval, 5s probe timeout). The three backends retain ownership.2,
source `fe0151e57d7962783007a7c885962e2f1083641f`, SHA-256
`37743f5acfd9ec8f83c40e4d5a951a3062bf8197f4c9278ca2e7f3fc3dd6c585`.
The wire protocol is compatible; no backend or Xray restart was required.

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
return, quoted null encryption and a shared source tuple per country.
Client .14 starts at 29998/29996/29997; .118 starts at 29998/29994/29995 after
confirmed old-tuple failures. Verified recovery can reserve a different ephemeral
source while retaining flags and destination; metrics expose the effective port. These baseline snapshots have **warn** logging and profiling off; current
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
