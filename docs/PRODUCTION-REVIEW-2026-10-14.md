# Production review — 14 October 2026

Production is healthy at the final checks around **16:17 UTC**, with an important
France reliability finding in the preceding day. No production service,
configuration, firewall rule or primary Xray/Nginx process was changed by this
review. Source and executable remain the deployed final.1 release.

## Current health and application checks

All five tunnel services are active, enabled, report health `ok`, and run SHA-256
`df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`.
Tunnel PIDs match the previous deployment and remain unchanged across the review;
automatic restart counts and cgroup OOM counters are zero. No carrier is currently
suspect, awaiting recovery or opening-blocked. Configuration hashes are unchanged
across these checks; warn logging and profiling-off settings remain untouched.

| Host | Active handled connections at final snapshot | Tunnel service memory |
|---|---:|---:|
| Client 94.139.180.109 | 6,077 | 279 MiB |
| Client 94.139.180.142 | 0 | 24 MiB |
| Germany | 2,421 | 127 MiB |
| Finland | 1,918 | 109 MiB |
| France | 1,606 | 92 MiB |

Client/backend counts are different views of handled streams; do not add them as
unique users. Service memory is cgroup `MemoryCurrent`, rather than Go heap alone.
The larger client had around 6,200 active streams during initial inspection;
its full observer window has peaked at 11,492. That peak can predate this review's
24-hour journal interval. .142 has little customer traffic, so its current
application checks do not qualify thousands of continuously active customers.

Both clients pass two authenticated **1 MiB HTTPS transfers per expected path**:
Germany and France Reality, plus Shadowsocks through all three countries. This
is ten passing client/application paths and twenty successful transfers.
Finland Reality times out on both clients, retaining the exception the user
previously accepted. Finland Shadowsocks passes. No inference that Finland's
whole tunnel is down is made from the Reality failure.

The existing observer services are active and writing fresh samples. Windows
remain bounded through 19 October on .109/backends and 20 October on .142. Disk
space and host memory remain available; no OOM, panic or fatal tunnel event is
found in the reviewed records. .142 kernel audit suppression messages are not
classified as tunnel crashes. Recent retained samples show no new application
errors on any host; .109 adds only five opening retries over its initial
approximately 3.5-hour sample tail and no additional retries during the review.
All hosts add zero application errors between initial and final checks.

## France findings that must remain visible

The preceding day is **not a clean stability pass**. Client .109 experienced two
large France opening-failure bursts:

| UTC window | Tehran window | Measured counter evidence |
|---|---|---|
| 13 October approximately 17:54–18:00 | 21:24–21:30 | The surrounding 17:50–18:05 samples add 40,756 retry starts and 12,914 failed handlers. |
| 13 October approximately 19:53–19:57 | 23:23–23:27 | The surrounding 19:50–20:05 samples add 33,181 retry starts and 9,157 failed handlers. |
| 13 October approximately 21:38 | 14 October 01:08 | Five probes, three successful replacements, two failed probes in the selected 15-minute interval. |
| 14 October approximately 06:36–06:38 | 10:06–10:08 | Eleven probes, four successful replacements, seven failed probes and seventeen failed handlers in the surrounding interval. |

Retry starts are attempts, not distinct customers. Handler failures are not
process crashes. The larger episodes fail mainly before ACK2 (the backend's
opening receipt), affecting both France port-443 and port-8880 forwards. Other
country traffic continues. The tunnel processes do not restart.

During the two large bursts, some France ACKed-byte and received-byte counters
continue to advance; sampled KCP remote windows remain 4094/4095 or greater and
mux receive-blocked gauges stay zero. No source recovery starts in those two
selected windows. This is **partial progress with failed openings**, not evidence
of a complete bidirectional IP blackout. The recovery policy deliberately resets
its failure baseline on acknowledged outgoing progress, which explains why a
fully stalled-carrier detector is insufficient evidence for recovery here.
Whether the trigger is selective packet impairment, ordered opening delivery,
application-level scheduling or another condition remains unproven. Ten-second
snapshots do not exclude shorter queue/CPU pressure.

Eight explicit France replacements are visible in the 24-hour warning journal:
three preserve established sessions, three fall back because migration capability
is unavailable, and two fall back after the old carrier's keepalive timeout.
Backend session-close warnings can include retired/probe carriers; they are not
all independently counted customer outages. Fresh-tuple probes sometimes fail,
so configured 10s detection cannot guarantee a verified replacement in 10s.
The partial-progress opening case and missed preservation are follow-up targets,
not problems claimed fixed by today's healthy checks.

## Treatment of operator restarts

The user reported Nginx/Xray restarts, and those are considered separately from
tunnel crashes. The currently observed backend Xray processes started around
**13 October 13:18 UTC**, before the large warning windows, and run in user-session
scopes rather than `xray.service`. Therefore an inactive systemd Xray unit does
not mean the running destination service is down. The relevant Nginx process
identities also predate the review window. Reloads, configuration operations or
other processes can change behavior without changing these identities; approximate
operator timestamps were requested to correlate the events. No unconfirmed
restart or filtering attribution is made. Nginx/Xray restarts do not require a
tunnel restart.

## Evidence and scope

The [review receipt](production-review-evidence-2026-10-14.json) records exact
hashes, services, memory events, counter intervals, route outcomes and private
receipt hashes. Full samples and journals stay in the private unique
`build/production-review-20261014/` output directories; public evidence contains
no application authentication configuration or customer payloads.

An initial collector query used an ISO offset timestamp that older systemd
versions rejected. Empty results from those failed commands were discarded;
explicit UTC queries then succeeded on every host. An initial isolated probe
controller used an unset default host; its owned executable was cleaned up and
the corrected matrix completed. Those failed setup receipts remain preserved.
Authentication entered only prompts/stdin, and every owned probe process and
executable was removed. No healthy customer service was restarted for testing.

This check establishes current expected-path functionality and finite observed
health. It does not erase the France incidents, certify all future links or
establish sustained maximum hardware capacity. No new tuning was deployed.
