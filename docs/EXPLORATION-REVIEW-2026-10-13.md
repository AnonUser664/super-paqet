# Selective exploration review — 13 October 2026

Reviewed `exploration/exp-ack-20261013` at `43a9792`, against master `beda8eb`.
This review leaves production hosts, services and configurations unchanged.
Production continues using `enterprise-2026.10.13-final.1`, runtime source
`37904d6`, SHA-256
`df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`.

## Evidence audit

The [original journal](archive/exp-ack-20261013/EXPLORATION-2026-10-13.md) is
preserved with its claims marked as historical. Its results mix at least four
candidate executables. Of 54 completed result receipts, only five name the final
`c81eaf1b...` executable: refined 100 MB download, 20,000-connection scale,
deep rate step, pcap and systemd lifecycle. Most of the 26-profile stress matrix
names `be344e00...`, an earlier candidate. The [receipt index](exploration-ack-receipt-index-2026-10-13.json)
records actual hashes, configurations and errors rather than transferring
qualification between revisions.

The final 20,000-connection result is useful finite capacity evidence, but uses
eight sessions, AES-GCM, default KCP options, PA outbound and unrestricted worker
budgets. It does not certify the four-session null/manual S/PA production profile.
Earlier asymmetric candidates reported four errors (two during warmup), then two
errors. Those failed attempts remain recorded alongside later passing attempts.
The original single-download/upload improvements are historical comparisons, not
repeated contemporary paired controls. Retry timings below 200 ms disappearing
under a raised timer floor alone do not prove all spurious retransmissions are
eliminated or establish a cause for every repeated segment.

Two added unit tests were weak: the low-RTT test acknowledged no new data and
returned before window targeting; the narrow-floor test observed only one
startup growth step, which could pass with a rigid 32-packet target. The timer
test repeated the formula without running the tuner. Stronger local candidates
exercise sustained delivery and the actual session retry timer. Their source
and qualification logs are preserved privately; tests requiring rejected
runtime behavior are not merged into master.

## Selection

- Retain verified upload support in `scripts/measure_ack_budget.py`: receiver-side
  SHA-256 verification, correct data/ACK side selection, directional capture
  coverage and explicit direction in receipts. Default download behavior stays
  compatible. Its helper SHA-256 `7222008e...` exactly matches the already-verified
  100 MB upload measurement; root Python checks also pass.
- Correct the configuration guide's obsolete claim that production still uses a
  50 ms reordering ceiling; final.1 already uses the bounded 200 ms ceiling.
- Preserve original experiment history, receipts, failed gates and the review.
- Exclude the full adaptive target/headroom/bulk-floor patch from the accepted
  runtime. Matched impaired-link tests expose application timeout failures and
  variable throughput; earlier passing profiles are insufficient to override
  those observations.
- Exclude the isolated delayed-ACK retry-floor change. In the longer impaired
  comparison it delivers 4.878 versus 5.381 Mbit/s (-9.33%) with four body
  timeouts. The baseline also fails this stress case; this is a comparison and
  a qualification limit, not a claim that the baseline is flawless.
- Exclude hard-coded manual/30/20 timing in `addTuner`; configured endpoint
  registration already derives its ACK allowance from real settings. No fixed
  policy should masquerade as configuration-independent adaptation.

No application runtime or dependency bytes change in the final selected tree.
This protects the live deployment from the experimental tradeoffs. It does not
claim universal performance optimality or eliminate the outstanding stress case.

## Independent matched checks

Exact deployed baseline `df9b3ccb...`, rebuilt full candidate `2b3cd26a...`, and
isolated timer candidate `f6eca498...` use separate immutable executables.
Cases use four sessions with fixed separate source ports, two backend capture
workers, one client capture worker, null encryption, manual 0/30/2/nc=1,
MTU1350 KCP within a 1500-byte interface, 4096 windows, deployed mux sizes,
S outbound / PA return, and fixed CPU placement (client 0–3, backend 4–5,
generators 6–11) with GOMAXPROCS 4/2. Seed 816 is retained. These component
screens omit production path recovery and use endpoint shaping rather than a
middle WAN bridge; they are not full fleet-capacity acceptance. Baseline and
candidates have identical fixture choices within each paired comparison.

