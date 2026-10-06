# Current status: final release deployed, measured limits recorded

Updated 2026-10-06. All four active hosts run enabled, healthy systemd services
with the new enterprise release. Live reload, validation, queue-pressure fixes,
shared fixed-source lanes, outstanding-ACK indexing and optional small-write
flush are deployed. The excluded backend was not contacted.

**Final production qualification remains limited by the Netherlands TLS tail:**
at 64 workers per client the final eight-lane cohort passed 1,020/1,024 requests;
four SSL connect timeouts remain unqualified. Post-cleanup public/bulk rechecks
also failed intermittently on .118 Netherlands. At 32 workers, all 1,024 requests
passed. Read [FINAL-DEPLOYMENT-REPORT.md](FINAL-DEPLOYMENT-REPORT.md) before treating
the release as universally production ready.

## Versions

- Running version: `enterprise-2026.10.06`, embedded source `abf04f6`.
- Main equivalent runtime: `97529dd`, subsequent harness checkpoint `5bb695d`.
- Running binary SHA-256:
  `47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`.
- Frozen local artifact: `build/final-production/super-paqet-small-flush`.
- Fresh configs/unit: [deployed-final/](deployed-final/README.md).
- Older deployed `1c77c55` and recovery snapshots in [deployed/](deployed/README.md)
  are retained as history and rollback evidence.
- Earlier unqualified timing/sequence edits are archived in branch
  `archive/pre-final-wire-experiments`, a named stash and byte-for-byte backup
  `build/final-production/preserved-experiments`. The main checkout is clean
  of those experiments; its builds retain the original outer number algorithm.

## Current paths and limits

Both clients 89.45.68.14/.118 forward 9001 through Germany's assigned primary
91.107.251.85:29999 to 116.202.177.233:2096, and 9003 through
171.22.132.226:29999 to its port 2096. Germany also retains its secondary-IP
listener. There is no relay topology. Existing excluded 9002 entries remain
unchanged/unavailable.

Eight KCP/mux lanes share each verified source tuple. Cipher is quoted `null`,
PA flags remain; all Go processors are available. Backends have two vCPUs/~4 GiB,
clients four vCPUs/~8 GiB. Go soft memory limits are 1,536/4,096 MiB respectively;
kernel/capture memory is additional. Netherlands retains one packet worker,
MTU128 and the original fast ACK/bulk batching policy; Germany uses the verified
manual immediate-write profile, MTU1350. Stronger Netherlands latency/fanout
candidates failed qualification and were rejected.

## Qualification collected

- Exact final executable established and fully verified 100,000 mostly idle
  forwards through a 120-second mixed soak with zero errors; simultaneous bulk
  2.084 Gbit/s and HTTP 1,168 req/s. Laptop swap was involved. This is not 100k
  busy customers or a real-host gigabit promise.
- Final deployment: 8,192 held forwards, every socket verified, zero forwarding
  errors during HTTP/churn/bulk. Peak tunnel RSS approximately 153–225 MiB.
- Eight authenticated 10 MiB transfers and both public-domain 1 MiB paths passed.
- Seven final-binary WAN profiles and 256-stream asymmetric/loss/reorder reload
  continuity passed; root races/vet and full fork suites passed.
- A saturated ACK-limited duplex configuration with timing/credit extensions
  disabled left a stream with zero measured bytes; that gate remains failed.
- Germany's initial controlled latency comparison saved about 27–36 ms. Later
  final-path warm medians were ~71–73 ms versus the original ~121 ms, but link
  conditions changed too. Netherlands remains ~122–123 ms; a blanket 40 ms
  improvement without tradeoffs was not achieved.

Final cleanup removed owned probe routes/units/files/rules and disabled pprof.
Services remain enabled, active, zero automatic restarts, exact hash verified,
config permissions 0600, health `ok`, config degraded flag zero. Rollback backups
are retained. [final-deployment-evidence.json](final-deployment-evidence.json)
contains sanitized collected results, and [working notes](FINAL-QUALIFICATION-WORKING-NOTES.md)
retain failed experiments and harness corrections.
