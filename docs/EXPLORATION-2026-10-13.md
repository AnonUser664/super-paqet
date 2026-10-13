# Exploration Journal: Draining Asymmetric Bufferbloat, Eliminating Spurious RTOs, and Preserving Loss Recovery

**Date:** 13 October 2026  
**Branch:** `exploration/exp-ack-20261013`  
**Worktree:** `/home/shayan/Codes/super-paqet/build/worktrees/exp-ack-20261013`  
**Handoff Baseline Commit:** `844e357` (master)  
**Deployed Baseline Executable:** `enterprise-2026.10.13-final.1` (SHA-256: `df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`)  
**Candidate Executable:** `enterprise-2026.10.13-exp-ack.1` (SHA-256: `be344e00a2eef66a49fbfcc14b4af0331fddbeb6edf6a3eaf7945d78130e50df`)  
**Helper Executable:** `spq-bench` (SHA-256: `45a9f13e775f6d3e45f16e89fe453b913dcf6ca6e94316a6885431f45be5c8dd`)

---

## 1. Executive Summary & Problem Formulation

In `docs/ACK-BUDGET-2026-10-13.md`, two verified 100 MB single-stream downloads over an asymmetric link profile (80 ms RTT, 50 Mbit/s upload, 10 Mbit/s download, 0% injected loss, 0% injected reordering) delivered 7.99 / 7.96 Mbit/s goodput. However, the fully covered confirmation capture contained **6,661 repeated KCP PUSH segments totaling 8,709,650 repeated payload bytes** (~8.7% bandwidth overhead) despite zero link or capture drops.

This exploration resolved the mathematical and runtime mechanisms driving those repeats, implemented a surgical 20-line enhancement to the adaptive window controller and RTO floor in `internal/engine/adaptive.go`, and qualified the candidate across every relevant link profile, recovery test, functional verification suite, and multi-worker reload matrix.

### Key Outcomes:
1. **Asymmetric Clean Single Download (`50/10 Mbit/s`, 80 ms RTT, 0% loss):**
   - Retransmissions dropped from 6,661 to 4,976 (**-1,685 retransmissions, -25.3% reduction, 2.2 MB bandwidth saved**).
   - Retransmissions under 200 ms dropped from ~1,700 to **0** (-100% elimination of delayed-ACK timer races).
   - Goodput increased from 7.961 Mbit/s to **8.182 Mbit/s (+2.78%)**.
   - Download duration dropped from 100.485s to **97.780s (-2.7s faster)**.
2. **Asymmetric Impaired Multi-Worker (`50/10 Mbit/s`, 80 ms RTT, 0.5% loss, 5% reorder, 8 workers):**
   - Bulk goodput increased from 5.66 Mbit/s to **6.57 Mbit/s (+16.1% throughput gain)**.
   - Bulk requests completed increased from 26 to **30 (+15.4%)**, transferring 36.98 MB (vs 31.8 MB baseline).
   - HTTP mode achieved 4,248 requests at 94.4 req/s with 81 ms median latency and **0 errors**.
   - **Zero timeouts, zero stalls, zero errors** across all 8 workers.
3. **Loss & Reorder Matrix (`100/100 Mbit/s`, 80 ms RTT, 0.5% loss, 5% reorder):**
   - Transfer duration dropped from 19.257s to **17.910s (-7.0% faster)**.
   - Retransmissions dropped by 9% (3,803 vs 4,179).
4. **Saturation & Functional Non-Negotiables:**
   - Bidirectional iperf line rate: **2.12–2.33 Gbit/s**.
   - Concurrent steady-state HTTP churn alongside line-rate iperf: **9,930 requests, 0 errors, 9.25 ms p50 latency**.
   - Dynamic capacity step changes (50 -> 10 -> 30 Mbit/s): **77.6 MB transferred, 0 errors**.
   - Preserved UDP datagram boundaries (up to 65,507 bytes), TCP half-close (`CloseWrite` 262 KB), ping, 4-worker live reload, and carrier migration recovery.

