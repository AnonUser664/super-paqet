# Current status

Updated 2026-10-06. The four active hosts run persistent systemd services with
`enterprise-2026.10.06`, embedded source `abf04f6` and main equivalent runtime
`97529dd`. Binary SHA-256:

`47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`

The exact executable is retained locally at
`build/final-production/super-paqet-small-flush`. Current source, configuration
validation, live reload, shared fixed-source lanes, ACK indexing and the
optional small-write flush remain unchanged by repository cleanup.

## Current deployment

Both clients, 89.45.68.14 and 89.45.68.118, have these forwards:

| Client port | Raw tunnel endpoint | Application destination | Latest result |
|---|---|---|---|
| 9001 | 91.107.251.85:29999 | 116.202.177.233:2096 | Germany passed 4/4 authenticated 10 MiB requests. |
| 9003 | 171.22.132.226:29999 | 171.22.132.226:2096 | Unavailable; current, previous enterprise and upstream controls failed. |
| 9002 | Excluded direct backend entry | 65.109.192.172:2096 | Unavailable/excluded; not contacted. |

Germany uses eight shared KCP carriers per client, Netherlands four. The
four-carrier Netherlands reversion did not restore forwarding. Cipher remains
quoted `null`, flags PA, and adaptation disabled on these physical-path recovery
profiles. Generic adaptive defaults have separate virtual-link evidence.

The Netherlands diagnosis stopped at the user's request. Saved production
configs were restored; services are active/enabled and diagnostic units/rules
are gone. This is process health, not a working Netherlands application path.
[Diagnosis and packet counts](NETHERLANDS-DIAGNOSIS.md).

Recorded [configs and unit](deployed/README.md) reflect the latest applied
Netherlands four-carrier configuration. Client profiling remains enabled from
that diagnostic edit; backend profiling is disabled. Metrics bind only to
localhost. No new remote configuration change was made during repository cleanup.

## Qualification and remaining work

The exact binary passed a local 100,000 mostly idle forward soak, separate
multi-gigabit bulk tests, seven virtual WAN profiles and 256-stream reload
continuity. Earlier deployed load verified 8,192 held forwards; this does not
qualify thousands of busy Reality customers or repair today's Netherlands path.

Current isolated unencrypted bulk measures 3.50–3.74 Gbit/s upload and
3.08–3.10 Gbit/s download on the laptop. The gap from earlier peaks remains
unattributed; throughput investigation is deferred. Saturated asymmetric duplex
with timing/credit extensions disabled has a known failed receiver-stream gate.

See [benchmarks](BENCHMARKS.md) and the
[release report](FINAL-DEPLOYMENT-REPORT.md) for methods and limits.
