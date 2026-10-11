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

## Queue experiment isolation

The kernel-yield candidate (`b08160d`, executable `7eee1040…`) passes race/vet but
its controlled mixed throughput gate still fails: combined bulk medians about
256.15 → 231.60 Mbit/s (−9.6%). Mean latency improves, while the p99 upper bound
stays at the coarse logarithmic 4.194 s upper bound. The idle wait measurement is not proof
that every Go timer under concurrent I/O takes a millisecond. Full receipts remain
in `fq-fixed-queue6/`; no production update occurred.

The next candidate restores the exact deployed transmit queue policy and tests the
opening correction in isolation. Queue counter growth is recoverable local loss,
not itself a process crash or proof of customer delivery failure. A lower drop
counter does not justify a throughput regression. Both smaller-prefix and kernel-
yield experiments remain in Git history for later investigation; neither is being
promoted on the strength of incomplete/failed gates. Recovery grace and opening
correctness remain separate qualification decisions.

The opening-only first pressure pair restores combined throughput to within about
2% of its control, but short runs suggest higher mean request latency. These
30-second runs contain few successful requests; longer measurement is required.
The earlier description of 4.194 s as a histogram ceiling was incorrect: it is a
logarithmic upper bound after the old 2-second fine range. The fixture now extends
its millisecond buckets through the HTTP client's 30-second timeout. Traffic and
request deadlines stay unchanged; this adds about 224 KiB to the load generator,
not to the tunnel. Old helper hashes/receipts remain, and both comparison versions
will use the same newly built helper.


## Interruption and production refresh — 10 October

Qualification resumed after the local service interruption. The long pressure
matrix retained three completed runs and an interrupted final control with no
result/cleanup receipt. No workload or namespace remained at resume. Its output
is preserved; the missing control is run in a new directory, never appended over
the interrupted run. Each completed 120-second pressure run uses the same fine
30-second histogram helper (`11e08c7b…`) and reports zero request/warmup errors.
Candidate combined throughput is about 252 Mbit/s versus the first control's
249 Mbit/s. Mean latency is 3.060 / 2.788 seconds versus 3.114 seconds in that
control, while candidate p99 is 4.362 / 4.862 seconds versus 4.186 seconds. These
are incomplete paired results, not a latency gate pass. Fixed socket fq policing
and separate pinned peers isolate queue pressure; they do not reproduce normal
four-sibling production admission.

The read-only refresh at approximately 15:54 UTC on 10 October still finds the
same latency.1 executable, original tunnel/Xray process identities, zero service
restarts and healthy endpoints on all five hosts. The original 24-hour observer
completed successfully, retaining about 8,560 samples per host. Client .118's
archive reaches 5,351 active forwards and 281,648 KiB peak RSS; it ends with 7,084
opening transport retries and ten reported opening errors. The live refresh has
11,091 retries and twelve errors. One additional warning explicitly reports a
Finland opening timeout at 07:12 UTC. At 01:13 UTC the same peer logs a successful
preserving migration; Finland closes its migrated conversation for keepalive
expiry approximately one minute later. That timing supports qualifying a backend
recovery grace, without establishing censorship as the cause.

Germany and France also record `read stream control: EOF` warnings with no
process restart. These mean a stream closed during its opening header; they do
not by themselves establish a broken carrier or customer-delivery failure.
Production's active binary/configuration remains unchanged. Observer renewal has
its own archive and receipts; a logging change is not a tunnel deployment.


The renewed recorder is verified active on all five hosts through 11 October
15:52:54 UTC, with a new `latency1-review-20261010T155254Z` archive. Binary/config
hashes, tunnel PIDs and Xray identities remain unchanged. The renewal's first
acknowledgement gate incorrectly required a nonempty warning journal on client
.14; it had no warnings in the selected interval. Read-only reconciliation verifies
its live recorder, successful metrics samples and journal-follow process without
restarting it again. Zero journal records is valid for a quiet host. Four initial
success receipts and all five reconciliation receipts are preserved.

The missing long pressure control completes with 179 requests, zero errors,
2.624 s mean / 4.080 s p99 and 258.77 Mbit/s combined bulk. Combining it with the
three retained runs gives approximately +1.9% candidate mean latency, −1.0%
combined throughput, and a higher p99 (candidate 4.362 / 4.862 s versus controls
4.186 / 4.080 s). The strict tail gate remains unresolved; these runs span the
local interruption and cannot be represented as an uninterrupted pair. The first
post-resume bulk launch stops on an invalid case name before load; its receipt
remains. The corrected runner uses the existing `multiworker-bulk` case.


## Avoiding replay costs when no sibling can exist

