# Current work checkpoint — step 1

User authorized continued diagnostics/testing/tuning; no deployment, commit or push.
Outer raw Ethernet/IP/TCP framing and firewall bypass must be preserved.

Earlier pass: 20 live profiles, 109 repeated deterministic virtual cases, 100k
connections held120s with3.24Gbps mixed traffic, full suites and systemd passed.
Evidence docs/tuning-qualification.json is the OLD pass2 hash396909..., not current.

Current pass discovered iperf3 --bidir direction assignment depends on accept order;
independent proxied TCP opens reorder, producing wrong-role zero-byte streams.
Harness now uses concurrent upload/reverse tests on separate target ports18083/18084,
forwards28092/28093; checks every receiver stream carries measured bytes. Previous
--bidir throughput is not valid qualification evidence. Local ESnet source confirms
role assignment at build/iperf-3.20/src/iperf_server_api.c lines819 onwards.

Retained edits under qualification:
* Reliable asynchronous coalesced smux UPD credits, one pending entry per stream,
  close unlink, no per-stream timer/goroutine; readers no longer block on opposite
  direction sends. Counter-wrap zero-credit fix for Read and WriterTo.
* KCP delayed ACK respects its deadline during data flush; delay1..5ms by RTT.
* Paced writes/inputs bring shared periodic wake forward; obsolete callbacks skip.
* Per-KCP output/control/ACK counters; log/metric pending credits.
* Controller recent delivery maximum2..8s, small4-segment startup with doubling,
  learn minRTT from control traffic, minimum pacing1024B/s, flight budget2BDP
  plus bounded ACK-delay headroom; congestion backoff .85 with hysteresis.
* Experimental CURRENT ACK receive timestamps4B encrypted payload; relative
  forward/reverse queue estimates need no clock synchronization, 30s floor expiry,
  wrap-safe. Enterprise enabled unless kcp.ack_timestamps:false. Evaluating now.
* kcp.adaptive_buffers override decouples stream buffers from KCP controller.
* Harness --kcp-options JSON allows controlled tuning comparisons.

Rejected experiment: expedited OOB credit hints removed entirely; no new application
data transport. Larger stream BDP factor4 performed worse and restored2.

Measured corrected comparisons:
* build/tuning3-corrected-baseline (async-credit-only older binary):11.13/10.86Mb
  simultaneous on20/100 cap80msRTT0.5%loss. All8data streams carried traffic.
* build/tuning3-filter-asymmetric:15.16/34.81Mb (recent delivery filter).
* build/tuning3-filter-acklimited:79.31Mb one-way download on1/100 caps50msRTT;
  bidirectional remains~0.9/1.22Mb before timestamp separation.
* build/tuning3-small-acklimited:88.92Mb download; duplex0.88/1.19Mb.
* build/tuning3-backoff-wan100:915.5Mb each one-way, corrected duplex604/602Mb.
  RTT-only queue control incorrectly throttles faster path when reverse queues.

Root PTY session50108 currently open, authorized sudo. Active sequential commands:
1 build/tuning3-transit-acklimited (45s, warmup8, corrected duplex1/100/RTT50)
2 build/tuning3-transit-asymmetric (30s,warmup8,corrected duplex20/100/RTT80/loss.5)
These run build/super-paqet-transit, before latest log fields and transit input clamps.
Inspect results before promoting transit design. No other benchmark should run
concurrently. CPU profiling currently10s per test. All harness changes isolatedNS.

Test exec session46663: root engine race then focused KCP transit/virtual/ACK/pacing
race, output build/tuning3-transit-regressions.log. Previous focused tests pass.
New tests: transit separates queues at clock offsets/wrap, optional ACKinterop;
controller reverse queue must not reduce forward pacing; smux full duplex/wrap and
blocked-carrier read tests; pacing earlier-wake and ACK-deadline tests.

Next: inspect transit live results; decide/tune using measured evidence, freeze
source, rebuild, run full corrected live matrix (20 profiles), extra fault seeds/soak,
100k mixed/clean gigabit acceptance, complete make vet test, systemd test. Consolidate
current BENCHMARKS/DIAGNOSTICS and evidence with same-binary hashes; honest residual
limits and no absolute optimum/real-WAN claim. Exit root shell and verify cleanup.

Latest after checkpoint: KCP WINS cumulative credit hints (not OOB) plus relative
ACK timestamps solved fast-path starvation:1/100 duplex0.37/89.97Mb,20/100 loss.5
duplex16.43/72.32Mb. New guards: stale non-PUSH control cannot reset generations;
unknown control cannot create carriers; actual50B innerKCP minimum validates crypto/FEC.
Hint reorder/drop/duplicate/fallback tests and callback-outside-lock tests pass.
Async write failure closes carrier and wakes all readers. Source now additionally
uses global bounded control priority and live-window/pacing-rate data frame budgets.
20ms pacing chunk ceiling with oneMSS floor preserves fast-link64K batching.
Mixed HTTP churn caught1 timeout on1/100 link and16.8s p99. Fair frames remove
timeout but p99 still8.4s; trace shows network queue drops3358, reverse ACK wire
traffic saturating1M while upload active. Testing20ms ACK coalescing WITH hints now
(previous20ms experiment lacked hints and did not help).
Active root50108 currently old latency trace may be complete; next compile exec
produces build/super-paqet-hints-ack20. Then run mixed1/100 and20/100, inspect errors
and latency. Need full final corrected matrix/100k/clean acceptance/library suites
after tune settles. Still no production readiness claim/deploy/commit.

