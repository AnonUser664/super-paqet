# Proposed independent source-port deployment

These five configurations are review artifacts, **not deployed snapshots**.
`docs/deployed/` continues to describe the running shared-source release.
The exact qualified migration candidate and benchmarks are in the
[migration report](../MIGRATION-2026-10-07.md). All five files passed read-only
`config validate` checks with that executable in synthetic host namespaces.
Actual-host preflight remains required at deployment.

Both clients use four independently reserved automatic source ports per country,
15-second stall/retry budgets, a five-second verified recovery probe and
`preserve_connections: true`. This now requires the migration runtime on **both**
clients and backends; older backends fall back to ordinary replacement. The two-vCPU backends use two packet capture workers so distinct client
tuples can spread receive processing across both CPUs. Backend
listener ports, country peer names, target/customer ports, S outbound / PA return,
null encryption, memory/admission limits and KCP parameters remain unchanged.
Backend listener settings remain conversation-aware and accept both layouts.

Logging stays at warn and profiling stays off. Restart selects fresh automatic
ports. Identical live reload retains effective/recovered ports. Deployment waits
for the user's review; preflight validation must run on each actual host.
