# Step 1 status

The final candidate passed the complete 26-profile live link matrix. Full
qualification is still running; do not treat this checkpoint as a completed
production acceptance. The strengthened scale soak and both fuzzers have now
passed; the twelve additional seeded WAN runs are in progress.

- Runtime checkpoint: `2d5f7a0`.
- Test/runner checkpoint: `64fe72a`.
- Candidate: `build/super-paqet-pcap-address-fix`.
- SHA-256: `e2ae9b8cce7dc864dae9d21c5f770bb353ab787c218f2faf1d794d8efe7003f6`.
- Main evidence: `build/step1-final-v5/matrix.json`.
- Active tail: `build/step1-final-v5-tail-final/checks.json`.
- Source checks: isolated worktree `build/step1-qualification-source`.

The first full-check attempt failed the upstream OOB-only test because unknown
OOB packets deliberately cannot establish listener sessions. The test now
performs a reliable handshake first; three focused repetitions passed. Runtime
behavior and the qualified binary are unchanged. The retry passed all full checks. Its first scale soak recorded one premature
local TCP timeout while loopback interfaces dropped packets under synchronized
keepalive load. Three sampled connections remained usable, exposing a gap in
the load generator: it now verifies every held connection. A focused test
confirmed that an isolated reset produces 99/100 verified and one error.

The final scale repeat temporarily raised host netdev_max_backlog from 1000 to
65536 and verified restoration to 1000 before subsequent tests. All 100,000
connections returned complete responses after 600 seconds, with zero errors.
Concurrent bulk averaged 1.572 Gbit/s and HTTP 722 requests/s with a 32.768 ms
p99 histogram upper bound. The service crash/restart check passed. Both 60-second
fuzzers passed (6.4 million control and 3.5 million frame executions). It also records swapped
memory and reduces descriptor scan overhead. Checkpoints `6567e2e` and `7ba4664`
contain these harness improvements; `e1df492` pins service tests to the candidate
binary rather than the concurrently rebuilt generic executable. The final tail
reuses the successful isolated source checks, then runs the strengthened ten-
minute scale soak, service recovery, fuzzers and twelve extra seeded profiles.

Other uncommitted configuration/deadline edits in the workspace are preserved
and excluded from this candidate. No service has been installed or deployed.

See [BENCHMARKS.md](BENCHMARKS.md) for measured results and limits,
[DIAGNOSTICS.md](DIAGNOSTICS.md) for the expanded logging, and
[TRANSPORT.md](TRANSPORT.md) for the preserved outer packet behavior.
