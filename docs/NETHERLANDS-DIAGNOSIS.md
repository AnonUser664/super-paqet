# Netherlands same-path diagnosis, 2026-10-06

The previously working enterprise executable now fails on **both** clients to
`171.22.132.226`. All four authenticated Reality requests timed out during TLS
setup. Unmodified upstream paqet also failed all four requests in the completed
unencrypted control. The user requested stopping when the older working version
failed; further diagnosis was interrupted and the original services/configs were
restored. Netherlands remains unavailable; no fix or production qualification is
claimed.

## Controls

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
comparisons. Captures are private artifacts; the committed
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

## Restoration and stop boundary

Comparisons temporarily stopped only the Netherlands backend process and removed
only the Netherlands peer/forwards from each client's live configuration.
Germany peer/forwards remained running. Remote rollback timers protected the
saved configurations; `finally` cleanup restored those exact saved bytes and
restarted/reloaded production services. Test units and owned upstream firewall
rules were removed. No production executable was replaced, no runtime source
change was made, and no bulk throughput work was performed.

The diagnosis stops here at the user's request. A working raw network path is
needed before Netherlands can be requalified.
