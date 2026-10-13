# Client .118 external connectivity failure

Investigation at 13:34–13:41 UTC on 12 October found `89.45.68.118` reachable
through `89.45.68.14`, but unable to communicate directly with the three backends
or the tested public Internet addresses. The tunnel process remained PID
1175580 with zero systemd restarts. Its process health endpoint returned `ok`;
that endpoint does not establish peer reachability.

No production binary, config, firewall, Xray, nginx or systemd service was changed.
Two sparse packet matrices held the shared test reservation. Owned captures were
stopped, downloaded and removed from each remote host. Raw packet source ports
were fresh diagnostic slots; occupied slots were skipped. No established tunnel
tuple was used for the injected packets.

## Evidence

Both IP raw-socket and Ethernet AF_PACKET tests produced the same result:

| Check | .118 | .14 control |
| --- | --- | --- |
| Raw PA, SYN and ACK toward backend ports 22/29999/443 | Present in sender capture; absent at every backend | Present at sender and destination |
| Backend PA, SYN and ACK toward client ports 22/443 | Present at backend; absent at client | Present at both ends |
| Ordinary TCP to backend SSH and Reality port 2096 | Timed out | Connected on all three backends |
| Ordinary TCP to 1.1.1.1:443 and 8.8.8.8:443 | Timed out | Connected |
| Ping gateway and same-subnet client | Passed | Passed |
| Ping public 1.1.1.1 and France | Failed | Passed |

The Ethernet test uses each host's configured tunnel interface and gateway MAC,
so the failure is reproduced independently of the app, KCP, kernel TCP connection
state and the choice between the tested flags. All capture kernel-drop counters
were zero. The [machine-readable receipt](client118-incident-2026-10-12.json)
contains exact sent/local/remote matches, skipped slots and capture hashes.
Unrelated capture records and private service configurations remain out of Git.

ICMP TTL probes from both clients receive replies from `94.139.180.1` at TTL 1
and `172.24.113.21` at TTL 2. At TTL 3/5 only .14 gets replies. This supports
loss farther upstream than the guest's local gateway path, but a missing TTL
reply does not locate the dropping device: routers may suppress those replies.

## Timeline and interpretation

The retained 10-second observer first marks two German carriers suspect at
10:48:40 UTC (14:18:40 Tehran). Finland starts deteriorating at 11:29:11 UTC;
France at 11:31:02 UTC. All twelve carriers are suspect by 11:33:02 UTC
(15:03:02 Tehran). The PID stays unchanged. At 13:40:53 UTC the observer records
5,522 recovery attempts and zero successes since its start, with 40,586 failed
forward openings. These counts represent repeated attempts, not distinct users
or distinct filtering events. The gradual carrier timeline need not identify
the exact instant an upstream policy changed.

The defensible diagnosis is a broad, IP-specific upstream restriction or routing
fault affecting .118's external IPv4 connectivity. An application crash or a
KCP-only port/flag problem does not explain these independent TCP/ICMP results.
The endpoint captures cannot distinguish censorship, provider mitigation,
blackholing or a routing failure, and cannot prove what triggered it.

Changing PA/S/ACK flags or rotating client source ports did not restore these
tested paths. Resolution requires the provider to restore .118's upstream
connectivity, a replacement reachable address, or a deliberately configured
relay. No relay was installed during diagnosis; moving customer traffic through
.14 would change topology and consume its capacity.
