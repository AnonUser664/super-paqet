# Netherlands same-path diagnosis, 2026-10-06

Netherlands forwarding is now restored on **both clients** with the same deployed
executable. The retained profile uses client-to-backend **S** flags and backend-to-client
**PA**, null encryption, the original 29997/29999 tuples, four shared KCP carriers,
and MTU1350. KCP packet windows were scaled to preserve their approximate byte
budgets. Germany settings and the binary remain unchanged.

The evidence supports selective, state-dependent failure of raw TCP traffic,
not a completely blocked IP. It does not identify the filtering device or prove
a flag-bit-only cause: SYN also selects the original encoder's different TCP
number/options branch. The current finite acceptance results are below;
production capacity and long-term classifier behavior still have separate limits.

## Earlier failed controls

Each completed comparison requested two 1 MiB HTTPS downloads per client,
through a temporary local Xray SOCKS probe and the backend's existing Reality
service on port 2096. Success required HTTP 200 and exactly 1,048,576 bytes, with
no retries. Every request instead timed out at the eight-second TLS deadline.

All variants retained the same physical IPv4 tunnel tuples: client source 29997,
backend destination 29999, default PA flags, fast KCP, MTU 128, and unencrypted
transport. The recorded older executable used its recorded single-carrier
configuration; the current deployment used four shared carriers. This is a path
control, not an identical inner-protocol comparison.

| Version | Executable SHA-256 | Requests passed |
|---|---|---|
| Current deployed release | `47c61ac2919564da49b0958f4347f716af0fb443cf4b5a0d52c2cb89d056a419` | 0/4 |
| Previously working enterprise, source `1c77c55` | `ecb8e002173f6f80cd3f403d3ee3080190f0e49889fa2ba9f883b893f96318f3` | 0/4 |
| Unmodified upstream, source `b9fa0bd93bf93b2eff27197742562ed82201f16e` | `0308d925e4f7c84ae2c4aff93337ec86eb4bc411f9a731c45a14c3f3998ff497` | 0/4 |

The planned AES controls were interrupted and are **not completed results**.
Germany-to-Netherlands direction testing was prepared but **not run**.

## Paired physical-interface captures

Captures ran on `ens160` on both clients and `net0` on the backend. Packet matching
used source/destination IPs and ports, outer sequence/ACK numbers, and captured
payload bytes. Capture filters reported zero kernel drops in the completed
comparisons. Raw captures were removed during repository cleanup; the committed
[evidence](NETHERLANDS-DIAGNOSTIC-EVIDENCE.json) contains only request results and
aggregate capture counts.

| Version | Client | Client sent / matching backend arrival | Backend sent / matching client arrival |
|---|---|---|---|
| Current | 89.45.68.14 | 185 / 21 | 538 / 5 |
| Current | 89.45.68.118 | 83 / 0 | 0 / 0 |
| Previously working | 89.45.68.14 | 77 / 0 | 0 / 0 |
| Previously working | 89.45.68.118 | 91 / 0 | 0 / 0 |
| Upstream null | 89.45.68.14 | 492 / 0 | 0 / 0 |
| Upstream null | 89.45.68.118 | 485 / 0 | 0 / 0 |

The current deployment's sparse bidirectional exchange shows that this is not a
complete IP outage. The evidence supports severe loss or filtering on the raw
TCP path between the host interfaces, rather than a failure specific to the
latest enterprise version. It does **not** identify the filtering device,
provider, policy, or prove that all ports/protocols for the IP are blocked.
Correct route/next-hop MACs and the listening backend application were checked.

## Earlier restoration and stop boundary

Comparisons temporarily stopped only the Netherlands backend process and removed
only the Netherlands peer/forwards from each client's live configuration.
Germany peer/forwards remained running. Remote rollback timers protected the
saved configurations; `finally` cleanup restored those exact saved bytes and
restarted/reloaded production services. Test units and owned upstream firewall
rules were removed. No production executable was replaced, no runtime source
change was made, and no bulk throughput work was performed.

That earlier comparison stopped at the user's request. The user subsequently
authorized the resumed investigation below.

## Resumed path investigation

The user subsequently authorized renewed testing of ports, flags and encryption.
Fresh ordinary TCP probes from both clients connected to SSH/22 and Xray/2096.
Five ICMP replies per client returned without loss, with mean RTT 89.313 ms
from `.14` and 83.505 ms from `.118`. A backend-local authenticated Reality
control downloaded exactly 1 MiB with HTTP 200 in 0.493 seconds. Its UUID and
short-ID matched the private probe; credentials are not included in evidence.

