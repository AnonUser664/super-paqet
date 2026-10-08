# Latency release rollout — 9 October 2026 (Tehran)

Deployment completed and accepted on all five hosts. This report records
actual-host acceptance as well as source qualification. The production build is **enterprise-2026.10.09-latency.1**, runtime source
`276c43a70873670a7e1665283620aba38594b1ab`, SHA-256 `256cfc65daaa6596ff2b31e2863296a3182975faa71b1a746295cc6172231958`. It links libpcap.so.0.8 and passed
a fresh exact-binary local functional namespace fixture with four KCP sessions,
two backend capture workers, S/PA flags, null encryption, payload verification,
UDP/half-close checks and owned-rule cleanup. Application runtime bodies match
locally qualified direct-read source 8f1ca0f; subsequent source changes are a
getBuffer comment and qualification documentation.

The four merged Git references were removed locally and from GitHub where they
existed: production-tuning, latency-read, exp2-control and completed-exp1. Their
commits remain in master history; unmerged/rejected archives remain. Removing the
old source worktree encountered root-owned test files. Its remaining source/test
archive is retained, its Git registration was removed, and the qualified native
binary's hash was verified intact. No failed or adverse qualification receipt
was deliberately discarded.

All five live migration.4 services were active/enabled with zero automatic
restarts before rollout. All six authenticated 1 MiB Reality baseline downloads
passed. The initial standalone probe attempt failed to execute its binary from
/run (noexec); no tunnel was changed. The corrected attempt uses an owned probe
executable beneath /root/super-paqet, unique loopback port 19096 and a private
/run config. Its process and files are temporary; backend Xray services/configs
are read-only.

The staged release validates each host's actual current config. This is a
binary-only upgrade: current config bytes, systemd service/drop-ins, four
independent source slots per country, two backend capture workers, warning logs,
quoted null encryption and S outbound/PA return are preserved. Germany retains
both listeners; clients use 91.107.251.85. France is 171.22.132.226 and Finland
65.109.249.222. Client ports 9001/9002/9003 and targets 2096 are unchanged.

Every host receives a complete pre-upgrade rollback archive and an independent
40-minute restore timer before activation. Services restart sequentially,
backends first, then clients. Process replacement disconnects live streams;
negotiated carrier migration cannot preserve state across a process restart.
Post-rollout acceptance and owned temporary-file cleanup are complete.

## Actual-host acceptance

| Host | New tunnel PID | Started (8 October UTC) | Automatic restarts |
|---|---:|---|---:|
| Germany, inventory 116.202.177.233 | 960629 | 21:49:14 | 0 |
| France, 171.22.132.226 | 3151995 | 21:58:28 | 0 |
| Finland, 65.109.249.222 | 250828 | 21:59:20 | 0 |
| Client 89.45.68.14 | 376268 | 21:59:32 | 0 |
| Client 89.45.68.118 | 1058941 | 21:59:41 | 0 |

The second controller attempt stalled waiting for an SSH exec acknowledgement
after Germany's replacement had committed. An independent read confirmed the
healthy new executable. The resumed controller bounded the complete SSH operation,
reconciled Germany by running/disk hashes, config, health and Xray identity, and
continued without repeating Germany's restart. Failed and interrupted controller
attempts remain in the private build directory alongside completed receipts.

All six routes passed two authenticated 1 MiB Reality downloads after activation:
**12/12 HTTP 200 responses with exactly 1,048,576 bytes**, following six successful
pre-upgrade baseline checks. These are application correctness probes through the
customer forwarding ports and backend Xray, not WAN capacity or latency comparisons.
Final audits verified matching disk/running executable hashes, active/enabled
services, healthy loopback endpoints, zero automatic restarts, unchanged config and
tunnel unit/drop-in bytes, stable backend Xray PIDs/start ticks, and no panic/fatal
in the checked tunnel journals. Xray services and configs were not edited.

Busy client .118 exercised four distinct active source ports in each country
(twelve total). Quiet client .14 had two logical carriers open per country during
acceptance; its config still reserves four independent slots per country. Carriers
open lazily. Both capture workers received traffic on France, Finland and Germany's
active 91.107.251.85 listener. Germany's unused 116.202.177.233 listener did not
exercise both workers; no such coverage claim is made for that alias.

The release includes the selectively reviewed integration and direct-read latency
fix. The [local qualification](LATENCY-2026-10-08.md) records matched comparative
workloads, including paced latency, bulk, WAN, churn, held flows and native/race
reload. Its two-worker uncapped upload/download medians were **5.688/4.442 Gbit/s**;
those are local comparative results, not production WAN capacity. A fresh exact
backend-compatible executable smoke additionally verified three 16 MiB payloads,
seven UDP datagrams up to 65,507 bytes, TCP half-close and HTTP/bulk with zero
application errors. Its settings and output are retained in the
[deployment receipt](latency-deployment-evidence-2026-10-09.json); this short smoke
is not the same workload as the comparative bulk qualification. The smoke
explicitly enables a 60-second mux recovery grace; current production YAML omits
that opt-in field and retains the zero default. Other fixture parameters are
recorded rather than asserted to equal the complete production configuration.

## Cleanup, rollback and observation

Every host retains its complete pre-upgrade archive and restore script at
`/root/super-paqet/rollback/20261008T214503Z-latency1/`. Automatic rollback timers
are disarmed. Owned staged binaries, compressed staging files, standalone probe
executables and temporary probe configs were removed. Existing customer services,
prior incident evidence, rollback archives and observation spools were retained.
Recorded [deployed YAML](deployed/README.md) remains byte-for-byte unchanged;
its migration.4 header describes configuration provenance, not the running binary.

All five independent `super-paqet-watch.service` collectors were renewed without
restarting a tunnel or Xray process. They sample every ten seconds and retain
private metric and journal files beneath:

`/var/log/super-paqet-watch/latency1-20261008T220541Z/`

The fixed deadline is **9 October 2026, 22:05:41 UTC** (10 October, 01:35:41 Tehran).
Journal capture starts at 8 October 21:49 UTC to include the rollout. Metric and
journal retention are bounded to 512 MiB and 64 MiB respectively per host. Warning
logging and disabled profiling remain. Each observer produced three initial
samples and journal records; tunnel/config/binary/Xray identities before and after
observer installation matched. Initial samples reported no error/recovery counter
increase; client .118 showed up to 2,665 active flows. This brief observation is
not a completed day of stability or a maximum-customer capacity result. The
[recorded observer unit](deployed/super-paqet-watch.service) has this window's fixed
deadline and requires explicit renewal for another window.

No configuration sweep, global sysctl change, CPU restriction or reboot was made
during this rollout. Receipts identify the exact executable, unchanged config
hashes, staged validation, actual-host process identities, route probes, cleanup,
initial observer samples and qualification limits.
