# Proposed independent source-port deployment

These five configurations retain their proposal comments. Their settings were
deployed as migration.4 after the user selected a 10-second stall threshold;
[deployed/](../deployed/README.md) records actual accepted snapshots.
All five passed read-only `config validate` checks on their actual hosts. See the
[rollout report](../MIGRATION-DEPLOYMENT-2026-10-07.md) and the earlier
[migration benchmark report](../MIGRATION-2026-10-07.md).

Both clients use four independently reserved automatic source ports per country,
10-second stall and 15-second retry budgets, a five-second verified recovery probe and
`preserve_connections: true`. This now requires the migration runtime on **both**
clients and backends; older backends fall back to ordinary replacement. The two-vCPU backends use two packet capture workers so distinct client
tuples can spread receive processing across both CPUs. Backend
listener ports, country peer names, target/customer ports, S outbound / PA return,
null encryption, memory/admission limits and KCP parameters remain unchanged.
Backend listener settings remain conversation-aware and accept both layouts.

Logging stays at warn and profiling stays off. Restart selects fresh automatic
ports. Identical live reload retains effective/recovered ports. Preflight
validation remains necessary for subsequent config edits.