Two paired physical-interface experiments sent tagged synthetic raw frames to
ports 22, 443, 2096, 2052, 29999 and 41080, with PA/A/S/SA/P flags and 64/512/1200
payload bytes, three copies per case. Sharing one source port delivered
1002/1080 frames, with missing packets confined to continuing client-to-backend
traffic on 29999. Fresh source ports per case delivered **1080/1080**. Both
experiments reported zero kernel capture drops. These synthetic probes do not
reproduce every upstream SYN header detail or establish actual TCP connections.
They exclude a complete IP/port outage and suggest tuple/traffic-state effects.

Real KCP echo tests use the exact deployed executable, a temporary loopback
backend echo service, and additive loopback client forwards. Success requires
full payload equality on separate 64-byte, 16 KiB and 256 KiB streams. Plaintext,
AES and AES-GCM PA profiles on 443 still lose continuing forward packets after
the small echo. AES on 2052 likewise fails the larger streams. The **S and SA
profiles on 443 passed all three sizes on both clients**. Capture matching
confirms delivery of thousands of their forward frames.

These candidates preserve raw Ethernet/IP/TCP injection and KCP reliability;
no kernel TCP tunnel or SSH carrier was introduced. S/PA and PA/S directional
profiles also passed on the original four-carrier tuples. S/PA with MTU1350
passed both original tuples and a never-used source-port-30301 control, including
exact 1 MiB echoes. Paired captures verified the actual S/PA direction flags.

After SYN comparisons, the original PA/PA tuple also passed repeat echoes.
That observation rules out a claim of a permanent PA flag ban. A stateful
middlebox/flow-state interpretation fits these observations, but a changing
network policy cannot be excluded without visibility into intervening devices.
Fresh all-PA cases repeatedly stalled after a small echo despite changing ports,
source ports, encryption and packet size. Alternating S/PA in each direction
failed larger echoes; it was not selected as an automatic fallback.

Each row requested three independent checked echoes per client. Counts include
the small echo; a 1/3 result is a failure of the larger transfer gate. `Original`
means source 29997 with four carriers; fresh rows use a never-used test source.
The final PA repeats follow SYN traffic on the original tuple and therefore do
not qualify PA startup on a cold path.

| Profile | Port | Cipher | Out / return | MTU | Source / lanes | .14 passes | .118 passes |
|---|---|---|---|---|---|---|---|
| nl443-aes-pa | 443 | aes | PA / PA | 128 | 30100 / 1 | 1/3 | 1/3 |
| nl443-null-pa | 443 | null | PA / PA | 128 | 30101 / 1 | 1/3 | 1/3 |
| nl2052-aes-pa | 2052 | aes | PA / PA | 128 | 30102 / 1 | 0/3 | 1/3 |
| nl443-gcm-pa | 443 | aes-128-gcm | PA / PA | 128 | 30103 / 1 | 1/3 | 1/3 |
| nl443-null-s | 443 | null | S / S | 128 | 30104 / 1 | 3/3 | 3/3 |
| nl443-null-sa | 443 | null | SA / SA | 128 | 30105 / 1 | 3/3 | 3/3 |
| nl443-null-a | 443 | null | A / A | 128 | 30106 / 1 | 1/3 | 1/3 |
| nl80-null-pa | 80 | null | PA / PA | 128 | 30107 / 1 | 1/3 | 1/3 |
| nl29999-aes-pa | 29999 | aes | PA / PA | 128 | 30108 / 1 | 1/3 | 1/3 |
| nl29999-null-fresh | 29999 | null | PA / PA | 128 | 30109 / 1 | 1/3 | 1/3 |
| nl443-aes-sap | 443 | aes | SAP / SAP | 128 | 30110 / 1 | 3/3 | 3/3 |
| nl443-null-cycle | 443 | null | S,PA / S,PA | 128 | 30111 / 1 | 1/3 | 1/3 |
| nl443-null-large | 443 | null | PA / PA | 1350 | 30112 / 1 | 1/3 | 1/3 |
| nl29999-null-s128-fresh | 29999 | null | S / S | 128 | 30201 / 1 | 3/3 | 3/3 |
| nl2052-null-s1350 | 2052 | null | S / S | 1350 | 30202 / 1 | 3/3 | 3/3 |
| nl443-aes-s128 | 443 | aes | S / S | 128 | 30203 / 1 | 3/3 | 3/3 |
| nl29999-null-s-original | 29999 | null | S / S | 128 | Original / 4 | 3/3 | 3/3 |
| nl29999-null-s-pa-original | 29999 | null | S / PA | 128 | Original / 4 | 3/3 | 3/3 |
| nl29999-null-pa-s-original | 29999 | null | PA / S | 128 | Original / 4 | 3/3 | 3/3 |
| nl29999-null-s1350-original | 29999 | null | S / S | 1350 | Original / 4 | 3/3 | 3/3 |
| nl-original-pa-control-before | 29999 | null | PA / PA | 128 | Original / 4 | 3/3 | 3/3 |
| nl-original-s-pa1350 | 29999 | null | S / PA | 1350 | Original / 4 | 3/3 | 3/3 |
| nl-original-pa-control-after | 29999 | null | PA / PA | 128 | Original / 4 | 3/3 | 3/3 |
| nl-cold-s-pa1350 | 29999 | null | S / PA | 1350 | 30301 / 4 | 3/3 | 3/3 |