The fixed-lane pressure fixture creates four separate one-session peers. Such a
peer cannot retry an acknowledged opening on another slot, so replay IDs, receipt
retention and a commit have no benefit there. A new local edit keeps PTCP2/PUDP2
for fixed one-slot pools. Multi-slot and growable pools retain PTCP3/PUDP3,
including a growable pool whose second reservation has not yet been allocated.
The choice is made once before opening; an attempt never downgrades after sending
its identity or application bytes. Production's four-slot pools therefore keep
the safe retry. Server-first traffic on fixed one-slot pools keeps its original
latency/handshake. This is a capability optimization, not a claim that the adverse
multi-carrier pressure case is solved. Real mux tests cover TCP/UDP legacy payload
boundaries and growable-pool commits; those new tests are still pending execution
behind the currently reserved performance matrix.

The exact opening-only candidate `5a540988…` completes two clean, CPU-placed pairs
with four pinned lanes and two capture workers. Upload medians are 5.503 → 5.544
Gbit/s; download 4.125 → 4.164 Gbit/s. Both workers carry bulk traffic, all payload
checks pass, and teardown preserves the unrelated rule. CPU budgets and warmup
are identical; finite changes below 1% are not claimed as universal speedups.
These receipts describe the pre-capability-edit executable, not a rebuilt final
artifact. Full reports are under `comparison-queue7-bulk/`.


The capability edit passes the complete application race suite, vet, and five
repetitions of the real-mux capability / late-after-receipt retry tests. All 13
observer tests pass with the established direct-script invocation. An attempted
unittest module invocation from the repository root failed importing the observer
before any test ran; both that failure and the corrected receipt are retained.
No observer source change was necessary.

The pre-capability candidate completes all 16 short WAN runs with zero workload
errors and clean teardown. High-delay churn mean medians are 516.77 → 514.43 ms,
p99 1368 → 1416.5 ms; ACK-bottleneck upload 73.97 → 73.01 Mbit/s. Reordering HTTP
means are 90.20 → 89.74 ms, p99 162 → 165.5 ms, but short bulk goodput is adverse:
89.35 → 82.11 Mbit/s (−8.1%). Carrier populations differ substantially between
runs, and the second bulk pair is nearly equal. This remains a failed short
throughput comparison pending a longer warmed follow-up, not an erased outlier.
The asymmetric bulk latency sample contains only one completed control request
per run and one/zero candidate completions; its quantiles cannot establish a
latency improvement. Its delivered-body rate includes partial canceled bodies
and is about 5.70 → 5.67 Mbit/s. Other HTTP requests complete normally.

An original latency.1 client with the new backend also passes the established
migration fixture: sequenced 64 KiB payloads retain their sessions through recovery,
with healthy sibling traffic and owned-rule cleanup intact. The exact old/new
hashes and command are retained in `legacy-client-new-server7/`. The new four-slot
client still requires backend-first promotion; the new one-slot path uses legacy
requests from the outset rather than an unsafe post-receipt fallback.


## Capability checkpoint: additional failed gates

Source `a348c5cc8b9c2fd053a22f4783562c404746bef1`, executable
`076cbd29ca7c6eec46bf1e1c6001d3194eea9a5f875ee45b18c22bbd10f506a3`,
passes the earlier race/vet/observer checks. It remains **unqualified for rollout**.
The CPU-placed, fixed-source, ordinary four-slot pool pressure test retains two
pairs with zero request errors, but fails performance: the candidate's combined
bulk median is about 174.64 versus 189.95 Mbit/s and its mean-request median about
408.48 versus 344.45 ms. The second pair concentrates bulk streams on one lane
in both versions and has much higher HTTP latency. Different placements prevent
attributing all of the difference to opening code; they do not authorize hiding
the failed gate. This ordinary pooled fixture complements the separate one-slot
fixture rather than substituting for it.

Four fixed-one-slot pressure runs also complete with zero errors and clean
teardown. They use legacy requests from the outset; mean medians are about
3.095 versus 2.999 seconds and combined throughput about 258.14 versus 252.98
Mbit/s. Their variable tails show why identical healthy wire paths do not imply
millisecond-repeatable percentiles under this artificial fq saturation. No
universal tail improvement is claimed.

The warmed reordering follow-up stops on eight **body timeouts during warmup**
in its first candidate run, preserving its complete report and clean teardown.
Small HTTP requests have zero errors and about 88.76 ms mean latency; delivered
bulk bytes average 94.94 Mbit/s, but successful bytes do not erase eight stalled
responses. The baseline run had no errors. Final metrics identify one backend
carrier with a four-segment window and 2,558 pending segments while siblings
have large windows. The fixture logs no opening failures. Adaptive delivery
under reorder therefore needs investigation independently of safe opening
ownership. Its queued debug comparison retains failures and continues to collect
both versions; it must not rewrite the failed warm-up gate. Subsequent exact-
artifact hold/recovery/reload work was not reached by that aborted matrix.


## Untimestamped RTT uncertainty follow-up

The debug comparison shows queue classification toggling while RTT variation is
large relative to the reported excess over the lifetime RTT minimum. Legacy
untimestamped adaptation discounts ACK scheduling but ignores measured variation
in that classification, unlike its transit-timestamp branch. This is a concrete
controller modeling gap, not yet proof that every observed stall has that cause.
The first three debug repeats have no workload errors; they do not erase the
earlier eight warmup timeouts. All debug receipts and original failed reports are
retained.

