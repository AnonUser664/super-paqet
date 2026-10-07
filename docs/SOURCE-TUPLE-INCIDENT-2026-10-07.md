# Live source-tuple outage, 7 October 2026

Client 89.45.68.118 → France was failing during investigation. The ownership.2
process remained PID 955116, without restart or recorded OOM. Germany continued
carrying traffic. Client .14 could establish new France carriers while .118 could
not. .118's France opening timeouts started around 04:13 UTC (07:43 Tehran);
The first retained Finland opening timeout was 01:12:38 UTC (04:42 Tehran). The current episode therefore
does not show simultaneous failure of every route or establish the cause of
other reported episodes.

Two paired, bounded interface captures found .118 sending raw S packets from
29997 to France port 29999, with no corresponding packets at France's net0.
The repeat used tcpdump immediate mode, a broad host/TCP-or-ICMP filter and a
larger buffer. Its 30-second captures recorded 95 packets on .118 and zero on
France, with zero capture drops on both. The initial 20-second capture recorded
72/zero packets. Private header-limited PCAPs remain under build/incident-20261006;
customer payload is not published. Configured interfaces, source addresses,
next-hop MACs and owned firewall rules matched the live route/neighbor state.

Ordinary .118 → France TCP connections to 22 and 2096 succeeded in approximately
95 and 90 ms. Three ICMP replies averaged 90 ms. A TCP connect to 29999 timed out,
but that alone is inconclusive: the raw tunnel deliberately drops kernel TCP
handling on that port.

The unchanged binary and reliability/encryption/flag configuration were tested
in an isolated temporary client instance with source port 29995, the same France
listener 29999, and forward 19003 to the same Reality service 2096. An authenticated
Reality/VLESS request downloaded 1 MiB successfully. Requests on the old live
9003 route had repeatedly timed out. Only France's source port was then changed
live to 29995. No main tunnel restart occurred, and Germany's peer was retained.
The authenticated public 9003 check passed afterward in 1.63 seconds.

The analogous isolated Finland test, using source 29994 and forward 19002,
also downloaded 1 MiB successfully. Finland's source was subsequently changed
live from 29996 to 29994. All six public client/country combinations then passed
an authenticated 1 MiB request. S outbound / PA return, null encryption,
adaptation, four initial/maximum shared-source carriers, destinations and the
executable remained unchanged. Temporary instances were stopped. Diagnostic
restore baselines and hash guards were updated without resetting their timers.
Main Xray was not modified.

This establishes a failure tied to the old source/destination tuple during this
episode. It does not locate the dropping device, prove censorship, identify the
trigger, or guarantee a newly chosen port cannot later be affected. KCP session
recreation alone reuses the same source tuple and cannot repair such a drop.
`session.connected` currently describes successful *local* carrier setup, not a
received peer response; RTT zero and repeated unanswered opening attempts are
consistent with that distinction. Safe automatic source-tuple recovery needs a
verified fresh-path probe before switching, protection for any progressing
carrier, bounded retries, scoped firewall cleanup and reload-race handling.


## Recovery implementation and qualification

`enterprise-2026.10.07-recovery.1` adds opt-in verified source-tuple recovery to
outgoing shared peers. Three failed transport opening attempts and 30 seconds
without progress permit a fresh-source PPING/PPONG probe. Any progressing
carrier protects its pool. Failed probes keep the old pool; commit also checks
concurrent reload, live reliability edits and cancellation. The configured source
remains the restart baseline; successful recovery exposes its transient effective
port in metrics. Independent peers and forwarding binds retain identity.

The exact release hash is
`5718f1c4e87f3e91bb9ca2399d3debdf5c7e4b3f8773e40cc8ebc2b94f757465`.
A persistent source-tuple drop recovered about 38 seconds after injection, with
zero independent-peer failures and preserved carrier identities. A 65-second
whole-peer fault produced three failed probes, preserved the old pool, then
resumed after link restoration. A single conversation drop avoided replacing
progressing siblings. Fixed-four ceilings, config reload, owned-rule cleanup and
unrelated-rule preservation passed. Root race/vet and 30 repetitions of the
health/transaction tests passed, including live reliability edits during a probe.
Clean null bulk was 4.534/4.831 Gbit/s upload/download; a 20-second changing-delay
128-worker churn run completed 10,535 requests with zero workload errors. These
are single local samples, with the production transport parameters; ordinary
benchmarks use PA/PA while the outage fixture and public routes use S/PA.

The shorter whole-peer fixture restored connectivity before it exercised a
failed probe. It resumed correctly but was insufficient for that assertion; the
expanded 65-second blackout supplied the required probe-failure evidence. Probe
retry/timeout in the deterministic fixture were 10s/3s; production uses 30s/5s.
Full receipts and limitations are in
[path-recovery qualification](path-recovery-qualification-2026-10-07.json).


## Accepted client rollout

Both clients now run recovery.1 with recovery enabled on all three outgoing
peers. Client .14 was upgraded and checked first, then .118. Each client had one
tunnel restart; existing streams on that client ended and needed reconnection.
Backend tunnel PIDs and all three main Xray PIDs stayed unchanged. Recovery
archives and diagnostic guards remain, with the original monitoring deadline.
Conditional rollback timers were disarmed after acceptance.

All six public routes passed authenticated Reality requests to example.com.
Each also passed SHA-256 verification of a 1 MiB upload and a 1 MiB download,
using temporary synthetic HTTP origins on a different backend. The origins were
stopped and their scripts removed after testing. Transport opening-error counters
remained zero after client replacement through the final inspection.

The first Cloudflare post-deployment check timed out on five routes and passed
one. Loopback-only synthetic targets timed out, and France self-destination
checks timed out despite the same France origin working through Germany and
Finland. These were retained as failed comparisons, not accepted successes or
proof of a tunnel regression. Cross-backend origins and another HTTPS site
provided independent acceptance. The precise external/self-target failure causes
were not investigated further, and Xray settings were not changed.

This repair does not guarantee immediate recovery of every type of filtering.
A sustained whole-path outage fails the fresh-source proof and preserves the old
pool. Changing a verified failed source tuple ends streams on that failed peer;
applications still need reconnect support. Full-day production observation is
unfinished and continues under the original finite collectors.
