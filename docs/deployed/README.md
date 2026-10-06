# Recorded deployment configuration

All five services run enterprise-2026.10.06, embedded source abf04f6, binary
SHA-256 `47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`.

Client and Finland snapshots were freshly fetched and audited after the new
Finland deployment. Netherlands/Germany backend snapshots match their previous
audits. Configs contain no encryption keys. Client profiling remains enabled;
backend profiling is disabled. Metrics listen only on loopback.

Germany has eight carriers per client, PA flags and MTU1350. Netherlands and
Finland have four, client S / backend PA, MTU1350 and scaled windows. Fixed client
source ports are Germany 29998, Netherlands 29997 and Finland 29996. All three
forwarding routes passed complete authenticated downloads after cleanup.
These snapshots are host-specific, not generic defaults or universal link profiles.

`super-paqet.service` is the shared base unit. Finland additionally uses the
recorded `65.109.249.222-address.conf` as
`/etc/systemd/system/super-paqet.service.d/20-finland-address.conf` to restore its
assigned secondary IPv4 before binding. Address/primary routes remain otherwise
unchanged; reboot behavior is described in the [Finland report](../FINLAND-CHECK.md).
The obsolete 65.109.211.233 snapshot remains available in Git history/remote rollback
backups. See [status](../STATUS.md), [deployment](../DEPLOYMENT.md) and the
[Netherlands](../NETHERLANDS-DIAGNOSIS.md) / [Finland](../FINLAND-CHECK.md) results.
