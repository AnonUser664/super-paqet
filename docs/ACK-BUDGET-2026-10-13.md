# Acknowledgment bandwidth for verified 100 MB transfers

## Latest clean asymmetric result in both directions

The 50 Mbit/s client-to-backend / 10 Mbit/s backend-to-client link at nominal
80 ms RTT, with no injected loss or reordering, produced:

| Direction | Applicable cap | Useful throughput | Time for 100 MB | Confirmed ACK IP/TCP bytes | Repeated KCP payload bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| Upload | **50 Mbit/s** | **40.701 Mbit/s** | 19.656 s | 108,904 | 2,830,282 |
| Download, confirmed | **10 Mbit/s** | **7.961 Mbit/s** | 100.485 s | 269,328 | 8,709,650 |

The earlier clean download repeat measured 7.992 Mbit/s. All application bodies
verify exactly 100,000,000 bytes with the same final.1 executable. Both directions
use roughly 80% of their respective caps; this does not establish a universal
normal efficiency or prove optimality. There is no demonstrated special download
collapse caused solely by unequal bandwidth. Loss/reordering penalties and the
unexplained repeated KCP sends remain separate investigation targets. Some cost
comes from framing/startup/feedback, but the contributions are not isolated.

The impaired single-download result was 5.399 Mbit/s with 0.5% loss and 5%
reordering. Do not compare it with the 50 Mbit/s upload ceiling or conflate this
single-stream body experiment with the earlier eight-worker release comparison.
Upload timing includes the receiver's verified receipt; the upload/download
runs were separate, not a matched steady-state duplex qualification.
See [upload evidence](ASYMMETRIC-UPLOAD-2026-10-13.md) and
[combined direction summary](asymmetric-directions-summary-2026-10-13.json).

## Original download ACK-cost screen

Four isolated transfers each verify exactly **100,000,000 application bytes**
with SHA-256 `5775b33226f152a0b1640906a59c1081149f8832aa4f7d0113453d0a864e8a22`.
They use the deployed final executable, SHA-256
`df9b3ccb9e76d1f597f09165624dd5d0fdc66b920dc92bccefc0ecf7f63dc96f`.
No production hosts or services are changed.

| Link profile | ACK record bytes | ACK cost including IP/TCP bytes | Percentage of payload | Estimated Ethernet occupancy bytes | Download seconds | Average ACK IP Mbit/s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Clean local, 100/100 Mbit/s | 37,512 | 126,432 | 0.1264% | 182,748 | 9.768 | 0.1036 |
| Clean 80 ms RTT, 100/100 Mbit/s | 33,288 | 116,388 | 0.1164% | 169,018 | 10.132 | 0.0919 |
| 80 ms RTT, 0.5% loss, 5% reorder, 100/100 Mbit/s | 1,695,504 | 1,785,264 | 1.7853% | 1,842,112 | 19.257 | 0.7417 |
| 80 ms RTT, 0.5% loss, 5% reorder, 50 upload / 10 download Mbit/s | 1,124,184 | 1,410,264 | 1.4103% | 1,591,448 | 148.189 | 0.0761 |

All captures report zero packets dropped by the capture kernel. All tunnel
processes exit normally, owned firewall rules are removed, unrelated firewall
sentinels survive, and the disposable namespaces are removed. The Python HTTP
target's expected SIGTERM status is -15; this is not a tunnel failure.
Six independent parser checks pass under the machine-wide test lock: full ACK
headers, packed ACK headers counted once, mixed-packet incremental attribution,
outer TCP flags distinguished from KCP commands, capture truncation rejected,
and malformed KCP rejected.

## What is counted

The parser identifies inner KCP command 82 in complete outbound Ethernet packet
captures on each endpoint. S/PA outer TCP flags do not identify KCP feedback.
No encryption, FEC or optional ACK timestamp extension obscures the record
boundaries. Capturing both endpoints records sending attempts before the middle
bridge's artificial loss; receiver-side observations would omit lost feedback.

The table describes client-to-backend acknowledgments of the downloaded data.
The captures span one cold opening, the single download and a 0.5-second final
feedback drain, including any background feedback on the configured pool.
The 100 MB denominator is the verified HTTP body. Opening/HTTP/mux/control
traffic is visible separately and is not mistaken for ACK records.

For a pure ACK packet, its complete IP/TCP header belongs to the ACK cost. For
an ACK mixed with another KCP command, only the ACK record bytes are charged;
the shared packet headers are not charged twice. Ethernet occupancy additionally
includes Ethernet headers, minimum frame size, FCS, preamble/SFD and inter-frame
gap. It is an occupancy estimate, not a measurement of Internet-provider
encapsulation. Average ACK rate divides emitted ACK cost by body transfer time;
it does not represent peak packet bursts.

## Interpretation

