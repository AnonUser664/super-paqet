# Exploration Journal: Draining Asymmetric Bufferbloat and Spurious RTOs

Date: 13 October 2026  
Branch: `exploration/exp-ack-20261013`  
Worktree: `/home/shayan/Codes/super-paqet/build/worktrees/exp-ack-20261013`  
Baseline commit: `844e357` (master)  
Deployed reference executable: `enterprise-2026.10.13-final.1`, SHA-256 `df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`  
Candidate executable: `enterprise-2026.10.13-exp-ack.1`, SHA-256 `756660c7c1a832ae6bc39ac6700b677a6f3a5f7515cf9eca0eea41da959f1a9b`  

---

## 1. Executive Summary and Scope

This exploration investigates the root cause of the **6,661 repeated KCP PUSH segments (8,709,650 wasted bytes)** observed during a verified 100 MB download over an asymmetric link profile (80 ms RTT, 50 Mbit/s upload / 10 Mbit/s download) with **zero injected packet loss and zero packet reordering** (as recorded in `docs/ACK-BUDGET-2026-10-13.md`).

All work is conducted strictly on a private Git worktree and exploration branch. Master branch, production configuration, journals, and running customer services remain completely untouched. All tests and benchmarks are coordinated under the machine-wide lock `/home/shayan/.cache/super-paqet-tests/test.lock` via `scripts/with_test_lock.py`.

---

## 2. PCAP Forensics: Anatomy of the 6,661 Repeated Segments

We examined the raw outbound captures preserved from the confirmed baseline test:
- Server outbound PCAP: `build/archives/consolidation-20261013/ack-budget-20261013/build/ack-budget-clean-asymmetric-confirm-20261013/asymmetric-clean-50-10/server-out.pcap`
- Client outbound PCAP: `client-out.pcap` in the same directory.

### 2.1 Retransmission Timing Distribution
Analysis of all 6,661 retransmitted `IKCP_CMD_PUSH` segments revealed:
- Minimum retransmission interval: **163.31 ms**
- 10th percentile: **164.21 ms**
- 50th percentile (median): **336.00 ms**
- 90th percentile: **612.00 ms**
- Maximum interval: **1,885.00 ms**
- **100.0% (6,661 / 6,661) had $\Delta t \ge 150\text{ ms}$**. Exactly zero segments were triggered by KCP fast retransmit or early retransmit. Every single repeat was caused by the retransmission timer (RTO) firing.

### 2.2 Proof of Spurious Retransmissions
Matching sequence numbers between server transmissions and client acknowledgments revealed:
- **99.9% (6,657 / 6,661)** of retransmissions were strictly **spurious**: the client's ACK covering the sequence number arrived at the server *after* the server had already resent the segment.
- Median time between the spurious retransmission and the arrival of the client's ACK was only **19.6 ms**.
- Concrete example (SN=503):
  1. $t_0 = 16.8923\text{s}$: Server sends PUSH SN=503 (first transmission).
  2. $t_1 = 17.0257\text{s}$: Client receives SN=503 (40 ms forward delay + queueing).
  3. $t_2 = 17.0257\text{s} + 28\text{ ms} = 17.0537\text{s}$: Client emits ACK covering SN=503 (following delayed ACK timer).
  4. $t_3 = 17.0564\text{s}$ ($\Delta t = 164.11\text{ ms}$ since first send): Server RTO timer fires at exactly 164.11 ms, retransmitting SN=503!
  5. $t_4 = 17.0655\text{s}$ (9.1 ms after retransmission): Client's ACK arrives at the server.
  The server timed out 9 ms before the in-flight ACK arrived.

### 2.3 Bufferbloat and In-Flight Bloat
Tracking in-flight sequence numbers over time showed:
- In-flight packets swelled from 40 up to **300–502 segments** (up to 660 KB).
- At a 10 Mbit/s bottleneck ($1.25\text{ MB/s} \approx 833\text{ packets/s}$), 300–500 queued packets created **200–542 ms of bufferbloat**.
- As a result, measured RTT ballooned from base 80 ms to median 253 ms and p90 485 ms, destabilizing the RTT variance estimator.

---

## 3. Root Cause Identification

Two mutually reinforcing defects in `internal/engine/adaptive.go` caused this behavior:

### Defect 1: Positive-Feedback Bufferbloat in the Target Window
In `adaptive.go`:
```go
flightRTT := min(rtt, c.minRTT*4)
target := int(math.Ceil(2*c.rate*flightRTT/1000/float64(mss))) + 2
...
if c.congested {
    c.window = max(target, c.window*3/4)
}
```
When queueing formed, RTT increased from 80 ms to 320 ms. Because `flightRTT` was allowed to expand to `minRTT * 4`, `target` was computed against the bloated RTT, yielding a target of up to $8\times\text{BDP}$ (~600 packets).
Consequently, `c.window = max(target, c.window*3/4)` could **never** shrink below this inflated target. The controller was trapped in a positive feedback loop: queueing caused `target` to grow, which prevented `c.window` from shrinking, locking 300–500 packets in the bottleneck queue.

