# Opening and local queue investigation — 9 October 2026

This is a **local candidate**, not a production deployment or completed
qualification. Production remains latency.1. Its
[17.6-hour review](PRODUCTION-REVIEW-2026-10-09.md) identified ten opening errors
coincident with Finland receive backpressure and 45,876 local transmit queue drops.

The existing cached admission hint handles a lane already full at selection.
It cannot free an opening that was selected earlier and received ACK2 before
another stream filled that carrier's ordered mux receiver. Blindly retrying that
acknowledged request could dial the target again. Increasing buffers would only
move the same boundary and could increase memory and latency.

The candidate introduces PTCP3/PUDP3 inner requests with a 16-byte random identity.
One listener-generation registry retains one target dial across retry attempts.
The winning stream sends a one-byte commit before target relay reads begin.
Failed/uncommitted carrier streams therefore cannot consume target banners or
close a target being reused by another attempt. A committed identity remains a
replay tombstone for the original server opening budget. No established relay is
stored in this registry. A failed commit is never retried because the write might
already have reached the server. Target rejection is never treated as a physical
path failure. Legacy PTCP2/PUDP2 handling remains for older clients; new clients
require upgraded backends first.

Only unfinished acknowledged openings check their receipt read at most every
250 ms. A full receiver with an eligible sibling causes a same-identity retry;
a healthy slow target retains its whole dial budget. Existing carrier selection,
outer raw TCP encoding, S/PA flags, KCP/mux data ordering and established relays
remain. Pre-receipt EOF/closed-pipe failures also retry an eligible sibling.
Capacity retries do not add tuple suspicion or trigger source migration merely
because a consumer is slow. Final failure messages include receipt stage,
conversation and local receive-buffer state.

Receipt storage is capped at twice the configured connection admission ceiling.
One engine timer and expiry heap retire pending targets and replay tombstones;
there is no retained timer or goroutine per committed relay. Registry occupancy,
pending targets, capacity retries and reused targets have explicit metrics.
This adds handshake bytes and transient per-opening memory/locking, so churn,
CPU/RAM and throughput qualification are mandatory before any rollout.

Initial whole-application race tests pass. Five race-instrumented repetitions of
the focused tests verify a receiver filling **after ACK2**, one healthy-sibling
retry, exactly one target dial, identical winning relay bytes, original held
stream payload continuity, no tuple-suspect flag and owned handler cleanup.
Registry tests also cover conflicting identities, one-time claim, delayed replay,
capacity, expiry, failed dial replay and late dial completion after retirement.
Receipts are in this worktree's `build/opening-fix-20261009/`.

Queue investigation remains open. The namespace fixture can now install an
isolated root fq with explicit per-socket flow_limit/maxrate, excluding unrelated
netem schedules. This provides an actual Linux ENOBUFS reproduction without
changing a host or production qdisc. No queue fix or benchmark improvement is
claimed at this checkpoint.

## Second checkpoint: queue-pressure candidate

The first baseline fixture attempt stopped at argument validation because the
new worktree lacked the local iperf helper. No workload ran; its failure receipt
remains. Copying the unchanged helper into this worktree allowed the reproduction.
The exact deployed binary (`256cfc65…`) produced 23,354 backend transmit drops
with fq flow_limit 100 and maxrate 50 Mbit/s per flow. This is an isolated socket
queue limit, not a claimed 50 Mbit/s aggregate WAN.

A local candidate shrinks only wholly rejected sendmmsg prefixes on ENOBUFS:
64 → 32 → 16 → 8, with the existing three waits and no healthy-path delay.
Partial sends return immediately. Persistent pressure discards/counts only the
last rejected prefix; KCP's existing batch cursor retains the untouched tail.
No accepted datagram is replayed and no queue error tears down a carrier.