Checkpoint committed3aaa214 (user explicitly requested checkpoint commits).
Current uncommitted work: adaptive peer carrier pool, sessions initial count and
max_sessions ceiling (CPU-derived unless fixed source port or adaptive:false).
Per-slot cached score avoids per-open KCP snapshot/locks; spare carrier grows only
when all carriers are busy. New ReceivedBytes/PendingBytes counters classify slow
bulk correctly. Growth serialized, failure backs off and preserves old carriers;
elastic port guards close with peer, firewall journal cleaned after worker exit.
Metrics takes peer RLock. Unit pool concurrency/limit/failure/cleanup race passes.

Mixed_HTTP still hadseconds latency despite frame bounds; started elastic pool.
First elasticity created5th carrier but slow uploads incorrectly classified light
due to low bytes/s. Latest build/super-paqet-elastic-bytes includes PendingBytes
threshold to avoid routing fresh short requests onto those stalled bulk carriers.
Root50108 now runs tuning3-elastic2-mixed-acklimited then asymmetric (each30s+8warm,
corrected duplex +4 HTTP churn). Inspect results. Previous elastic first run results
remain diagnostic. ACKdelay20 WITH hints kept for these trials, source latest.

Still need final source freeze, full corrected matrix/multi seeds,100k mixed soak,
clean multiGb and full library/race/systemd checks, docs/evidence and second commit.
No deployment and no production readiness claim yet.

Current candidate frozen: build/super-paqet-final-step1 (hash from latest build log).
Packet-rate BDP fixed tiny control-only carrier window starvation; data pacing only
activates after bulk observed. Sticky bulk classification survives delivery gaps,
expires2..10s. Mixed1/100 test now13.15HTTPchurn/s median262ms p991.049s,0errors,
bulk0.47/88.03Mb;20/100 loss.5 now19.13/s median262ms p99524ms,0errors,bulk15.8/41.9Mb.
All data streams carried bytes. Larger throughput-only earlier gains still valid
for their mixes; do not conflate mixed and bulk-only profiles.
ACKdelay nowadaptive1..20ms (ceiling kcp.ack_delay_max_ms default20), write frame
budget20ms (kcp.write_batch_ms default20), oneMSS floor and live-window bound.
Diagnostic selected flows nowlogstream_id; debug failures log evenunsampled.

Root50108 actively running scripts/stress_links.py26profile full same-binary matrix
withdebug+CPUprofiling20s, output build/step1-final-matrix. Includes added mixed
HTTPchurn/asym+ACK-limited, standalonechurn, harsh loss/reorder/delay,100->1->100
rate step, pcap functional/restart/capture fallback. Monitorconsole/matrix progress.
Need all26pass, extra seeds/soak,100k mixed600s,clean>=2Gbps,full make vet test,
systemd samecandidate, complete docs/evidence and nextcheckpoint commit.
Application source frozen except failures requiring correction. Test/doc edits
do not change binary, but rebuild/hash before final acceptance if code changes.

Frozen candidate SHA678d5c4f34ab721bda2502b891ca2ffe957f1f15b863434f17f9f3411f326fe3.
Final matrixclean passed4.918Gb upload3.734Gb download; correctedduplex2.570/1.950Gb.
wan100 passed902.7/902.4Mb one-way,903.3/899.4Mb simultaneous,0errors. Root50108
continues26profiles (currentlysatellite); queued make vet test afterwards, output
build/step1-final-full-tests.log. Focused latest root/pool,smux,KCP racespass.
Next aftermatrix:inspect failures/all26,extra seeds,100k mixed600s,systemd samebinary,
fuzz60s each andfinaldocs/evidence+checkpoint. No further app edits unlessfailedtest.

Code review found an elastic selector retry bug: failed low-pressure slot could
be selected again ahead of a healthy busy slot. Added per-opening exclusions only
on failure (normal admission allocation unchanged), with focused race regression.
This error-path fix postdates frozen678d5... candidate; current matrix remains
evidence for that candidate. Rebuild final, rerun corrected whole matrix and
scale/service on latest hash before final acceptance; no stale same-binary claim.
Root50108 old matrix currentlyasymmetric, make vet test queued afterward.

Old678d5 matrix deliberately interrupted afterretryerror-pathfix. SIGINT stopped
alltestprocesses butleftnamespaces311884; explicitly ranfirewall-cleanup in each,
assertednoSPQownedchains,then deletedall3. Oldpartialmatrix remains diagnostic.
Current finalcandidate ce821890c829a1548ad3ecddb6a01ec16edf48833d2ddac955e0d1f7364e5150
build/super-paqet-final-step1-v2 matchesbuild/super-paqet. No additional code edits
unless a regression fails. Start26profile fullmatrix newoutputstep1-qualified-v2,
thenfullsuite/100k600s/systemd/extraseeds/fuzz. Needsecond checkpoint afterpassing
focusedtests; thirdqualificationcommitafteractualfinalevidence. User requestedcommits.