A new local edit discounts twice KCP's smoothed RTT deviation in the untimestamped
queue signal. Existing entry/exit hysteresis, maximum credit, delivery horizon,
wire behavior and timestamp-attributed branch remain. A deterministic controller
scenario requires noisy saturated delivery to retain progress, then requires a
stable RTT increase with little variation to reduce pacing. Full correctness and
real-link-shaped performance tests are pending, including bottlenecks where a
variation allowance could incorrectly delay congestion response.

The same edit copies the controller's input RTT/deviation, ACK budget, window and
sample time into debug records. Existing live stats are collected later; the old
record could therefore mix decision inputs and newer ACK observations. Named
snapshot fields and monotonic sample age make that distinction explicit without
per-packet work or warning-level snapshot overhead. No production promotion is
claimed at this checkpoint.


The RTT-uncertainty edit now passes the full application race suite, vet, five
focused adaptive repetitions and all 13 observer tests. The same new regression
was also run against an isolated archive of source `a348c5c`: the old controller
fails with `congested=true`, window five and rate 21,216 / pacing 6,061 bytes/s.
The archive/added-test hash, expected failure and log are retained separately.
The new model also verifies that a stable subsequent queue increase still backs
off. This establishes the modeled bug/fix, not a production-causality claim.

All four pre-fix debug reordering repeats finish without errors or leaked rules;
candidate bulk values are 72.17 / 96.31 Mbit/s versus controls 85.96 / 91.87.
Variation and the original eight-timeout gate remain explicit. The current
capability executable also completes its exact-artifact clean bulk check:
upload medians 5.436 → 5.674 Gbit/s and download 4.210 → 4.136 Gbit/s. Both capture
workers carry traffic, all integrity checks pass and teardown is clean. The
upload difference is not attributed to a causal optimization; the overall
candidate remains unqualified because of its failed pooled/reordering gates.
The next rebuilt artifact must test the new controller under the same failure
profile and under stable bottlenecks before any promotion.

## 11 October checkpoint: production and independently capped links

The [fresh production review](PRODUCTION-REVIEW-2026-10-11.md) separates repeated
France transport failures from destination-service refusals. The operator
confirmed Xray/nginx restarts in the reviewed interval. No tunnel candidate was
deployed; finite observers were renewed on all five hosts without changing
their tunnel/configuration/binary/Xray identities across the renewal.

Source `8e7140e` was rebuilt as `enterprise-2026.10.11-opening.2`, SHA-256
`70239e4684f9d276d0a653bfd11517e13c07b55ca42f930ebcdd1b883ae1d8c0`.
Six warmed reordering experiments compare production, the pre-correction
candidate and the RTT-corrected candidate in alternating order. All six have
zero workload/warmup errors and clean teardown. Bulk payload rates are
75.09 / 78.24 Mbit/s for production, 90.04 / 92.04 for the previous candidate,
and 114.13 / 101.43 for the correction. Small HTTP means stay around 88 ms.
These are results of the historical **combined netem rate/reorder model**;
they do not establish delivery below a physical 100 Mbit/s bandwidth ceiling.
Private receipts: `comparison-jitter9-reorder/receipt.json`.

An independent fixture check exposes the model limitation directly. With
`netem rate 20mbit reorder 50%`, tunnel upload reaches 25.57 Mbit/s, direct TCP
24.87 / 27.47 Mbit/s, and offered-load UDP 38.87 Mbit/s. Netem can release
reordered packets without applying its rate-delay calculation. Adding a
separate parent TBF bounds every packet: direct TCP 18.08 / 18.18 Mbit/s,
UDP 19.41 Mbit/s and this candidate's tunnel upload 12.98 Mbit/s. The latter
still needs a same-model control before judging adaptive efficiency. All
processes exit cleanly; unrelated firewall rules are preserved. Private
receipts: `strict-rate-fixture9/receipt.json`.

The optional `--strict-rate` fixture retains historical behavior by default;
positive directional rates are required across schedules. Its separate rate
limiter, bounded burst and impairment queue are documented in BENCHMARKS.md.
This is a local harness correction, not a production qdisc change. A queued
WAN matrix was canceled before launch to qualify the limiter first. The first
replacement launcher then failed on an unused module-level `case` reference,
before creating an output directory or starting workloads. Both attempts are
retained separately; the corrected launcher now compares identical strict
profiles. Its results and the earlier pooled-pressure gate remain pending.

### Shared-host activity and incomplete strict WAN qualification

At **2026-10-11 17:06 UTC**, the operator reported using the test laptop.
The activity's start time and resource demand are unspecified. Preserve these
receipts as diagnostic observations; do not accept throughput or latency
differences from this session as evidence for deployment. A controlled repeat
is required for performance acceptance. Concurrent activity does not establish
the cause of any timeout, and a later successful repeat must retain the failures.
The private notice is `shared-host-activity-20261011.json`.

