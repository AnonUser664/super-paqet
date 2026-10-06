# Final deployment and measured qualification

Updated 2026-10-06. The requested final release is deployed on the four active
hosts, with persistent enabled systemd services, rollback backups, live reload,
configuration validation, and owned-rule cleanup. The excluded backend was not
contacted. **The remaining Netherlands burst failures and complete application-path outages mean
this is not an unconditional production-readiness or universal-optimality claim.**

Latest recheck: reverting Netherlands peers to four lanes applied on both
clients, but did not recover acceptance. Germany passed four of four 10 MiB
Reality downloads; Netherlands failed four of four from both clients with TLS
timeouts. The public Netherlands request also timed out. The eight-lane snapshots
below describe the measured capacity configuration before that diagnostic
reversion. The [test results audit](TEST-RESULTS-AUDIT.md) separates historical
throughput workloads and examines what the captures establish about filtering.

The [same-path Netherlands diagnosis](NETHERLANDS-DIAGNOSIS.md) now also records
four failed requests with the previously working enterprise executable and four
with unmodified upstream paqet. Paired captures show severe loss before packets
reach the remote host interface. Testing stopped at the user's request; filtering
is plausible, but its exact cause is not established.

## Executable and implementation

Version `enterprise-2026.10.06`, embedded release source `abf04f6` (main equivalent
runtime `97529dd`), binary SHA-256:

`47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419`

Every host runs `/root/super-paqet/super-paqet run -c /root/super-paqet/conf.yaml`.
The binary and live config hashes were fetched and checked after test cleanup;
config permissions are 0600. Services are enabled/active with zero automatic
restarts, healthy local metrics, and no degraded config transactions. Source
checkpoints are committed. The separate old timeout/outer-sequence experiments
are archived on `archive/pre-final-wire-experiments`, in a named stash and a
byte-for-byte local backup; the main checkout excludes them.

The final release retains fabricated Ethernet/IP/TCP headers, PA flags,
source-port reservations, raw capture/injection, and the original sequence/ACK
number algorithm. It adds independent KCP conversations on a shared verified
source tuple, bounded outstanding-segment indexing for fast-ACK processing,
static-mode carrier pressure sampling, improved adaptive RTT fallback, and an
optional small-write flush exception to bulk batching. The inner conversation
routing changes require both endpoints to enable `shared_source`; FEC is rejected
in shared mode because parity-only packets cannot identify their conversation.

Full root race/vet and fork suites passed. The indexed sparse-gap CPU benchmark
fell from approximately 16,000 to 92 ns/op; its reference differential test covers
ACK order, timestamps, cumulative removal, ring growth and sequence wrap. The
small-write threshold tests check bulk/default/vector behavior and byte order;
the final full KCP suite passed in 132.998 seconds.

## Final topology and resource settings

| Clients | Forward | Raw tunnel endpoint | Application target |
|---|---|---|---|
| Both .14 and .118 | 9001 | 91.107.251.85:29999, the assigned primary address of the German host | 116.202.177.233:2096 |
| Both .14 and .118 | 9003 | 171.22.132.226:29999 | 171.22.132.226:2096 |
| Both clients | 9002 | Existing excluded direct entry retained | 65.109.192.172:2096, excluded/unavailable |

Germany also retains its 116.202.177.233 listener. Public client ports remain
bound on all IPv4 interfaces. The application target and Reality credentials
have not been changed. There is no relay through an extra backend.

The measured eight-lane capacity configuration uses **eight independent KCP/mux lanes**, one fixed verified
source port, `shared_source: true`, and quoted `enc: 'null'`. All available Go
processors are usable; no CPU quota or affinity restriction was added.
Each backend has two vCPUs/~4 GiB RAM; each client has four vCPUs/~8 GiB RAM.
Soft Go memory limits are 1,536 MiB on backends and 4,096 on clients; kernel,
capture and other application memory is additional. Services have LimitNOFILE
524,288 and TasksMax 65,536. No global host networking/CPU tuning is left behind.

Germany retains pcap, MTU1350, a conservative normal-retry/manual 30 ms update
policy with immediate writes, and the selected small-write threshold 32.
Netherlands retains AF_PACKET, MTU128, the original `fast` ACK/bulk batching
policy, **one packet receive worker**, and `small_write_flush: 0`. Independent
KCP, mux and relay work still uses the available processors. Two receive workers,
faster ACK profiles, larger MTUs and interactive flushes were not promoted on
Netherlands because authenticated stress exposed regressions. Reliability
adaptation remains disabled on these physical-path recovery profiles; generic
adaptive defaults retain separate virtual-link qualification.

Fresh sanitized configs and unit are in [deployed-final/](deployed-final/README.md).
The complete source guide is [CONFIGURATION.md](CONFIGURATION.md). Live safe
reliability changes and edit-impact rules are in [LIVE-RELOAD.md](LIVE-RELOAD.md).
Use `config validate -c /root/super-paqet/conf.yaml --json`, then atomically
replace the file. Automatic polling applies it; `systemctl reload super-paqet`
requests immediate reconciliation. Check `config.applied` and config revision
metrics for completion. Structural peer/listener changes interrupt only the
affected endpoint, and process replacement still ends established streams.

## Measured results

Subsequent isolated clean-link tests of this exact executable measured
**3.50–3.74 Gbit/s upload and 3.08–3.10 Gbit/s download** with null encryption
and eight initial shared lanes. Those runs exclude the 100k hold/HTTP workload
and are detailed in [CLEAN-LINK-CURRENT.md](CLEAN-LINK-CURRENT.md). The gap from
the historical 7.854 Gbit/s upload peak remains unattributed across changed
runtime, cipher and carrier configurations.