The clean 80 ms case sends 1,387 ACK records. The loss/reorder 100/100 case sends
70,646 ACK records, packed into 1,510 packets containing ACKs. Contiguous
progress can use cumulative acknowledgment and suppress redundant records;
gaps require more selective receipt information. The impaired run therefore
already batches approximately 46.8 ACK records per packet on average. Simply
packing those same records more densely has less room to help than compacting
the selective information or improving timing/recovery.

The asymmetric case delivers 5.399 Mbit/s of useful payload, while average ACK
IP traffic consumes only 0.152% of its 50 Mbit/s return capacity. Average return
bandwidth exhaustion does not explain its underutilized 10 Mbit/s download.
This does not rule out feedback delay/compression, burst queues, reordering,
retransmission cost, multiplexed ordering or adaptive-controller decisions.
It is one stream, rather than the earlier eight-worker HTTP comparison, and
must not be described as an improvement over that benchmark.

The current source uses adaptive control, four pooled carriers with distinct
source ports, two backend capture workers, client/backend GOMAXPROCS 4/2,
S outbound / PA return, no encryption or FEC, MTU 1350, manual KCP
0/30/2/nc1, windows 4096, deployed mux buffers and optional extensions disabled.
The complete commands, fixture hashes, cleanup receipts and byte counts are in
[the evidence](ack-budget-evidence-2026-10-13.json).

## Reproduction and limits

Use `scripts/measure_ack_budget.py` under `scripts/with_test_lock.py`, with sudo
inside the reservation. Supply an explicit binary, any required shared-library
directory and a new output directory. The default three profiles cover clean
local, clean WAN and loss/reorder WAN; select `--profiles asymmetric-50-10`
for the fourth profile, or `--profiles asymmetric-clean-50-10` for clean asymmetry.
Use `--direction upload` to measure the opposite data direction; download remains
the default. Upload checks the receiver's digest and accounts ACKs at the backend.
The script makes a checked private adaptation of the
existing namespace fixture rather than changing its historical results.

Raw captures and generated fixture/helper remain in unique local build output
directories. This is an exploratory measurement branch, not a new deployed
release. The single runs measure emitted feedback cost at the specified caps;
they do not qualify maximum throughput, thousands of active customers, real
backend traffic, upload cost or peak return-link requirements. No batching or
protocol change is implemented as a consequence of this measurement.

## Clean asymmetric follow-up

The user requested the same 50 Mbit/s upload / 10 Mbit/s download profile with
80 ms RTT, but **zero configured loss and zero reordering**. Two transfers
verify the same 100,000,000-byte body using the same final executable and
adaptive/KCP parameters:

| Run | Body transfer seconds | Useful download Mbit/s | ACK IP/TCP bytes | ACK payload percentage | Average ACK IP Mbit/s |
| --- | ---: | ---: | ---: | ---: | ---: |
| Initial | 100.102 | 7.992 | 270,312 | 0.2703% | 0.02160 |
| Confirmation with full capture coverage | 100.485 | 7.961 | 269,328 | 0.2693% | 0.02144 |

All virtual-link qdiscs show zero drops and zero reordering; capture kernel drops
are zero, payload verification passes and owned cleanup succeeds. The confirmed
run improves useful delivery approximately 47.5% over the prior impaired single
download's 5.399 Mbit/s. Its rate is still only 79.6% of the nominal download cap;
this does not demonstrate perfect utilization or that asymmetry has no cost.

The confirmation captures **6,661 repeated KCP PUSH segments containing
8,709,650 payload bytes**. Distinct sender payload totals 100,054,655 bytes,
covering the whole application body plus mux/protocol bytes. The repeated
segments consume capacity without duplicating bytes delivered to the customer.
Their presence with loss/reordering disabled is a reliability/pacing investigation
opportunity. This experiment has no per-packet timer/receiver-drop trace and does
not prove every repeat was unnecessary or identify the exact trigger.

The first follow-up exposed a measurement limitation: the buffered tcpdump
capture contained less distinct KCP payload than the verified application body,
despite zero kernel drops. Stopping capture can omit its trailing partial block.
The initial transfer's SHA-256 and timing remain valid, but its ACK cost is an
estimate. The confirmation adds an explicit readiness gate, immediate capture,
a 1.5-second feedback drain and a distinct-payload coverage check. The helper
now rejects incomplete download captures; seven parser checks pass, including
not treating repeated sends as unique payload. Historical raw receipts and ACK
estimates remain preserved, rather than silently replaced.

[Follow-up evidence](ack-budget-clean-asymmetric-evidence-2026-10-13.json) includes
both attempts, confirmed byte accounting and the actual qdisc counters. Select
`--profiles asymmetric-clean-50-10` to reproduce the clean case. There is no
production runtime/configuration change or deployment.
