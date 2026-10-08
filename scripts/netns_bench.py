#!/usr/bin/env python3
# Module purpose: Own disposable links/processes, emulate directional faults and verify
# payload/resources/cleanup for one workload.
"""Root-only, isolated raw TCP tunnel benchmark. Never changes host firewall/routes."""
import argparse
import json
import os
from pathlib import Path
import resource
import signal
import sys
import subprocess
import time
import hashlib
import threading
import math

ROOT = Path(__file__).resolve().parents[1]

# run: Run one command with a checked exit status so failures cannot silently enter
# acceptance evidence.
def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, **kwargs)

# main: Own disposable links/processes, emulate directional faults and verify
# payload/resources/cleanup for one workload.
def main():
    p = argparse.ArgumentParser()
    p.add_argument('--binary', default='build/super-paqet')
    p.add_argument('--duration', type=int, default=10)
    p.add_argument('--cold-read-bytes', type=int, help='size of the first integrity-checked transfer before warm-up')
    p.add_argument('--workers', type=int, default=32)
    p.add_argument('--loss', type=float, default=0)
    p.add_argument('--delay-ms', type=float, default=0, help='one-way delay')
    p.add_argument('--jitter-ms', type=float, default=0)
    p.add_argument('--reorder', type=float, default=0, help='percentage; requires delay')
    p.add_argument('--burst-loss', type=float, nargs=4, metavar=('P','R','BAD_LOSS','GOOD_LOSS'), help='netem Gilbert-Elliott percentages')
    p.add_argument('--seed', type=int)
    p.add_argument('--rate-mbit', type=int, default=0)
    p.add_argument('--down-rate-mbit', type=int, help='reverse direction bandwidth cap')
    p.add_argument('--sessions', type=int, default=4)
    p.add_argument('--max-sessions', type=int, help='carrier ceiling; defaults to --sessions so qualification uses a fixed pool')
    p.add_argument('--path-recovery', action='store_true', help='enable verified fresh-source recovery on the outgoing peer')
    p.add_argument('--preserve-connections', action='store_true', help='enable negotiated live-session source migration')
    p.add_argument('--shared-source', action='store_true', help='independent KCP lanes on one peer source tuple')
    p.add_argument('--client-memory-mib',type=int,default=0,help='optional client soft Go memory budget')
    p.add_argument('--server-memory-mib',type=int,default=0,help='optional backend soft Go memory budget')
    p.add_argument('--client-procs',type=int,default=0,help='optional client GOMAXPROCS for target-hardware qualification')
    p.add_argument('--server-procs',type=int,default=0,help='optional backend GOMAXPROCS for target-hardware qualification')
    p.add_argument('--client-cpus', default='', help='optional comma-separated CPU affinity for client tunnels')
    p.add_argument('--server-cpus', default='', help='optional comma-separated CPU affinity for backend tunnels')
    p.add_argument('--workload-cpus', default='', help='optional comma-separated CPU affinity for generators/targets/profilers')
    p.add_argument('--packet-workers',type=int,help='explicit backend capture worker count for matched deployment tests')
    p.add_argument('--conversation-listener', action='store_true', help='retain the deployed conversation-aware listener with separate client source ports')
    p.add_argument('--client-flag', default='PA', help='client outer packet flags (use S for the deployed profile)')
    p.add_argument('--server-flag', default='PA', help='backend outer packet flags')
    p.add_argument('--source-ports', type=int, nargs='+', help='fixed distinct initial client source ports for matched capture fanout')
    p.add_argument('--hold', type=int, default=0)
    fixture = p.add_mutually_exclusive_group()
    fixture.add_argument('--enterprise', dest='enterprise', action='store_true', help='enterprise configuration fixture (default)')
    fixture.add_argument('--legacy', dest='enterprise', action='store_false', help='role-based upstream fixture; select its binary explicitly')
    p.set_defaults(enterprise=True)
    p.add_argument('--profile', action='store_true')
    p.add_argument('--debug', action='store_true', help='structured transport diagnostics, sampled flow events')
    p.add_argument('--flow-sample',type=int,default=1000)
    p.add_argument('--backend',choices=['packet','pcap'],default='packet')
    p.add_argument('--duplex-http',action='store_true',help='HTTP connection churn alongside simultaneous bulk transfers')
    p.add_argument('--mtu',type=int,default=1500)
    p.add_argument('--ipv6',action='store_true')
    p.add_argument('--warmup',type=int,default=0,help='iperf3 omitted startup seconds')
    p.add_argument('--duplex-http-gap-ms',type=int,default=0,help='per-worker HTTP pause beside bulk; zero retains saturation')
    p.add_argument('--duplex-http-steady',action='store_true',help='omit the same configured startup duration from mixed HTTP statistics; keep legacy timing by default')
    p.add_argument('--schedule',help='JSON list of link changes relative to each workload start')
    p.add_argument('--mode', choices=['http','http-churn','bulk','both'], default='both')
    p.add_argument('--ramp-timeout', type=int, default=600)
    p.add_argument('--queue-packets', type=int, default=0)
    p.add_argument('--adaptive', choices=['on','off'], default='on')
    p.add_argument('--kcp-options', default='{}', help='JSON KCP parameter overrides for controlled experiments')
    p.add_argument('--functional', action='store_true')
    p.add_argument('--block', default='aes-128-gcm')
    p.add_argument('--fec',type=int,nargs=2,default=[0,0],metavar=('DATA','PARITY'))
    p.add_argument('--iperf', action='store_true')
    p.add_argument('--iperf-rate-mbit', type=int, default=0, help='aggregate TCP offered rate per direction; divided across parallel streams, zero is unlimited')
    p.add_argument('--pinned-lanes', action='store_true', help='use one peer per carrier and pin equal iperf streams to each; excludes pool-placement variance')
    p.add_argument('--iperf-directions', nargs='+', choices=['upload','download','bidirectional'], default=['upload','download','bidirectional'])
    p.add_argument('--direct-iperf', action='store_true', help='also measure the same virtual link without the tunnel')
    p.add_argument('--tcp-buffer-mib', type=int, default=0, help='namespace-only TCP autotuning ceiling')
    p.add_argument('--capture', action='store_true')
    p.add_argument('--restart', action='store_true')
    p.add_argument('--mixed', action='store_true')
    p.add_argument('--socket-buffer-mib', type=int, default=0)
    p.add_argument('--host-backlog',type=int,default=0,help='explicit temporary host netdev_max_backlog override, restored on exit')
    p.add_argument('--bridge', action='store_true', help='shape a middle bridge instead of endpoint socket queues')
    p.add_argument('--output', default='build/bench')
    a = p.parse_args()
    # Worktrees do not inherit ignored build dependencies. Reject a missing
    # workload binary before creating namespaces or measuring an empty run.
    if a.iperf:
        iperf_path = ROOT/'build/iperf-local/bin/iperf3'
        if not iperf_path.is_file() or not os.access(iperf_path, os.X_OK):
            p.error('iperf requires executable build/iperf-local/bin/iperf3 in this worktree')
    if a.cold_read_bytes is not None and not 1 <= a.cold_read_bytes <= 16777216:
        p.error('cold read size must be 1..16777216 bytes')
    if a.flow_sample<1:p.error('flow sample must be positive')
    if a.host_backlog<0 or a.host_backlog>1000000:p.error('host backlog must be 0..1000000')
    if a.duplex_http and (not a.enterprise or not a.iperf or 'bidirectional' not in a.iperf_directions):p.error('duplex HTTP requires enterprise bidirectional iperf')
    if a.duplex_http_steady and not a.duplex_http:p.error('duplex HTTP steady measurement requires duplex HTTP')
    if a.duplex_http_gap_ms < 0 or (a.duplex_http_gap_ms and not a.duplex_http):p.error('duplex HTTP gap requires duplex HTTP and cannot be negative')
    kcp_overrides=json.loads(a.kcp_options)
    if not isinstance(kcp_overrides,dict) or 'key' in kcp_overrides:p.error('KCP overrides must be an object without key')
    if any(x<0 for x in a.fec) or sum(a.fec)>256 or (a.fec[0]==0)!=(a.fec[1]==0):p.error('FEC requires both positive shards, total <=256, or both zero')
    if a.mtu<576 or a.mtu>9000: p.error('test MTU must be 576..9000')
    if a.ipv6 and a.mtu<1280: p.error('IPv6 requires MTU >=1280')
    if a.ipv6 and a.functional: p.error('use standalone IPv6 workloads; the multi-address fixture is IPv4')
    if a.warmup<0 or (a.warmup and not a.iperf): p.error('warmup requires iperf and cannot be negative')
    epochs=json.loads(a.schedule) if a.schedule else []
    allowed={'at','rate_mbit','down_rate_mbit','delay_ms','jitter_ms','loss','reorder','queue_packets'}
    for event in epochs:
        if not isinstance(event,dict) or 'at' not in event or set(event)-allowed: p.error('invalid link epoch fields')
        if any(not isinstance(v,(int,float)) or not math.isfinite(v) or v<0 for v in event.values()): p.error('link epochs require finite nonnegative numbers')
        if any(event.get(k,0)>100 for k in ('loss','reorder')): p.error('epoch percentages must be 0..100')
        if event['at']>=a.duration+a.warmup:p.error('epoch must occur within the workload interval')
    epochs.sort(key=lambda event:event['at'])
    if a.preserve_connections and (not a.path_recovery or a.shared_source):p.error('preserve-connections requires independent source ports and path-recovery')
    if a.path_recovery and not a.enterprise:p.error('path-recovery requires enterprise')
    if min(a.client_memory_mib,a.server_memory_mib)<0:p.error('memory budgets cannot be negative')
    if min(a.client_procs,a.server_procs)<0:p.error('process core budgets cannot be negative')
    for cpu_list in (a.client_cpus, a.server_cpus, a.workload_cpus):
        if cpu_list:
            try:
                cpus = [int(x) for x in cpu_list.split(',')]
            except ValueError:
                p.error('CPU affinity requires comma-separated integer CPU IDs')
            if len(set(cpus)) != len(cpus) or not set(cpus) <= os.sched_getaffinity(0):
                p.error('CPU affinity must contain distinct currently available CPU IDs')
    if a.packet_workers is not None and not 1<=a.packet_workers<=64:p.error('packet workers must be 1..64')
    if a.duration < 1 or a.workers < 1 or a.sessions < 1: p.error('duration, workers and sessions must be positive')
    if a.max_sessions is None: a.max_sessions = a.sessions
    if not a.sessions <= a.max_sessions <= 256: p.error('max-sessions must be sessions..256')
    if a.source_ports is not None:
        if not a.enterprise or a.shared_source or len(a.source_ports)<a.max_sessions or len(a.source_ports)>256 or len(set(a.source_ports))!=len(a.source_ports) or any(not 1<=x<=65535 for x in a.source_ports):
            p.error('source-ports requires enterprise independent sources, distinct valid ports and enough entries for max-sessions')
    if min(a.delay_ms,a.jitter_ms,a.rate_mbit,a.down_rate_mbit or 0,a.tcp_buffer_mib,a.queue_packets) < 0: p.error('delays and resource budgets cannot be negative')
    if any(not 0<=x<=100 for x in [a.loss,a.reorder,*(a.burst_loss or [])]): p.error('percentages must be within 0..100')
    if (a.jitter_ms or a.reorder) and not a.delay_ms: p.error('jitter and reordering require delay')
    if a.burst_loss and a.loss: p.error('choose random or burst loss')
    if a.direct_iperf and not a.iperf: p.error('--direct-iperf requires --iperf')
    if a.pinned_lanes and (not a.enterprise or not a.iperf or a.shared_source or a.functional or a.hold or a.direct_iperf or a.workers % a.sessions):
        p.error('pinned-lanes requires enterprise independent-source iperf, divisible workers and no functional/hold/direct fixture')
    if not 0 <= a.iperf_rate_mbit <= 200000 or (a.iperf_rate_mbit and not a.iperf): p.error('iperf-rate-mbit requires iperf and must be 0..200000')
    if (a.restart or a.functional or a.mixed) and not a.enterprise: p.error('these workloads require --enterprise')
    if os.geteuid() != 0:
        p.error('run as root; host backlog changes require the explicit --host-backlog option')
    resource.setrlimit(resource.RLIMIT_NOFILE, (500000, 500000))
    out = ROOT / a.output
    out.mkdir(parents=True, exist_ok=True)
    c, s = f'spq-c-{os.getpid()}', f'spq-s-{os.getpid()}'
    procs, files, namespaces, tunnel_procs, expected_killed = [], [], [], [], set()
    backlog_path=Path('/proc/sys/net/core/netdev_max_backlog')
    host_settings={}
    tracked={}
    schedule_stop=threading.Event()
    schedule_threads=[]
    schedule_errors=[]
    shaping=[]
    # ns: Execute inside the owned test namespace; target changes must not alter unrelated host
    # networking.
    def ns(n, *args):
        try:
            return run('ip', 'netns', 'exec', n, *args, capture_output=True)
        except subprocess.CalledProcessError as err:
            (out/'last-error.log').write_text((err.stdout or '')+(err.stderr or ''))
            raise
    # spawn: Start a fixture process with retained log/ownership handles for bounded teardown.
    def spawn(n, name, *args):
        f = open(out / (name+'.log'), 'w'); files.append(f)
        cores = a.client_procs if name in ('client','second-client') else a.server_procs if name in ('server','restarted-server') else 0
        if cores:args=('env',f'GOMAXPROCS={cores}',*args)
        affinity = a.client_cpus if name in ('client','second-client') else a.server_cpus if name in ('server','restarted-server') else a.workload_cpus
        if affinity: args=('taskset', '-c', affinity, *args)
        proc = subprocess.Popen(['ip', 'netns', 'exec', n, *args], stdout=f, stderr=subprocess.STDOUT)
        procs.append(proc)
        if name in ('target','server','client'): tracked[name]=proc
        if name=='restarted-server': tracked['server']=proc
        if name in ('server','client','second-client','restarted-server'): tunnel_procs.append(proc)
        return proc
    # stop: Terminate the owned fixture and record expected crash cases separately from
    # unexpected failures.
    def stop(*_):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, stop)
    peaks = {}
    next_fd_sample={}
    # sample: Record process CPU/RSS/FD state while limiting the observer cost charged to the
    # workload.
    def sample():
        for name, proc in tracked.items():
            try:
                status = Path(f'/proc/{proc.pid}/status').read_text()
                rss = int(next(x for x in status.splitlines() if x.startswith('VmRSS:')).split()[1])
                now=time.monotonic()
                stat = Path(f'/proc/{proc.pid}/stat').read_text().split()
                rec = peaks.setdefault(name, {'peak_rss_kib':0, 'peak_fds':0})
                rec['peak_rss_kib'] = max(rec['peak_rss_kib'], rss)
                values={line.split(':',1)[0]:int(line.split()[1]) for line in status.splitlines() if line.startswith(('VmSwap:','VmHWM:'))}
                swap=values.get('VmSwap',0)
                rec['peak_swap_kib']=max(rec.get('peak_swap_kib',0),swap)
                rec['peak_rss_plus_swap_kib']=max(rec.get('peak_rss_plus_swap_kib',0),rss+swap)
                rec['rss_high_water_kib']=max(rec.get('rss_high_water_kib',0),values.get('VmHWM',rss))
                # Scanning 100k descriptors every 250 ms makes the observer a
                # significant load generator itself. Keep CPU/RSS sampling fast
                # but enumerate descriptors at most once every two seconds.
                if now>=next_fd_sample.get(proc.pid,0):
                    fd=sum(1 for _ in Path(f'/proc/{proc.pid}/fd').iterdir())
                    rec['peak_fds'] = max(rec['peak_fds'], fd)
                    next_fd_sample[proc.pid]=now+2
                rec['cpu_seconds'] = (int(stat[13])+int(stat[14])) / os.sysconf('SC_CLK_TCK')
            except (FileNotFoundError, StopIteration, ProcessLookupError):
                pass
    try:
        if a.host_backlog:
            host_settings={'netdev_max_backlog_original':int(backlog_path.read_text()),'netdev_max_backlog_applied':a.host_backlog,'restored':False}
            (out/'host-settings.json').write_text(json.dumps(host_settings,indent=2)+'\n')
            backlog_path.write_text(str(a.host_backlog))
        for n in (c,s):
            run('ip','netns','add',n); namespaces.append(n)
            ns(n,'ip','link','set','lo','up')
            ns(n,'sysctl','-qw','net.ipv4.ip_local_port_range=1024 65535')
            if a.tcp_buffer_mib:
                budget = str(a.tcp_buffer_mib<<20)
                ns(n,'sysctl','-qw',f'net.ipv4.tcp_rmem=4096 131072 {budget}',f'net.ipv4.tcp_wmem=4096 16384 {budget}')
        if a.enterprise:
            ns(s,'iptables','-A','INPUT','-p','udp','--dport','31111','-j','DROP')
        host_c,host_s=f'sc{os.getpid()}',f'ss{os.getpid()}'
        if a.bridge:
            router=f'spq-r-{os.getpid()}'
            run('ip','netns','add',router);namespaces.append(router)
            host_rc,host_rs=f'rc{os.getpid()}',f'rs{os.getpid()}'
            run('ip','link','add',host_c,'type','veth','peer','name',host_rc)
            run('ip','link','add',host_s,'type','veth','peer','name',host_rs)
            for old,new in ((host_rc,'spq-rc'),(host_rs,'spq-rs')):
                run('ip','link','set',old,'netns',router)
                ns(router,'ip','link','set',old,'name',new)
            ns(router,'ip','link','add','br0','type','bridge')
            ns(router,'ip','link','set','br0','up')
            for dev in ('spq-rc','spq-rs'):
                ns(router,'ip','link','set',dev,'master','br0')
                ns(router,'ip','link','set',dev,'up')
        else:
            run('ip','link','add',host_c,'type','veth','peer','name',host_s)
        run('ip','link','set',host_c,'netns',c)
        run('ip','link','set',host_s,'netns',s)
        ns(c,'ip','link','set',host_c,'name','spq-c')
        ns(s,'ip','link','set',host_s,'name','spq-s')
        for n, iface, addr, mac in ((c,'spq-c','198.18.0.1/24','02:00:00:00:00:01'),(s,'spq-s','198.18.0.2/24','02:00:00:00:00:02')):
            ns(n,'ip','link','set',iface,'address',mac)
            ns(n,'ip','link','set',iface,'mtu',str(a.mtu))
            ns(n,'ip','addr','add',addr,'dev',iface)
            ns(n,'ip','link','set',iface,'up')
            down = (n==c) if a.bridge else (n==s)
            rate = a.down_rate_mbit if down and a.down_rate_mbit is not None else a.rate_mbit
            if a.loss or a.burst_loss or a.delay_ms or rate or a.reorder or epochs:
                # netem's delay queue includes packets "in propagation". Size
                # for small ACK frames as well as MTU-sized data; using 1500
                # imposes an artificial reverse-path PPS cap on asymmetric links.
                queue = a.queue_packets or (max(32,int(rate*1e6/8*((a.delay_ms+4*a.jitter_ms)/1000+.025)/64)) if rate else 10000)
                shaping_ns=n;shaping_if=iface
                if a.bridge: shaping_ns=router;shaping_if='spq-rc' if n==c else 'spq-rs'
                args = ['tc','qdisc','add','dev',shaping_if,'root','netem','limit',str(queue)]
                if a.delay_ms: args += ['delay',f'{a.delay_ms}ms']
                if a.jitter_ms: args += [f'{a.jitter_ms}ms','distribution','normal']
                if a.loss: args += ['loss','random',f'{a.loss}%']
                if a.burst_loss: args += ['loss','gemodel',*[f'{x}%' for x in a.burst_loss]]
                if a.reorder: args += ['reorder',f'{a.reorder}%']
                if rate: args += ['rate',f'{rate}mbit']
                if a.seed is not None: args += ['seed',str(a.seed)]
                ns(shaping_ns,*args)
                shaping.append((shaping_ns,shaping_if,down))
        # start_epochs: Start the configured fault schedule relative to this workload's actual start
        # time.
        def start_epochs(workload):
            if not epochs:return
            # apply_epochs: Apply each owned link change at its recorded relative time; cancellation
            # prevents teardown races.
            def apply_epochs():
                started=time.monotonic()
                values={'rate_mbit':a.rate_mbit,'down_rate_mbit':a.down_rate_mbit,'delay_ms':a.delay_ms,'jitter_ms':a.jitter_ms,'loss':a.loss,'reorder':a.reorder,'queue_packets':a.queue_packets}
                try:
                    for event in epochs:
                        if schedule_stop.wait(max(0,event['at']-(time.monotonic()-started))):return
                        values.update({k:v for k,v in event.items() if k!='at'})
                        for n,iface,down in shaping:
                            rate=values['down_rate_mbit'] if down and values['down_rate_mbit'] is not None else values['rate_mbit']
                            queue=int(values['queue_packets'] or (max(32,int(rate*1e6/8*((values['delay_ms']+4*values['jitter_ms'])/1000+.025)/64)) if rate else 10000))
                            args=['tc','qdisc','change','dev',iface,'root','netem','limit',str(queue)]
                            if values['delay_ms']:args+=['delay',f"{values['delay_ms']}ms"]
                            if values['jitter_ms']:args+=[f"{values['jitter_ms']}ms",'distribution','normal']
                            if values['loss']:args+=['loss','random',f"{values['loss']}%"]
                            if values['reorder']:args+=['reorder',f"{values['reorder']}%"]
                            if rate:args+=['rate',f'{rate}mbit']
                            if a.seed is not None:args+=['seed',str(a.seed)]
                            ns(n,*args)
                        with (out/(workload+'-epochs.jsonl')).open('a') as log: log.write(json.dumps({'elapsed':time.monotonic()-started,'parameters':dict(values)})+'\n')
                except Exception as err:schedule_errors.append(str(err))
            thread=threading.Thread(target=apply_epochs,daemon=True);schedule_threads.append(thread);thread.start()
        base = 'log: {level: error}\ntransport:\n  protocol: kcp\n  conn: '+str(a.sessions)+'\n  kcp: {key: benchmark-only-key, mode: fast3, rcvwnd: 4096, sndwnd: 4096}\n'
        client = 'role: client\n'+base+'network:\n  interface: spq-c\n  ipv4: {addr: "198.18.0.1:0", router_mac: "02:00:00:00:00:02"}\nserver: {addr: "198.18.0.2:29999"}\nforward:\n'
        for i in range(8): client += f'  - {{listen: "127.0.0.1:{28080+i}", target: "127.0.0.{i+1}:18080", protocol: tcp}}\n'
        if a.iperf and not a.enterprise: client += '  - {listen: "127.0.0.1:28092", target: "127.0.0.1:18083", protocol: tcp}\n  - {listen: "127.0.0.1:28093", target: "127.0.0.1:18084", protocol: tcp}\n'
        server = 'role: server\n'+base+'network:\n  interface: spq-s\n  ipv4: {addr: "198.18.0.2:29999", router_mac: "02:00:00:00:00:01"}\nlisten: {addr: ":29999"}\n'
        if a.enterprise:
            # New schema, filled in alongside the application implementation.
            client = f'peers:\n  remote:\n    address: 198.18.0.2:29999\n    key: benchmark-only-key\n    sessions: {a.sessions}\n    max_sessions: {a.max_sessions}\n    network: {{interface: spq-c, ipv4: {{addr: "198.18.0.1:0", router_mac: "02:00:00:00:00:02"}}}}\nforwards:\n'
            for i in range(8): client += f'  - {{listen: "127.0.0.1:{28080+i}", peer: remote, target: "127.0.0.{i+1}:18080"}}\n'
            server = f'listeners:\n  - address: 198.18.0.2:29999\n    sessions: {a.sessions}\n    max_sessions: {a.max_sessions}\n    key: benchmark-only-key\n    network: {{interface: spq-s, ipv4: {{addr: "198.18.0.2:29999", router_mac: "02:00:00:00:00:01"}}}}\n'
            if a.pinned_lanes:
                # Four carriers retain the same raw envelope and receiver,
                # while one-slot peers make workload placement deterministic.
                # Ordinary pool tests remain available to test admission itself.
                peers = ''
                for i in range(a.sessions):
                    name = 'remote' if i == 0 else f'remote_lane_{i}'
                    source = f'    source_ports: [{a.source_ports[i]}]\n' if a.source_ports is not None else ''
                    peers += f'  {name}:\n    address: 198.18.0.2:29999\n{source}    key: benchmark-only-key\n    sessions: 1\n    max_sessions: 1\n    network: {{interface: spq-c, ipv4: {{addr: "198.18.0.1:0", router_mac: "02:00:00:00:00:02"}}}}\n'
                client = 'peers:\n' + peers + 'forwards:' + client.split('forwards:', 1)[1]
            if a.source_ports is not None and not a.pinned_lanes:
                # Keep initial tuple values fixed for repeated comparisons;
                # worker distribution must still be measured in each namespace.
                # Recovery may reserve a fresh replacement source.
                client=client.replace('    address:', '    source_ports: '+json.dumps(a.source_ports)+'\n    address:',1)
            if a.path_recovery:
                recovery_fields='{enabled: true'+(', preserve_connections: true' if a.preserve_connections else '')+'}'
                client=client.replace('    address:', '    path_recovery: '+recovery_fields+'\n    address:',-1 if a.pinned_lanes else 1)
            client += 'metrics: 127.0.0.1:29090\n'
            server += 'metrics: 127.0.0.1:29090\n'
            kcp_options=json.dumps({'block':a.block,'dshard':a.fec[0],'pshard':a.fec[1],**kcp_overrides})
            client = client.replace('    key: benchmark-only-key\n',f'    key: benchmark-only-key\n    kcp: {kcp_options}\n')
            server = server.replace('    key: benchmark-only-key\n',f'    key: benchmark-only-key\n    kcp: {kcp_options}\n')
            if a.profile:
                client += 'profiling: true\n'
                server += 'profiling: true\n'
            if a.debug:
                client += f'log: {{level: debug, format: json, interval: 1s, flow_sample: {a.flow_sample}}}\n'
                server += f'log: {{level: debug, format: json, interval: 1s, flow_sample: {a.flow_sample}}}\n'
            if a.ipv6:
                for namespace,iface,address in ((c,'spq-c','fd42:198:18::1/64'),(s,'spq-s','fd42:198:18::2/64')):
                    ns(namespace,'ip','-6','addr','add',address,'dev',iface,'nodad')
                client=client.replace('198.18.0.2:29999','[fd42:198:18::2]:29999').replace('ipv4:','ipv6:').replace('198.18.0.1:0','[fd42:198:18::1]:0')
                server=server.replace('198.18.0.2:29999','[fd42:198:18::2]:29999').replace('ipv4:','ipv6:')
                client=client.replace('address: [fd42:198:18::2]:29999','address: "[fd42:198:18::2]:29999"')
                server=server.replace('address: [fd42:198:18::2]:29999','address: "[fd42:198:18::2]:29999"')
            if a.adaptive=='off':
                client = client.replace('    address:','    adaptive: false\n    address:')
                server = server.replace('  - address:','  - adaptive: false\n    address:')
            if a.functional:
                ns(s,'ip','addr','add','198.18.0.3/24','dev','spq-s')
                server = server.replace('metrics:', '  - address: 198.18.0.3:29999\n    key: benchmark-only-key\n    network: {interface: spq-s, ipv4: {addr: "198.18.0.3:29999", router_mac: "02:00:00:00:00:01"}}\nmetrics:')
                peer2 = f'  other:\n    address: 198.18.0.3:29999\n    key: benchmark-only-key\n    sessions: {a.sessions}\n    max_sessions: {a.max_sessions}\n    network: {{interface: spq-c, ipv4: {{addr: "198.18.0.1:0", router_mac: "02:00:00:00:00:02"}}}}\n'
                client = client.replace('forwards:', peer2+'forwards:')
                client = client.replace('metrics:', '  - {listen: "127.0.0.1:28088", peer: other, target: "127.0.0.1:18080"}\n  - {listen: "127.0.0.1:28090", peer: remote, target: "127.0.0.1:18081", protocol: udp}\n  - {listen: "127.0.0.1:28091", peer: remote, target: "127.0.0.1:18082"}\nmetrics:')
            if a.iperf:
                forwards = ''
                for i in range(a.sessions if a.pinned_lanes else 1):
                    name = 'remote' if i == 0 else f'remote_lane_{i}'
                    for offset in (0, 1):
                        forwards += f'  - {{listen: "127.0.0.1:{28092+2*i+offset}", peer: {name}, target: "127.0.0.1:{18083+2*i+offset}"}}\n'
                client = client.replace('metrics:', forwards + 'metrics:')
            if a.backend=='pcap':
                client=client.replace('network: {interface:', 'network: {backend: pcap, interface:')
                server=server.replace('network: {interface:', 'network: {backend: pcap, interface:')
        if a.shared_source:
            if not a.enterprise:
                p.error('--shared-source requires --enterprise')
            client = client.replace('    address:', '    shared_source: true\n    address:')
            server = server.replace('  - address:', '  - shared_source: true\n    address:')
        if a.enterprise:
            if a.client_memory_mib:client+=f'limits: {{memory_mib: {a.client_memory_mib}}}\n'
            if a.server_memory_mib:server+=f'limits: {{memory_mib: {a.server_memory_mib}}}\n'
            if a.conversation_listener and not a.shared_source:server=server.replace('  - address:', '  - shared_source: true\n    address:')
            if a.packet_workers is not None:server=server.replace('    address:', f'    packet_workers: {a.packet_workers}\n    address:',1)
            client=client.replace('network: {', f'network: {{tcp: {{local_flag: [{a.client_flag}], remote_flag: [{a.server_flag}]}}, ',-1 if a.pinned_lanes else 1)
            server=server.replace('network: {', f'network: {{tcp: {{local_flag: [{a.server_flag}], remote_flag: [{a.client_flag}]}}, ',1)
        (out/'client.yaml').write_text(client); (out/'server.yaml').write_text(server)
        if a.socket_buffer_mib:
            client = client.replace('network: {',f'network: {{pcap: {{sockbuf: {a.socket_buffer_mib<<20}}}, ',1)
            server = server.replace('network: {',f'network: {{pcap: {{sockbuf: {a.socket_buffer_mib<<20}}}, ',1)
            (out/'client.yaml').write_text(client); (out/'server.yaml').write_text(server)
        if not a.enterprise:
            for args in (['-t','raw','-A','PREROUTING','-p','tcp','--dport','29999','-j','NOTRACK'], ['-t','raw','-A','OUTPUT','-p','tcp','--sport','29999','-j','NOTRACK'], ['-t','mangle','-A','OUTPUT','-p','tcp','--sport','29999','--tcp-flags','RST','RST','-j','DROP']): ns(s,'iptables',*args)
        spawn(s,'target',str(ROOT/'build/spq-bench'),'-mode','hold-serve' if a.hold else 'serve','-addr',':18080')
        binary = str((ROOT/a.binary).resolve())
        if a.enterprise:
            for namespace,label in ((c,'client'),(s,'server')):
                checked=ns(namespace,binary,'config','validate','-c',str(out/(label+'.yaml')),'--json')
                (out/(label+'-validation.json')).write_text(checked.stdout)
        sp = spawn(s,'server',binary,'run','-c',str(out/'server.yaml'))
        cp = spawn(c,'client',binary,'run','-c',str(out/'client.yaml'))
        time.sleep(2)
        if sp.poll() is not None or cp.poll() is not None: raise RuntimeError('tunnel exited; see logs')
        running_sha = hashlib.sha256(Path(f'/proc/{sp.pid}/exe').read_bytes()).hexdigest()
        if running_sha!=hashlib.sha256(Path(f'/proc/{cp.pid}/exe').read_bytes()).hexdigest(): raise RuntimeError('server/client binaries changed during startup')
        if a.capture:
            spawn(s,'capture','tcpdump','-i','spq-s','-n','-w',str(out/'wire.pcap'),'-c','128','tcp','port','29999')
            time.sleep(.25)
        # Force a full integrity check before a performance claim.
        caps = [x for x in (a.rate_mbit,a.down_rate_mbit) if x]
        verify_size = 1048576 if caps and min(caps)<=100 else 16777216
        if a.cold_read_bytes is not None:
            verify_size = a.cold_read_bytes
        # Capture the first transfer before any performance warm-up. A good
        # steady-state rate must not conceal slow startup on new carriers.
        verify_started = time.monotonic()
        result = ns(c,str(ROOT/'build/spq-bench'),'-mode','verify','-addr','127.0.0.1:28080','-verify-size',str(verify_size))
        first_transfer = json.loads(result.stdout)
        first_transfer['cold_transfer_seconds'] = time.monotonic() - verify_started
        reports = [first_transfer]
        if a.restart:
            expected_killed.add(sp.pid);sp.kill();sp.wait()
            ns(s,sys.executable,'-c','from pathlib import Path; assert list(Path("/run/super-paqet").glob("*.json")), "missing crash journal"')
            sp = spawn(s,'restarted-server',binary,'run','-c',str(out/'server.yaml'))
            time.sleep(2)
            started = time.monotonic();attempts=0
            while True:
                attempts+=1
                try:
                    result=ns(c,str(ROOT/'build/spq-bench'),'-mode','verify','-addr','127.0.0.1:28080')
                    reports.append({'server_crash_recovered_seconds':time.monotonic()-started,'attempts':attempts,'verification':json.loads(result.stdout)})
                    break
                except subprocess.CalledProcessError:
                    if time.monotonic()-started>90: raise RuntimeError('server restart did not recover')
                    time.sleep(.25)
        if a.functional:
            spawn(s,'udp-target',str(ROOT/'build/spq-bench'),'-mode','udp-serve','-addr',':18081')
            spawn(s,'half-target',str(ROOT/'build/spq-bench'),'-mode','half-serve','-addr',':18082')
            second = client.replace('2808','3808').replace('2809','3809').replace('29090','39090')
            (out/'second-client.yaml').write_text(second)
            spawn(c,'second-client',binary,'run','-c',str(out/'second-client.yaml'))
            time.sleep(2)
            for mode, address in [('verify','127.0.0.1:28088'),('verify','127.0.0.1:38080'),('udp-verify','127.0.0.1:28090'),('half-verify','127.0.0.1:28091')]:
                result = ns(c,str(ROOT/'build/spq-bench'),'-mode',mode,'-addr',address)
                reports.append(json.loads(result.stdout))
            result = ns(c,binary,'ping','-c',str(out/'client.yaml'),'--peer','remote')
            reports.append({'ping':result.stdout.strip()})
        modes = (['http','bulk'] if a.mode=='both' else [a.mode]) if not a.hold else ['hold']
        if a.iperf: modes = []
        for mode in modes:
            sample()
            cpu_before = {name:v.get('cpu_seconds',0) for name,v in peaks.items()}
            args = [str(ROOT/'build/spq-bench'),'-mode',mode,'-duration',f'{a.duration}s','-workers',str(a.workers),'-addr','127.0.0.1:28080']
            if a.hold: args[-1] = ','.join(f'127.0.0.1:{28080+i}' for i in range(8)); args += ['-connections',str(a.hold)]
            proc = spawn(c,'load-'+mode,*args)
            start_epochs(mode)
            profiles = []
            if a.profile and a.enterprise:
                for name, namespace in (('client',c),('server',s)):
                    profile_path = str(out/(name+'-'+mode+'.pprof'))
                    profiles.append(spawn(namespace, 'profile-'+name, sys.executable, '-c', f'import urllib.request; data=urllib.request.urlopen("http://127.0.0.1:29090/debug/pprof/profile?seconds={max(1,min(10,a.duration-1))}",timeout=30).read(); open({profile_path!r},"wb").write(data)'))
            deadline = time.monotonic()+max(a.ramp_timeout,a.duration+60)
            hold_sampled = False
            mixed_procs = []
            while proc.poll() is None:
                sample()
                if a.hold and a.enterprise and not hold_sampled and 'established' in (out/('load-'+mode+'.log')).read_text():
                    for name, namespace in (('client',c),('server',s)):
                        metrics = ns(namespace, sys.executable, '-c', 'import urllib.request; print(urllib.request.urlopen("http://127.0.0.1:29090/metrics",timeout=10).read().decode())').stdout
                        (out/(name+'-hold-established-metrics.txt')).write_text(metrics)
                    hold_sampled = True
                    if a.mixed:
                        for workload in ('http','bulk'):
                            mixed_procs.append(spawn(c,'mixed-'+workload,str(ROOT/'build/spq-bench'),'-mode',workload,'-addr','127.0.0.1:28080','-workers','8','-duration',f'{max(1,a.duration-2)}s'))
                if time.monotonic() > deadline: raise TimeoutError('benchmark timed out')
                time.sleep(.25)
            if proc.returncode: raise RuntimeError('load generator failed')
            for profiler in profiles:
                profiler.wait(timeout=30)
            workload_reports = [json.loads(line) for line in (out/('load-'+mode+'.log')).read_text().splitlines()]
            sample()
            for result in workload_reports:
                if 'seconds' in result: result['tunnel_cpu_cores']={name:round((peaks[name]['cpu_seconds']-cpu_before.get(name,0))/result['seconds'],3) for name in ('client','server') if name in peaks}
            reports += workload_reports
            for load in mixed_procs:
                load.wait(timeout=30)
            if mixed_procs:
                for workload in ('http','bulk'):
                    reports.append({'mixed_workload':workload,**json.loads((out/('mixed-'+workload+'.log')).read_text())})
            if a.enterprise:
                for name, namespace in (('client',c),('server',s)):
                    metrics = ns(namespace, sys.executable, '-c', 'import urllib.request; print(urllib.request.urlopen("http://127.0.0.1:29090/metrics",timeout=5).read().decode())').stdout
                    (out/(name+'-'+mode+'-metrics.txt')).write_text(metrics)
        if a.iperf:
            iperf = str(ROOT/'build/iperf-local/bin/iperf3')
            cases = [(name,flags,False) for name,flags in [('upload',[]),('download',['-R']),('bidirectional',[])] if name in a.iperf_directions]
            if a.direct_iperf:
                cases += [('direct-upload',[],True),('direct-download',['-R'],True),('direct-udp',['-u','-b',f'{a.rate_mbit or 1000}M','-l','1400'],True)]
            for name,flags,direct in cases:
                idle_started = time.monotonic()
                if a.enterprise:
                    # A finished load generator does not imply its buffered
                    # tunnel data has drained, especially on asymmetric links.
                    while True:
                        idle = True
                        for side, namespace in (('client',c),('server',s)):
                            metrics = ns(namespace, sys.executable, '-c', 'import urllib.request; print(urllib.request.urlopen("http://127.0.0.1:29090/metrics",timeout=10).read().decode())').stdout
                            (out/(side+'-iperf-'+name+'-ready-metrics.txt')).write_text(metrics)
                            values = [int(line.rsplit(' ',1)[1]) for line in metrics.splitlines() if line.startswith('super_paqet_session_pending{')]
                            active = next(int(line.split()[1]) for line in metrics.splitlines() if line.startswith('super_paqet_active_connections '))
                            idle = idle and active==0 and all(value<=2 for value in values)
                        if idle: break
                        if time.monotonic()-idle_started>30: raise TimeoutError('previous tunnel workload did not drain')
                        time.sleep(.25)
                idle_wait = time.monotonic()-idle_started
                # iperf3 --bidir chooses roles by accept order, which independent
                # proxied TCP opens cannot guarantee. Two concurrent one-way
                # tests on separate targets identify direction unambiguously.
                specs = [(name,28092,18083,flags)]
                if name=='bidirectional':
                    specs=[(name+'-upload',28092,18083,[]),(name+'-download',28093,18084,['-R'])]
                if a.pinned_lanes:
                    specs = [(label+f'-lane-{i}', listen_port+2*i, target_port+2*i, flags)
                             for i in range(a.sessions) for label, listen_port, target_port, flags in specs]
                iperf_servers=[spawn(s,'iperf-server-'+label,iperf,'-s','-1','-p',str(target_port)) for label,_,target_port,_ in specs]
                time.sleep(.3)
                sample()
                cpu_before = {name:v.get('cpu_seconds',0) for name,v in peaks.items()}
                cpu_started=time.monotonic()
                # TCP bitrate is per parallel stream in iperf3. Equal offered
                # load complements the unlimited capacity tests: a faster
                # implementation must not be judged at a different demand level
                # when comparing latency beside bulk traffic.
                offered_rate = ['-b', str(max(1, a.iperf_rate_mbit*1000000//a.workers))] if a.iperf_rate_mbit else []
                streams_per_lane = a.workers//a.sessions if a.pinned_lanes else a.workers
                iperf_clients=[spawn(c,'iperf-'+label,iperf,'-c',('fd42:198:18::2' if a.ipv6 else '198.18.0.2') if direct else '127.0.0.1','-p',str(target_port if direct else listen_port),'-P',str(streams_per_lane),'-t',str(a.duration),'-O',str(a.warmup),'-J',*offered_rate,*test_flags) for label,listen_port,target_port,test_flags in specs]
                http_churn=None
                if name=='bidirectional' and a.duplex_http:
                    http_timing=['-duration',f'{a.duration}s','-warmup',f'{a.warmup}s'] if a.duplex_http_steady else ['-duration',f'{a.duration+a.warmup}s']
                    http_churn=spawn(c,'duplex-http',str(ROOT/'build/spq-bench'),'-mode','http-churn','-addr','127.0.0.1:28080','-workers','4','-request-gap',f'{a.duplex_http_gap_ms}ms',*http_timing)
                start_epochs(name)
                profiles = []
                if a.profile and a.enterprise and not direct:
                    for side, namespace in (('client',c),('server',s)):
                        profile_path = str(out/(side+'-iperf-'+name+'.pprof'))
                        profiles.append(spawn(namespace, 'profile-'+side, sys.executable, '-c', f'import urllib.request; data=urllib.request.urlopen("http://127.0.0.1:29090/debug/pprof/profile?seconds={max(1,min(10,a.duration-1))}",timeout=30).read(); open({profile_path!r},"wb").write(data)'))
                deadline = time.monotonic()+a.duration+a.warmup+60
                snapshot_at = time.monotonic()+a.warmup+a.duration/2
                sampled = False
                while any(proc.poll() is None for proc in iperf_clients):
                    sample()
                    if a.enterprise and time.monotonic()>snapshot_at and not sampled:
                        for side, namespace in (('client',c),('server',s)):
                            metrics = ns(namespace, sys.executable, '-c', 'import urllib.request; print(urllib.request.urlopen("http://127.0.0.1:29090/metrics",timeout=10).read().decode())').stdout
                            (out/(side+'-iperf-'+name+'-metrics.txt')).write_text(metrics)
                        sampled = True
                    if time.monotonic()>deadline: raise TimeoutError('iperf3 timed out')
                    time.sleep(.25)
                for proc, (label, _, _, _) in zip(iperf_clients, specs):
                    if proc.returncode:
                        raise RuntimeError(f'iperf3 client {label} exited {proc.returncode}; inspect iperf-{label}.log')
                iperf_reports=[json.loads((out/('iperf-'+label+'.log')).read_text()) for label,_,_,_ in specs]
                for profiler in profiles:
                    if profiler.wait(timeout=30): raise RuntimeError('CPU profile collection failed')
                for proc,report in zip(iperf_clients,iperf_reports):
                    if proc.returncode or 'error' in report: raise RuntimeError(f'iperf3 failed: {report.get("error")}')
                    if name=='bidirectional' and any(stream['receiver']['bytes']==0 for stream in report['end']['streams']):
                        raise RuntimeError('duplex stream carried no measured bytes')
                for iperf_server in iperf_servers:
                    if iperf_server.wait(timeout=30): raise RuntimeError('iperf3 server failed; inspect server log')
                if http_churn is not None:
                    if http_churn.wait(timeout=30):raise RuntimeError('duplex HTTP generator failed')
                    http_result=json.loads((out/'duplex-http.log').read_text())
                    reports.append({'mixed_workload':'http-churn',**http_result})
                    if http_result['errors']:raise RuntimeError('duplex HTTP forwarding errors')
                sample()
                observed_seconds=time.monotonic()-cpu_started
                cpu = {side:round((peaks[side]['cpu_seconds']-cpu_before.get(side,0))/observed_seconds,3) for side in ('client','server') if side in peaks}
                report=iperf_reports[0]
                end=report['end']
                if a.pinned_lanes:
                    def aggregate(direction):
                        # Every lane has its own unambiguous sender/receiver
                        # pair. Sum received payload rates, retaining original
                        # reports so omissions/startup boundaries stay visible.
                        selected = [r['end'] for r, spec in zip(iperf_reports, specs)
                                    if name != 'bidirectional' or ('-R' in spec[3]) == direction]
                        combined = dict(selected[0])
                        for field in ('sum_sent', 'sum_received'):
                            combined[field] = dict(selected[0][field])
                            for key in ('bytes', 'bits_per_second'):
                                combined[field][key] = sum(e[field][key] for e in selected)
                        combined['streams'] = [stream for e in selected for stream in e['streams']]
                        return combined
                    end = aggregate(False)
                    if name == 'bidirectional':
                        reverse = aggregate(True)
                        end.update({'sum_sent_bidir_reverse': reverse['sum_sent'], 'sum_received_bidir_reverse': reverse['sum_received']})
                elif name=='bidirectional':
                    end={**end,'sum_sent_bidir_reverse':iperf_reports[1]['end']['sum_sent'],'sum_received_bidir_reverse':iperf_reports[1]['end']['sum_received']}
                row={'iperf':name,'end':end,'tunnel_cpu_cores':cpu,'idle_wait_seconds':round(idle_wait,3)}
                if name=='bidirectional':row.update({'method':'concurrent_unidirectional','direction_reports':iperf_reports})
                if a.pinned_lanes:row.update({'pinned_lanes':a.sessions,'lane_reports':iperf_reports})
                reports.append(row)
        if a.capture:
            decoded = ns(s,'tcpdump','-n','-vv','-r',str(out/'wire.pcap')).stdout
            (out/'wire.txt').write_text(decoded)
            rules = ns(s,'iptables-save','-c').stdout
            (out/'firewall-during.txt').write_text(rules)
        if schedule_errors:raise RuntimeError('link schedule failed: '+str(schedule_errors))
        report = {'parameters':vars(a),'results':reports,'processes':peaks,'kernel':os.uname().release,'binary_sha256':running_sha,'benchmark_helper_sha256':hashlib.sha256((ROOT/'build/spq-bench').read_bytes()).hexdigest()}
        for n in namespaces:
            (out/(n.split('-')[1]+'-qdisc.json')).write_text(ns(n,'tc','-s','-j','qdisc','show').stdout)
        (out/'results.json').write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps(report,indent=2),flush=True)
        if any(row.get('errors',0) for row in reports):raise RuntimeError('workload reported errors; inspect results.json')
    finally:
        if host_settings:
            current=int(backlog_path.read_text())
            original=host_settings['netdev_max_backlog_original']
            if current in (a.host_backlog,original):
                backlog_path.write_text(str(original))
                host_settings['restored']=int(backlog_path.read_text())==original
            else:
                host_settings['concurrent_value']=current
            (out/'host-settings.json').write_text(json.dumps(host_settings,indent=2)+'\n')
        schedule_stop.set()
        for thread in schedule_threads:thread.join(timeout=5)
        for proc in reversed(procs):
            if proc.poll() is None: proc.terminate()
        for proc in reversed(procs):
            try: proc.wait(timeout=8)
            except subprocess.TimeoutExpired: proc.kill(); proc.wait()
        for f in files: f.close()
        cleanup = {'process_exit_codes':[p.returncode for p in procs], 'firewall_clean':True}
        try:
            if a.enterprise:
                for n in namespaces:
                    rules = ns(n,'iptables-save').stdout
                    (out/(n.split('-')[1]+'-firewall-after.txt')).write_text(rules)
                    if 'SPQ_' in rules: cleanup['firewall_clean'] = False
                if s in namespaces:
                    ns(s,'iptables','-C','INPUT','-p','udp','--dport','31111','-j','DROP')
                    cleanup['unrelated_rule_preserved'] = True
        except subprocess.CalledProcessError as err:
            cleanup['firewall_clean'] = False
            cleanup['inspection_error'] = str(err)
        finally:
            (out/'cleanup.json').write_text(json.dumps(cleanup,indent=2)+'\n')
            for n in reversed(namespaces): subprocess.run(['ip','netns','delete',n],check=False)
        if 'SUDO_UID' in os.environ:
            for path in [out,*out.rglob('*')]: os.chown(path,int(os.environ['SUDO_UID']),int(os.environ['SUDO_GID']))
        if not cleanup['firewall_clean']: raise RuntimeError('owned firewall rules leaked; see cleanup.json')
        if host_settings and not host_settings['restored']:raise RuntimeError('host backlog changed concurrently; inspect host-settings.json')
        if any(p.returncode!=0 and p.pid not in expected_killed for p in tunnel_procs): raise RuntimeError('tunnel exited abnormally; see cleanup.json and logs')

if __name__ == '__main__': main()
