# Transport contract

Baseline: b9fa0bd. Linux is the deployment target.

The outer transport is **not a TCP connection**. Each Ethernet/IP/TCP frame
carries one independent KCP datagram. There is no outer TCP handshake or kernel TCP data transport. The enterprise
engine does reserve local kernel TCP listeners as port guards; they do not
carry the tunnel payload. Capture must still see packets before the host firewall.
KCP, rather than outer TCP sequence numbers, provides ordering, retransmission,
RTT estimation and delivery. Replacing this with ordinary TCP or UDP would
change the defining behavior of this project.

Preserve these properties while optimizing:

* Ethernet source is the selected interface; destination is the next-hop MAC.
* IPv4: TCP protocol, TTL 64, TOS 184, DF, checksummed headers.
* IPv6: TCP next-header, hop limit 64, traffic class 184.
* Outer TCP source/destination ports identify tunnel endpoints. Client source
  port may be allocated automatically. One server port accepts multiple clients.
* Configurable local and remote TCP flag cycles; default PSH+ACK.
* Window 65535, fabricated sequence/acknowledgment numbers and timestamps.
  SYN packets include MSS 1460, SACK permitted, timestamps and window scale 8.
  Other packets include two NOPs and timestamps. The baseline timestamp and
  sequence progression is intentional and must be regression-tested.
* Payload encryption, KCP ordering/retransmission and smux multiplexing remain
  inside the raw TCP envelope. Default baseline MTU is 1350 KCP packet bytes.
* Avoid kernel RST and conntrack interference for the selected tunnel ports.
  Firewall ownership must be scoped to this instance and cleanup must preserve
  unrelated rules. SIGKILL/power loss cannot execute cleanup; document recovery.

The user confirmed that compatibility with unmodified paqet is not required.
The v1 control header gains enterprise TCP/UDP message types (0x06/0x07).
TCP opening receives an explicit success/failure response. UDP uses a two-byte
big-endian datagram length, including zero-length datagrams. smux v2 keeps its
frame layout, with directional FIN semantics enabled at both enterprise ends.
Its encrypted command 5 closes both directions, releasing writers after target
abort; directional FIN still supports a response after request EOF. Both
enterprise endpoints must implement this extension. Default upstream smux mode
does not use it.
Optional ACK receive timestamps (eight inner payload bytes) separate forward
queue estimates from a delayed ACK path. KCP WINS can carry an inner 16-byte
stream-credit hint (marker, stream ID, cumulative consumed bytes, window). Hints
may be dropped/reordered; ordinary reliable smux UPD remains the recovery path.
No application data uses the hint path. Disable with `kcp.ack_timestamps: false`
and `kcp.credit_hints: false` when comparing the legacy control behavior.
These inner extensions follow the selected cipher: they are plaintext in null
mode. They do not introduce an outer TCP handshake. The actual recovery profile
disables both extensions.

Packet timing, rate and retransmission schedules change with optimization and
adaptation. Byte-format equivalence does not prove equivalent detectability
against arbitrary traffic classifiers. Packet capture tests must report which
properties they actually checked.

## Baseline fabricated counters and limits of the rationale

The committed running base and queue-pressure patch use the original fabricated
counter scheme. For ordinary non-SYN packets, sequence is `initial_time +
(counter << 7)`; acknowledgment is `sequence - (counter & 0x3ff) + 1400`.
Timestamp is `initial_time + (counter >> 3)` and its echo is synthetically offset.
SYN sequences/options use the baseline's separate branch. These numbers do not
track actual remote TCP byte consumption; a payload larger than 128 bytes can
therefore overlap the next fabricated sequence range. KCP's own sequence numbers
and ACKs supply reliability.

Preserving these characteristics protects the tested upstream packet contract
while avoiding an accidental switch to ordinary TCP. The record does not prove
why each magic constant was selected originally, or that each is essential for
every classifier. Packet-size-dependent failures and the separate byte-sequence
experiment motivate further study; they are not proof that overlap alone caused
all observed drops. The experiment `4c7aa6a` changes those number semantics and
remains excluded from the running base and `20227d3` patch.

Source compatibility is different from behavior: both enterprise endpoints need
the new control/half-close semantics, and source interoperability with an
unmodified paqet endpoint is not a supported deployment claim. TCP application
ordering and UDP boundaries remain required even when inner framing changes.

## Why the layers exist

Raw TCP envelopes retain the packet characteristics of the original project.
KCP turns unreliable datagrams into reliable delivery even when packets are
lost or reordered. Multiplexing carries many application connections over a
small number of KCP sessions, reducing per-session memory, timers, encryption
state and capture sockets. Multiple sessions allow distribution across CPUs
and limit the scope of ordered-delivery stalls caused by packet loss.

## Measurement and acceptance

Working targets: >=100,000 established TCP forwards and a separate >=2 Gbit/s
bulk test, with CPU, RSS, heap, goroutines, file descriptors, retransmissions,
error rates and latency recorded. See BENCHMARKS.md for achieved local results
and outstanding WAN limits.
Report active requests separately from idle established connections. Report
application goodput separately from wire bitrate. Include unshaped, bandwidth
capped, delayed, random-loss, burst-loss, mixed-flow and reconnect workloads.

Test traffic and firewall changes belong in disposable network namespaces.
Never infer WAN performance from a same-host veth result. Never claim universal
optimality or production readiness from a throughput benchmark.

Primary references:
* https://github.com/xtaci/kcp-go (protocol and implementation)
* https://github.com/xtaci/smux (shared buffers and stream flow control)
* https://docs.kernel.org/networking/packet_mmap.html (packet socket batching)
