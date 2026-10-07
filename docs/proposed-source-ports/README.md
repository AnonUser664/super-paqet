# Proposed independent source-port deployment

These five configurations are review artifacts, **not deployed snapshots**.
`docs/deployed/` continues to describe the running shared-source release.

Both clients use four independently reserved automatic source ports per country,
15-second stall/retry budgets and a five-second verified recovery probe. Backend
listener ports, country peer names, target/customer ports, S outbound / PA return,
null encryption, memory/admission limits and KCP parameters remain unchanged.
Backend listener settings remain conversation-aware and accept both layouts.

Logging stays at warn and profiling stays off. Restart selects fresh automatic
ports. Identical live reload retains effective/recovered ports. Deployment waits
for the user's review; preflight validation must run on each actual host.
