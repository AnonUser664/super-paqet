# Repository consolidation

The accepted integration and opening/backpressure fixes are consolidated on
`master` in `/home/shayan/Codes/super-paqet`. The current code checkpoint is
`2d0e8d1`; subsequent checkpoint commits document its exact executable checks.

The earlier exp1/exp2 integration was selective: accepted packet/KCP/mux/relay
changes are already in master. Rejected read batching, balancing and pacing
experiments remain excluded. Merging their old tips indiscriminately would
reintroduce known regressions. See the integration review and opening attempt
ledger for the reasons and retained failed results.

The local consolidation archive is `build/consolidation-20261012/`. It preserves
all original branch references in a Git bundle, full independent repository and
worktree archives (including ignored evidence), and the unfinished optimization
worktree patch. These are historical artifacts, not additional active checkouts.
Old branch/folder names in earlier receipts describe the original experiment
locations. They should not be interpreted as live deployment paths.

Production promotion is separately recorded in the deployment receipt. The user
authorized this checkpoint with its recorded performance limits; this does not
erase the short lossy-link p99 failure or establish universal production capacity.

Consolidation is complete: only the main repository folder and local `master`
branch remain. The [archive manifest](consolidation-evidence-2026-10-12.json)
records verified archive hashes and old-to-new artifact locations. Original
qualification receipts retain their executed paths; the former fixes worktree's
`build/opening-fix-20261009/` is now at the same relative location inside the
main repository. Conflicting standalone benchmark/iperf artifacts were retained
under the consolidation archive rather than overwriting the originals.