The first screen checks impaired 50/10 Mbit/s at 80 ms RTT with 0.5% loss/5%
reorder, unbounded clean upload/download, narrow 1 Mbit/s at 20 ms RTT with 1%
loss, and simultaneous 100/100 Mbit/s at 80 ms RTT beside HTTP churn. Clean
upload/download is 5.788/3.374 Gbit/s baseline versus 5.947/3.474 candidate.
The short impaired run changes 5.182 to 3.055 Mbit/s, while improving completed
worker coverage from two to eight and avoiding its baseline timeout errors.
Mixed latency improves, with a small throughput reduction: no universal win.
Both narrow four-worker bulk runs complete zero timed requests, so their zero
error counts are explicitly **inconclusive**, not acceptance.

A second matrix lengthens impaired bulk to 60s measured plus 15s warmup, reduces
narrow bulk to one worker so completed transfers are observable, and repeats
mixed load. The impaired full candidate yields 5.455 Mbit/s and three total
errors, baseline 5.381 Mbit/s and ten, isolated timer 4.878 Mbit/s and four.
The error counts include warmup and distinguish body timeouts from request EOF.
`warmup_errors` is a subset of `errors`; adding them would double-count it.
The timer-only run also transfers more bytes attributed to requests begun during
warmup. Across the complete 75-second interval, baseline/timer byte totals differ
by only about 0.6%; the 9.33% measured-window drop alone does not establish a
sustained capacity regression. It remains a failed error-free selection gate.
Improved failure counts do not establish error-free qualification.

All workloads run serially under the machine-wide reservation, with unique
output directories, exact executable hashes, commands, placement, resources,
workload results and verified teardown. An initial fixture attempt lacked the
baseline libpcap search path and used too small an interface MTU; no workloads
ran successfully. A native attempt picked up private `.go` snapshots as a
package; those snapshots were renamed `.go.txt` and the native checks rerun.
Both failed setup attempts are retained rather than overwritten.

## Remaining investigation

A future exploration should first reproduce the sustained multiplexed impaired
case on a middle bridge with production recovery enabled, then separate socket
queue loss, ordered-delivery stalls, carrier fairness and the 30-second HTTP
request deadline. Compare multiple seeds and alternate baseline/candidate order.
The clean-link retry reductions warrant further timed original/ACK correlation;
a minimum interval or eventual ACK alone is insufficient causal attribution.
The rejected changes remain recoverable through source ancestry and private
archives, so future work can start from these observations without repeating
unsupported acceptance claims.

## Completed checks and cleanup

The [independent receipt](exploration-ack-review-evidence-2026-10-13.json) retains
all seventeen completed screen/ablation cases and the eight failed fixture setup
attempts, with their exact results. The longer one-worker narrow test completes
three requests for baseline/full and two for timer-only, with no errors; useful
rates are 0.747/0.721/0.702 Mbit/s. Mixed-load follow-up shows benefits alongside
large baseline variability; it does not overturn the failed impaired gates.
These measurements justify retaining the controller unchanged, rather than
claiming that every candidate is inherently worse on every network.

The selected tree passes an uncached root race suite, root vet, all 26 Python
checks, and a byte-diff check proving no application runtime/dependency changes.
The new upload helper exactly matches the prior receiver-verified 100 MB run.
Source ancestry records the reviewed branches while retaining the selected tree;
all original candidate source remains recoverable without enabling it.

Completed worktrees and local branch refs are removed after preserving their
ignored evidence under `build/archives/exploration-review-20261013/`. A Git bundle
and manifest map original paths to archived files with sizes and SHA-256 values.
Private receipt paths are historical execution locations; consult that mapping
after cleanup. Failed attempts and interrupted evidence are retained. No active
agent workload is terminated, and no production host is modified.

[Completed cleanup receipt](exploration-ack-cleanup-evidence-2026-10-13.json)
records the sole master checkout, retained 1,738 artifacts, archive manifest and
Git bundle hashes. All retired candidate source is reachable from master.