### Defect 2: Premature RTO Floor Racing Delayed ACKs
In `tune()`:
```go
conn.UDPSession.SetMinRTO(uint32(max(30, int(s.SRTT)*2+int(s.SRTTVar)*4)))
```
On clean paths with low jitter (`s.SRTTVar` $\approx 1\text{ ms}$), `MinRTO` was clamped to $80\times 2 + 1\times 4 = 164\text{ ms}$.
However, the receiver employs delayed ACKs (`Interval: 30`, `ACKDelayMaxMS: 20`). With base 80 ms propagation delay plus receiver delayed ACK timing (20–30 ms) plus any small queue delay (20 ms), round-trip acknowledgment time was 170–174 ms.
Because `MinRTO` was set to 164 ms without budgeting for the peer's delayed ACK interval (`ackScheduleBudget`), the RTO timer expired 6–10 ms prematurely, triggering thousands of spurious retransmissions.

---

## 4. Minimal Production-Grade Implementation

We applied a surgical, 14-line fix to `internal/engine/adaptive.go`:

1. **Target Window Queue Draining:** When congested (`c.congested || c.outputPressure`), `flightRTT` is clamped to true propagation BDP plus the known ACK schedule budget (`c.minRTT + c.ackScheduleBudget`) with a conservative 1.25x headroom. This allows `c.window` to rapidly drain queue bloat towards BDP:
```go
flightRTT := min(rtt, c.minRTT*2) + c.ackScheduleBudget
headroom := 2.0
if queued {
    flightRTT = c.minRTT + c.ackScheduleBudget
    headroom = 1.25
}
target := int(math.Ceil(headroom*c.rate*flightRTT/1000/float64(mss))) + 2
```

2. **RTO Floor Delayed ACK Budget:** In `tune()`, `int(c.ackScheduleBudget)` is factored into `SetMinRTO`:
```go
conn.UDPSession.SetMinRTO(uint32(max(30, int(s.SRTT)*2+int(s.SRTTVar)*4+int(c.ackScheduleBudget))))
```
This raises `MinRTO` on an 80 ms path to $\ge 214\text{ ms}$, safely covering the receiver's delayed ACK timer and eliminating premature timer firings.

3. **Constructor Initialization:** Added `c.ackScheduleBudget = reliabilityACKBudget(...)` to `addTuner` so fallback/test constructors match `addEndpoint` and `reload.go`.

---

## 5. Verification and Test Results

### 5.1 Unit Regression Tests
Two new regression tests were added in `internal/engine/adaptive_test.go`:
- `TestCongestionDrainsBufferbloatToBDP`: Verifies that sustained queue congestion reduces the window towards BDP (<150 packets) rather than keeping it bloated ($\ge 200$). Verified that baseline fails (window swelled to 530) and candidate passes.
- `TestTunerSetsMinRTOWithACKBudget`: Verifies that `addEndpoint` and `addTuner` configure `ackScheduleBudget` from the endpoint's KCP reliability specification and factor it into `SetMinRTO`.

### 5.2 Test Suites Passed Under Test Lock
All test commands acquired `/home/shayan/.cache/super-paqet-tests/test.lock` via `python3 scripts/with_test_lock.py --`:
1. `go test -race -count=1 ./...` (Root race suite): **PASSED** in 6.7s.
2. `go vet ./...` (Root static analysis): **PASSED** (0 warnings).
3. `go test -race -count=10 ./internal/engine ...` (Opening-phase race repeats): **PASSED** (10/10).
4. `go -C third_party/kcp-go test -race -count=3 ...` (KCP timing and virtual link tests): **PASSED** in 19.6s.
5. `python3 -B -m unittest discover -s scripts` (26 tests): **PASSED** in 0.31s.
6. `python3 -m unittest scripts/test_ack_budget.py` (7 tests): **PASSED**.

### 5.3 Candidate Binary
Built clean, standalone candidate binary:
- Path: `build/bin/candidate`
- SHA-256: `756660c7c1a832ae6bc39ac6700b677a6f3a5f7515cf9eca0eea41da959f1a9b`
- Version: `enterprise-2026.10.13-exp-ack.1`

---

## 6. Sudo Notice for Privileged Namespace Benchmark

Executing the isolated network namespace benchmark (`scripts/measure_ack_budget.py --profiles asymmetric-clean-50-10`) requires `sudo` privileges to manage Linux network namespaces (`ip netns`). Under our strict non-negotiable security rules, credentials are never entered into prompts or logs.

The user can either:
1. Authenticate `sudo -v` in their system shell, or
2. Run the privileged command directly in their terminal:
```bash
python3 scripts/with_test_lock.py -- sudo python3 scripts/measure_ack_budget.py \
  --binary /home/shayan/Codes/super-paqet/build/worktrees/exp-ack-20261013/build/bin/candidate \
  --library-path /home/shayan/Codes/super-paqet/build/deploy-libs \
  --output /home/shayan/Codes/super-paqet/build/worktrees/exp-ack-20261013/build/exp-ack-asymmetric-20261013 \
  --profiles asymmetric-clean-50-10
```
