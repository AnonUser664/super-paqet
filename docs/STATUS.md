# Step 1 complete: local qualification passed

Expanded diagnostics, testing and tuning are complete for the frozen candidate.
The next stage is user review and the final required feature check. Deployment
has not been performed.

- Runtime source checkpoint: `2d5f7a0`; isolated full checks used `64fe72a`.
- Qualified executable: `build/super-paqet-pcap-address-fix`.
- SHA-256: `e2ae9b8cce7dc864dae9d21c5f770bb353ab787c218f2faf1d794d8efe7003f6`.
- [Verified evidence](step1-qualification.json): 38 live profile/seed runs.
- Main artifacts: `build/step1-final-v5`.
- Final scale/service/fuzz/extra-seed artifacts: `build/step1-final-v5-tail-final`.
- Full-check log: `build/step1-final-v5-tail-retry/full-checks.log`.
- Final host cleanup: `build/step1-final-cleanup.json`.

The 26-profile main matrix and twelve additional runs passed integrity,
regression floors, workload-error checks and owned-rule cleanup. Clean bulk
measured 4.387 Gbit/s upload and 3.708 Gbit/s download; simultaneous duplex was
2.361 + 2.127 Gbit/s. A 1000 Mbit/s / 100 ms RTT single-flow path delivered
approximately 915 Mbit/s in either direction. High-delay, asymmetric, jitter,
reordering and lossy-path results are documented with their limits.

The 600-second mixed soak established 100,000 mostly idle forwards in 9.216
seconds and verified a complete response on every held socket afterward.
All 100,000 remained usable, with zero verification/mixed load errors. Concurrent
bulk averaged 1.572 Gbit/s and HTTP 722 requests/s, with a 32.768 ms p99 histogram
upper bound under memory pressure. This scale run temporarily raised the host
receive backlog from 1000 to 65536 and verified restoration to 1000. An earlier
local TCP timeout and loopback drops prompted full-socket verification and
reduced monitoring overhead; the successful repeat used both changes.

Full vet checks, root/smux race suites and full KCP tests passed. Both 60-second
fuzzers passed (6.4 million control / 3.5 million raw-frame executions). The
candidate passed transient systemd restrictions, crash/restart, integrity and
firewall recovery. Final inspection found no test namespaces, transient test
units or host SPQ rules. Diagnostic logs recorded clean graceful shutdowns with
zero active flows and no dropped logs in the main matrix and scale soak.

The source candidate preserves the fabricated raw Ethernet/IP/TCP mechanism.
Finite local qualification does not prove universal optimality, arbitrary
firewall/NAT behavior, low latency on every saturated link, or multi-day
endurance. The 1/100 Mbit/s mixed runs still show p99 bounds of 2.097–4.194 seconds.
See [BENCHMARKS.md](BENCHMARKS.md), [DIAGNOSTICS.md](DIAGNOSTICS.md), and
[TRANSPORT.md](TRANSPORT.md) for evidence, interpretation and the packet contract.

Four separate uncommitted files are preserved and excluded from this candidate:
`internal/conf/kcp.go`, `internal/engine/config.go`, `internal/engine/peer.go`, and
`internal/engine/relay.go`. They add encryption aliases/unencrypted operation,
change opening-timeout handling and impose a five-second half-close read deadline.
The generic `build/super-paqet` was rebuilt with separate workspace changes;
use the qualified named binary for reviewing these results. Incorporating those
edits requires their own review and relevant qualification before deployment.

## Subsequent real deployment

The initial five-host null/2052 deployment installed successfully but failed
real-link forwarding. The excluded backend/9002 direct route remains unchanged.
A four-host recovery now uses null/29999 with a conservative legacy profile.
Twenty authenticated small requests passed through persistent 9001/9003 on both
clients; both public-domain ports passed from the laptop. However, three of
four 1 MiB downloads stalled, with successful direct backend controls. AES did
not resolve sustained transfers. Recovery is partial and diagnosis continues;
this is not real-WAN production qualification. See [DEPLOYMENT.md](DEPLOYMENT.md)
for exact settings, hashes, rollback backups and controlled evidence.
