# Shared-workspace collaboration

Use a separate Git worktree and branch for each agent's implementation work.
Do not switch branches, reset files, merge changes or overwrite another agent's
artifacts in a worktree that another agent is using. Coordinate overlapping file
edits explicitly. Production changes must be distinguished from local candidates.

## Mandatory test coordination

The user requires tests to be serialized across agents from 8 October 2026.
Before **every** test command or test matrix, check for another running workload
and acquire the machine-wide lock:

`/home/shayan/.cache/super-paqet-tests/test.lock`

Run the entire command/matrix under `flock -n` on that path. Hold the lock for all
child workloads and cleanup, including tests in other worktrees. A busy lock means
wait or continue non-test work; do not bypass it or terminate the holder. The lock
is outside Git/worktrees so separate branches share the same reservation.

Also inspect running processes before launch: earlier/manual tests may not hold
the lock. Look for `go test`, test binaries, `iperf3`, `spq-bench`, namespace/link
benchmarks, recovery/stress runners and other load generators. If another agent's
test is active, do not start. Confirm ownership before stopping any process or
deleting namespaces, firewall rules, journals or output directories. Never
terminate another agent's test. Coordinate heavy builds/profiling too when they
would distort a performance measurement.

Use a unique output directory for every experiment. Preserve completed receipts,
failed gates and interrupted attempts; never overwrite them to make a comparison
look successful. Record exact executable hashes, adaptation/configuration,
placement, cleanup and acceptance limits. Checkpoint incomplete qualification
explicitly; a pushed commit is not a production deployment or acceptance claim.

Example, after the process check and creating the private lock directory:

```sh
flock -n /home/shayan/.cache/super-paqet-tests/test.lock go test -race ./...
```

For privileged namespace matrices, place `flock` outside `sudo` so the reservation
also covers authentication, the complete matrix and cleanup. Never put passwords
in command arguments, environment variables, files or logs.