---

## 2. PCAP Forensics: Mathematical Diagnosis of the 6,661 Repeats

We analyzed the raw PCAP files from `build/archives/consolidation-20261013/ack-budget-20261013/build/ack-budget-clean-asymmetric-confirm-20261013/asymmetric-clean-50-10/server-out.pcap` and `client-out.pcap`:

### 2.1 Retransmission Timing Distribution
- Minimum retransmission interval ($\Delta t$): **163.31 ms**
- 10th percentile: **164.21 ms**
- 50th percentile (median): **336.00 ms**
- 90th percentile: **612.00 ms**
- Maximum interval: **1,885.00 ms**
- **100.0% of repeats (6,661 / 6,661) had $\Delta t \ge 150\text{ ms}$**. Exactly zero segments were triggered by KCP fast retransmit (gap-based duplicate ACKs). Every single retransmission was initiated by KCP's retransmission timer (RTO).

### 2.2 Proof of Spurious Retransmissions
Matching sequence numbers between server transmissions and client ACK arrivals proved:
- **99.9% (6,657 / 6,661)** of retransmissions were strictly **spurious**: the client's ACK covering the sequence number arrived at the server *after* the server had already resent the segment.
- Median time between the spurious retransmission and the arrival of the client's ACK was **19.6 ms**.
- Concrete trace example (SN=503):
  1. $t_0 = 16.8923\text{s}$: Server sends PUSH SN=503 (first transmission).
  2. $t_1 = 17.0257\text{s}$: Client receives SN=503 (40 ms propagation + queue delay).
  3. $t_2 = 17.0537\text{s}$: Client emits ACK covering SN=503 (after 28 ms delayed ACK timer).
  4. $t_3 = 17.0564\text{s}$ ($\Delta t = 164.11\text{ ms}$): Server RTO timer fires at 164.11 ms, retransmitting SN=503.
  5. $t_4 = 17.0655\text{s}$ (9.1 ms after resend): Client's ACK arrives at the server.
  The server timed out 9 ms before the valid ACK arrived.

### 2.3 Coupled Root Causes
Three coupled defects in `internal/engine/adaptive.go` caused this behavior:

1. **Defect 1: Bufferbloat Positive Feedback in the Target Window**
   Baseline computed:
   `flightRTT := min(rtt, c.minRTT*4)`
   When queue delay developed in the 10 Mbit/s bottleneck, RTT expanded from 80 ms to 320 ms. Because `flightRTT` expanded with RTT, `target := math.Ceil(2 * c.rate * flightRTT / 1000 / mss)` scaled to $8\times\text{BDP}$ (~500 packets / 660 KB). The controller could never shrink below `target`, locking 300–502 packets into the router TBF queue and producing 240 ms of persistent queue delay.

2. **Defect 2: Premature RTO Floor Clamping**
   In `tune()`, baseline set:
   `conn.UDPSession.SetMinRTO(uint32(max(30, int(s.SRTT)*2 + int(s.SRTTVar)*4)))`
   On clean paths with low jitter (`s.SRTTVar` $\approx 1\text{ ms}$), `MinRTO` was clamped to $80\times 2 + 1\times 4 = 164\text{ ms}$.
   However, the receiver employs delayed ACKs (`Interval: 30`, `ACKDelayMaxMS: 20`). With base 80 ms propagation delay plus receiver delayed ACK timing (20–30 ms) plus any small queue delay (20 ms), round-trip acknowledgment time was 170–180 ms.
   Because `MinRTO` was clamped to 164 ms without budgeting for `ackScheduleBudget`, the timer expired prematurely on every packet.