In the first independently capped asymmetric pair (50 Mbit/s upstream,
10 Mbit/s downstream, 80 ms nominal RTT, 0.5% loss, 5% reorder), the corrected
candidate delivers 4.459 Mbit/s of response-body payload and the deployed control
4.452 Mbit/s. The candidate reports four body timeouts and four warmup errors;
the control reports eleven and eleven. `errors` includes `warmup_errors`;
the latter is a subset, not an additional failure count. Successful-request latency excludes
failed requests and cannot demonstrate an improvement by itself. Both runs
fail the zero-error requirement, despite small HTTP requests having no errors.

The second asymmetric control reports zero errors and 4.675 Mbit/s; the second
candidate reports thirteen body timeouts, thirteen warmup errors and
4.190 Mbit/s. The candidate therefore remains unqualified. This also shows why
one favorable run cannot clear the failed gate. All four fixtures preserve clean
owned firewall teardown. The matrix was still running at this checkpoint;
`comparison-jitter9-wan/receipt.json` retains each completed attempt. A separate
matched debug pair is queued behind the machine-wide reservation to identify
controller, retransmission and ordered-delivery behavior. No production binary,
configuration, tunnel service or Xray service was changed.

The HTTP helper attributes each response's byte count to its request-start
phase. Bodies that begin during warmup and finish during measurement are
excluded from `bytes`, including bytes received after the measurement boundary.
Its reported `goodput_gbps` therefore describes the measured-start cohort,
not all payload arriving during that wall-clock interval. This matters for
multi-second responses: the first asymmetric pair's whole-run body averages,
including warmup and partial bodies, are 5.359 / 5.822 Mbit/s; the second pair
is 5.657 Mbit/s for the control and 5.332 for the candidate. These are different
estimands and remain diagnostic. None is a precise steady-state physical-link
utilization measurement. The timeouts and zero-error gate remain unchanged.

### Completed strict matrix and quiet-window retry diagnosis

All twenty ordinary strict WAN attempts are retained in the
[compact diagnostic receipt](opening-wan-diagnostics-2026-10-11.json).
Four attempts fail: the first production reordering run and three of the four
asymmetric runs. All owned firewall cleanups pass. Other profiles complete
without workload errors, but concurrent laptop use prevents treating these
performance differences as an acceptance result. The quiet-window notice was
recorded at **17:15:22 UTC**; the original attempts are preserved separately.

The subsequent quiet debug pair completes with zero workload errors in both
versions. Corrected candidate response latency averages 24.10 seconds versus
22.87 for the deployed control, and whole-run body payload averages
5.216 / 5.473 Mbit/s. A zero-error repeat does not clear earlier failed gates.
The candidate routes all sixteen bulk workers through one carrier in this
attempt. Its first bulk seconds expand the send window to 4,096 segments,
increase RTT/RTO sharply, then oscillate between probing and queue draining.
This combines ordered-carrier head-of-line blocking with retry/queue feedback;
it is not evidence that asymmetry inherently requires poor utilization.
Private evidence: `asymmetric-jitter9-debug/receipt.json`.

Source `24df993` exposes already existing per-carrier observations in debug
samples: `spurious_retransmit_delta`, `spurious_retransmit_total`, and
`reorder_delay_ms`. It changes no recovery policy or warning-level snapshot
work. Full application race tests and vet pass. The first check launcher omitted
the runtime libpcap search path and failed before the affected package tests
could run; its failed receipt and canceled pre-authentication debug launch are
retained. The corrected launcher passes and builds SHA-256
`1095ea78df13f88577da68b35414246c47f223dc5d8fb0ca9299ff8385ae889b`.

That diagnostic executable completes its quiet asymmetric exercise without
workload errors. The busy carrier reports 16,744 retries, 26,150 new segments
and 5,297 ACK observations consistent with possible spurious retries. These
observations are not proof that every retry was unnecessary; some ACKs may
belong to an earlier retransmission. The learned reorder-delay signal repeatedly
hits its 50 ms ceiling. Whole-run payload averages about 5.30 Mbit/s, while
the measured-start request cohort reports 3.797 Mbit/s. Exact logs, executable
identity and clean teardown remain in `asymmetric-observe10-debug/receipt.json`.

A virtual-clock regression reproduces an ACK arriving after 60 ms of gap
evidence but within the caller's 100 ms learned tolerance. The old setter clamps
that tolerance to 50 ms and retransmits before the original ACK arrives. Its
expected failure is retained in `grace11-old-model/`. A local experiment raises
the finite adaptive/library ceiling to 200 ms while retaining zero grace on
low-RTT carriers and independent timer expiry. A genuine gap may consequently
wait up to 150 ms longer than the former ceiling; loss/reorder/latency comparisons
are required before accepting that tradeoff. This experiment is not deployed
or accepted. Full application and vendored-library checks are in progress.

