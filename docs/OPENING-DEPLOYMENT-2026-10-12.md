# Opening/backpressure production promotion, 12 October 2026

All five hosts were promoted backend-first to
`enterprise-2026.10.12-opening.10`, runtime source
`2d0e8d10c13a79a6b71668fc91aa8fb6a94af7b1`, executable SHA-256
`22b2f1ab66b625403048cc52743fb811ece853b2c84c771932c4e66de9091f7c`.
The [deployment receipt](opening-deployment-evidence-2026-10-12.json) contains
per-host identities, validation and authenticated route checks.

The user authorized this tested checkpoint for production observation with the
[recorded local qualification limits](raw-batch-qualification-2026-10-12.json).
It is not a new claim of universally optimal performance or full-capacity VPS
qualification. The short mobile HTTP tail failure remains recorded; the longer
comparison did not reproduce an allocation-edit-specific difference. The 100,000
connection laptop hold used swap. Both rejected pacing experiments remain absent.

Configuration and customer-service unit bytes remain unchanged. Adaptation is
enabled; each country peer has four separate automatic source-port slots,
10-second stall detection and 15-second retry cooldown. Backend capture workers
remain two, client capture workers one, with existing Go worker budgets. The
existing S outbound / PA return behavior, null encryption, recovery grace zero,
warning logging and disabled profiling remain. Existing 9001/9002/9003 forwards
to 2096 and 8001/8002/8003 forwards to 8880 remain in place.

All six Reality routes passed two authenticated 1 MiB downloads after promotion.
Running/disk hashes, active/enabled services, health endpoints, unchanged Xray
process identities and zero automatic restarts were verified. Both client
snapshots had four active distinct source ports for each country.

The first German restart lost its SSH acknowledgment; a fresh connection
confirmed rollback to the previous binary before proceeding. The resumed German
restart also lost its acknowledgment, but fresh inventory confirmed the new
exact binary and healthy service. Failed and resumed attempts are both retained.
Service restarts interrupted existing forwards. Backend warning closures for the
old client carrier ports during client restarts are part of this rollout window;
they should not be misclassified as spontaneous new-version path failures.

Rollback timers were disarmed after the route and service checks. Complete
previous binary/config backups remain beneath
`/root/super-paqet/rollback/20261012T074131Z-opening/` on each host. Rollback order
is clients first, then backends, because new multi-carrier opening clients require
upgraded servers. Do not roll an old server underneath new clients.

The updated independent observer collects warning journals, ten-second metrics,
per-carrier output-loss counters, RTT/window/buffer pressure, source-port/recovery
signals, CPU/memory/FD/cgroup metadata and incident queue/kernel metadata. This
avoids enabling per-packet debug logs or production CPU profiling. Its archive,
fixed seven-day UTC expiry and installed unit are recorded in the receipt.
Samples rotate at 16 × 64 MiB; warning journals rotate at 8 × 8 MiB, with separate
full-window summary peaks and counter deltas. Rotation does not guarantee all raw
samples for the entire week. Existing observation archives remain preserved.

Observer setup caught `journalctl` rejecting the first German unit's ISO-Z
timestamp. The corrected unit uses epoch `@seconds` and actual records were
verified on all hosts. A supplemental old-format diagnostic split quoted dates
on the other hosts; those failed diagnostics are annotated as fixture errors,
not prior observer failures. Failed archives and diagnostic receipts are retained.

The 800x checks returned HTTP 404 responses, proving forwarded HTTP reachability;
they do not qualify an authenticated application session on port 8880.

The final read-only handoff sample verified the same five new PIDs/hashes and
healthy services after observer installation, with 51–55 samples per host and
no automatic restarts. Finland's observer captured three packet-drop-growth
incidents; these remain diagnostic signals, not an incident-free assertion.
The other observers had no incident triggers in this short window. This is
initial observation only, not a completed seven-day stability result.