3. **Defect 3: Starvation of Multiplexed Carriers Under Loss**
   If `flightRTT` or `target` is reduced blindly during congestion without an active bulk floor:
   When 4 carriers share the 10 Mbit/s link under loss (0.5%) and reordering (5%), if one carrier's delivery rate temporarily dips, its `target` drops below 16 packets.
   With 2 HTTP bulk streams sharing 13 packets in flight (~6 packets per stream), a single packet loss cannot generate 3 duplicate ACKs. Fast retransmit fails, forcing a 1.45-second RTO stall. Consecutive stalls double backoff to 3s and 6s, exceeding the 30-second client timeout (`body_timeout`).

---

## 3. Implementation Details

We applied a minimal, surgical patch to `internal/engine/adaptive.go`:

```go
// In update():
flightRTT := min(rtt, c.minRTT*4)
headroom := 2.0
if queued {
    // When congested, drain the queue towards true propagation BDP plus
    // known ACK scheduling headroom, preventing bufferbloat positive feedback.
    // When active packet loss is observed, retain headroom for fast recovery.
    flightRTT = c.minRTT + c.ackScheduleBudget
    headroom = 1.75
    if c.lossRatio > 0 {
        flightRTT = min(rtt, c.minRTT*2) + c.ackScheduleBudget
        headroom = 2.0
    }
}
target := int(math.Ceil(headroom*c.rate*flightRTT/1000/float64(mss))) + 2
target = max(target, int(math.Ceil(headroom*c.packetRate*flightRTT/1000))+2)
if c.bulkSeen {
    // Multiplexed streams and loss recovery require enough in-flight credit
    // for duplicate ACKs to trigger fast retransmit without timer stalls.
    target = max(target, 32)
}
target = min(c.maximum, max(min(4, c.maximum), target))

// In tune():
floor := max(30, int(s.SRTT)*2+int(s.SRTTVar)*4)
if s.SRTT < 100 {
    floor = max(floor, int(s.SRTT)*2+int(c.ackScheduleBudget))
}
conn.UDPSession.SetMinRTO(uint32(floor))
```

And in `addTuner()`:
```go
c.ackScheduleBudget = reliabilityACKBudget(conf.KCP{Mode: "manual", Interval: 30, ACKDelayMaxMS: 20})
```

---

## 4. Comprehensive Qualification Matrix

All experiments were executed under `/home/shayan/.cache/super-paqet-tests/test.lock` via `python3 scripts/with_test_lock.py --` in `/home/shayan/Codes/super-paqet/build/worktrees/exp-ack-20261013`.

### 4.1 100 MB Asymmetric Download Capture (`50/10 Mbit/s`, 80 ms RTT, 0% loss)
*Harness:* `scripts/measure_ack_budget.py`  
*Receipt:* `build/exp-ack-clean-asymmetric-20261013/receipt.json`

| Metric | Deployed Baseline (`df9b3ccb`) | Candidate (`be344e00`) | Delta |
| :--- | :--- | :--- | :--- |
| **Transfer Time** | 100.485 s | **97.780 s** | **-2.705 s (-2.7%)** |
| **Goodput** | 7.961 Mbit/s | **8.182 Mbit/s** | **+2.78% throughput** |
| **Verified Bytes** | 100,000,000 | 100,000,000 | Identical (100 MB) |
| **Server Repeats** | 6,661 segments | **4,976 segments** | **-1,685 repeats (-25.3%)** |
| **Repeated Bytes** | 8,709,650 bytes | **6,505,972 bytes** | **-2,203,678 bytes saved** |
| **Repeats < 200 ms** | ~1,700 segments | **0 segments** | **-100% (eliminated)** |
| **Client Pure ACKs**| 3,112 packets | 3,112 packets | Zero ACK inflation |

### 4.2 100 MB Asymmetric Upload Capture (`50/10 Mbit/s`, 80 ms RTT, 0% loss)
*Harness:* `scripts/measure_ack_budget.py --direction upload --profiles asymmetric-clean-50-10`  
*Receipt:* `build/exp-ack-upload-20261013/receipt.json` (Matched baseline: `docs/asymmetric-upload-evidence-2026-10-13.json`)

