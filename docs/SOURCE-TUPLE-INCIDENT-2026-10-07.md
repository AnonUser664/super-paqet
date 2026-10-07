# Live source-tuple outage, 7 October 2026

Client 89.45.68.118 → France was failing during investigation. The ownership.2
process remained PID 955116, without restart or recorded OOM. Germany continued
carrying traffic. Client .14 could establish new France carriers while .118 could
not. .118's France opening timeouts started around 04:13 UTC (07:43 Tehran);
Finland failures were already present by 01:45 UTC. The current episode therefore
does not show simultaneous failure of every route or establish the cause of
other reported episodes.

Two paired, bounded interface captures found .118 sending raw S packets from
29997 to France port 29999, with no corresponding packets at France's net0.
The repeat used tcpdump immediate mode, a broad host/TCP-or-ICMP filter and a
larger buffer. Its 30-second captures recorded 95 packets on .118 and zero on
France, with zero capture drops on both. The initial 20-second capture recorded
72/zero packets. Private header-limited PCAPs remain under build/incident-20261006;
customer payload is not published. Configured interfaces, source addresses,
next-hop MACs and owned firewall rules matched the live route/neighbor state.

Ordinary .118 → France TCP connections to 22 and 2096 succeeded in approximately
95 and 90 ms. Three ICMP replies averaged 90 ms. A TCP connect to 29999 timed out,
but that alone is inconclusive: the raw tunnel deliberately drops kernel TCP
handling on that port.

The unchanged binary and reliability/encryption/flag configuration were tested
in an isolated temporary client instance with source port 29995, the same France
listener 29999, and forward 19003 to the same Reality service 2096. An authenticated
Reality/VLESS request downloaded 1 MiB successfully. Requests on the old live
9003 route had repeatedly timed out. Only France's source port was then changed
live to 29995. No main tunnel restart occurred, and Germany's peer was retained.
The authenticated public 9003 check passed afterward in 1.63 seconds.

The analogous isolated Finland test, using source 29994 and forward 19002,
also downloaded 1 MiB successfully. Finland's source was subsequently changed
live from 29996 to 29994. All six public client/country combinations then passed
an authenticated 1 MiB request. S outbound / PA return, null encryption,
adaptation, four initial/maximum shared-source carriers, destinations and the
executable remained unchanged. Temporary instances were stopped. Diagnostic
restore baselines and hash guards were updated without resetting their timers.
Main Xray was not modified.

This establishes a failure tied to the old source/destination tuple during this
episode. It does not locate the dropping device, prove censorship, identify the
trigger, or guarantee a newly chosen port cannot later be affected. KCP session
recreation alone reuses the same source tuple and cannot repair such a drop.
`session.connected` currently describes successful *local* carrier setup, not a
received peer response; RTT zero and repeated unanswered opening attempts are
consistent with that distinction. Safe automatic source-tuple recovery needs a
verified fresh-path probe before switching, protection for any progressing
carrier, bounded retries, scoped firewall cleanup and reload-race handling.
