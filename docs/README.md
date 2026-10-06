# Documentation

Start with [current status](STATUS.md). The five-host deployment is finalized,
all six paths pass authenticated checks, and country peers use S outbound / PA
return. 171.22.132.226 is France; the historical Netherlands diagnosis describes
that same host. Measured capacity limitations remain explicit in the status and
[production evidence](production-deployment-evidence.json).

| Document | Purpose |
|---|---|
| [STATUS.md](STATUS.md) | Running version, current paths and outstanding work. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Package layout, ownership, data paths, concurrency and lifecycle. |
| [TRANSPORT.md](TRANSPORT.md) | Raw packet contract, inner protocols and reasons for their design. |
| [CONFIGURATION.md](CONFIGURATION.md) | Every YAML field, defaults, constraints and adaptation boundaries. |
| [LIVE-RELOAD.md](LIVE-RELOAD.md) | Validation, watching, edit impact and rollback. |
| [DEPLOYMENT.md](DEPLOYMENT.md) | Deployment topology, installation and service settings. |
| [deployed/](deployed/README.md) | Recorded configurations and deployed systemd unit. |
| [OPERATIONS.md](OPERATIONS.md) | Monitoring, rollout, troubleshooting and rollback procedures. |
| [DIAGNOSTICS.md](DIAGNOSTICS.md) | Logs, metrics, profiling and reproducible tests. |
| [BENCHMARKS.md](BENCHMARKS.md) | Current qualification results, methods and known failed gates. |
| [CLEAN-LINK-CURRENT.md](CLEAN-LINK-CURRENT.md) | Exact deployed executable's isolated unencrypted bulk results. |
| [FINAL-DEPLOYMENT-REPORT.md](FINAL-DEPLOYMENT-REPORT.md) | Release measurements, resource limits and deployment evidence. |
| [FINLAND-CHECK.md](FINLAND-CHECK.md) | Finland working deployment, previous failed IP control and final state. |
| [NETHERLANDS-DIAGNOSIS.md](NETHERLANDS-DIAGNOSIS.md) | Same-path version comparisons and paired packet captures. |

Compact evidence: [release](final-deployment-evidence.json),
[clean bulk](clean-null-current-evidence.json),
[Netherlands](NETHERLANDS-DIAGNOSTIC-EVIDENCE.json).

Superseded reports, deployment snapshots and generated experiments were removed
from the checkout. Their committed versions remain available in Git history.