The first full vendored race run reaches its ten-minute suite deadline while
`TestReliability` is running. The 1 GiB and 6 GiB echo cases had completed;
the dump also retains thousands of accepted echo sessions from earlier fixtures.
These fixtures closed their listeners but did not own/close the accepted
sessions. Production intentionally keeps accepted carriers independent of
listener ownership for migration; the test must provide explicit teardown.
Fixture-owned wrappers now close their accepted sessions, and lossy-link
fixtures close their accepted/client/packet resources. Production listener
ownership is unchanged. The failed full-race receipt is retained in `checks11/`.
The corrected qualification separates the full native suite (including both
large integrity echoes) from the race suite excluding only those two large
echo workloads. It records each scope explicitly rather than calling the timed
out full-race run a pass.

A second clock regression proves that a late timer ACK with no gap evidence
previously sets the learned reorder delay to its ceiling by subtracting a zero
gap timestamp. The fix still counts the possible spurious retry but learns gap
delay only while fast-ACK evidence remains present. The old failure is retained
in `grace11-timer-old/`; three race-instrumented repetitions of the new gap,
timer, finite-ceiling and real fixture-ownership tests pass. Whole application
race and vet checks also pass at this checkpoint. Full native/library race
qualification and matched real-link comparisons remain pending.


### Completed reorder experiment and low-RTT pacing follow-up, 11 October

The corrected KCP fixture qualification completes: the full native library suite
passes, as does the race suite excluding only the 1 GiB and 6 GiB echo workloads.
Those large integrity workloads passed in the native suite; the original full-race
timeout remains preserved. Root race/vet also pass. Scope and timing are recorded
in `checks11-v2/receipt.json`; this is not a full vendored race-suite pass.

The [matched reorder experiment](reorder-grace-diagnostics-2026-10-11.json)
compares candidate `8ef31b9` with the log-only executable, using fixed CPU roles,
four pooled carriers, two capture workers, null encryption, and independently
capped 50/10 Mbit/s directions with 80 ms RTT, 0.5% loss and 5% reordering.
Two raw download pairs improve their median from 5.592 to 6.623 Mbit/s (+18.44%).
All four raw workloads finish successfully. The candidate HTTP comparison has
zero errors; its control has two response-body timeouts (also counted in its
warmup subset). Conditional successful-request latency is not comparable when
the control loses its slow responses. A single 50/50 Mbit/s loss-only pair without
reordering completes with zero errors and similar whole-run body rates
(42.50 versus 41.97 Mbit/s). These finite positives do not prove universal safety.

The original pooled-pressure acceptance gate still fails. Two alternating pairs
against the deployed executable combine sixteen bidirectional bulk workers with
four HTTP churn workers under per-kernel-flow FQ pressure. Candidate median
combined throughput is about 8% lower; small-request mean and p99 are worse.
All workloads complete and their owned cleanup passes, but successful runner
exit codes are not performance acceptance. The FQ cap is per kernel flow, not a
strict physical-link aggregate cap. Receipts remain in `pressure-loss-grace11/`.
The candidate and deployment differ in opening and controller logic too, so this
comparison cannot assign that regression exclusively to the reordering ceiling.

The next local candidate addresses a separate controller policy: a lifetime
minimum RTT below 10 ms unconditionally disables byte pacing even when later
observations classify real queue congestion. Retain pacing for a bounded quiet
interval after such congestion (one second to four current RTTs, capped at eight
seconds); ordinary low-RTT operation remains unpaced after that interval. Existing
small-message/control exceptions and window/rate estimators are unchanged.
This policy needs matched pooled-pressure and clean-link comparisons before use.

The first regression fixture failed to establish the propagation floor: its
initial sample only initialized counter deltas. This was a fixture error, and
both the original and revised first attempts are preserved. The corrected
fixture adds a second fast observation before queue growth. Running that exact
corrected fixture with a Go overlay of the committed prior controller fails the
congestion-pacing assertion; the new controller passes congestion, brief queue
relief, and return-to-unpaced checks. Five focused race repetitions, full root
race tests, and vet pass (`checks-build12-v2/`). Its initial executable records a
dirty source patch explicitly; performance comparisons use a rebuilt committed
artifact. No performance acceptance or production deployment is claimed.


The [four matched low-RTT pacing runs](low-rtt-pacing-diagnostics-2026-10-11.json)
finish with zero workload errors and clean teardown, but fail the mean/p99 gate.
Median combined throughput rises from 134.23 to 213.95 Mbit/s while median
small-request latency rises from 815.86 to 2299.93 ms. Placement is highly
variable: one control run isolates small requests and averages 0.60 ms, whereas
the other averages 1631 ms. This is a rejected acceptance result, not evidence
that the entire median change is caused by the pacing policy. Candidate
`c288585`, SHA-256 `945971a4f2c122155b6f4945417b0f50ae26fc2764705125b97bfebe4e0a74b6`,
remains local. Debug observations confirm low-floor congestion with pacing off
in the control, but RTT-based classification still misses much local pressure.

