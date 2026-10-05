# Documentation map

Read [STATUS.md](STATUS.md) first. It distinguishes the running base, earlier
local benchmarks, committed undeployed fixes and separate experiments. The user
has verified one active customer; thousands of active customers remain untested.

| Document | Purpose |
|---|---|
| [DEVELOPMENT-HISTORY.md](DEVELOPMENT-HISTORY.md) | Recorded failures, approaches, rejected experiments, evidence mistakes and decisions through the review pause. |
| [CODE-COMMENTS.md](CODE-COMMENTS.md) | Comment coverage, ownership/contract explanations and behavior-preservation verification. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Current package layout, process/object ownership, TCP/UDP paths, packet drivers, concurrency and lifecycle. |
| [CONFIGURATION.md](CONFIGURATION.md) | Every enterprise YAML field, effective defaults, precedence, constraints, manual retransmission and adaptation boundaries. |
| [DEPLOYMENT.md](DEPLOYMENT.md) | Current four-host recovery topology, overrides, tests, resources and backup checkpoints. |
| [deployed/README.md](deployed/README.md) | Recorded actual deployed YAML/unit snapshots; hashes checked against the last manifests. |
| [OPERATIONS.md](OPERATIONS.md) | Build identification, installation layouts, monitoring, troubleshooting, future rollout and rollback. |
| [TRANSPORT.md](TRANSPORT.md) | Raw packet contract, inner protocols, why layers exist and what compatibility/detectability checks mean. |
| [DIAGNOSTICS.md](DIAGNOSTICS.md) | Events/counters, sampling, known version differences and test tooling. |
| [BENCHMARKS.md](BENCHMARKS.md) | Frozen earlier local qualification, workload/resource measurements and limits. |
| [BENCHMARKS-HISTORY.md](BENCHMARKS-HISTORY.md) | Earlier measurements and invalidated/limited comparisons, retained as history. |
| [step1-qualification.json](step1-qualification.json) | Machine-readable earlier local candidate evidence. |
| [deployment-recovery-evidence.json](deployment-recovery-evidence.json) | Sanitized real-link comparisons and recovery checks. |
| [queue-pressure-evidence.json](queue-pressure-evidence.json) | Confirmed ENOBUFS/stale-chain fixes and their local regression evidence. |

No single document establishes universal production readiness. Performance
results must be read with their binary, cipher, packet size, controller settings,
concurrency, hardware and measurement method. None of this review documentation
authorizes or performs another deployment.
