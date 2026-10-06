# Recorded deployment configuration

All four services run enterprise-2026.10.06, embedded source abf04f6, binary
SHA-256 `47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`.

Backend configs and the service unit were recorded after release cleanup. Client
configs reflect the later applied Netherlands four-carrier edit, whose saved
bytes were restored after same-path diagnosis. This is not a new remote audit.
Client profiling remains enabled by that diagnostic edit; backend profiling is
disabled. Configs contain no encryption keys.

Germany retains eight carriers per client; Netherlands four. Netherlands remains
unavailable despite healthy services. See [current status](../STATUS.md), the
[deployment guide](../DEPLOYMENT.md) and [diagnosis](../NETHERLANDS-DIAGNOSIS.md).
These are host-specific snapshots, not generic defaults or proof of reachability.
