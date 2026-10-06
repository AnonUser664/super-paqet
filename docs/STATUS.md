# Current status

Updated 2026-10-06. The five active hosts run persistent systemd services with
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
| 9003 | 171.22.132.226:29999 | 171.22.132.226:2096 | Restored; final Reality 10 MiB requests passed 8/8 across both clients. |
| 9002 | 65.109.249.222:29999 | 65.109.249.222:2096 | Working; 4/4 10 MiB Reality requests and external domain/both-client checks passed. |

Germany uses eight shared KCP carriers per client with client S / backend PA flags and MTU1350.
France and Finland use four, client S / backend PA, MTU1350 and scaled packet windows,
with the same original source/destination ports. Cipher remains quoted `null`
and adaptation disabled on these physical-path profiles. Generic adaptive
defaults have separate virtual-link evidence.

The resumed Netherlands qualification passed 1024 checked connection-churn
echoes, sixteen continuous streams over two minutes, eight full 10 MiB Reality
requests and four post-cleanup 1 MiB requests. Germany regression passed 4/4;
public-domain 9001/9003 downloads also passed. This is a repaired, finitely tested
path, not universal production readiness. [Diagnosis and limits](NETHERLANDS-DIAGNOSIS.md).

[Client and Finland snapshots](deployed/README.md) were freshly fetched
and audited after Finland qualification. Netherlands/Germany snapshots match
their previous audits. Profiling is disabled on all instances;
metrics bind only to localhost. Temporary diagnostic resources were removed.

Finland now runs the current release on `65.109.249.222:29999`, using the
Netherlands profile and client source 29996. Qualification passed 1024 connection
churn echoes, sixteen continuous streams for two minutes, full authenticated
10 MiB downloads and regression/public checks. Its owned startup drop-in restores
the secondary IPv4 before service binding; the original primary address stays.
[Finland results and reboot-test boundary](FINLAND-CHECK.md).

## Qualification and remaining work

The exact binary passed a local 100,000 mostly idle forward soak, separate
multi-gigabit bulk tests, seven virtual WAN profiles and 256-stream reload
continuity. Earlier deployed load verified 8,192 held forwards; this does not
qualify thousands of busy Reality customers or qualify multi-day behavior of the repaired Netherlands path.

Current isolated unencrypted bulk measures 3.50–3.74 Gbit/s upload and
3.08–3.10 Gbit/s download on the laptop. The gap from earlier peaks remains
unattributed; throughput investigation is deferred. Saturated asymmetric duplex
with timing/credit extensions disabled has a known failed receiver-stream gate.

See [benchmarks](BENCHMARKS.md) and the
[release report](FINAL-DEPLOYMENT-REPORT.md) for methods and limits.

## Production qualification in progress

Peers are now named `germany`, `finland`, and `france`; 171.22.132.226 is France
(the earlier Netherlands label was incorrect). Client S / backend PA is now
applied on every route. All six paths passed two full authenticated 1 MiB
downloads each after the change. Current counts remain Germany eight carriers,
Finland/France four; application code and raw framing are unchanged.

The new customer requirement is thousands of active connections and sustained
multi-gigabit traffic. Deployed-host capacity/window/session comparisons and
cleanup remain in progress; the small-path checks do not establish that target.