The first candidate run (`c46cda9a…`, opening plus queue change) produced
12,228 drops and 120.30 Mbit/s download versus the baseline's 94.85 Mbit/s.
Client/server mean CPU was 0.147/0.204 cores versus 0.172/0.190. Cold 16 MiB
transfer was 3.486 s versus 3.276 s: retain this adverse startup result. These
single runs are exploratory, not acceptance or a production capacity claim.
The pressure branch can spend more aggregate time sending a rejected batch's
tail, so mixed latency and repeated comparisons remain required. No pacing,
outer encoder, flag cycle, socket ownership or production qdisc is changed.

Listener-generation cleanup now cancels reservation and removes its unfinished
targets immediately, rebuilding the expiry heap only on retirement. Other
listeners and relay-owned sockets remain independent. Race tests cover
concurrent claims (one winner among 32), reload retirement, a healthy 600 ms
target dial with no retry, and a target banner surviving late receive blockage.
Three repetitions of the expanded opening race cases and the socket race suite
pass. Whole-suite and namespace performance qualification remain pending.

## Costs and explicit limits

Opening identity/commit adds 17 inner control bytes and bounded transient cache
storage. Server-first protocols must wait for the commit before their banner is
read; banner arrival can therefore gain approximately one carrier RTT. Client-first
request bytes follow the commit on the same ordered stream. This is a correctness
tradeoff, not a claim that every protocol's startup latency is unchanged. Failed
commits are terminal; established application exchanges are never replayed.

The replay guarantee lasts through the original server opening budget. A request
arriving after its tombstone was retired can create an uncommitted dial, but a
well-behaved client's already-expired opening cannot commit it. This is not durable
exactly-once execution, authentication, or an unbounded replay cache.

A read-only fleet refresh at 16:35 UTC confirmed the same five runtime hashes,
PIDs, unchanged Xray identities, healthy endpoints and zero restarts. Opening
errors remained ten; .118 transport retries rose from 3,387 to 3,391. Finland
transmit drops rose from 45,876 to 46,021. Original private refresh receipts remain
in this worktree's build directory. No customer host was changed.

The first unpinned short clean pair was adverse (upload 7.239 → 5.445 Gbit/s,
download 2.979 → 2.704); it is retained, not acceptance. First churn was 10,102 →
10,008 requests/s with backend peak RSS 39.6 → 113.2 MiB, consistent with temporary
receipt/tombstone storage under rapid churn. Mixed screening stopped because the
initial copied benchmark helper lacked the new `-warmup` argument. This was a
fixture failure, not a successful latency result. The current helper was rebuilt
from cmd/bench. The first 60-second-warmed, pinned-carrier/CPU-role bulk pair
measured 5.338 → 5.403 Gbit/s upload and 4.162 → 4.204 download, both workers
active. Reverse-order, latency and WAN qualification remain pending.

## Coalescing investigation after the failed mixed gate

The complete two-pair placed comparison retains bulk medians of 5.400 →
5.436 Gbit/s upload and 4.195 → 4.180 download. Churn medians are 12,720 →
12,377 requests/s. The mixed mean gate **fails**: 1,119 → 1,193 microseconds
(+6.6%), despite p99 upper-bound medians improving from 8 to 7 ms and equal
1 Gbit/s each-way offered traffic. This candidate is not accepted for rollout.

The next candidate retains queue retries and opening identity/ownership, but
coalesces the commit with up to 4,095 already queued TCP application bytes after
target readiness. One nonblocking local TCP read cannot wait for application
data; an empty/server-first socket sends the ordinary commit immediately. It
borrows a 4 KiB pool class only for this attempt, releases it before an empty
commit, and returns populated scratch after mux payload ownership transfers.
Only accepted application bytes enter the existing relay counter. Failed reads
or commits are terminal; no coalesced application bytes are replayed on retries.
UDP/generic stream callers retain the ordinary commit path.

