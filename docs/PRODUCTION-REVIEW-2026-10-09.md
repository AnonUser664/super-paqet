# Production log review — 9 October 2026, 15:46–15:49 UTC

All five latency.1 tunnel services remain active/enabled with the original rollout
PIDs and **zero automatic restarts**. Running/disk hashes still match
`256cfc65daaa6596ff2b31e2863296a3182975faa71b1a746295cc6172231958`, source
`276c43a70873670a7e1665283620aba38594b1ab`. Config hashes and backend Xray process
identities match the accepted deployment. All loopback health endpoints respond.

The record is **not incident-free**: France had carrier recovery events, and busy
client .118 accumulated 3,387 opening retries across routes plus ten opening errors
in Finland warning bursts. Full-window tunnel memory/task cgroup event counters
show no OOM, memory-limit or task-limit event; no sampled tunnel swap or capture
drop was recorded. Every ten-second observer remains active with zero restarts.
No process, binary, config, firewall, host queue or Xray setting was changed in this
review. No load generator, packet capture or new authenticated route probe ran.

Archives span 8 October **22:12 UTC** through 9 October **15:48–15:49 UTC**, about
17.6 hours, with 6,310–6,340 samples per host. All available rotated sample files
were parsed in chronological order. No metrics-unavailable sample was found; the
largest adjacent interval was 10.164 seconds. Journals were parsed separately and
matched by process identity so rollout warnings from old client processes are
not presented as new-runtime incidents. The full observation deadline remains
9 October **22:05:41 UTC**; this is not a completed 24-hour pass.

[Compact review receipts](production-review-2026-10-09.json) retain exact identities,
resource and counter aggregates, incident sample metrics, current-process journal
events and inspection limits. Private first-read snapshots and fuller retry
interval receipts remain under `build/production-review-20261009T1545Z/`; the
original rotating archives remain on each host under
`/var/log/super-paqet-watch/latency1-20261008T220541Z/`.

## Observed resource use

CPU uses successive tunnel cgroup CPU deltas divided by elapsed time; 1.0 means
one busy core. Traffic is accepted relay bytes in both directions, not wire rate,
link capacity or a comparative performance benchmark. RSS excludes Xray and the
observer. These traffic mixes differ and do not justify extrapolating capacity.

| Host | Peak active forwards | Peak RSS MiB | Mean tunnel CPU cores | Mean / peak relay Mbit/s |
|---|---:|---:|---:|---:|
| Germany | 2,792 | 132.1 | 0.239 | 38.71 / 314.55 |
| France | 1,874 | 83.0 | 0.124 | 10.17 / 175.01 |
| Finland | 1,522 | 83.7 | 0.206 | 17.76 / 344.91 |
| Client .14 | 161 | 117.1 | 0.107 | 0.80 / 266.41 |
| Client .118 | 4,776 | 275.0 | 1.064 | 65.68 / 328.89 |

Client .118 peaked at 4,776 simultaneous forwards with about 275 MiB tunnel RSS;
its largest ten-second tunnel CPU interval was 1.706 cores on the four-vCPU host.
Tunnel memory and task limits were not reached. Host CPU pressure was nevertheless
present during the Finland failure windows: sampled `some avg10` was about
24–34% and `full avg10` about 7–9%. These host-wide readings include other services
and do not isolate a tunnel defect or prove CPU pressure caused the failures.

## France recovery events

At **8 October 23:29 UTC** (9 October 02:59 Tehran), both clients recorded France
carrier trouble. Client .14 session 1 recovered from source 49386 to 44464 at
23:29:44.607, about 2.719 seconds after its probe started; the log explicitly
reports `connections_preserved: true`. Its session 3 had a timed-out probe and then
a closed-pipe probe failure; the original session closed on keepalive timeout at
23:30:11.254. That carrier did not have a successful preserving recovery in the
record, although .14's opening error counter stayed zero.

Client .118 session 1 first timed out probing, then succeeded on its next attempt
at 23:29:58.358, changing 55261 to 39533 with connections preserved. That was about
15.439 seconds after the first probe, not a duration measured from the interruption
itself. France's backend recorded six stream-control read timeouts during this
sequence. Neither service restarted.

