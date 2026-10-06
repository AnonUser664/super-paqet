Current release and measured limits: [final deployment report](FINAL-DEPLOYMENT-REPORT.md),
[status](STATUS.md), [fresh snapshots](deployed-final/README.md),
[sanitized evidence](final-deployment-evidence.json).

# Documentation map

Read [STATUS.md](STATUS.md) first. It distinguishes the running base, earlier
local benchmarks, committed undeployed fixes and separate experiments. The user
has verified one active customer; thousands of active customers remain untested.

| Document | Purpose |
|---|---|
| [CLEAN-LINK-CURRENT.md](CLEAN-LINK-CURRENT.md) | Fresh isolated unencrypted bulk measurements of the exact current executable, CPU/memory, small-write comparison and historical performance gap. |
| [TEST-RESULTS-AUDIT.md](TEST-RESULTS-AUDIT.md) | Throughput comparison by workload/build, latest local and remote results, Netherlands failure evidence and remaining causal gaps. |
| [test-results-inventory.json](test-results-inventory.json) | Compact inventory of all retained structured result files, matrix gates and final-stage remote reports, including failures. |
| [DEVELOPMENT-HISTORY.md](DEVELOPMENT-HISTORY.md) | Recorded failures, approaches, rejected experiments, evidence mistakes and decisions through the review pause. |
| [LIVE-RELOAD.md](LIVE-RELOAD.md) | File watching, validation CLI, exact edit impact, replacement/rollback semantics and qualification. |
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
| [live-reload-evidence.json](live-reload-evidence.json) | Versioned live reload continuity, resource cleanup and validation results. |
| [queue-pressure-evidence.json](queue-pressure-evidence.json) | Confirmed ENOBUFS/stale-chain fixes and their local regression evidence. |

No single document establishes universal production readiness. Performance
results must be read with their binary, cipher, packet size, controller settings,
concurrency, hardware and measurement method. Deployment authorization was subsequently provided by the user; the final
report records what was actually deployed and the outstanding qualification limits.
