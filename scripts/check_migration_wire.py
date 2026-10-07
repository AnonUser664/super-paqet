#!/usr/bin/env python3
"""Validate retained S/PA outer headers in an isolated Ethernet pcap.

This checks framing and fabricated field rules, not resistance to filtering.
The fixture starts capture before either tunnel process opens its socket.
"""
import collections
import pathlib
import struct


def check(path):
    """Check both directions and each new source's initial encoder sequence."""
    data = pathlib.Path(path).read_bytes()
    assert data[:4] in (b'\xd4\xc3\xb2\xa1', b'\x4d\x3c\xb2\xa1'), 'expected little-endian pcap'
    assert struct.unpack_from('<I', data, 20)[0] == 1, 'expected Ethernet capture'
    at, packets, source_first, backend_seed = 24, collections.Counter(), {}, None
    while at < len(data):
        size = struct.unpack_from('<I', data, at + 8)[0]
        frame = data[at + 16:at + 16 + size]; at += 16 + size
        if len(frame) < 54 or frame[12:14] != b'\x08\x00':
            continue
        ip = frame[14:]; ihl = (ip[0] & 15) * 4
        if ip[9] != 6:
            continue
        tcp = ip[ihl:]; src, dst, seq, ack = struct.unpack_from('!HHII', tcp)
        if src != 29999 and dst != 29999:
            continue
        # Retired probe ports can receive late returns and generate an ordinary
        # zero-payload kernel RST after owned firewall cleanup. It is not KCP.
        if tcp[13] & 4 and struct.unpack_from('!H',ip,2)[0] == ihl + (tcp[12] >> 4) * 4:
            packets['kernel_RST_without_payload'] += 1
            continue
        assert ip[1] == 184 and ip[8] == 64 and struct.unpack_from('!H', ip, 6)[0] & 0x4000, 'outer IPv4 envelope changed'
        assert struct.unpack_from('!H', tcp, 14)[0] == 65535, 'outer TCP window changed'
        kinds, timestamp, pos, end = [], None, 20, (tcp[12] >> 4) * 4
        while pos < end:
            kind = tcp[pos]; kinds.append(kind)
            if kind in (0, 1):
                pos += 1
            else:
                length = tcp[pos + 1]
                assert length >= 2 and pos + length <= end, 'malformed TCP option'
                if kind == 8: timestamp = struct.unpack_from('!II', tcp, pos + 2)
                pos += length
        if dst == 29999:
            packets['outbound_S'] += 1
            assert tcp[13] == 2 and 1 <= seq <= 8 and ack == 0, 'S flag/sequence rule changed'
            assert kinds == [2, 4, 8, 1, 3] and timestamp[1] == 0, 'S option rule changed'
            if src not in source_first:
                source_first[src] = {'seq': seq, 'timestamp': timestamp[0]}
                assert seq == 2, 'fresh source did not initialize its encoder normally'
        else:
            packets['return_PA'] += 1
            assert tcp[13] == 24 and kinds == [1, 1, 8], 'PA flag/option rule changed'
            if backend_seed is None:
                # Parallel workers may deliver counter 2 before counter 1.
                difference=(seq-timestamp[0]) & 0xffffffff
                counter=next((n for n in range(1,65537) if (128*n-(n>>3)) & 0xffffffff == difference),None)
                assert counter is not None, 'cannot infer initial backend encoder'
                backend_seed=(seq-128*counter) & 0xffffffff
            distance = (seq - backend_seed) & 0xffffffff
            assert distance % 128 == 0, 'backend encoder seed changed'
            counter = distance // 128
            assert ack == (seq - (counter & 0x3ff) + 1400) & 0xffffffff, 'PA ACK rule changed'
            assert timestamp == ((backend_seed + (counter >> 3)) & 0xffffffff, (backend_seed + (counter >> 3) - (counter % 200 + 50)) & 0xffffffff), 'PA timestamp rule changed'
    assert packets['outbound_S'] and packets['return_PA'], 'missing captured direction'
    return {'packets': dict(packets), 'sources': source_first, 'backend_seed_retained': True}
