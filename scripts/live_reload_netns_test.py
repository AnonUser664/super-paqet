#!/usr/bin/env python3
"""Verify live reload against active raw-TCP/KCP paths in owned Linux namespaces.

Run as root; results retain binary identity, per-step continuity and owned-rule
cleanup. Traffic workers keep real application streams active during every edit.
"""
import argparse
import asyncio
import concurrent.futures
import copy
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import struct
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[1]


def checked(*args):
    """Execute an owned fixture command and retain stderr on failure."""
    return subprocess.run(args, check=True, text=True, capture_output=True)


def exact(conn, size):
    """Require a full response; EOF and truncation fail integrity checks."""
    result = bytearray()
    while len(result) < size:
        block = conn.recv(size-len(result))
        if not block:
            raise EOFError('incomplete echo response')
        result.extend(block)
    return bytes(result)


def exchange(conn, tag, size=512):
    """Verify a destination-specific tag and deterministic full payload."""
    payload = bytes(range(256))*(size//256) + bytes(range(size % 256))
    conn.sendall(struct.pack('!I', size)+payload)
    response = exact(conn, size+1)
    if response != tag.encode()+payload:
        raise RuntimeError('target tag or payload mismatch')
    return len(payload)


async def echo_servers():
    """Host TCP and UDP tagged destinations behind the receiving tunnel."""
    async def tcp(reader, writer, tag):
        """Echo complete framed requests until the application stream ends."""
        try:
            while True:
                size, = struct.unpack('!I', await reader.readexactly(4))
                if size == 0xffffffff:
                    # Explicit directional EOF while keeping the write side idle.
                    writer.write_eof()
                    await reader.read()
                    return
                if size > 2 << 20:
                    raise ValueError('fixture frame too large')
                payload = await reader.readexactly(size)
                writer.write(tag+payload)
                await writer.drain()
        except (asyncio.IncompleteReadError, ConnectionError):
            pass
        finally:
            writer.close()
            await writer.wait_closed()

    class DatagramEcho(asyncio.DatagramProtocol):
        """Tag whole UDP datagrams without changing their payload boundaries."""
        def __init__(self, tag):
            self.tag = tag

        def connection_made(self, transport):
            self.transport = transport

        def datagram_received(self, data, addr):
            self.transport.sendto(self.tag+data, addr)

    loop = asyncio.get_running_loop()
    servers = []
    transports = []
    for port, tag in ((18080, b'A'), (18081, b'B'), (18084, b'C')):
        servers.append(await asyncio.start_server(lambda r, w, tag=tag: tcp(r, w, tag), '127.0.0.1', port))
    for port, tag in ((18082, b'A'), (18083, b'B'), (18085, b'C')):
        transport, _ = await loop.create_datagram_endpoint(lambda tag=tag: DatagramEcho(tag), local_addr=('127.0.0.1', port))
        transports.append(transport)
    try:
        await asyncio.Future()
    finally:
        for server in servers:
            server.close()
        for transport in transports:
            transport.close()


class TrafficGroup:
    """Keep established streams exchanging verified bytes while configs change."""
    def __init__(self, port, tag, count):
        self.stop = threading.Event()
        self.lock = threading.Lock()
        self.bytes = self.errors = self.responses = 0
        self.connections = []
        self.threads = []

        def connect(_):
            """Open and verify before counting a socket as established."""
            conn = socket.create_connection(('127.0.0.1', port), timeout=8)
            exchange(conn, tag)
            return conn

        with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
            self.connections = list(pool.map(connect, range(count)))
        for conn in self.connections:
            thread = threading.Thread(target=self.transfer, args=(conn, tag), daemon=True)
            thread.start()
            self.threads.append(thread)

    def transfer(self, conn, tag):
        """Count integrity failures separately from intentional fixture teardown."""
        while not self.stop.is_set():
            try:
                size = exchange(conn, tag)
                with self.lock:
                    self.bytes += size
                    self.responses += 1
            except (OSError, EOFError, RuntimeError):
                if not self.stop.is_set():
                    with self.lock:
                        self.errors += 1
                return
            self.stop.wait(.02)

    def snapshot(self):
        """Read coherent counters for continuity and progress assertions."""
        with self.lock:
            return dict(bytes=self.bytes, errors=self.errors, responses=self.responses,
                        established=len(self.connections), running=sum(t.is_alive() for t in self.threads))

    def close(self):
        """Stop all fixture traffic before releasing sockets and joining tasks."""
        self.stop.set()
        for conn in self.connections:
            conn.close()
        for thread in self.threads:
            thread.join(timeout=10)


def traffic_worker():
    """Expose local namespace probes over JSON stdin/stdout, keeping groups alive."""
    groups, busy, idle = {}, {}, []
    try:
        for line in sys.stdin:
            command = json.loads(line)
            try:
                op = command['op']
                if op == 'group':
                    groups[command['name']] = TrafficGroup(command['port'], command['tag'], command['count'])
                    result = groups[command['name']].snapshot()
                elif op == 'stats':
                    result = {name: group.snapshot() for name, group in groups.items()}
                elif op == 'remove':
                    result = groups[command['name']].snapshot()
                    groups.pop(command['name']).close()
                elif op == 'halfidle':
                    for _ in range(command['count']):
                        conn = socket.create_connection(('127.0.0.1', command['port']), timeout=6)
                        conn.sendall(struct.pack('!I', 0xffffffff))
                        if conn.recv(1):
                            raise RuntimeError('fixture directional EOF missing')
                        idle.append(conn)
                    result = len(idle)
                elif op == 'probe':
                    with socket.create_connection(('127.0.0.1', command['port']), timeout=6) as conn:
                        result = dict(bytes=exchange(conn, command['tag'], command.get('size', 512)))
                elif op == 'udp':
                    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as conn:
                        conn.settimeout(6)
                        payload = b'reload-datagram'
                        conn.sendto(payload, ('127.0.0.1', command['port']))
                        if conn.recv(65507) != command['tag'].encode()+payload:
                            raise RuntimeError('UDP integrity or target mismatch')
                        result = dict(bytes=len(payload))
                elif op == 'busy':
                    conn = socket.socket()
                    conn.bind(('127.0.0.1', command['port']))
                    conn.listen()
                    busy[command['port']] = conn
                    result = True
                elif op == 'unbusy':
                    busy.pop(command['port']).close()
                    result = True
                elif op == 'quit':
                    print(json.dumps(dict(ok=True, result=True)), flush=True)
                    return
                else:
                    raise ValueError('unknown worker operation')
                print(json.dumps(dict(ok=True, result=result)), flush=True)
            except Exception as err:
                print(json.dumps(dict(ok=False, error=str(err))), flush=True)
    finally:
        for group in groups.values():
            group.close()
        for conn in busy.values():
            conn.close()
        for conn in idle:
            conn.close()


def main():
    """Own links/processes, perform edits, assert continuity and always clean up."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', default='build/super-paqet-live-reload')
    parser.add_argument('--output', default='build/live-reload-netns')
    parser.add_argument('--streams', type=int, default=32, help='active streams per original peer')
    parser.add_argument('--carriers', type=int, default=1, help='deterministic source-port carriers per original peer, 1..8')
    parser.add_argument('--shared-source', action='store_true', help='keep one fixed source port for all original peer carriers')
    parser.add_argument('--small-write-flush', type=int, default=0, help='exercise fast-mode bulk batching with this interactive-write threshold')
    parser.add_argument('--delay-ms', type=int, default=0, help='one-way virtual link delay')
    parser.add_argument('--loss', type=float, default=0)
    parser.add_argument('--reverse-delay-ms', type=int)
    parser.add_argument('--rate-mbit', type=int, default=0)
    parser.add_argument('--reverse-rate-mbit', type=int, default=0)
    parser.add_argument('--reorder', type=float, default=0)
    parser.add_argument('--cycles', type=int, default=12)
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error('run as root; only owned network namespaces are modified')
    if not 0 <= args.small_write_flush <= 65535 or not 1 <= args.carriers <= 8 or args.streams < 1 or args.cycles < 1 or min(args.delay_ms, args.reverse_delay_ms or 0, args.rate_mbit, args.reverse_rate_mbit) < 0 or not 0 <= args.loss <= 100 or not 0 <= args.reorder <= 100:
        parser.error('invalid workload bounds')
    binary = (ROOT/args.binary).resolve()
    out = (ROOT/args.output).resolve()
    out.mkdir(parents=True, exist_ok=True)
    report = dict(binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), steps=[],
                  streams_per_original_peer=args.streams, one_way_delay_ms=args.delay_ms, reverse_delay_ms=args.reverse_delay_ms, loss_percent=args.loss, rate_mbit=args.rate_mbit, reverse_rate_mbit=args.reverse_rate_mbit, reorder_percent=args.reorder)
    ident = str(os.getpid())
    client_ns, server_ns = 'spq-reload-c-'+ident, 'spq-reload-s-'+ident
    namespaces, processes, files = [], [], []
    worker = None

    def ns(namespace, *command):
        """Run a command inside one of this test's owned namespaces."""
        return checked('ip', 'netns', 'exec', namespace, *command)

    def spawn(namespace, name, *command, pipes=False):
        """Retain process/log handles so every started fixture is reaped."""
        log = open(out/(name+'.log'), 'w')
        files.append(log)
        proc = subprocess.Popen(['ip', 'netns', 'exec', namespace, *command],
                                stdin=subprocess.PIPE if pipes else subprocess.DEVNULL,
                                stdout=subprocess.PIPE if pipes else log, stderr=log, text=True)
        processes.append(proc)
        return proc

    def request(op, **fields):
        """Require a complete JSON worker response; errors fail the current step."""
        worker.stdin.write(json.dumps(dict(op=op, **fields))+'\n')
        worker.stdin.flush()
        result = json.loads(worker.stdout.readline())
        if not result['ok']:
            raise RuntimeError(result['error'])
        return result['result']

    def metrics(namespace, port=29090):
        """Read the running instance's reload and carrier metrics."""
        return ns(namespace, sys.executable, '-c',
                  'import urllib.request; print(urllib.request.urlopen("http://127.0.0.1:%d/metrics", timeout=3).read().decode())' % int(port)).stdout

    def counter(namespace, name, port=29090):
        """Extract one exact scalar metric without accepting an absent sample."""
        prefix = 'super_paqet_'+name+' '
        for line in metrics(namespace, port).splitlines():
            if line.startswith(prefix):
                return int(line[len(prefix):])
        raise RuntimeError('missing metric '+name)

    def wait_until(predicate, message, timeout=15):
        """Bound asynchronous reload/recovery waits and report their last failure."""
        deadline = time.monotonic()+timeout
        failure = ''
        while time.monotonic() < deadline:
            try:
                if predicate():
                    return
            except Exception as err:
                failure = str(err)
            time.sleep(.1)
        raise RuntimeError(message+': '+failure)

    def write(side, config, mode='atomic'):
        """Write strict JSON-as-YAML using both supported editor write patterns."""
        path = out/(side+'.yaml')
        data = json.dumps(config, indent=2)+'\n'
        if mode == 'atomic':
            tmp = path.with_suffix('.yaml.tmp')
            tmp.write_text(data)
            os.chmod(tmp, 0o600)
            tmp.replace(path)
        else:
            path.write_text(data)

    def apply(side, config, mode='atomic', port=29090):
        """Require a revision increment from the same service process."""
        namespace = client_ns if side == 'client' else server_ns
        before = counter(namespace, 'config_revision', port)
        write(side, config, mode)
        wait_until(lambda: counter(namespace, 'config_revision', config['metrics'].rsplit(':', 1)[1]) > before,
                   side+' reload did not commit')

    def continuity(label, names=('a', 'b')):
        """Require verified byte progress and zero errors on unaffected groups."""
        before = request('stats')
        start = time.monotonic()
        # KCP recovery on a capped/reordered link can exceed one RTT. Require
        # progress within the fixture's existing 8s application read deadline,
        # while every reset, corruption or timed-out stream remains a failure.
        while True:
            time.sleep(.1)
            after = request('stats')
            for name in names:
                if after[name]['errors'] or after[name]['running'] != after[name]['established']:
                    raise RuntimeError('unaffected group failed at '+label+': '+str(after[name]))
            if all(after[name]['responses'] > before[name]['responses'] for name in names):
                break
            if time.monotonic()-start > 8:
                raise RuntimeError('unaffected group made no progress at '+label)
        report['steps'].append(dict(name=label, groups=after, continuity_wait_ms=round((time.monotonic()-start)*1000, 3)))
        print(label, flush=True)

    def endpoint(address, iface, source, mac):
        """Keep transport setup explicit so route discovery does not affect tests."""
        return dict(address=address, enc='null', sessions=1, max_sessions=1, packet_workers=1,
                    adaptive=False, network=dict(interface=iface, backend='packet', ipv4=dict(addr=source, router_mac=mac)),
                    kcp=dict(mode='fast' if args.small_write_flush else 'fast3', small_write_flush=args.small_write_flush,
                             sndwnd=1024, rcvwnd=1024, smuxkalive=1, smuxktimeout=4))

    def rule_count(namespace):
        """Count only owned chain definitions, excluding unrelated fixture rules."""
        return sum(line.startswith(':SPQ_') for line in ns(namespace, 'iptables-save').stdout.splitlines())

    try:
        for namespace in (client_ns, server_ns):
            checked('ip', 'netns', 'add', namespace)
            namespaces.append(namespace)
            ns(namespace, 'ip', 'link', 'set', 'lo', 'up')
        hc, hs = 'rlc'+ident, 'rls'+ident
        checked('ip', 'link', 'add', hc, 'type', 'veth', 'peer', 'name', hs)
        for namespace, old, iface, ip, mac in ((client_ns, hc, 'c0', '198.19.1.1/24', '02:00:00:19:01:01'),
                                               (server_ns, hs, 's0', '198.19.1.2/24', '02:00:00:19:01:02')):
            checked('ip', 'link', 'set', old, 'netns', namespace)
            ns(namespace, 'ip', 'link', 'set', old, 'name', iface)
            ns(namespace, 'ip', 'link', 'set', iface, 'address', mac)
            ns(namespace, 'ip', 'addr', 'add', ip, 'dev', iface)
            ns(namespace, 'ip', 'link', 'set', iface, 'up')
            ns(namespace, 'iptables', '-A', 'INPUT', '-p', 'udp', '--dport', '31111', '-j', 'DROP')
            delay = args.reverse_delay_ms if namespace == server_ns and args.reverse_delay_ms is not None else args.delay_ms
            rate = args.reverse_rate_mbit if namespace == server_ns else args.rate_mbit
            if delay or args.loss or rate or args.reorder:
                netem = ['tc', 'qdisc', 'add', 'dev', iface, 'root', 'netem', 'limit', '10000']
                if delay:
                    netem += ['delay', str(delay)+'ms']
                if args.loss:
                    netem += ['loss', str(args.loss)+'%']
                if args.reorder:
                    if not delay:
                        raise ValueError('reordering requires a positive delay in both directions')
                    netem += ['reorder', str(args.reorder)+'%', '25%', 'gap', '5']
                if rate:
                    netem += ['rate', str(rate)+'mbit']
                netem += ['seed', '7411']
                ns(namespace, *netem)
        reload = dict(interval='50ms', debounce='100ms')
        server = dict(listeners=[endpoint('198.19.1.2:29999', 's0', '198.19.1.2:29999', '02:00:00:19:01:01'),
                                 endpoint('198.19.1.2:29996', 's0', '198.19.1.2:29996', '02:00:00:19:01:01')],
                      metrics='127.0.0.1:29090', log=dict(level='debug', interval='100ms', flow_sample=1000), reload=reload)
        client = dict(peers={'a': endpoint('198.19.1.2:29999', 'c0', '198.19.1.1:29998', '02:00:00:19:01:02'),
                             'b': endpoint('198.19.1.2:29996', 'c0', '198.19.1.1:29997', '02:00:00:19:01:02')},
                      forwards=[dict(listen='127.0.0.1:28080', peer='a', target='127.0.0.1:18080', protocol='tcp'),
                                dict(listen='127.0.0.1:28081', peer='b', target='127.0.0.1:18081', protocol='tcp'),
                                dict(listen='127.0.0.1:28082', peer='a', target='127.0.0.1:18082', protocol='udp'),
                                dict(listen='127.0.0.1:28083', peer='b', target='127.0.0.1:18083', protocol='udp')],
                      metrics='127.0.0.1:29090', log=dict(level='debug', interval='100ms', flow_sample=1000), reload=reload)
        if args.shared_source:
            for peer in client['peers'].values():
                peer.update(sessions=args.carriers, max_sessions=args.carriers, shared_source=True)
            for listener in server['listeners']:
                listener['shared_source'] = True
        elif args.carriers > 1:
            for index, peer in enumerate(client['peers'].values()):
                peer.update(sessions=args.carriers, max_sessions=args.carriers,
                            source_ports=list(range(31000+index*16, 31000+index*16+args.carriers)))
                peer['network']['ipv4']['addr'] = '198.19.1.1:0'
        report['carriers_per_original_peer'] = args.carriers
        report['shared_source'] = args.shared_source
        write('server', server)
        write('client', client)
        report['validation_prestart'] = {side: json.loads(ns(namespace, str(binary), 'config', 'validate', '-c', str(out/(side+'.yaml')), '--json').stdout) for side, namespace in (('client', client_ns), ('server', server_ns))}
        spawn(server_ns, 'echo', sys.executable, str(Path(__file__).resolve()), '--echo')
        server_proc = spawn(server_ns, 'server', str(binary), 'run', '-c', str(out/'server.yaml'))
        client_proc = spawn(client_ns, 'client', str(binary), 'run', '-c', str(out/'client.yaml'))
        worker = spawn(client_ns, 'worker', sys.executable, str(Path(__file__).resolve()), '--worker', pipes=True)
        wait_until(lambda: counter(client_ns, 'config_revision') == 1 and counter(server_ns, 'config_revision') == 1, 'startup')
        for name, port, tag in (('a', 28080, 'A'), ('b', 28081, 'B')):
            request('group', name=name, port=port, tag=tag, count=args.streams)
        request('udp', port=28082, tag='A')
        request('udp', port=28083, tag='B')
        continuity('initial TCP and UDP')
        report['validation_while_running'] = json.loads(ns(client_ns, str(binary), 'config', 'validate', '-c', str(out/'client.yaml'), '--json').stdout)
        (out/'client.yaml').write_text('unknown: true\nkey: SECRET-DO-NOT-LOG\n')
        wait_until(lambda: counter(client_ns, 'config_reload_rejected_total') >= 1, 'invalid YAML rejection')
        continuity('invalid config retains active paths')
        write('client', client)
        server['listeners'].append(endpoint('198.19.1.2:29994', 's0', '198.19.1.2:29994', '02:00:00:19:01:01'))
        apply('server', server)
        client['peers']['c'] = endpoint('198.19.1.2:29994', 'c0', '198.19.1.1:29995', '02:00:00:19:01:02')
        client['forwards'] += [dict(listen='127.0.0.1:28084', peer='c', target='127.0.0.1:18084', protocol='tcp'),
                               dict(listen='127.0.0.1:28085', peer='c', target='127.0.0.1:18085', protocol='udp')]
        apply('client', client)
        request('group', name='c', port=28084, tag='C', count=8)
        request('udp', port=28085, tag='C')
        continuity('add peer, listener, TCP and UDP forwards', ('a', 'b', 'c'))
        client['forwards'][0]['target'] = '127.0.0.1:18081'
        client['forwards'][2]['target'] = '127.0.0.1:18083'
        apply('client', client, 'inplace')
        request('probe', port=28080, tag='B')
        request('udp', port=28082, tag='B')
        continuity('target edits preserve established TCP', ('a', 'b', 'c'))
        client['limits'] = dict(connections=512, sessions=256, memory_mib=256, open_timeout='8s', dial_timeout='3s', udp_idle='4s')
        client['log'].update(level='info', interval='200ms', format='text', flow_sample=100)
        client['profiling'] = True
        apply('client', client)
        continuity('live limits, logging, deadlines and profiling', ('a', 'b', 'c'))
        client['reload']['enabled'] = False
        apply('client', client)
        revision = counter(client_ns, 'config_revision')
        client['forwards'][0]['target'] = '127.0.0.1:18080'
        write('client', client)
        time.sleep(.5)
        if counter(client_ns, 'config_revision') != revision:
            raise RuntimeError('disabled watcher applied an edit')
        client_proc.send_signal(signal.SIGHUP)
        wait_until(lambda: counter(client_ns, 'config_revision') > revision, 'SIGHUP manual reload')
        request('probe', port=28080, tag='A')
        continuity('SIGHUP reload with polling disabled', ('a', 'b', 'c'))
        client['reload']['enabled'] = True
        write('client', client)
        client_proc.send_signal(signal.SIGHUP)
        wait_until(lambda: counter(client_ns, 'config_revision') > revision+1, 're-enable polling')
        original = copy.deepcopy(client)
        before_rules = rule_count(client_ns)
        rejected = counter(client_ns, 'config_reload_rejected_total')
        request('busy', port=28099)
        client['peers']['staged'] = endpoint('198.19.1.2:29994', 'c0', '198.19.1.1:0', '02:00:00:19:01:02')
        client['forwards'].append(dict(listen='127.0.0.1:28099', peer='staged', target='127.0.0.1:18084', protocol='tcp'))
        write('client', client)
        wait_until(lambda: counter(client_ns, 'config_reload_rejected_total') > rejected, 'busy bind rejection')
        if rule_count(client_ns) != before_rules:
            raise RuntimeError('failed transaction leaked staged firewall rules')
        continuity('busy addition rolls back without interrupting paths', ('a', 'b', 'c'))
        # The same file retries after temporary resource pressure is removed.
        revision = counter(client_ns, 'config_revision')
        request('unbusy', port=28099)
        wait_until(lambda: counter(client_ns, 'config_revision') > revision, 'transient bind retry', timeout=12)
        request('probe', port=28099, tag='C')
        continuity('unchanged config retries temporary bind failure', ('a', 'b', 'c'))
        client = original
        apply('client', client)
        client['forwards'] = client['forwards'][:4]
        apply('client', client)
        continuity('removed TCP binds retain established streams', ('a', 'b', 'c'))
        request('remove', name='c')
        del client['peers']['c']
        apply('client', client)
        continuity('remove unused peer and owned rules')
        client['peers']['a']['kcp'].update(mode='manual', nodelay=1, interval=20, resend=1, nocongestion=1)
        apply('client', client)
        continuity('KCP retransmission settings update established carriers')
        server['listeners'][0]['kcp'].update(mode='manual', nodelay=1, interval=30, resend=1, nocongestion=1)
        apply('server', server)
        continuity('listener retransmission settings update established carriers')
        request('halfidle', port=28080, count=4)
        wait_until(lambda: counter(client_ns, 'active_connections') >= args.streams*2+4, 'half-closed streams were not retained')
        client['peers']['a']['kcp']['sndwnd'] = 2048
        apply('client', client)
        wait_until(lambda: request('stats')['a']['errors'] > 0, 'changed peer did not retire affected streams')
        report['affected_peer_closed'] = request('remove', name='a')
        wait_until(lambda: counter(client_ns, 'active_connections') <= args.streams+2, 'retired peer retained idle half-closed relays')
        report['half_closed_relays_released'] = True
        wait_until(lambda: bool(request('probe', port=28080, tag='A')), 'changed KCP peer recovery')
        continuity('structural window edit replaces only affected peer', ('b',))
        server['listeners'][0]['kcp'].update(mode='fast2', sndwnd=2048)
        apply('server', server)
        wait_until(lambda: bool(request('probe', port=28080, tag='A')), 'changed listener recovery', timeout=20)
        continuity('listener transport edit replaces only affected listener', ('b',))
        for config, ep in ((server, server['listeners'][0]), (client, client['peers']['a'])):
            ep['enc'] = 'aes-128-gcm'
            ep['key'] = 'fixture-key-rotation'
            apply('server' if config is server else 'client', config)
        wait_until(lambda: bool(request('probe', port=28080, tag='A', size=1 << 20)), 'cipher rotation recovery')
        continuity('endpoint cipher rotation preserves unrelated peer', ('b',))
        for config, ep in ((server, server['listeners'][0]), (client, client['peers']['a'])):
            ep['enc'] = 'null'
            ep.pop('key')
            ep['network']['backend'] = 'pcap'
            apply('server' if config is server else 'client', config)
        wait_until(lambda: bool(request('probe', port=28080, tag='A', size=1 << 20)), 'driver replacement recovery')
        continuity('null cipher and pcap driver replacement', ('b',))
        base_rules = rule_count(client_ns)
        base_fds = len(list(Path('/proc/%d/fd' % client_proc.pid).iterdir()))
        for cycle in range(args.cycles):
            client['peers']['cycle'] = endpoint('198.19.1.2:29994', 'c0', '198.19.1.1:0', '02:00:00:19:01:02')
            client['forwards'].append(dict(listen='127.0.0.1:28086', peer='cycle', target='127.0.0.1:18084', protocol='tcp'))
            apply('client', client)
            request('probe', port=28086, tag='C')
            del client['peers']['cycle']
            client['forwards'].pop()
            apply('client', client)
            if rule_count(client_ns) != base_rules:
                raise RuntimeError('cycle leaked firewall chain')
        time.sleep(5)
        after_fds = len(list(Path('/proc/%d/fd' % client_proc.pid).iterdir()))
        report['cycle_fd_counts'] = dict(before=base_fds, after=after_fds, cycles=args.cycles)
        if after_fds > base_fds+8:
            raise RuntimeError('reload cycles retained descriptors')
        continuity('repeated add/remove cycles preserve active traffic', ('b',))
        client['metrics'] = '127.0.0.1:29091'
        apply('client', client)
        continuity('metrics bind move preserves data plane', ('b',))
        report['client_revision'] = counter(client_ns, 'config_revision', 29091)
        report['server_revision'] = counter(server_ns, 'config_revision')
        report['final_groups'] = request('stats')
        report['same_processes'] = client_proc.poll() is None and server_proc.poll() is None
        if not report['same_processes']:
            raise RuntimeError('reload restarted a process')
        request('quit')
        worker.wait(timeout=15)
        report['passed'] = True
    finally:
        for proc in reversed(processes):
            if proc.poll() is None:
                proc.terminate()
            try:
                proc.wait(timeout=20)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)
            if proc in (locals().get('client_proc'), locals().get('server_proc')) and proc.returncode != 0:
                report.setdefault('shutdown_failures', []).append(proc.returncode)
        for file in files:
            file.close()
        try:
            clean = True
            for namespace in namespaces:
                rules = ns(namespace, 'iptables-save').stdout
                (out/(namespace+'.rules')).write_text(rules)
                clean = clean and 'SPQ_' not in rules
                ns(namespace, 'iptables', '-C', 'INPUT', '-p', 'udp', '--dport', '31111', '-j', 'DROP')
            report['firewall_clean'] = clean
            report['unrelated_rule_preserved'] = True
            if not clean:
                raise RuntimeError('shutdown leaked owned rules')
            if report.get('shutdown_failures'):
                raise RuntimeError('tunnel process shutdown failed')
            if (out/'client.log').exists() and 'SECRET-DO-NOT-LOG' in (out/'client.log').read_text():
                raise RuntimeError('invalid YAML secret leaked to daemon logs')
        finally:
            for namespace in reversed(namespaces):
                subprocess.run(['ip', 'netns', 'delete', namespace], capture_output=True)
            (out/'results.json').write_text(json.dumps(report, indent=2)+'\n')
    print(json.dumps({key: value for key, value in report.items() if key != 'steps'}, indent=2))


if __name__ == '__main__':
    if '--echo' in sys.argv:
        asyncio.run(echo_servers())
    elif '--worker' in sys.argv:
        traffic_worker()
    else:
        main()