A new experiment samples already-existing host queue loss at the controller
cadence. More than four drops and one percent of attempted output during a
sample activates local output pressure. Socket identity is checked across
migration; decreasing counters are not subtracted as unsigned loss. Shared
backend worker counters represent shared worker pressure, not per-user loss.
Per-session output-pipeline drops are also sampled. Known local pressure stops
window probing, permits bounded window reduction and uses 85% delivery-rate
pacing for that sample. A one-second quiet interval suppresses new probes while
retaining surviving credit; network loss alone does not enter this policy.
Low-RTT pacing still has the earlier bounded expiry. Small-message exceptions
remain. This adds no new packet-path counters, timers or user-stream controllers.

The KCP session exposes its immutable current output-route socket read-only for
this sampling. Ownership stays with the session/listener, and migration updates
the observed physical socket. Existing migration tests verify that the accessor
follows both ends of an actual move, including AES/null transfer integrity.
Models cover ACK-masked local loss, retained credit during quiet recovery,
resumed probing, isolated drops, counter resets, socket changes and pipeline
loss. Five focused race repetitions, full application race/vet and three
vendored migration race repetitions pass. Performance qualification is pending.

The [evening production follow-up](production-followup-2026-10-11-evening.json)
finds the same tunnel PIDs, healthy enabled services and zero automatic restarts.
Bounded warning records report backend 2096 refusals around 18:38/18:41 UTC and
matching client target rejections. Xray identities changed outside this work;
these messages show destination refusals rather than establish path filtering.
Observers remain active. No candidate binary or transport policy was deployed.


### Output-pressure experiment and attribution correction

The [twelve output-pressure runs](output-pressure-diagnostics-2026-10-11.json)
retain all successes and failures. Against `8ef31b9`, candidate `7a42c0c`
improves the two-pair pooled-pressure medians from 180.71 to 273.65 Mbit/s
combined bulk, from 1266.61 to 64.70 ms small-request mean, and from 2457.5 to
136 ms small-request p99. Its first pair also reduces summed independent client
socket drop attempts from about 2.4 million to 3,500. Carrier placement varies;
one control gives small requests spare lanes and averages 0.60 ms, whereas the
candidate averages 1.33 ms with more bulk throughput. The medians pass the local
comparison limits, not a comparison against the production binary or a proof of
per-run nonregression.

Two clean pairs use four fixed one-carrier peers, two capture workers, null
crypto, and equal four-thread CPU budgets at each end. Median upload changes
from 5.64096 to 5.66244 Gbit/s (+0.38%) and download from 4.21199 to 4.20023
Gbit/s (-0.28%), within the 5% bulk limit. The unequal direction rates remain
in this equal-budget experiment; increasing Go budgets alone does not establish
its cause. No structural packet-format or protocol framing change was made.

The asymmetric HTTP/body candidate finishes without errors; its control has
eight warmup body timeouts (included in eight total errors). Whole-run body
payload is 6.257 versus 6.388 Mbit/s; small means are 89.73 versus 88.04 ms.
Candidate small p99 is 305 versus 272 ms, outside a strict 10% tail limit in
this single pair. Loss-only body means are 3.317 versus 3.168 seconds and rates
40.906 versus 41.395 Mbit/s, with zero errors in both versions, but candidate
bulk p99 is 7.395 versus 3.831 seconds. This tail regression is preserved and
requires further investigation/repeats. It is not described as a universal pass.

Review identifies a correctness limitation in the feedback policy before any
promotion: backend output sockets are shared between independent clients.
Applying the socket's drop increment to every carrier can throttle a good link
because another client's tuple has a full queue. The queued comparison against
production is canceled at its sudo prompt, before authentication or workload
launch. Its canceled receipt remains in `production-control13-canceled/`.

The next revision reports soft loss from the exact driver batch back to its
own KCP session. The AF_PACKET injection path returns an additional loss count
without changing attempted-frame construction, bounded queue retry, partial
send semantics or permanent errors. A cached optional batch interface reports
this count to KCP; only reported failed batches increment one per-session atomic
counter. No payload parsing, tuple map, per-packet healthy-path counter or new
connection-tracking state is added. This works with opaque encryption/FEC output
and retains the logical counter across migration. The adaptive controller now
samples only that carrier's reported loss plus its own pipeline overflow.
Shared worker totals remain diagnostic. The pcap adapter retains its existing
behavior and reports no precise per-call soft losses; only pipeline/RTT signals
apply there. That limitation is explicit rather than inferring carrier loss from
shared totals.

A deterministic concurrent test shares one output socket between a permanently
lossy tuple and a healthy tuple. All 256 reported losses belong to the affected
session and none to its healthy neighbor; a physical move retains the logical
count. Existing native migration integrity cases verify actual AES/null moves.
Application race/vet and repeated migration/attribution races pass. Debug logs
add exact `carrier_output_drop_delta`/`carrier_output_drops_total`, keeping these
separate from socket-scoped `tx_queue_drops`. Corrected-policy benchmarks remain
pending; the earlier shared-worker executable is not promoted.

### Precisely attributed feedback: current qualification checkpoint

