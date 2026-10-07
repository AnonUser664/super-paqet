# Production observation — 7–8 October 2026

All five hosts have an enabled, independent `super-paqet-watch.service`, sampling
local metrics and process/host counters every ten seconds. The fresh window ends
at **2026-10-08 19:14:08 UTC** (**22:44:08 Asia/Tehran**). Its fixed UTC deadline
survives observer restarts/reboots; an expired service exits normally. The tunnel
continues running after collection ends, and files remain available for review.
Collection does not send alerts or autonomously repair a problem.

Production stays on `enterprise-2026.10.07-migration.4`, warning logs and profiling
off. Installing the observer did not restart or change the tunnel, its YAML or
Xray processes. All five tunnel PIDs and executable/config hashes were checked
before and after installation. Actual Xray process start identities were checked,
because these hosts do not run Xray under a unit named `xray.service`.

## Data retained on each host

The common root-only directory is:

```
/var/log/super-paqet-watch/migration4-20261007T191408Z/
```

| File | Contents |
|---|---|
| `samples.jsonl`, numbered backups | Ten-second UTC snapshots: live connections, traffic, RTT/transport/queue metrics, source ports, process/FDS, CPU/memory pressure and cgroup counters. |
| `latest.json` | Last complete snapshot; check its timestamp for stalled collection. |
| `summary.json` | Full-window peaks, within-PID failure/recovery deltas, triggers and completion state, preserved across raw-file rotation. |
| `tunnel.journal.jsonl`, numbered backups | Durable copy of service journal messages, including warnings/recovery events and process/boot/cursor metadata. |
| `journal-state.json` | Cursor and last timestamp for resuming journal collection. |
| `window.json`, `installation.json`, `final-check.json` | Window settings and before/after installation receipts. |
| `capture-N.json` | Incident causes and captured artifact metadata, when an incident occurs. |

Samples rotate through sixteen 32 MiB files (512 MiB total). The journal copy
rotates through eight 8 MiB files (64 MiB total). Observed 20–42 KB snapshots
project to roughly 165–346 MiB/day at this traffic/metric cardinality; these are
bounded archives, not unlimited retention. Increased metrics or unusually noisy
warnings can evict earlier raw records. Durable summary totals/peaks survive
rotation. The ordinary service journal is also available through `journalctl`.
Directory permissions are 0700 and file permissions 0600. No packet payloads or
configuration secrets are collected. Raw archives stay on the hosts and outside
Git; the repository contains only compact metadata receipts.

The journal reader catches up from 18:28 UTC, before the accepted migration.4
rollout, then follows new messages. It resumes after the saved cursor. If journald
has vacuumed that cursor, it retries from the saved timestamp; a duplicate final
record is possible and can be deduplicated by `__CURSOR`. Already-vacuumed records
cannot be reconstructed. A failed disk write does not advance the cursor.

## Signals for tomorrow

The observer identifies PID changes, unavailable metrics, growing error/opening
retry/admission/reload/drop counters, packet-driver drops, recovery attempts,
successes/rejections, existing-slot source-port changes and newly suspect/pending
carriers. Counter resets and new slots are distinguished from transitions within
one process. These signals identify where to investigate; they do not prove that
a firewall or censor caused a stall.

Triggered incidents also retain bounded service/kernel journal tails, qdisc and
socket summaries, netstat and softnet metadata, with at least five minutes between
expensive captures. CPU/goroutine profiles require explicitly enabled profiling;
profiling is off for this production window, so none is expected. Resource counters
and transport snapshots allow correlation without enabling debug logs globally.

On each host, start the review with:

```sh
systemctl status super-paqet super-paqet-watch --no-pager
python3 -m json.tool /var/log/super-paqet-watch/migration4-20261007T191408Z/summary.json
python3 -m json.tool /var/log/super-paqet-watch/migration4-20261007T191408Z/latest.json
journalctl -u super-paqet --since '2026-10-07 18:28:00 UTC' --no-pager
journalctl -u super-paqet-watch --since '2026-10-07 19:14:00 UTC' --no-pager
```

Collect the entire window directory, including numbered backups, before changing
settings. Compare UTC outage times with carrier/source-port transitions, pending
bytes/ACK progress/RTT, queue drops, process identity, cgroup memory events and CPU
pressure. Compute traffic rates from successive byte counters within one process.
Check `completed`, `last_utc`, journal-recorder errors and observer journal entries
before treating absence of incidents as evidence of stability. `triggers` counts
observations, not uniquely deduplicated outages. Germany administration still uses
a backend SSH jump. Keep these archives private.

## Verified installation and cleanup

Twelve deterministic observer tests passed: process resets/replay, unavailable metrics, extended raw
rotation, recovery counters/tuples, fixed deadline, partial journal frames, cursor
resume/vacuum fallback, failed writes and journal rotation. The live check verified
increasing samples and journal persistence on all five hosts, with no observer
errors and zero tunnel/observer automatic restarts. At the final check:

| Host | Unchanged tunnel PID | Samples | Peak forwarded connections | Observer memory |
|---|---|---|---|---|
| 116.202.177.233 | 889772 | 33 | 1900 | 13.3 MiB |
| 171.22.132.226 | 3092527 | 33 | 764 | 17.5 MiB |
| 65.109.249.222 | 189268 | 33 | 1000 | 12.8 MiB |
| 89.45.68.14 | 337241 | 33 | 17 | 13.2 MiB |
| 89.45.68.118 | 1023401 | 33 | 3425 | 16.0 MiB |

These are short installation observations, not one-day qualification. Startup
journal catch-up is included in resource use. New tunnel PIDs had no warning events
in the copied journal at this check; summary error/recovery/drop deltas were zero.
The [compact receipts](production-observation-2026-10-07.json) preserve times,
hashes, limits and process identities. An observer-only restart additionally
verified cursor/summary resumption without changing the fixed deadline. The [installed observer unit](deployed/super-paqet-watch.service)
records the exact deadline and archive caps.

The first install check exposed the old observer-only
`10-remaining-duration.conf` override: it redirected the new observer to the old
collection directory. It was archived and removed before the accepted install;
the tunnel was unaffected. Prior observer code/unit/summary and diagnostic restore
files remain under `/var/lib/super-paqet-watch/archive/`. The obsolete diagnostic
restore timer/service were stopped and verified inactive, and their archived
original files were removed from the active installation. A superseded window
containing only installation metadata was archived. Existing incident spools and
full tunnel rollback archives were retained. Deployment staging and owned test
probe files are absent. Locally, two obsolete FreeBSD test executables (~21 MiB)
and task bytecode caches were removed; release binaries, benchmark receipts and
private diagnostic evidence remain available.