Five repetitions of expanded opening race cases pass with the coalescing code.
Tests verify the real late-backpressure sibling retry with/without queued client
bytes, a retained target banner, exact hello/reply payloads and one target dial.
Separate real TCP tests cover empty sockets, directional EOF, closed sockets,
bounded prefix consumption, untouched unread tails and short-write accounting.
Performance qualification for this new candidate remains pending.

The first coalescing prototype has two retained failures. It could exceed smux's
512-byte priority cap on large first requests; no production rollout occurred.
The next edit caps the combined prefix at 512 (511 application bytes), and adds
a real-mux 8 KiB first-request test plus a priority-limited mock. Its remaining
TCP tail stays unread for normal relay. The initial coalescing benchmark is also
unaccepted: mixed mean medians 1,103 → 1,160 microseconds (+5.2%), p99 upper
bounds 8 → 11.5 ms. Churn control repetitions were 17,722 and 12,776 requests/s,
candidate 12,685 and 12,861; no consistent churn speedup is claimed. All original
receipts remain. Neither the large-prefix fix nor coalescing is deployed.

The namespace fixture now accepts optional `--open-timeout` / `--dial-timeout`
so later retention/capacity checks can match production's 15s / 5s. Omitted
options preserve its historical 10s / 5s defaults. Application config validation
checks the resulting durations. Existing comparison deadlines remain unchanged.

The next local candidate removes a channel allocation from healthy opening
tombstones: only a replay joining an unfinished dial allocates a shared wakeup.
Waiters copy the channel under the registry lock; publication/expiry close it
and release the ticket's reference. Fresh synchronous dials need no extra wait
lock. Expiry now processes at most 128 receipts per critical section and yields
between groups, retaining complete cleanup while allowing live claims/dials to
interleave. Shutdown retains full bounded-map cleanup without unnecessary yields.
Five repetitions of the expanded opening race cases pass, including publication
racing 32 waiters, one concurrent claim winner, a 1,024-entry expiry burst and
large-preface handling against the actual mux. Performance evidence is pending.

## Lazy-wakeup comparison and clock evidence

The queue4 candidate (`bc876ee4…`, source `1e15632`) passed the complete
application race suite and vet. Its two mixed comparisons retain mean-latency
medians 1,118 → 1,115 microseconds and p99 upper-bound medians 6.5 → 7.5 ms,
within the declared +5% mean / +2 ms p99 limits. These are finite observations,
not proof of a universal speedup. Clock samples show the first mixed candidate
ran at higher sampled client/server clocks than its control, so causal claims
remain unqualified. Full receipts and one-second frequency/CPU-tick samples are
in `comparison-queue4-paced/`.

The first churn pair measured 17,229 → 12,529 requests/s, while the reverse pair
measured 12,975 → 12,493. Preserve both. Churn had no warmup: the first control's
average sampled client clock was about 3.61 GHz versus 2.53 GHz for its candidate.
Those instantaneous clock samples do not prove all variation came from clocks.
They do show why an unmatched cold pair cannot establish a runtime regression
or improvement. A separate `--http-warmup` fixture option now keeps startup
requests/bytes/errors in receipts, measures latency by request start after warmup,
and rejects warmup errors. CPU accounting covers the complete workload, including
warmup. Existing defaults remain unchanged.

The next runtime edit replaces nested target-dial deadline contexts with one
context enforcing the earlier of the opening and dial deadlines. Parent
cancellation is retained. This removes an extra timer/context and child map on
the healthy opening path; its performance effect must still be measured. The
metrics response also sets its Prometheus Content-Type before writing the body.
No production host has been changed.

## Warmed queue5 checkpoint

Source `ee24e70`, executable
`7787e9be748549f6917466481629f7658f4034dcb6c542b7c10f098eada8fbc8`,
passed the application race suite and vet. Four independent source carriers, two
capture workers, client CPUs 0–3, backend CPUs 4/6 and generator CPUs 8–11 were
retained. Each of eight alternating runs had 60 seconds warmup and 20 seconds
measurement, zero warmup/measurement errors, exact verified cold payload and
clean firewall/process teardown. This laptop is an i5-13420H with 12 logical CPUs.

