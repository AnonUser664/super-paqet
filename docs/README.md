# Documentation

Start with [current status](STATUS.md). The final 13 October release runs on
the replacement client and three backends, with four independent client source
ports per country, adaptive control, S outbound / PA return and ten-second stall
detection. Current qualification limits and the separate Finland Reality
dependency are explicit in the status and [final review](FINAL-PASS-2026-10-13.md).
171.22.132.226 is France; the historical Netherlands diagnosis describes that
same host. Earlier deployment/review documents below describe their dated
checkpoints and do not supersede the current status.

| Document | Purpose |
|---|---|
| [STATUS.md](STATUS.md) | Running version, current paths and outstanding work. |
| [Branch consolidation — 13 October](CONSOLIDATION-2026-10-13.md) | Every retired experiment's outcome, selective merge decisions, archived evidence and remaining paths. |
| [Production-led exploration — 12 October](EXPLORATION-2026-10-12.md) | Full writer, pacing, reordering, RTT, diagnostics and measurement journal, including failed gates. |
| [ACK bandwidth — 13 October](ACK-BUDGET-2026-10-13.md) | Verified 100 MB feedback cost, clean asymmetric confirmation and repeated-segment finding. |
| [Latest rollout — 9 October](LATENCY-DEPLOYMENT-2026-10-09.md) | Accepted five-host latency.1 deployment, unchanged configs, cleanup, rollback and renewed observer window. |
| [PRODUCTION-OBSERVATION-2026-10-07.md](PRODUCTION-OBSERVATION-2026-10-07.md) | Earlier five-host observer window, retention and review procedures. |
| [Production log review — 9 October](PRODUCTION-REVIEW-2026-10-09.md) | 17.6-hour latency.1 review, France preserving recoveries, opening backpressure, queue drops and unchanged fleet identities. |
| [Production review — 8 October](PRODUCTION-REVIEW-2026-10-08.md) | Three-hour customer-load review, France recovery incident, profiling and six-route acceptance. |
| [Production follow-up — 8 October](PRODUCTION-FOLLOWUP-2026-10-08.md) | Fifteen-hour review, six-route checks, Finland receiver backpressure and measured transmit-queue pressure. |
| [Recovery grace — 8 October](RECOVERY-GRACE-2026-10-08.md) | Opt-in extended-outage preservation, bounded expiry, diagnostic causes and local regression measurements before selective integration. |
| [Receive processing — 8 October](RECEIVE-PATH-2026-10-08.md) | Receive-only optimization, rejected admission experiments, measurement controls and remaining WAN checks at that checkpoint. |
| [Coordinated exploration — 8 October](EXPLORATION-2026-10-08.md) | Branch checkpoint, failed backpressure latency gate, packed-pressure alternative and cross-agent test coordination. |
| [Selective integration — 8 October](INTEGRATION-REVIEW-2026-10-08.md) | Exp1 completion, exp2 ownership corrections, rejected shortcuts and combined qualification. |
| [Capture-worker coverage — 8 October](MULTIWORKER-2026-10-08.md) | Actual two-worker traffic evidence, native/race reload checks and unresolved paced mixed-load latency gate. |
| [Paced latency follow-up — 8 October](LATENCY-2026-10-08.md) | Direct TCP reads, adaptive bounded scratch, forty comparisons, paired bulk results and retained adverse reordering evidence. |
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
local smoke and rollout: [latency deployment receipts](latency-deployment-evidence-2026-10-09.json).

Earlier compact evidence: [release](final-deployment-evidence.json),
[clean bulk](clean-null-current-evidence.json),
[Netherlands](NETHERLANDS-DIAGNOSTIC-EVIDENCE.json).

Superseded reports, deployment snapshots and generated experiments were removed
from the checkout. Their committed versions remain available in Git history.

[Source-tuple incident and verified recovery](SOURCE-TUPLE-INCIDENT-2026-10-07.md)
records the live .118 France/Finland outage, scoped repair, client rollout and
[exact recovery qualification](path-recovery-qualification-2026-10-07.json).

## Final production review — 13 October 2026

- [Final decisions, failures and tradeoffs](FINAL-PASS-2026-10-13.md)
- [Production counters and bounded warning interpretation](PRODUCTION-DIAGNOSTICS.md)
- [Current exact deployment and dependency status](STATUS.md)
- [Exact-artifact qualification](final-artifact-qualification-2026-10-13.json)
- [Real-host 20,000-connection gate](final-real-scale-2026-10-13.json)
- [Post-cleanup health](final-health-evidence-2026-10-13.json)
