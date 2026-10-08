# Documentation

Start with [current status](STATUS.md). Migration.4 is deployed on five hosts
with four independent client source ports per country, two backend capture
workers and ten-second stall detection. The finite rollout passed;
the earlier one-day observation has no completed stability result here,
all six paths pass authenticated checks, and country peers use S outbound / PA
return. 171.22.132.226 is France; the historical Netherlands diagnosis describes
that same host. Measured capacity limitations remain explicit in the status and
[production evidence](production-deployment-evidence.json).

| Document | Purpose |
|---|---|
| [STATUS.md](STATUS.md) | Running version, current paths and outstanding work. |
| [PRODUCTION-OBSERVATION-2026-10-07.md](PRODUCTION-OBSERVATION-2026-10-07.md) | Current five-host metric/journal window, cleanup and tomorrow's review instructions. |
| [Production review — 8 October](PRODUCTION-REVIEW-2026-10-08.md) | Three-hour customer-load review, France recovery incident, profiling and six-route acceptance. |
| [Production follow-up — 8 October](PRODUCTION-FOLLOWUP-2026-10-08.md) | Fifteen-hour review, six-route checks, Finland receiver backpressure and measured transmit-queue pressure. |
| [Recovery grace — 8 October](RECOVERY-GRACE-2026-10-08.md) | Opt-in extended-outage preservation, bounded expiry, diagnostic causes and local regression measurements; not deployed. |
| [Receive processing — 8 October](RECEIVE-PATH-2026-10-08.md) | Receive-only optimization, rejected admission experiments, measurement controls and remaining WAN checks; not deployed. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Package layout, ownership, data paths, concurrency and lifecycle. |
| [TRANSPORT.md](TRANSPORT.md) | Raw packet contract, inner protocols and reasons for their design. |
| [CONFIGURATION.md](CONFIGURATION.md) | Every YAML field, defaults, constraints and adaptation boundaries. |
| [LIVE-RELOAD.md](LIVE-RELOAD.md) | Validation, watching, edit impact and rollback. |
| [DEPLOYMENT.md](DEPLOYMENT.md) | Deployment topology, installation and service settings. |
| [MIGRATION-DEPLOYMENT-2026-10-07.md](MIGRATION-DEPLOYMENT-2026-10-07.md) | Exact migration.4 rollout, ten-second qualification and accepted live paths. |
| [MIGRATION-2026-10-07.md](MIGRATION-2026-10-07.md) | Migration protocol, bounded diagnostics and prior build's full local benchmarks. |
| [deployed/](deployed/README.md) | Recorded configurations and deployed systemd unit. |
| [OPERATIONS.md](OPERATIONS.md) | Monitoring, rollout, troubleshooting and rollback procedures. |
| [DIAGNOSTICS.md](DIAGNOSTICS.md) | Logs, metrics, profiling and reproducible tests. |
| [BENCHMARKS.md](BENCHMARKS.md) | Current qualification results, methods and known failed gates. |
| [CLEAN-LINK-CURRENT.md](CLEAN-LINK-CURRENT.md) | Latest ownership bulk result and earlier executable comparisons. |
| [FINAL-DEPLOYMENT-REPORT.md](FINAL-DEPLOYMENT-REPORT.md) | Release measurements, resource limits and deployment evidence. |
| [FINLAND-CHECK.md](FINLAND-CHECK.md) | Finland working deployment, previous failed IP control and final state. |
| [NETHERLANDS-DIAGNOSIS.md](NETHERLANDS-DIAGNOSIS.md) | Same-path version comparisons and paired packet captures. |

The [customer incident report](PRODUCTION-INCIDENT-2026-10-06.md) records observed
failures, fixes, attribution limits and the monitoring window. Latest same-binary
local qualification and rollout: [migration deployment receipts](migration-deployment-evidence-2026-10-07.json).

Earlier compact evidence: [release](final-deployment-evidence.json),
[clean bulk](clean-null-current-evidence.json),
[Netherlands](NETHERLANDS-DIAGNOSTIC-EVIDENCE.json).

Superseded reports, deployment snapshots and generated experiments were removed
from the checkout. Their committed versions remain available in Git history.

[Source-tuple incident and verified recovery](SOURCE-TUPLE-INCIDENT-2026-10-07.md)
records the live .118 France/Finland outage, scoped repair, client rollout and
[exact recovery qualification](path-recovery-qualification-2026-10-07.json).