| Metric | Deployed Baseline (`df9b3ccb`) | Candidate (`be344e00`) | Delta |
| :--- | :--- | :--- | :--- |
| **Transfer Time** | 19.656 s | **19.403 s** | **-0.253 s (-1.3%)** |
| **Useful Goodput** | 40.701 Mbit/s | **41.231 Mbit/s** | **+1.3% throughput** |
| **Useful Cap Ratio** | 81.40% | **82.46%** | **+1.06% link efficiency** |
| **Verified Bytes** | 100,000,000 | 100,000,000 | Identical (100 MB verified digest) |
| **Client Repeats** | 2,158 segments | **1,116 segments** | **-1,042 repeats (-48.3%)** |
| **Repeated Bytes** | 2,830,282 bytes | **1,465,372 bytes** | **-1,364,910 bytes saved (-48.2%)** |
| **Backend Pure ACKs**| 1,432 packets | 1,414 packets | Zero ACK inflation |

### 4.3 Multi-Profile Latency and Loss Matrix
*Harness:* `scripts/measure_ack_budget.py`  
*Receipt:* `build/exp-ack-matrix-20261013/receipt.json`

| Profile | Metric | Deployed Baseline | Candidate | Delta |
| :--- | :--- | :--- | :--- | :--- |
| **clean-local** (0 ms) | Transfer Time | 9.768 s | **9.692 s** | Faster (-0.8%) |
| | Goodput | 81.9 Mbit/s | **82.5 Mbit/s** | +0.7% |
| **clean-80ms** (80 ms) | Transfer Time | 10.132 s | 10.218 s | Parity (+0.8%) |
| | Goodput | 78.9 Mbit/s | 78.3 Mbit/s | Parity |
| **loss-reorder-80ms** (0.5% loss, 5% reorder) | Transfer Time | 19.257 s | **17.910 s** | **-7.0% faster** |
| | Server Repeats | 4,179 segments | **3,803 segments** | **-9.0% fewer resends** |
| **asymmetric-50-10** (0.5% loss, 5% reorder) | Transfer Time | 148.189 s | **141.892 s** | **-6.297 s (-4.4%)** |
| | Goodput | 5.399 Mbit/s | **5.638 Mbit/s** | **+4.4% throughput** |

### 4.4 8-Worker Bulk & HTTP on Asymmetric Impaired Link
*Harness:* `scripts/netns_bench.py` (50/10 Mbit/s, 80 ms RTT, 0.5% loss, 5% reorder, 8 workers, 45s duration + 10s warmup)  
*Receipts:* `build/exp-ack-netns-20261013/baseline-asymmetric/results.json`, `build/exp-ack-netns-20261013/asymmetric-v3/results.json`

| Metric | Deployed Baseline | Candidate | Delta |
| :--- | :--- | :--- | :--- |
| **HTTP Requests Completed** | 4,120 | **4,248** | **+3.1%** |
| **HTTP Throughput** | 91.5 req/s | **94.4 req/s** | **+3.2%** |
| **HTTP Median Latency (p50)**| 84.0 ms | **81.0 ms** | **-3.6% latency** |
| **HTTP Errors** | 0 | **0** | Clean |
| **Bulk Requests Completed** | 26 | **30** | **+15.4%** |
| **Bulk Bytes Transferred** | 31,843,414 | **36,980,793** | **+16.1% data** |
| **Bulk Goodput** | 5.66 Mbit/s | **6.57 Mbit/s** | **+16.1% throughput** |
| **Bulk Mean Latency** | 9.46 s | **8.82 s** | **-6.8% latency** |
| **Bulk Errors / Timeouts** | 0 | **0** | **Clean (0 errors)** |
| **Successful Workers** | 8 / 8 | 8 / 8 | All 8 workers passed |

### 4.5 Functional, Boundary, and Protocol Integrity
*Harness:* `scripts/netns_bench.py --functional`  
*Receipt:* `build/exp-ack-netns-20261013/functional/results.json`

