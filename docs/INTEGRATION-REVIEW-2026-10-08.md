# Selective exploration integration review — 8 October 2026

Production is unchanged. Review is in `exploration/production-tuning-20261008`;
source worktrees and their completed/interrupted artifacts are preserved.

## Source identity and scope

Exp2 is commit `24b772c75511349fbbf54a1c47888126c6c13b31` in
`/home/shayan/Codes/super-paqet-exp2`, based on `2be348d`.
Its report gives component microbenchmarks and suite claims; these are not
end-to-end proof of a faster production tunnel. We independently qualify the
selected implementation.

Exp1's supplied root is an unchanged master checkout. Its actual unfinished
implementation is in `/home/shayan/Codes/super-paqet-opt-explore-20261008`, an
exp1-owned worktree on `exploration/perf-optimization-20261008`, based on
`2be348d`. Three uncommitted KCP files attempt multi-segment stream reads.
No new tests or experiment report were present. We do not edit that worktree.

## Review findings before qualification

| Proposal | Review decision / reason |
|---|---|
| Exp2 wider checksum accumulation | Candidate; independently check all short lengths, large odd payloads and packet byte shape. |
| Exp2 direct TCP flag construction | Candidate; equivalent bits, but the old temporary literal does not by itself prove a heap allocation. |
| Exp2 output FIFO batch pop / buffer-vector reuse | Candidate after clearing consumed request/vector references; borrowed arrays must not retain recycled packet storage while idle. |
| Exp2 binary endpoint key | Candidate with endpoint identity tests; preserve IPv4/mapped IPv6 normalization, IPv6 zone and arbitrary address fallback. |
| Exp2 skipping payload snapshots without deadline/context | Reject as written. Session close and socket-write failure can still return while carrier output owns a caller's buffer. Existing timeout/cancellation ownership must remain. |
| Exp2 embedded credit payload | Candidate after unlinking stale neighbor references; a dispatched entry must not retain the entire pending credit list. |
| Exp2 larger initial adaptive stream window | Defer. It changes memory/admission behavior and advertised capacity; needs separate evidence for bandwidth and high-connection memory tradeoffs. |
| Exp2 removal of TCP MSG_PEEK | Candidate; retain immediate buffer return on EAGAIN/EOF and cancellation/half-close semantics. |
| Exp2 small UDP datagram combined write | Candidate; validate short writes, empty/maximum boundaries and allocation cost. |
| Exp1 stream read batching | Complete as whole-segment draining. Do not retain `seg.data[copied:]`: that loses original pooled storage capacity and makes recycle fail. Preserve message mode and the existing short-buffer contract. |

Every accepted runtime change still requires deterministic correctness tests,
race checks and matched-binary workload comparisons. This document will retain
rejected experiments and the final decision rather than replacing them with only
favorable results.


## Ownership corrections retained during integration

The raw exp1 patch is preserved privately with SHA-256
`77db922f286907dfa96b4370b70c1f794febe5e64906e2743a36a418146ad7f3`.
Its proposed partial-segment slicing loses the original pooled slice capacity;
`bufferPool.Put` explicitly rejects such a tail. Whole-segment batching avoids
adding offsets/storage fields to every KCP segment and preserves `Recv`'s -2
short-buffer error. Small `UDPSession.Read` calls retain the existing receive
scratch path. Session reads must return the actual drained byte count.

The selected exp2 subset additionally clears local batch/vector references when
packets are consumed/recycled, clears an unlinked credit entry's neighbors, and
uses pooled scratch for combined small UDP records. Passing an array through
`io.Writer` is not a guarantee of stack allocation. Wider checksum checks cover
all lengths 0..256, large carry-heavy/odd payloads through 65536 bytes, eight
alignments and three patterns against an independent byte-pair oracle.

Full root race/vet checks passed. Focused KCP race checks passed in 156.000 s,
including all four plaintext/CFB/Salsa20/AEAD 100 MiB echo tests with FEC,
batch queue concurrency and shared endpoint routing. The full smux race suite
is in progress; no performance or deployment acceptance is inferred yet.


The full selected smux race suite passed in **545.290 s**, and its vet check
passed. An isolated copy of exp2's original snapshot-elision code ran the new
`TestNoDeadlineWriteOwnsPayloadDuringClose` and failed deterministically:
`early return retained caller bytes: "recycled"` (0.002 s). This is a reproduced
correctness failure, not merely a speculative objection. The selected runtime
continues snapshotting queued payloads and passes the same test.

The completed exp1 read implementation has no new segment field or ring
`PushFront` method. Focused race checks passed in 28.688 s, including whole
segments, small session reads, full-window reopening, preserved fragmented
message boundaries, the original 36,000-operation receive oracle, deterministic
virtual links and duplicated traffic. Its full KCP suite and end-to-end
performance comparisons are still pending at this checkpoint.


The completed KCP suite passed in **140.709 s**; combined root race/vet and
fork vet checks passed. The receive-batch microbenchmark, which excludes the
session mutex and network reads, was slower (median 2592 → 2821 ns per 32
segments, +8.8%, identical allocations). It is retained as an adverse result:
we must measure the intended session/read benefit end-to-end rather than claiming
that fewer calls automatically improve every layer. Small combined UDP records
use **0 B/op, 0 allocs/op** after pool warmup; the relay benchmark also reports
zero allocations. Final runtime acceptance is still pending matched binaries.
