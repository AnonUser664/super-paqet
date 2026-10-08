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