## Retained profile and final acceptance

The exact current executable tested 24 tunnel profiles across 80/443/2052/29999,
null/AES/AES-GCM, PA/A/S/SA/SAP and alternating S/PA, with MTU128/1350,
single/four carriers and original/fresh tuples. The complete per-profile
results, packet counts and failed byte-integrity checks are in the existing
[credential-free evidence](NETHERLANDS-DIAGNOSTIC-EVIDENCE.json).

Each client’s `backend3.network` includes:

```yaml
tcp:
  local_flag: [S]
  remote_flag: [PA]
```

The Netherlands listener’s `network` includes:

```yaml
tcp:
  local_flag: [PA]
  remote_flag: [S]
```

These blocks sit under `network`; address/source settings stay unchanged.
MTU is 1350, windows are client sndwnd 131 / rcvwnd 522 and backend 1044 / 1044.
Fast KCP scheduling and four shared lanes remain. No runtime source/binary change
was needed; this uses the retained encoder's existing configurable packet profiles.
It changes the selected outer packet profile on this path and does not establish
universal detectability invariance. Generic flags/defaults remain PA.

| Final check | Result |
|---|---|
| 64-worker connection churn | 1024/1024 checked 64 KiB echoes; 512 per client, zero failures. |
| Continuous two-minute echo soak | 16 retained streams total; all byte comparisons passed without reconnect/retry. |
| Reality/Vision, 10 MiB per request | 8/8, full HTTP 200 and exact bytes, no retries. |
| Germany regression, 1 MiB | 4/4, full authenticated downloads. |
| Post-cleanup client listener 9003 | 4/4 authenticated 1 MiB requests through loopback on both clients. |
| Laptop through public domain and both client IPs | Domain 9001/9003 and each client IP:9003 returned HTTP 200 and exactly 1 MiB (4/4). |

Client `89.45.68.14` checked 423.5 MiB in each direction during the soak (29.57 Mbit/s each direction); churn p99 2.650 s under concurrent load. These are finite workload measurements, not maximum WAN throughput.

Client `89.45.68.118` checked 444.7 MiB in each direction during the soak (31.05 Mbit/s each direction); churn p99 1.945 s under concurrent load. These are finite workload measurements, not maximum WAN throughput.

Host `171.22.132.226`: peak tunnel RSS 55.0 MiB, mean occupied CPU 0.187 cores, peak FDs 152. These are whole tunnel-process samples, including any unrelated live routes.

Host `89.45.68.14`: peak tunnel RSS 73.4 MiB, mean occupied CPU 0.477 cores, peak FDs 138. These are whole tunnel-process samples, including any unrelated live routes.

Host `89.45.68.118`: peak tunnel RSS 75.4 MiB, mean occupied CPU 0.436 cores, peak FDs 172. These are whole tunnel-process samples, including any unrelated live routes.

Configuration rollback backups are under `/root/super-paqet/rollback/20261006T083827Z-nl-syn/` on each modified host. Independent rollback timers stayed armed through qualification and were disarmed after successful final checks. Temporary echo services, loopback forwards and probe profiles were removed. Services remain active/enabled; binary SHA-256 is unchanged. The [fresh snapshots](deployed/README.md) record the retained settings. No clean-link bulk investigation was performed.

This verifies the repaired path under this test workload. It does not qualify thousands of simultaneously busy Reality users, multi-day stability, every firewall policy, or the unresolved saturated virtual asymmetric gate.