| Warmed medians | Deployed baseline | Candidate |
|---|---:|---:|
| Churn connections/s | 14,007.7 | 13,757.0 |
| Churn mean latency | 4,567.3 us | 4,649.9 us |
| Churn p99 upper bound | 10 ms | 11 ms |
| Mixed mean latency | 924.1 us | 906.2 us |
| Mixed p99 upper bound | 5.5 ms | 4.5 ms |

Churn is 1.8% slower, within the 5% throughput/mean limits; p99 adds 1 ms, within
the +2 ms limit. Mixed traffic retains approximately 1 Gbit/s in each direction.
Both mixed pairs independently satisfy the latency limits. First churn sampled
clocks closely match, but reverse mixed clocks vary by about 7%; preserve the
clock observations and do not interpret aggregate latency as a universal speedup.
The opening ownership cache has a measurable cost: server peak RSS under this
continuous churn is about 136.8 MiB versus 40.6 MiB. This is bounded transient
receipt retention, not a zero-cost change. Queue pressure, held-connection, WAN,
recovery and reload gates for this exact executable remain incomplete.

A read-only 17:42 UTC production refresh still finds original PIDs/hashes and no
restarts on all five hosts. Client .118 has ten errors and 4,086 retries; Finland
queue drops total 46,360, with zero capture drops. Other service and Xray identities
remain unchanged. Local observer changes add capacity-retry/reused-target counters
to summaries and incident triggers, with legacy missing metrics/reset handling;
13 observer unit tests pass. Neither the observer nor runtime changes are deployed.

## Pressure, capacity and recovery follow-up

The exact queue5 executable verifies all 10,000 held forwards in both control and
candidate runs, plus 100,000 in a candidate-only capacity run (7.70 s ramp). Every
held socket is rechecked, not merely counted. The 100k run peaks at about 1.67 GiB
server / 1.72 GiB client RSS and records nonzero swap on this laptop; this is an
integrity/capacity result, not simultaneous saturated-traffic latency acceptance.
Functional checks cover multiple clients/listeners, UDP and directional EOF.
Repeated migration preserves all four established sequenced 64 KiB streams and
logical carriers with zero healthy-peer errors. A 70-second failed-probe fixture
also preserves all four using negotiated 60-second recovery grace. Live reload
passes 64 active streams, four carriers, two capture workers and three edit cycles.
All owned rules/processes/namespaces are cleaned. Original receipts remain under
`followup-queue5/`.

Short, unplaced fq download runs are adverse (medians 133.96 → 126.56 Mbit/s),
despite fewer drops; variable placement is not erased or treated as success. The
first controlled-fq launch stops before load because its generated output literal
still points to an existing directory; mkdir prevents overwriting it. The corrected
fixture uses fixed source ports, pinned equal carrier populations, separate CPU
roles, 20 seconds warmup / 30 seconds measurement and production 15s / 5s budgets.
Controlled download medians are 135.18 → 136.77 Mbit/s; first-pair drops fall from
331,483 to 50,744. Both receive workers are active.

The controlled mixed-pressure gate **fails throughput**: median combined bulk is
258.94 → 222.29 Mbit/s (about −14%), although mean HTTP latency falls from 2.655 s
to 0.936 s and p99 upper bounds from 4.194 s to about 1.415 s. Zero generator errors
and clean teardown do not override that regression. Queue5 is not deployed.

A serialized 1,000-pair local wait experiment (`queue-wait-timing/`) measures the
50 us Go sleep at mean 1,048.8 us versus kernel nanosleep at mean 102.1 us. The next
Linux-only candidate uses a best-effort kernel yield after ENOBUFS; interruptions
end the yield, and admission/retry bounds remain unchanged. It does not spin, add
healthy-path syscalls or promise a strict 50 us deadline. Blocking syscall thread
use and pressure/bulk/latency outcomes must be qualified before promotion.
