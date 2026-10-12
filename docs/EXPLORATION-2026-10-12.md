# Production-led exploration after opening.10

Production remains opening.10, source `2d0e8d1`, SHA-256 `22b2f1ab…`.
This branch is a local candidate; source/measurement commits are not deployments.

## Production review

The read-only review at 09:38 UTC covers approximately 95 minutes since the new
observer started. All five services are active/enabled, healthy, on the same
binary/config/Xray identities, with zero automatic restarts and no new recorded
opening failures/recovery events. Warning records before observer startup belong
to the rollout window; they are retained rather than called fresh incidents.
Finland has 18 packet-drop-growth incident samples. Within matched labels,
carrier consumed-loss growth and worker loss growth both total 1,930 attempts;
these describe overlapping evidence and must not be added together. Its `eth0`
fq qdisc reports `flow_limit 100` and growing `flows_plimit` drops. That is evidence
of per-flow queue pressure, not proof of hardware saturation or filtering.
The [review receipt](production-review-2026-10-12-opening10.json) retains scope.

## Cached transmit callback hypothesis

Profiles from the previous checkpoint found syscall CPU dominant. The actual
Unix sendmmsg microbenchmark also measured three allocations/72 bytes for each
single-packet raw send. The existing writer mutex already serializes batch
headers, payload references and callback completion. Keeping one callback and
its count/result/error storage on that physical socket removes escaping
per-invocation captures without introducing a worker, queue or changing retry
timing. Additional storage is 40 bytes per physical raw socket, not per customer
connection. The constructor initializes the callback; partial test/adaptor
construction has a one-time guarded fallback. All results reset before/after
RawConn.Write, including readiness/deadline failure. Payload pointers still clear
after synchronous submission, and source migration keeps existing route ownership.

Actual socket tests cover healthy/permanent-error payload integrity, a real
queue-full readiness timeout followed by reuse, and four concurrent writers
sharing one physical socket. Five race repetitions pass. Single-packet medians
change 971.8 to 888.3 ns; 32-packet medians 21,901 to 22,000 ns. Both move to
zero measured bytes/allocations. These unpinned microbenchmarks do not establish
a tunnel throughput gain or explain the directional rate gap. Full native and
whole-tunnel gates remain before accepting this candidate. See the
[micro receipts](raw-callback-micro-2026-10-12.json).

No queue policy, host sysctl, outer header/flag cycle, session count, adaptation
algorithm or production service has been changed. Earlier failed pacing/ramp
experiments remain excluded.
