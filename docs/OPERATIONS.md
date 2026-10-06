# Operations and review runbook

The final release is deployed; see [the final report](FINAL-DEPLOYMENT-REPORT.md)
for exact hashes, snapshots and remaining qualification limits. These commands describe procedures; documentation
updates do not install packages, restart services or deploy code. Establish the
selected source/binary/configuration before carrying out a future deployment.

## Build an identifiable candidate

Requires Linux, Go 1.27+, a C compiler, libpcap headers/library, iproute2 and
iptables/ip6tables. A generic `make build` includes the current working tree.
Build a selected clean checkout
when qualifying a release and record `git rev-parse HEAD`, dirty status and
`sha256sum` of the executable.

```sh
make build bench-build
./build/super-paqet version
sha256sum ./build/super-paqet
ldd ./build/super-paqet
./build/super-paqet run --check -c config.yaml
```

A local ELF binary can require a different libpcap soname/glibc than a target.
The deployed runtime was linked to Ubuntu-compatible libpcap.so.0.8. Verify the
candidate on the target before stopping its working service. Do not overwrite
an executing binary in place; stage separately and atomically rename after stop.

## Two installation layouts

| Layout | Binary | Configuration | Unit |
|---|---|---|---|
| Repository generic unit | `/usr/local/bin/super-paqet` | `/etc/super-paqet/config.yaml` | `deploy/super-paqet.service` |
| Requested current deployment | `/root/super-paqet/super-paqet` | `/root/super-paqet/conf.yaml` | `docs/deployed/super-paqet.service` snapshot |

The generic unit uses `ProtectHome=true`, which hides /root. The current unit
uses `ProtectHome=read-only` so the /root binary is accessible. Do not mix its
paths with the generic unit. Configs are mode 0600 and binaries 0755. The
optional generic environment file is `/etc/super-paqet/environment`; the current
null-mode service has no encryption key/environment-file requirement.

The service waits for network-online, restarts on failure after two seconds,
has TimeoutStopSec 30 and LimitNOFILE 524288, retains a private runtime directory,
and performs firewall cleanup in ExecStopPost. Root/raw/admin privileges and
its capabilities are needed for raw capture/injection, socket budget requests,
port guards and iptables. An ordinary unprivileged service is insufficient.

## A future replacement procedure

Before replacement: snapshot current executable/config/unit/hash, enabled/active
state, interface/IP/next-hop data and actual forwarding results. Keep a rollback
copy of all files. Preserve the working direct topology unless a change is
explicitly selected. Check candidate execution/linkage, strict configuration,
unit paths/permissions and unit validation first.

Then stop the service, atomically replace staged files, reload systemd if the
unit changed, enable/start, verify the running `/proc/PID/exe` hash, config hash,
health, restart count and application behavior. A failed start needs immediate
restoration of the saved version. Start backends before clients. This procedure
was used during the recorded deployment; it is not authorization to repeat it.

After start, test small and large authenticated transfers through every forward,
multiple clients together and the intended public ingress. A process health
check, open port, TLS hello or KCP ping alone is insufficient. Match response
size/byte integrity as well as HTTP status. Record latency/goodput separately
from encryption, flags, MTU, windows, active customers and concurrency.

## Inspect a running current service

Run on the selected host:

```sh
systemctl status super-paqet.service
systemctl show super-paqet.service --property=MainPID,ActiveState,SubState,UnitFileState,NRestarts
journalctl -u super-paqet.service --since '10 minutes ago' --no-pager
curl -fsS http://127.0.0.1:29090/healthz
curl -fsS http://127.0.0.1:29090/metrics
```

Check failed opens, aborted/rejected flows, active sessions, KCP retransmissions,
RTT/RTO, pending queues, capture/transmit/pipeline drops, heap/goroutines,
FD count, RSS+swap and cgroup/kernel memory. Application byte totals and KCP
output bytes are different; output counts include attempts/retransmissions and
are not unique delivered goodput. No-encryption changes neither the need for
Reality authentication nor the need to complete the actual body.

The deployed release counts transient ENOBUFS on both pcap and AF_PACKET as
datagram loss, letting KCP retransmit. Permanent device/injection errors still
abort the affected carrier. See [DIAGNOSTICS.md](DIAGNOSTICS.md) for counters.

## Troubleshoot in evidence order

1. Verify running hash/config and both endpoint modes/keys; distinguish deployed,
   committed and dirty source.
2. Check service startup errors, permissions, interface/source/gateway MAC and
   local destination service. Confirm a real authenticated direct target control.
3. Test the complete actual application path and payload. Compare the same
   workload through upstream when diagnosing a supposed regression.
4. Capture simultaneously on physical source and destination interfaces;
   distinguish packets missing in transit from packets arriving but not decoded.