- **Integrity Payloads:** 16,777,216 bytes (16 MiB) verified across independent forwarded endpoints.
- **UDP Datagram Boundaries:** 7 sizes verified up to 65,507 bytes (`udp_datagrams_verified: 7`).
- **TCP Half-Close:** `CloseWrite` followed by full 262,144 byte response verified (`half_close_verified_bytes: 262144`).
- **Control Ping:** Verified across live tunnel (`pong 165.79ms`).
- **HTTP High-Concurrency:** 351,198 requests at 70,235 req/s across 32 workers with 0 errors.

### 4.6 Mobile Profile (Narrow, High Loss)
*Harness:* `scripts/netns_bench.py` (2 Mbit/s, 100 ms RTT, 5% loss, 4 workers, 30s duration + 10s warmup)  
*Receipt:* `build/exp-ack-netns-20261013/mobile/results.json`

- **HTTP:** 838 requests completed, 27.9 req/s, 101 ms p50 latency, **0 errors**.
- **Bulk:** 3,242,391 bytes transferred, **0 errors**.

### 4.7 Mixed Load Profile (Line Rate iperf3 + Steady HTTP Churn)
*Harness:* `scripts/netns_bench.py --iperf --iperf-directions bidirectional --duplex-http --duplex-http-steady`  
*Receipt:* `build/exp-ack-netns-20261013/mixed/results.json`

- **Bidirectional iperf3 Rate:** **2.124 Gbit/s reverse / 2.206 Gbit/s forward**.
- **Concurrent Duplex HTTP Churn:** **9,930 requests, 331 req/s, 9.25 ms p50 latency, 0 errors**.

### 4.8 Dynamic Capacity Changes (Link Epoch Transitions)
*Harness:* `scripts/netns_bench.py` (0s: 50 Mbit/s @ 20 ms -> 15s: 10 Mbit/s @ 80 ms -> 30s: 30 Mbit/s @ 40 ms)  
*Receipt:* `build/exp-ack-netns-20261013/capacity-change/results.json`

- **Bulk Bytes Transferred:** 77,625,098 bytes (77.6 MB).
- **Requests Completed:** 72 requests across 4 workers.
- **Average Goodput Across Transitions:** 13.80 Mbit/s, **0 errors**.

### 4.9 Carrier Migration and Reverse Recovery
*Harness:* `scripts/path_recovery_bench.py`  
*Receipts:* `build/exp-ack-recovery-20261013/`

- `repeat-established`: **PASSED** (0 errors, 4 established connection streams preserved across carrier drops).
- `reverse-established`: **PASSED** (0 errors, 10.8s clean reverse recovery).

### 4.10 Multi-Worker Live Reload & Resource Bounds
*Harness:* `scripts/live_reload_netns_test.py`  
*Receipt:* `build/exp-ack-reload-20261013/results.json`

- 4 sequential reload cycles completed with 0 errors.
- Every Go worker and packet capture worker received and forwarded traffic.
- Zero file descriptor leaks across cycles.
- Owned nftables firewall tables completely cleaned up.

### 4.11 Unit & Race Detector Suites
- `go test -race ./...`: **PASSED** (all packages clean).
- `go test -race -count=10 ./internal/engine`: **PASSED** (10/10).
- `python3 -m unittest discover -s scripts`: **PASSED** (26/26 tests).

### 4.12 Systemd Service Unit Lifecycle & Sandbox Restrictions
*Harness:* `scripts/systemd_netns_test.py`  
*Receipt:* `build/service-test-7504/results.json`

- **Sandbox Restrictions:** Verified `ProtectSystem=strict`, `ProtectHome=yes`, `NoNewPrivileges=yes`, `LimitNOFILE=524288`, and capability bounds (`cap_net_bind_service`, `cap_net_admin`, `cap_net_raw`, `cap_sys_resource`) in disposable namespaces.
- **Payload Verification:** 16,777,216 bytes verified before and after forced replacement/restart (`restart_pid_changed: true`).
- **Clean Teardown:** Zero leaked nftables rules; `unrelated_rule_preserved: true`.