At **9 October 04:13 UTC** (07:43 Tehran), .118's same France carrier changed
39533 to 44402, again with connections preserved. The successful probe took about
0.369 seconds after its start. No further client recovery attempts were recorded
through the collected window. Some backend `session.closed` events occur roughly
one minute after probes; logs alone do not distinguish every expired temporary
probe session from an application carrier, so they are not all counted as separate
customer outages.

France had three preserving recoveries across the clients, two rejected probes
on .14 and one rejected probe on .118. Simultaneous trouble on the same destination
is consistent with a path interruption, but the record does **not** establish
filtering, censorship or the precise external cause. There is no logged
`path.migration_early_stall` event proving an immediately blocked adopted tuple.

## Opening backpressure remains actionable

Client .118 accumulated **3,387 transport opening retries** across multiple routes
and **ten final opening errors**. Germany and France backend error totals require
separate interpretation: Germany's seven were already present when observation
began, with no later growth; France's six grew during the 23:29 carrier incident.
Finland's backend opening error count remained zero.

One .118 error appeared at **12:01:01 UTC** (15:31 Tehran), followed by nine more
in the **12:05:42 UTC** burst. Sampled warning messages name Finland; warning
sampling emits only the first five errors, so the metric count of ten must not be
replaced with the journal-message count of five.

At both error intervals, Finland session 1, conversation 2566316826/source 53007,
had **4,259,813 buffered receive bytes** against its 4,194,304-byte mux budget,
`mux_receive_blocked = 1` and `carrier_opening_blocked = 1`. The bounded overshoot
is one permitted admitted frame, not unbounded memory growth. Other sampled
Finland carriers were unblocked, and no Finland source-port recovery occurred.
This supports application/mux backpressure contributing to delayed opening replies;
it does not prove the cause of every failed or retried opening. The ten-second
samples do not identify the individual slow-reading customer stream or the carrier
chosen by each opening.

Across all .118 retry intervals, 1,300 retries coincided with a sampled mux-blocked
state; the other intervals had no such state at their sampling instant. Some large
retry bursts occurred without a sampled block, so all 3,387 retries cannot be
attributed to the same buffer or to Finland. The existing adaptive admission hint
was active and correctly reported the blocked lane. Its presence has not eliminated
this behavior, and previously assigned openings can still wait behind data.

At the live check, all clients' suspect, pending-recovery and opening-blocked gauges
were zero; all sampled session receive-blocked gauges were also zero. Errors had
not increased after the 12:05 burst, while opening retries continued intermittently.
The concrete follow-up is an isolated slow-consumer test with concurrent new
openings, covering requests already assigned before a carrier fills, selection of
unblocked siblings, control-reply delay and bounded retry latency. This review does
not deploy a speculative buffer increase or source-port rotation for backpressure.

## Finland transmit queue pressure

Finland recorded **45,876 driver transmit queue drops** across its two workers
(13,460 and 32,416). Capture drop growth was zero on all hosts. France had 41 transmit
drops; Germany and the clients had none. Finland's host root `fq` has a per-flow
limit of 100 packets, with a cumulative `flows_plimit` drop count of 70,682 at the
live read. This host-wide qdisc count includes other services and is not a count
of tunnel-only losses during the observation window.

Finland's protocol OutSegs counter was about 111.1 million at collection. The driver
drop count is roughly 0.041% of that segment count, but datagrams and protocol
segments differ and bursts matter. There was no Finland carrier recovery or backend
opening error growth. Queue pressure remains an optimization opportunity; enlarging
queues blindly can increase latency and affect Xray/WARP traffic. An isolated queue
burst test should verify pacing, bounded ENOBUFS retry and tail latency together.

## Inspection limits

The France kernel journal subprocess exceeded its 15-second budget; the failed
read is retained in the receipts, not converted into a clean kernel result.
France's full sampled tunnel cgroup memory/task event counters are zero. Other
hosts' queried warning journals contain no OOM evidence; .14 includes one audit
callback-suppression message. No new packet trace or customer payload was collected.
Healthy loopback endpoints and passive traffic do not replace authenticated
end-to-end route checks. Source and config remained unchanged throughout this
read-only review; it is not a new deployment or universal production acceptance.
