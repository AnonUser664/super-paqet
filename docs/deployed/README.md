# Recorded deployed configuration snapshots

These files reproduce the last successfully checked live recovery settings on
2026-10-05. They were copied from staged local files and checked against the
recorded final manifest hashes. They are not a fresh SSH inspection or generic
recommended defaults. The user subsequently confirmed one active-user success.
No secrets are present: endpoints explicitly use `enc: 'null'` and no keys.

| File | Role | SHA-256 |
|---|---|---|
| [116.202.177.233.yaml](116.202.177.233.yaml) | Server | `a8ef7187c2aef93d4347e8b9c56b2a68223bcc2320af145d4d7c448957ad0980` |
| [171.22.132.226.yaml](171.22.132.226.yaml) | Server | `1a592f2874c5cc2ca553bea30576b0a585b8891642eb1dbe36a15d1f78b78469` |
| [89.45.68.14.yaml](89.45.68.14.yaml) | Client | `4f0e2d48809daec029e43c3a1292436fa3e5ed7c58995e068547d2e4a6096eee` |
| [89.45.68.118.yaml](89.45.68.118.yaml) | Client | `257ca77ee9c66ed9a3a9bf6b108f81c7e4c4d3db32782f9af3e7133375f86baf` |

The [unit](super-paqet.service) uses /root paths and read-only home access.
Source `1c77c55`, deployed executable SHA-256
`ecb8e002173f6f80cd3f403d3ee3080190f0e49889fa2ba9f883b893f96318f3`.
The newer queue-pressure patch is not deployed. Client backend2/9002 entries
were preserved exactly; they are excluded from the current working-path claim.
Read [CONFIGURATION.md](../CONFIGURATION.md) before editing and
[DEPLOYMENT.md](../DEPLOYMENT.md) for the selection/evidence history.
