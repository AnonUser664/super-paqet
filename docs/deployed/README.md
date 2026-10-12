# Recorded production configuration

All five hosts run **enterprise-2026.10.12-opening.10**, runtime source
`2d0e8d10c13a79a6b71668fc91aa8fb6a94af7b1`, executable SHA-256
`22b2f1ab66b625403048cc52743fb811ece853b2c84c771932c4e66de9091f7c`.
These YAML files remain byte-for-byte identical to the live configurations
verified at the [12 October promotion](../OPENING-DEPLOYMENT-2026-10-12.md);
the [receipt](../opening-deployment-evidence-2026-10-12.json) records hashes,
service identities and authenticated route checks. Configuration comments from
earlier releases describe provenance rather than the current executable.

Both clients have country peers `germany`, `finland`, `france`, with four slots
per country, separate automatically reserved source ports, negotiated live-session
preservation and **10s stall / 15s retry / 5s probe** budgets. All four distinct
active source ports per country were verified on both clients after promotion.
Carriers activate lazily; recovery and restart can rotate ports. Identical reload
retains effective reservations.

Backend listeners use two capture workers, matching their two-vCPU hosts; clients
retain one capture worker and their existing Go worker budgets. Germany retains
both 116.202.177.233 and 91.107.251.85 listeners; clients use the latter. France is
171.22.132.226; Finland is 65.109.249.222. Customer ports 9001/9002/9003 forward
to the respective backend's 2096 target. TCP 8001/8002/8003 bind on all IPv4
interfaces and target Germany/Finland/France port 8880 respectively. The latter
checks returned HTTP 404 responses, proving HTTP reachability; they are not an
authenticated proxy-session qualification for port 8880. Both authenticated
1 MiB Reality downloads passed on each 900x route.

All endpoints retain **S outbound / PA return**, quoted null encryption, manual
KCP base 0/30/2/nc=1, 4096-segment ceilings, MTU1350, small-write threshold 256
and adaptation enabled. Recovery grace remains zero. Adaptive receive buffers,
ACK timestamps and mux credits remain off. IP/interface/MAC values and role
memory limits remain deployment-specific. Logging is warn; profiling is off;
metrics listen only on loopback. Backend Xray identities and tunnel unit bytes
remained unchanged during promotion.

`super-paqet.service` is enabled on all five hosts. Finland retains
`65.109.249.222-address.conf` as
`/etc/systemd/system/super-paqet.service.d/20-finland-address.conf` to restore its
assigned secondary IPv4 before binding. No reboot test was performed.

Each host retains the previous binary/config and restore script at
`/root/super-paqet/rollback/20261012T074131Z-opening/`; earlier rollback archives
also remain. Rollback order is clients first, then backends. Temporary rollback
timers were disarmed, and owned staged/probe files were removed.

`super-paqet-watch.service` is enabled on all hosts and collects ten-second
metrics plus warning journals until **19 October 2026, 08:01:28 UTC**, beneath
`/var/log/super-paqet-watch/opening10-20261012T080128Z/`. Journal records and
metric samples were verified without changing tunnel PIDs or configuration.
Sample files rotate within 1 GiB and warning journals within 64 MiB, with
full-window summary peaks and counter deltas. Existing observer archives remain.
The fixed deadline survives observer restart/reboot; create a new dated window
to extend collection after expiry. See [diagnostics](../DIAGNOSTICS.md).

Local performance and capacity limits remain in the
[exact-executable qualification](../raw-batch-qualification-2026-10-12.json).
This finite deployment check does not establish sustained customer capacity or
universal lossy-link latency equivalence.
