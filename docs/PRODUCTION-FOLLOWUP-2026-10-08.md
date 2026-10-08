# Production follow-up — 8 October 2026, 10:22–10:29 UTC

All five original tunnel processes remained active with **zero automatic
restarts**. No tunnel cgroup OOM, memory-limit or task-limit event was recorded.
All twelve authenticated 1 MiB Reality downloads passed: two on each of the six
client/country routes. Closing checks confirmed unchanged tunnel and Xray process
identities, executable/config hashes, warning logs, disabled profiling, valid
configs, healthy metric endpoints and active observers. Temporary probes were
removed. No tunnel config, binary, Xray service, host queue or firewall behavior
was changed in this follow-up.

The downloaded metric/journal archives span **7 October 19:14 to 8 October
10:22–10:23 UTC**, about fifteen hours. Closing identity/health checks completed
at 10:29 UTC. The full observer window still ends at **19:14:08 UTC / 22:44:08
Tehran**. This is not a completed day-long or incident-free stability result.

The runtime remains migration.4, source
`90e98b1186646c3c56660616309e5fe5573e4208`, SHA-256
`633750998daa210ca8499ad5081bcb51a775b5c6e713758d709313606f37bd0f`.
All six paths use four independent sources, adaptation enabled, S outbound/PA
return and null encryption. The locally qualified recovery-grace and receive
optimizations are **not deployed**. [Compact receipts](production-followup-2026-10-08.json)
retain timestamps, aggregates, event history, closing identities and authenticated
results. Private raw archives remain in `build/production-followup-20261008/`.

## Customer-load observations

CPU is successive cgroup CPU deltas divided by elapsed time; 1.0 means one busy
core. Traffic is accepted relay bytes in both directions, not wire rate or tested
link capacity. RSS covers the tunnel process, excluding the observer and other
services. The longest adjacent metric interval was about 10.16 seconds.

| Host | Peak active forwards | Peak RSS, MiB | Mean tunnel CPU, cores | Mean / peak relay Mbit/s |
|---|---:|---:|---:|---:|
| Client .14 | 147 | 47.3 | 0.120 | 0.25 / 8.86 |
| Client .118 | 3900 | 178.9 | 1.004 | 51.88 / 326.44 |
| Germany | 2425 | 103.0 | 0.218 | 30.05 / 227.22 |
| France | 2172 | 74.3 | 0.119 | 8.26 / 93.04 |
| Finland | 1231 | 112.0 | 0.183 | 13.95 / 287.06 |

These are different traffic mixes on different hosts. They do not establish
throughput per core or justify linear extrapolation to multi-gigabit traffic.
Client .118 remains the largest measured tunnel CPU consumer. Existing profiles
identified syscall/futex and protocol work; the receive optimization attacks a
measured receive cost without changing production's adaptive decisions.

## Outages and recovery evidence

The longer history retains the France events already identified in the
[initial review](PRODUCTION-REVIEW-2026-10-08.md): the simultaneous 22:01 UTC
interruption, another .118 nonpreserving recovery at 23:11 UTC, and the 00:17–00:20
opening-failure/probe sequence. Some original sessions disappeared before a
successful move, so those connections could not be preserved. Candidate grace
addresses the bounded watchdog-lifetime issue; it cannot prove or eliminate the
external cause of delivery interruption.

There were **no new recorded recovery attempts or error-counter increases after
the 00:55 UTC review through archive collection**. Client .118 did record another
**576 transport opening retries**, with no eventual opening errors. This statement
uses metric deltas as well as journal messages: quiet warning logs alone would
not prove the absence of an outage. At closing, healthy metric endpoints and all
authenticated routes passed. Short unobserved disturbances between ten-second
samples remain possible.

Across the full collected window, .118 recorded 18 recovery attempts, five
successes and 13 rejected candidates. Client .14 recorded seven attempts, two
successes and five rejections. Only France had recovery events. Some rejected
candidates have no cause in the deployed warning logs; seven probes around 00:20
were discarded without adopting a source. The newer candidate's first-cause and
fallback diagnostics are intended to distinguish revived original paths,
generation changes and session expiry. This review does not retroactively assign
a cause that migration.4 did not record.

