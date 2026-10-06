# Current deployment status

Finalized 2026-10-06 for production operation on five hosts. All six routes use
the same four-carrier adaptive KCP profile, client S / backend PA flags, quoted
null encryption, AF_PACKET with one receive worker and MTU1350. Country peers
are `germany`, `finland`, `france`; 171.22.132.226 is France.

## Exact release and topology

Version `enterprise-2026.10.06-startup`, source `5f403c23e53c58f4917ad2b0c5a480bb96719516`, SHA-256:

`e4bbd4915cded878ce3b8038c9f114d58cab8c20e4271c0a73646a892d4c4294`

The exact executable is retained at `build/final-production/super-paqet-startup-final`.
The original raw Ethernet/IP/TCP shaping and sequence/ACK behavior are preserved.
Startup learns once per observed RTT within 50–250 ms bounds; established and
idle controllers retain 250 ms updates. Carrier count stays fixed at four while
pacing, send window, ACK scheduling, reorder allowance and RTO floor adapt.
Receive-buffer ceilings remain explicit. Matching KCP settings are recorded in
[deployed configurations](deployed/README.md).

| Client port, on both .14 and .118 | Peer | Raw endpoint | Application target |
|---|---|---|---|
| 9001 | germany | 91.107.251.85:29999 | 116.202.177.233:2096 |
| 9002 | finland | 65.109.249.222:29999 | 65.109.249.222:2096 |
| 9003 | france | 171.22.132.226:29999 | 171.22.132.226:2096 |

Clients are 89.45.68.14 and 89.45.68.118. Germany retains its second local
116.202.177.233 listener. Source ports stay 29998/29996/29997 respectively.
The working raw tunnel port is 29999, superseding the initial 2052 request.

Services are enabled systemd units with failure restart, live config reload,
configuration validation and owned firewall cleanup. All available processors
are usable: two vCPUs/~4 GiB per backend, four vCPUs/~8 GiB per client. There is
no CPU quota or affinity restriction. LimitNOFILE is 524288 and TasksMax 65536;
soft Go memory limits are 1536/4096 MiB by role. Log level is **warn** on every
host; profiling is off and metrics listen only on loopback. The binary rollout
required one tunnel restart; final logging/route cleanup applied live without
another restart. Xray configurations/services were not edited or restarted.

## Qualification and operational limits

The full application race suite and vet passed. Four disposable local profiles
passed byte integrity and workload checks: clean 100 Mbit/s/80 ms RTT, 1% loss
with jitter/reordering, delayed asymmetric 10/100 Mbit/s with loss, and a narrow
1 Mbit/s/100 ms RTT link with 2% loss. Cleanup preserved unrelated firewall rules.
In a matched four-session local control, first 1 MiB time fell from 2.34 to
1.44 seconds; this is one paired run, not a universal speed guarantee.

On the deployment, six-path simultaneous bulk measured **0.776 Gbit/s download /
0.956 Gbit/s upload**, in separate directions, with all 96 receiver streams
carrying payload and no iperf errors. Earlier static-profile measurements were
0.532/0.259 Gbit/s. The profiles differ in more than adaptation, so the full
configuration is the unit of comparison. Neither result establishes each
backend's physical link capacity. The user clarified 1 Gbit/s was an estimate,
not an acceptance threshold.

All **12000 held forwards** survived and every connection was reverified; every
one of **3072 authenticated request workers** completed full responses with no
application benchmark errors. Mixed-load tunnel peaks on clients were about
180/200 MiB, with no swap. Long backend SSH monitor channels ended early, so
backend peak resource usage in that mixed run is not established.

The extreme authenticated workload showed 4.6–7.0 second means and **308 tunnel
opening timeouts on .14 → France**, plus 25 backend control EOF errors. Successful
request-worker results do not erase those failures. They are unresolved follow-up
work for the separate empty server requested by the user. Backend-local Xray
controls also showed multi-second latency but do not conclusively attribute it.
The same-size internal 16 KiB test (3072 workers, 2-second pause) passed with means
87–115 ms and p99 bounds 113–206 ms; idle socket reuse means workers are not a
simultaneous-socket count. Held-forward evidence proves connection retention.

There were no observed crashes, fatal config/cleanup failures, rejected reloads,
degraded transactions or dropped logs. Canceled requests/aborted relays at
benchmark deadlines are counted separately. This is a finitely qualified
operational deployment, not an unconditional claim of optimality or readiness
for arbitrary saturation, all link types or hundreds of thousands of busy users.

## Cleanup and handoff

Only the binary, `conf.yaml` and `rollback/` remain in each deployment folder.
Test routes, native/proxy probes, test units, temporary filtering rules and
historical owned files are removed. Each host retains two complete recovery
archives: current release and previous accepted release, including config/unit
and the Finland address drop-in where applicable. Hosts were not rebooted.

See [production evidence](production-deployment-evidence.json),
[deployment](DEPLOYMENT.md), [configuration guide](CONFIGURATION.md),
[live reload](LIVE-RELOAD.md) and [operations](OPERATIONS.md).