5. Change one factor: address/port, cipher/envelope, source port, MTU, driver,
   optional inner controls or adaptation. A multi-factor recovery is a useful
   working profile, not causal isolation of one knob.
6. Repeat sustained transfers and concurrency, then profile queues/CPU/memory.
   Preserve failed evidence and revert variants that do not pass.

On WARP/policy-routing/multiple-address hosts, startup discovery can select an
undesired path. Explicit physical overrides are currently required in the live
profile. Matching MAC entries and checksums narrow hypotheses; they do not
prove which intervening device filters traffic.

## Stop and recover firewall ownership

```sh
systemctl stop super-paqet.service
/root/super-paqet/super-paqet firewall-cleanup
```

Normal stop closes resources and removes owned chains. Crash/SIGKILL cannot
execute in-process cleanup; ExecStopPost or a later startup/cleanup recovers
journals for dead owners in the same namespace. Never flush shared tables or
unrelated WARP/Xray/firewall rules. Avoid saving temporary SPQ chains in a
persistent host iptables dump. The deployed cleanup tolerates an owned chain
already being absent and recovers dead-owner journals.

Established TCP streams cannot survive process replacement. A restart is not
hitless migration. Check new authenticated flows after restart and owned-rule
cleanup after shutdown; retain unrelated-rule controls in test environments.

## Rollback

Backups reside in `/root/super-paqet/rollback/<timestamp>/` and contain the
version at that particular checkpoint. Later intermediate configs are not
necessarily the original service or the final working recovery. Select a
matching binary/config/unit set deliberately; inspect prior state when available.

A chosen restore follows this pattern:

```sh
systemctl stop super-paqet.service
TASK_BACKUP=/root/super-paqet/rollback/SELECTED_TIMESTAMP
cp "$TASK_BACKUP/super-paqet" /root/super-paqet/super-paqet
cp "$TASK_BACKUP/conf.yaml" /root/super-paqet/conf.yaml
cp "$TASK_BACKUP/super-paqet.service" /etc/systemd/system/super-paqet.service
systemctl daemon-reload
systemctl enable --now super-paqet.service
```

Validate the restore and complete application transfers afterward. The excluded
65.109.192.172 backend is outside the current work scope; its direct 9002 client
entries remain unchanged and previously unavailable.

## Reproduce qualification without confusing profiles

Read [BENCHMARKS.md](BENCHMARKS.md) for the current executable and measured
workloads; read [DIAGNOSTICS.md](DIAGNOSTICS.md) for individual workloads. Local
namespaces are disposable; running them still consumes real host CPU/RAM.
One-way `--delay-ms` contributes twice that amount to base RTT. Use fixed seeds
for fault schedules; live scheduling is still variable. Deterministic virtual
KCP tests use a simulated clock and repeated output comparison.

```sh
make vet test
make build bench-build
sudo python3 scripts/netns_bench.py --enterprise --binary build/super-paqet \
  --functional --backend pcap --block null --adaptive off --sessions 1 \
  --rate-mbit 100 --delay-ms 40 --debug --flow-sample 1 --duration 5
sudo python3 scripts/stress_links.py --binary build/super-paqet \
  --duration 20 --profile --output build/review-matrix
```

The first profile exercises capped-link pcap queue pressure. Record the selected
binary hash; these commands describe reproduction rather than certifying a new
build. Full source tests use each fork's own
module directory. The full smux race suite can run for several minutes.

The 100k test's explicit `--host-backlog 65536` changes a global host setting,
records its original value and verifies restoration. Normal runs do not change
it. Do not conflate the successful mostly-idle soak with thousands of
simultaneously active customers. Final active-customer acceptance must specify
traffic mix, per-user rates, concurrency, latency/error budget and actual host
resources. The next deployment waits for the user's document/feature decision.

## Validate and reload a running instance

The new source supports:

```sh
super-paqet config validate -c /etc/super-paqet/config.yaml
super-paqet config validate -c /etc/super-paqet/config.yaml --json
systemctl reload super-paqet
```

Validation checks schema/defaults/discovery without binding forward/tunnel ports
or installing rules. A valid result does not establish remote reachability or
available ports. Automatic polling applies file changes without a service
restart. The updated unit template uses SIGHUP for `ExecReload`; SIGHUP requests
reload and returns before asynchronous validation/application completes. Check
`config.applied`, `config.rejected` and `super_paqet_config_revision` for outcome.
The final deployed unit supports ExecReload/SIGHUP. Use the recorded deployed/ snapshots for the current service layout.

See [LIVE-RELOAD.md](LIVE-RELOAD.md) before changing transport settings. Some
edits preserve all streams; structural endpoint changes interrupt only their
carriers. Failed fixed-bind replacement can restore settings but cannot restore
streams already interrupted. The source supports neither distributed rollout
coordination nor surviving process replacement.