Client .118's full-window opening error total is 1982 and transport retries total
7636. Warning sampling produced only 20 `connection.failed` messages; that count
must not be mistaken for the total failures. Those failures occurred in the
earlier France interruption sequence. No admission rejection or dropped-log
counter increase was recorded. Aggregate relay-abort counts include customer
socket departures and do not by themselves establish tunnel outages.

### New actionable finding: openings behind a full mux receiver

The 576 later retries all belong to **Finland session 1**, conversation
1797257986, source port 45938. From about 09:44:45 through 09:46:15 UTC,
the client recorded `mux_receive_blocked = 1` with **4,259,813 buffered bytes**
against its 4,194,304-byte shared budget. The bounded overshoot is one admitted
frame, which the existing mux contract permits. The remote KCP window remained
4096 and KCP RTT about 100–114 ms; the source and conversation did not change.
The receiver became unblocked and retries stopped without path recovery.

This supports **local application/mux backpressure delaying new opening replies**,
not a failed physical tuple. KCP progress alone cannot tell whether the ordered
mux reader can parse an application receipt. Existing retry selection found
siblings, explaining why eventual opening errors did not increase, but choosing
the blocked lane first adds avoidable delay. The warning logs do not identify
which customer stream stopped reading; that detail is not inferred here.

The candidate now samples the existing mux-blocked flag and remote zero-window
state at the ordinary controller cadence. New openings prefer an unblocked
equally healthy sibling before comparing original cached bulk/count pressure.
It does not close a full lane, rotate its source or migrate established streams.
When every lane is blocked, original pressure ordering supplies a bounded
fallback. This policy is being qualified locally and is **not deployed**; see
the receive investigation for its experiment history.

## Finland transmit pressure: investigate, do not enlarge queues blindly

Finland's driver transmit-drop counters increased by **8159 datagrams** across
both capture workers, with 72 ten-second intervals meeting the observer's
drop-trigger threshold. Capture drops remained zero on every host. Germany,
France and both clients recorded no transmit-queue drop growth.

Finland emitted about 76.54 million KCP segments during this window; the driver
drop count is roughly 0.011% of that segment count. This is contextual only:
driver datagrams and KCP segments are different counting units, and bursts can
matter more than the whole-window ratio. There was no Finland path recovery or
connection-error counter increase. KCP retransmissions continued handling loss.

Captured host queue reports show root `fq` with `flow_limit 100` and rising
`flows_plimit` drop counters. They are host-wide, including Xray/WARP and other
traffic; they cannot attribute every queue drop to the tunnel. Snapshots often
show an empty backlog after a burst, which does not disprove transient pressure.
The existing three bounded ENOBUFS retries remain in use. Requests to sleep
50 microseconds may sleep longer under scheduler pressure.

No queue enlargement, longer retry budget or new global pacing was deployed.
Those changes could trade fewer drops for more tail latency, block other writes
or interfere with unrelated services. Any follow-up should reproduce short
per-flow queue pressure on an isolated instance, compare tail latency as well as
goodput, preserve partial-send ownership, and verify that persistent pressure
remains recoverable datagram loss. This is a measured improvement opportunity,
not evidence of a present process-crash defect.

## Collection errors and limits

The first France archive attempt failed because a bounded diagnostic subprocess
exceeded its 20-second timeout. A retry with a 60-second subprocess budget
succeeded; the tunnel process identity and health were unchanged. This was an
inspection failure, not a tunnel restart. All five archives were parsed locally;
the collector was already running independently on each host.

No new customer payload capture or runtime profile was taken. Download probes
generated only their own authenticated test traffic and never changed the
customer Xray configuration. The observation does not certify untested NIC
capacity, every firewall condition, future path stability or a full 24-hour pass.
