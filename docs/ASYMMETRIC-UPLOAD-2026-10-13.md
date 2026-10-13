# Verified upload on the 50 Mbit/s direction

The user requested the missing upload measurement on the same asymmetric link
without interfering with another agent's ongoing experiments. This isolated
namespace run uses the exact deployed final.1 executable, SHA-256
`df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`.
At the measurement checkpoint, production and master were unchanged. This
report can be integrated independently of the measurement harness changes.

## Link and workload

- Client-to-backend cap: **50 Mbit/s**.
- Backend-to-client cap, carrying upload acknowledgments: **10 Mbit/s**.
- Propagation delay: **40 ms each way**, nominal **80 ms RTT**.
- Injected loss/reordering: **zero**; observed qdisc drops/reordering: **zero**.
- One cold HTTP POST containing exactly **100,000,000 application bytes**.
- Four configured carrier slots, two backend capture workers, adaptive control,
  S outbound / PA return, no encryption/FEC and the deployed KCP/mux settings.

The target hashes the received body and returns its actual byte count and
SHA-256. The client compares the receipt with the deterministic expected digest
`5775b33226f152a0b1640906a59c1081149f8832aa4f7d0113453d0a864e8a22`.
Timing covers connection opening, upload and arrival of that verification receipt,
rather than merely successful writes to the client's local TCP socket.

## Result

| Measurement | Value |
| --- | ---: |
| Verified application bytes | 100,000,000 |
| Transfer time | 19.655545 seconds |
| Useful upload throughput | **40.700983 Mbit/s** |
| Useful rate / nominal upload cap | **81.402%** |
| Returning ACK bytes including IP/TCP attribution | 108,904 |
| ACK overhead / body bytes | 0.108904% |
| Average ACK bandwidth | 0.044325 Mbit/s |
| Unique captured upload KCP payload | 100,025,789 bytes |
| Repeated upload KCP payload | 2,830,282 bytes in 2,158 repeated PUSH records |

Both endpoint captures report zero capture-kernel drops; distinct KCP upload
payload covers the full application body plus protocol/mux bytes. The virtual
link's two directional rate settings are confirmed by retained qdisc receipts.
Application logs show zero errors/aborts, the tunnel processes exit normally,
owned firewall rules are removed and the unrelated sentinel survives. The Python
target's expected SIGTERM is distinguished from tunnel process failures.

For comparison, the earlier clean download on the **10 Mbit/s direction** measured
7.99/7.96 Mbit/s for two verified 100 MB bodies. Thus the measured cold upload and
download both use approximately 80% of their respective raw caps. This is not a
proof of identical steady-state efficiency: directions differ in framing and
feedback, and upload timing also includes the receiver's receipt.

Repeated KCP payload remains present despite no injected loss/reordering. Its
exact cause is not established by this run; no retransmission/controller change
was applied. Protocol framing, repeated sends and startup/feedback waiting can
all contribute to useful throughput being below the cap. Average ACK traffic
uses only about **0.443%** of the 10 Mbit/s return capacity, so average return
bandwidth exhaustion does not explain this particular result.

## Coordination and reproduction

The other agent's functional/mobile test was detected and left untouched.
Preparation occurred in `measurement/asymmetric-upload-20261013` and its private
worktree. The shared test wrapper checked processes/namespaces and acquired
`/home/shayan/.cache/super-paqet-tests/test.lock` only after the prior workload
ended. It retained that reservation across sudo, the transfer, capture and owned
cleanup. After release, the other agent resumed its mixed-load test. None of its
processes, worktrees, namespaces, files or shared build outputs were modified.

The integrated `scripts/measure_ack_budget.py` harness accepts `--direction upload` and selects the client
capture for complete payload coverage and the backend capture for ACK cost.
Download remains the default. Each target/generator uses bounded 64 KiB chunks.
Seven accounting parser checks also pass after waiting for the other agent's
mixed-load test to release the reservation.
The [receipt](asymmetric-upload-evidence-2026-10-13.json) records the full command,
hashes, actual qdiscs, byte accounting, cleanup and capture logs. Raw evidence is
in this worktree's unique `build/asymmetric-upload-20261013-run1/` directory.

The original run used measurement checkpoint `61a384a`; its upload support is
now integrated on master after [selective review](EXPLORATION-REVIEW-2026-10-13.md).
Run future measurements under `scripts/with_test_lock.py` outside sudo and use
a new output directory. Supply `--direction upload --profiles asymmetric-clean-50-10`
with an explicit final executable and any required shared-library directory.
This one-stream single run does not qualify multi-customer capacity, lossy upload,
steady-state peak throughput or a new production release.
