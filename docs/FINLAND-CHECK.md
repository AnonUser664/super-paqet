# Replacement Finland IP check, 2026-10-06

The qualified Netherlands profile **did not work** on `65.109.211.233` from either
client in this bounded check. Four authenticated 1 MiB Reality requests failed
at the eight-second TLS deadline. Six independent echo checks (64 bytes, 16 KiB
and 256 KiB per client) also failed. No 10 MiB success or capacity qualification
is claimed. [Evidence](FINLAND-CHECK-EVIDENCE.json).

## Configuration correction

The replacement host still tried to bind `65.109.192.172:2052`, which was no
longer assigned. Its systemd service repeatedly exited with `cannot assign
requested address`. Both clients also retained the old raw endpoint and target.

Only those IP references were corrected: backend listen/source becomes
`65.109.211.233:2052`; client backend2 becomes that endpoint, and port 9002 targets
`65.109.211.233:2096`. Exact originals were backed up under each modified host's
`/root/super-paqet/rollback/*-finland-ip/`. The corrected service is active and
enabled; the existing 9002 profile still failed four 1 MiB requests. Germany and
Netherlands endpoint settings were preserved.

Finland's production executable remains the older enterprise build, SHA-256
`ecb8e002173f6f80cd3f403d3ee3080190f0e49889fa2ba9f883b893f96318f3`.
The old excluded IP was not contacted.

## Matched Netherlands-profile test

Because that executable lacks shared-source carriers, an isolated systemd
listener used the exact current `47c61ac2…` binary on port 29999. The Netherlands
profile was copied with its existing S outbound / PA return flags, null cipher,
MTU1350, four shared carriers, fast KCP, client windows 131/522 and server
1044/1044, and timing/credit/adaptive-buffer extensions disabled. Backend source,
interface and gateway MAC were changed to the verified new host values. Client
test sources were 29996; test forwards were loopback-only on 28082/28084. This
was a genuine matched-profile control, not an old/current shared-source mismatch.
The temporary listener's health endpoint passed before testing.

Paired physical-interface captures (`ens160` / `eth0`, full 1600-byte snapshot)
showed:

| Direction | Sent at client | Matching backend arrival |
|---|---|---|
| .14 → Finland | 252 | 0 |
| .118 → Finland | 322 | 0 |

The backend capture saw no frames in the selected tunnel tuples. Every capture
reported zero kernel drops. The separate checked echoes rule out attributing
this failure solely to VLESS/Reality credentials or proxy egress.

## Reachability controls and limits

Both clients established ordinary TCP connections to Finland ports 22 and 2096,
and received the SSH banner. Each received one of three ICMP replies (~90/95 ms);
that short probe cannot distinguish ICMP rate limiting from packet loss. A
backend-local Reality request returned HTTP 200 and exactly 1 MiB in 0.188 s.

This excludes a complete IP outage from the clients and supports selective
failure of this raw TCP profile/tuple before the backend capture. It does not
identify a filtering device, prove all ports/flags fail, or establish that the
new IP is permanently blocked. A broader port/flag study was not performed in
this check.

Temporary listeners, binaries, echo services, captures and loopback forwards
were removed from the hosts; exact saved client configuration bytes were
restored. Only the requested replacement-IP corrections remain. The current
Netherlands SYN profile was **not** promoted to Finland's production 9002 route.
Current snapshots are in [deployed/](deployed/README.md).

Final regression requests through ports 9001 and 9003 returned HTTP 200 and
exactly 1 MiB from both clients (4/4). All three modified hosts were active/enabled;
owned test services and rollback timers were inactive, temporary files absent,
and live config hashes matched the recorded snapshots.