Source `3d4387f`, exact executable `cd693a37…`, passes the full native KCP suite
including the large echo workloads. The preserved pre-commit implementation patch
also passes root race/vet, five focused controller race repetitions, and three
library migration/shared-output-attribution race repetitions. No candidate binary
is installed on production.

The [candidate receipts](output-pressure-carrier-qualification-2026-10-12.json)
retain complete commands, hashes, configurations, placements, resource peaks,
cleanup and comparison limits. In two ordinary four-slot pooled pressure pairs
against the exact production executable, median combined bulk changes from
213.24 to 347.07 Mbit/s (+62.76%); small-request mean changes from 1923.91 to
118.65 ms and p99 from 3671 to 204.5 ms. All request errors are zero. The 50 Mbit/s
fq setting limits individual kernel flows, not aggregate physical link capacity.
Placement varies, so these are finite paired medians, not per-run guarantees.
Debug samples confirm actual per-carrier local-output-pressure observations in
both endpoint roles. Shared worker counters are excluded from controller inputs.

Two clean pairs retain four fixed one-carrier peers, null crypto, two capture
workers, pinned equal four-thread endpoint CPU budgets, and 16 streams. Median
upload changes from 5.535 to 5.658 Gbit/s (+2.21%) and download from 4.211 to
4.226 Gbit/s (+0.36%), within the 5% bulk limit. These comparisons use preceding
controller `8ef31b9`; they do not explain the remaining directional rate gap.

On the independently capped 50/10 Mbit/s link with 80 ms RTT, 0.5% loss and 5%
reordering, the candidate has zero request errors; its predecessor has 11 body
timeouts (the same 11 included as warmup errors). Whole-run body payload rates
are 6.816 versus 6.608 Mbit/s. Small HTTP mean/p99 are 87.95/261 ms versus
89.04/279 ms. Candidate body p99 is 20.59 seconds versus a conditional 12.122
seconds in the failing control; censoring prevents calling this an uncensored
latency regression or improvement. On the symmetric 50 Mbit/s, 80 ms RTT,
0.5%-loss link, both versions have zero errors: body rates are 41.432 versus
41.806 Mbit/s, means 3.139 versus 3.106 seconds, and p99 5.379 versus 5.841
seconds. The previous executable's single-pair body-tail regression is not
reproduced here; its adverse receipt remains intact.

Repeated outbound and reverse-path tuple failures retain all four established
streams exchanging sequenced 32 KiB payloads. Healthy peers report zero errors;
only the affected carrier changes source tuple. Capture verifies S outbound/PA
return and retained backend sequence seed. Two-worker live reload with 32 streams
per original peer, four carriers and three remove/re-add cycles passes, exercises
both workers, keeps process identities and releases half-closed relays. Client
file descriptors return to 55 after starting at 55. Owned namespaces/rules are
removed and unrelated sentinel rules remain.

Direct comparisons against production on the two WAN shapes and a longer
100,000-connection hold of this exact artifact remain pending. Production refresh
through 21:42 UTC finds no new selected warnings or opening-error increments;
Finland adds 2,714 transmit drops, a concrete reason to finish the feedback
qualification. Production binaries, configs and Xray identities remain unchanged.

### Extended checks and warning-level visibility

The exact candidate's direct production comparison completes without errors on
both WAN shapes. Asymmetric body payload changes from 5.526 to 6.438 Mbit/s,
mean from 22.843 to 21.857 seconds, and p99 from 24.701 to 24.896 seconds;
small mean/p99 changes from 87.95/259 to 89.90/275 ms, within 10%. Symmetric-loss
body payload changes from 41.543 to 41.748 Mbit/s and p99 from 7.145 to 5.632
seconds; small mean/p99 remains within 1%. All owned cleanup checks pass.

100,000 connections establish in 6.276 seconds, remain held for 120 seconds,
all pass payload verification and close with zero errors. Client/server peak
RSS is 1,736,400/1,733,336 KiB and peak swap is 262,136/308,352 KiB. These peaks
need not occur at the same sample; the simultaneous resident-plus-swap peaks
are separately retained. Generator/target/kernel memory also consumes laptop
RAM. This is a finite correctness/resource result, not a RAM-only limit for the
smaller production hosts or a simultaneous full-rate/100,000-flow qualification.

240 ms RTT churn means are 480.897 versus 480.813 ms with identical 482 ms p99
and no errors. Across two 100/1 Mbit/s upload/ACK-return pairs, candidate rates
are 76.887/76.685 versus control 80.527/75.800 Mbit/s: medians retain 98.24% of
control, within the 5% allowance. The first pair was near that limit and prompted
the reverse-order repeat rather than a per-run speedup claim.

The 50 → 5 → 50 Mbit/s body workload completes without errors, improves measured
payload/mean, but has adverse p99 of 3.263 versus 2.319 seconds. Its transitions
occur in the 20-second warmup. Latency classifies requests by start time; in-flight
warmup responses do not enter measured quantiles. These quantiles characterize
post-transition behavior, rather than latency during the bandwidth reduction.
The full schedule, warmup statistics and subsequent quantiles remain in the
receipt. A reverse-order repeat is pending before acceptance. Exact samples show
no local-output-pressure events on either version; the local-drop feedback is
therefore not established as the cause. Predecessor logs lack some newer
spurious-retry fields: missing values must not be interpreted as measured zeroes.