| Workload | Result | Scope |
|---|---|---|
| Exact final binary: 100k forwards | 6.952 s ramp, 120 s soak, all 100,000 verified, zero errors | Laptop namespaces; predominantly idle connections |
| Bulk alongside that 100k hold | 2.084 Gbit/s, HTTP 1,168 req/s, p99 histogram upper bound 32.768 ms | Adaptive unencrypted shared-source fixture, not conservative deployment profile |
| Earlier shared/null isolated bulk | 3.933 Gbit/s upload, 2.970 download; duplex 1.829+1.849 | Earlier shared implementation; separate binary boundary |
| Final eight-lane deployed hold | 4,096 forwards per client, **8,192 verified**, zero errors | Both backends; HTTP/churn/bulk alongside hold |
| Deployed 256-worker HTTP/churn | Zero forwarding errors; roughly 1,700–2,200 HTTP req/s and 580–1,020 churn req/s per route/client | Final eight-lane stage; deadline cancellations excluded |
| Final deployed tunnel memory | Clients ~153/155 MiB peak RSS; backends ~184/225 MiB | 8k hold and measured load; excludes test generators/kernel/Xray |
| Authenticated 10 MiB downloads | Earlier 8/8 passed; latest four-lane NL recheck passed Germany 4/4 and failed Netherlands 0/4 from both clients | Full VLESS/Reality/Vision, finite bulk checks |
| Public-domain complete 1 MiB | Both ports passed earlier; post-cleanup Germany passed and Netherlands timed out at 12 s | Laptop → deir.cloudnet1.ir |
| Authenticated burst, 32 workers/client | 1,024/1,024 passed | 256 requests on each of four paths |
| Authenticated burst, 64 workers/client | Germany 1,024/1,024; Netherlands **1,020/1,024** | Four Netherlands SSL connect timeouts, no retry masking |

The final 100k fixture used about 1.83 GiB peak RSS per tunnel and approximately
1.90/2.32 GiB RSS+swap client/server. The laptop swapped under that load. Its
explicit temporary backlog increase from 1,000 to 65,536 was restored, test
namespaces were removed, and unrelated firewall rules survived.

Seven final-binary shared/null/fast/small-write-32 virtual profiles passed:
random loss, reordering, harsh jitter/loss, asymmetric rates, ACK-limited duplex,
outage and pcap functional/restart. The final fast/interactive reload test also
passed 256 active streams through 18 edit checkpoints on 20/70 ms directional
delay, 20/50 Mbit/s caps, 0.3% loss and 5% reorder. Unaffected streams had no
errors; half-close release, descriptor stability and owned-rule cleanup passed.

## Latency and unresolved limits

The original controlled warm HTTP medians were approximately 121–124 ms.
Immediate-write German measurements were 86–98 ms, and later final-path samples
were about 71–73 ms. Link conditions changed between samples, so the entire
later reduction cannot be attributed to code alone. The controlled initial
comparison supports roughly **27–36 ms** saved by removing write-cycle waits.
New-connection medians also improved by about 58–73 ms in that comparison.

Netherlands retains the safe batching policy: warm medians remain approximately
122–123 ms and new-connection medians approximately 245 ms. Low-concurrency
larger-MTU/microflush results looked faster, but burst qualification rejected
those changes. A blanket 40 ms improvement without tradeoffs was **not** achieved
on Netherlands and is not claimed.

The four SSL timeouts at 64 workers/client and subsequent complete Netherlands
download failures from both clients remain an unresolved acceptance
limit. A post-cleanup public-domain retest also exceeded the 12-second deadline
on Netherlands while Germany passed; this is recorded as a failed check. Direct backend Xray controls passed, while ordinary client-to-backend
VLESS controls failed, so neither aggregate TCP reachability nor a localhost
Xray test attributes the remaining tail conclusively. Packet capture previously
showed valid failing raw traffic leaving clients without arriving at the server;
that does not prove a particular provider/firewall/DPI mechanism. One-worker
Netherlands and the German primary alias restored forwarding. The two-worker
Netherlands physical-path regression is unresolved; default multi-worker paths
retain their generic tests, not qualification on this host.

A virtual 1/100 Mbit/s duplex configuration with peer timestamps and credit hints
disabled left one upload stream without measured receiver bytes, including a
longer retry. This is a known failed gate for that configuration. Enabled-feature
profiles pass; the guide must not promise the disabled-telemetry fallback is
optimal on saturated asymmetric paths.

These results establish finite forwarding/capacity behavior. They do not prove
100k busy customers, gigabit throughput on these two-vCPU backends, multi-day
sustained service, universal detectability invariance, or an absolute maximum
across every network. Production qualification of the authenticated high-load
Netherlands path remains open.

## Cleanup and rollback

Owned test forwards, IPv6/alternate-port probes, transient HTTP/hold/legacy
processes, test files and the tagged benchmark INPUT rule were removed. Temporary
AES diagnostics were reverted; final configs contain no keys. Pprof is disabled
in final configs; local health/metrics and structured info logs remain enabled.
No excluded host, Xray service, WARP configuration or unrelated firewall rule was
modified by cleanup.

Each rollout retained a mode-0700 directory beneath `/root/super-paqet/rollback`
with the previous binary, config and unit. The final binary rollout backup is
`/root/super-paqet/rollback/20261006T002600Z-final` on each host. Stop the
service, copy all three saved files back, run `systemctl daemon-reload`, then
start it and verify the hash, config and real application path. Backups and
previous working executables are retained for operator rollback.

The full sanitized report is [final-deployment-evidence.json](final-deployment-evidence.json).
[Working notes](FINAL-QUALIFICATION-WORKING-NOTES.md) retain chronological failures,
harness corrections and rejected candidates; their older in-progress statements
are superseded by this report.
