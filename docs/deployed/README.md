# Recorded deployment configuration

All four services run enterprise-2026.10.06, embedded source abf04f6, binary
SHA-256 `47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`.

Client and Netherlands backend configs were freshly fetched after successful
Netherlands repair qualification. Germany backend and unit remain recorded from
release cleanup. Configs contain no encryption keys. Client profiling remains
enabled; backend profiling is disabled. Metrics listen only on loopback.

Germany has eight carriers per client, PA flags and MTU1350. Netherlands has four,
client S / backend PA, MTU1350 and scaled windows on the original 29997/29999
ports. Both clients passed complete authenticated downloads after cleanup.
These snapshots are host-specific, not generic defaults or a universal link
profile. See [status](../STATUS.md), [deployment](../DEPLOYMENT.md), and the
[diagnosis and test limits](../NETHERLANDS-DIAGNOSIS.md).