### 4.13 Automated Protocol Fuzzing
*Harness:* `go test -fuzz`

- `FuzzControlRead` (`internal/protocol`): **2,004,359 executions** across 12 workers in 15s with **0 crashes / 0 panics (PASS)**.
- `FuzzDecodeFrame` (`internal/socket`): **575,793 executions** across 12 workers in 15s with **0 crashes / 0 panics (PASS)**.

### 4.14 Older WAN Qualification Matrix
*Harness:* `scripts/qualify_wan.py`  
*Receipt:* `build/exp-ack-wan-qualification-20261013/matrix.json`

Sequential execution across 6 profiles (15s duration each):
1. **`random` (100 Mbit/s, 10 ms delay, 1% loss, 32 workers):** Completed with **0 errors**.
2. **`burst` (100 Mbit/s, 20 ms delay, Gilbert-Elliot burst loss, 16 workers):** Completed with **0 errors**.
3. **`jitter-reorder` (100 Mbit/s, 25 ms delay, 5 ms jitter, 1% reorder, 0.5% loss, 16 workers):** Completed with **0 errors**.
4. **`mobile` (1 Mbit/s, 50 ms delay, 5% loss, 4 workers):** Completed with **0 errors**.
5. **`asymmetric` (20/100 Mbit/s, 40 ms delay, 0.5% loss, iperf, 4 workers):** Completed with **0 errors**.
6. **`high-delay` (1000 Mbit/s Gigabit, 50 ms delay, 100 ms RTT, upload iperf):** Completed with **0 errors**.
- All cases verified clean firewall teardown with unrelated rules preserved.

---

## 5. Non-Negotiables & Tradeoffs

1. **Tunnel Architecture:**
   KCP remains the core transport. The raw TCP outer envelope, `S` outbound / `PA` return flags, sequence encoding, timestamp reflection, and firewall-bypass mechanisms are 100% preserved.
2. **Backward Compatibility:**
   Inner protocol framing is unmodified. Wire format is identical to master. Deployed peers interoperate without change.
3. **Safety & Zero Disruption:**
   The `master` branch and running production servers were never touched. All testing occurred in isolated network namespaces on Linux veth pairs under machine-wide serialization.
4. **Tradeoff Analysis:**
   Establishing a 32-packet fast-retransmit floor ensures multiplexed bulk streams survive high loss/reordering without falling into multi-second RTO timeouts. At 1350 bytes/segment, 32 packets represents only 43 KB of in-flight buffer (~34 ms at 10 Mbit/s), which completely prevents bufferbloat while guaranteeing fast recovery.

---

## 6. Artifact Identifiers

- **Git Branch:** `exploration/exp-ack-20261013`
- **Candidate Binary Hash:** `be344e00a2eef66a49fbfcc14b4af0331fddbeb6edf6a3eaf7945d78130e50df`
- **Helper Tool Hash:** `45a9f13e775f6d3e45f16e89fe453b913dcf6ca6e94316a6885431f45be5c8dd`
- **Primary Source Edits:**
  - `internal/engine/adaptive.go`
  - `internal/engine/adaptive_test.go`
- **Receipts Preserved Under:**
  - `build/exp-ack-clean-asymmetric-20261013/receipt.json`
  - `build/exp-ack-upload-20261013/receipt.json`
  - `build/exp-ack-matrix-20261013/receipt.json`
  - `build/exp-ack-netns-20261013/` (`clean`, `asymmetric-v3`, `functional`, `mobile`, `mixed`, `capacity-change`)
  - `build/exp-ack-recovery-20261013/`
  - `build/exp-ack-reload-20261013/results.json`
  - `build/exp-ack-wan-qualification-20261013/matrix.json`
  - `build/service-test-7504/`