Four native CPU profiles of the first ten clean workload seconds (including
warmup) show syscall CPU as the dominant sampled cost, approximately 43–54% in
the endpoint profiles examined. These are CPU profiles, not lock-wait traces;
they do not prove a transmit-mutex explanation for the directional gap. Compiler
escape diagnostics additionally confirm that the new captured `dropped` scalar
allocates per raw batch. A follow-up can avoid that allocation while preserving
batch outcome/retry semantics; no such packet-path edit is yet in this checkpoint.

Production warning logs bypass debug samples, so the follow-up exposes
`super_paqet_session_local_output_drops_total{conv,remote}` by reading the existing
per-carrier atomic at scrape time. The finite observer records its growth without
summing duplicate worker/session evidence. A new regression checks legacy metric
absence, migration label changes, decreasing counters and PID replacement.
Observer tests, root race and vet pass. These edits add no packet-path state or
adaptation changes, and no production observer/binary is replaced.

### Queue relief pacing ramp: model checkpoint

The reverse-order bandwidth-step repeat also completes without errors. Candidate
body p99 is 2.991 versus 2.743 seconds in that second pair; the two-pair medians
are 3.127 versus 2.531 seconds (+23.55%). Throughput and mean improve, but the
tail concern remains. The original gate is retained rather than cleared by the
second pair's smaller difference. Both versions place persistent requests 4/2/2
across three bulk carriers; equal counts do not prove identical scheduling.

Exact-epoch diagnostics show candidate pacing alternating between roughly one
quarter of its learned rate and the full rate when a transient queue indication
clears. A new local controller policy bounds upward gain recovery by one per
second during the existing recent-pressure horizon (1–8 seconds, based on RTT).
Reductions remain immediate, new delivered capacity is still learned, and
ordinary probes resume after expiry. Healthy startup, small-message bypass and
the quiet low-RTT unpaced path remain. State adds one float and one timestamp
per carrier, without per-forward state, timer, allocation or packet-format edit.

The [deterministic model and race receipts](pacing-recovery-models-2026-10-12.json)
verify immediate drain, bounded recovery, repeated-epoch idempotence, expiry and
resumed healthy probing. The same model overlaid onto the old controller fails
at the expected immediate-refill assertion; five native race repetitions pass
on the candidate. The first full root command fails because Go recursively
discovers the retained old controller text as an incomplete package under build;
that owned text is retained as `.go.txt`. Engine race checks pass in that failed
attempt, and the corrected full root race/vet command passes. This is a harness
correction, not an erased failure. Exact-binary performance comparisons remain
pending, so the pacing ramp is not accepted or deployed at this checkpoint.

### Pacing ramp rejected after native comparisons

The [six-run, alternating three-way comparison](pacing-recovery-performance-2026-10-12.json)
uses exact production, preceding candidate `cd693a37…`, and ramp `c766e873…`, with
identical placement/configuration. All workloads and cleanup pass correctness.
Ramp medians are 29.12 Mbit/s payload, 1824.59 ms mean and 3059.5 ms body p99,
versus preceding candidate 35.15 Mbit/s, 1571.39 ms and 2686 ms. Throughput falls
17.14%, mean rises 16.11% and p99 rises 13.90%, failing the finite limits.
The normal recovery ramp restricts useful sending along with probes; deterministic
desirability does not establish real link performance. The runtime edit and its
specific model fixture are removed, retaining all attempted source/receipts.

The production medians in this matrix are 32.68 Mbit/s, 1640.31 ms and 2905.5 ms.
The preceding candidate's earlier body-tail regression does not reproduce here,
illustrating why neither a single favorable pair nor a sparse p99 establishes a
universal claim. Earlier adverse paired results remain in their original report.
The next limited trial will preserve ordinary sending and suppress only the
extra capacity probe briefly after proven RTT congestion, using existing
per-carrier quiet state. It remains local until measured; production is unchanged.

### Queue probe pause: model checkpoint

The next trial retains ordinary post-queue pacing and credit growth caused by
increasing delivered capacity. Only speculative periodic/startup probes wait
500 ms to 2 seconds after higher-RTT congestion (two regular samples or two RTTs,
bounded). It reuses the existing pacing deadline for this higher-RTT purpose;
low-RTT retention and host-output-loss quiet behavior remain. No new state,
allocation, timer, packet format, source-port logic or stream ownership is added.

The [model/race/vet receipt](queue-probe-models-2026-10-12.json) verifies draining,
immediate ordinary sending after relief, suppressed extra probes, finite expiry
and small-message exemption. Five focused race repetitions, full root race and
vet pass. Exact-executable comparisons remain pending. This trial is distinct
from the rejected gain ramp and does not throttle normal recovery sending.
