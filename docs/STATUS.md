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
| 9003 | 171.22.132.226:29999 | 171.22.132.226:2096 | Restored; final Reality 10 MiB requests passed 8/8 across both clients. |
| 9002 | 65.109.211.233:2052 | 65.109.211.233:2096 | Replacement IP corrected; forwarding failed. The isolated Netherlands-profile control also failed. |

Germany uses eight shared KCP carriers per client with PA flags and MTU1350.
Netherlands uses four, client S / backend PA, MTU1350 and scaled packet windows,
with the same original source/destination ports. Cipher remains quoted `null`
and adaptation disabled on these physical-path profiles. Generic adaptive
defaults have separate virtual-link evidence.

The resumed Netherlands qualification passed 1024 checked connection-churn
echoes, sixteen continuous streams over two minutes, eight full 10 MiB Reality
requests and four post-cleanup 1 MiB requests. Germany regression passed 4/4;
public-domain 9001/9003 downloads also passed. This is a repaired, finitely tested
path, not universal production readiness. [Diagnosis and limits](NETHERLANDS-DIAGNOSIS.md).

[Client and Netherlands snapshots](deployed/README.md) were freshly fetched
and audited after the profile's qualification. Germany's snapshot/unit remain
from release cleanup. Client profiling is enabled, backend profiling disabled;
metrics bind only to localhost. Temporary diagnostic resources were removed.

The replacement Finland host `65.109.211.233` retains the older enterprise
binary, SHA-256 `ecb8e002173f6f80cd3f403d3ee3080190f0e49889fa2ba9f883b893f96318f3`.
Its stale old-IP bind was corrected and its service now runs. Both clients’ 9002
references were updated. A temporary matched-current-binary Netherlands-profile
control on 29999 failed; it was removed. [Finland check](FINLAND-CHECK.md).

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
