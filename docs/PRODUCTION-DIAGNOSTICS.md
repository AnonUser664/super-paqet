# Production diagnostics

The release candidate keeps warning-level logging and loopback-only `/metrics`
on port 29090, with profiling disabled. The finite systemd observer retains
bounded metric samples, process identity, kernel queue/drop/scheduling evidence
and warning journals. It never collects customer payloads or authentication
configuration. These additions are pending exact-artifact qualification and
rollout; the deployed opening.10 executable does not yet export the new fields.

## Recovered opening disruptions

`errors_total` counts unsuccessful application handling. A successful retry can
hide an opening disruption from that total. `opening_transport_retries_total`
counts subsequent transport retry starts; the new counters describe failed
attempt phases independently of their eventual outcome:

| Counter suffix | Evidence | Interpretation limits |
| --- | --- | --- |
| `opening_syn_timeouts_total` | Stream SYN submission exhausted its deadline. | A locally blocked mux writer can cause this; it does not prove filtering. |
| `opening_receipt_timeouts_total` | Request write or receipt wait timed out before ACK2. | Could be ordered-carrier congestion, capacity or path interruption. |
| `opening_result_timeouts_total` | Result wait after ACK2 caused a retry. | Target delay and a full receive buffer can both delay this phase. |
| `opening_closed_attempts_total` | An unfinished opening saw EOF or a closed transport. | Peer shutdown and configuration changes can also close it. |

Each name has the `super_paqet_` prefix. Existing
`opening_capacity_retries_total` distinguishes receive-capacity retries from
physical-tuple failure evidence. These counters count attempts, not distinct
customers or proven network outages; phase totals need not equal retry starts.

`opening.retry_burst` emits one aggregate warning per observer interval when
retry starts reach at least 10 and at least approximately 1% of admissions.
It includes interval duration, accepted/retry deltas, active count and the four
phase deltas. Quiet intervals advance the baseline. Counter resets and observer
startup do not invent a historical burst. There are no per-customer or target
addresses in the event, and it does not label a cause as censorship.

## Per-carrier evidence

Outgoing and accepted carriers expose these existing transport snapshots:

- `session_acked_bytes_total`, `session_received_bytes_total` and
  `session_pending_bytes`: actual progress versus outstanding data.
- `session_retransmitted_segments_total` and
  `session_spurious_retransmissions_total`: retry evidence, not a direct estimate
  of packet loss or a diagnosis of filtering.
- `session_rtt_ms`, `session_rto_ms`, `session_rttvar_ms`: measured timing and the
  current retry budget.
- `session_send_queued`, `session_receive_queued`,
  `session_receive_reordered`, `session_pipeline_queued`: queue locations.

Each has the `super_paqet_` prefix and conversation/remote labels. Compare
within a process and carrier generation; migration can change endpoint labels.
A new label or counter reset is not a new loss. Existing receive-buffer capacity,
buffered bytes and blocked-state metrics help distinguish local backpressure
from loss of ACK/data progress. Kernel qdisc and softnet counters cover drops
that occur after successful injection. Missing kernel tools stay explicit.
Do not add worker and per-carrier drop counts together: they can describe the
same attempted output. A ten-second sampled snapshot can miss a shorter event.

## Release timing behavior

The corrected RTT sampler considers transmitted, live, unacknowledged sequences
before cumulative ACK processing retires them. It ignores retired duplicates,
unsent sequences and impossible future echoes, retains old-attempt bootstrap on
long links, and samples the last eligible echo in datagram order. Delivery and
packet layout are unchanged. The deployed adaptive controller and 200 ms learned
reordering ceiling are retained; 200 ms is a ceiling, not a fixed retry delay.

The unexplained 17:47 UTC retry cohort on 12 October is not retrospectively
classified by these new counters. Its old receipts remain valid. Use the added
phase and queue evidence to investigate any recurrence rather than claiming the
new metrics prove its cause.
