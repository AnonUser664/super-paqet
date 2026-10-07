#!/usr/bin/env python3
"""Exercise verified tuple recovery on owned Linux namespaces and a middle bridge.

Drops occur before the backend packet socket: INPUT firewall rules would be an
invalid emulator because AF_PACKET receives packets before kernel TCP filtering.
Only fixture namespaces/interfaces/rules are changed; customer hosts are unused.
"""
import argparse, concurrent.futures, hashlib, json, os, pathlib, re, signal, socket, struct, subprocess, sys, time

from check_migration_wire import check as check_wire

ROOT = pathlib.Path(__file__).resolve().parents[1]


def main():
    """Own fixture startup, fault injection, verification and bounded teardown."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True)
    parser.add_argument('--case', choices=['tuple', 'whole-peer', 'one-lane', 'all-tuples', 'reverse-tuple', 'established', 'reverse-established', 'short-loss', 'repeat-tuple', 'repeat-established', 'early-stall', 'all-established'], default='tuple')
    parser.add_argument('--server-binary', help='optional older backend executable for wire compatibility qualification')
    parser.add_argument('--shared-source', action='store_true', help='qualify legacy pool recovery instead of independent carrier recovery')
    parser.add_argument('--preserve-connections', action='store_true', help='require continuity of established streams during source migration')
    parser.add_argument('--sequenced-payloads', action='store_true', help='prefix every held exchange with its unique sequence number')
    parser.add_argument('--held-payload-bytes', type=int, default=0, help='larger integrity-checked payloads on held streams')
    parser.add_argument('--packet-workers', type=int, default=2, help='backend fanout workers, including cross-worker migration')
    parser.add_argument('--stall-seconds', type=int, default=15, help='qualifying delivery stall threshold; retry interval stays 15 seconds')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    if args.stall_seconds <= 0:
        parser.error('--stall-seconds must be positive')
    if os.geteuid() != 0:
        parser.error('requires root for disposable namespaces')
    binary = pathlib.Path(args.binary).resolve()
    out = pathlib.Path(args.output).resolve(); out.mkdir(parents=True, exist_ok=True)
    suffix = str(os.getpid()); client, router, server = [f'spq-rec-{x}-{suffix}' for x in ('c', 'r', 's')]
    namespaces, processes, handles, rows, events = [], [], [], [], []
    config = out / 'client.json'; env = os.environ.copy(); repeated = False

    def run(*command):
        """Bound checked host commands; no global network settings are modified."""
        return subprocess.run(command, check=True, capture_output=True, text=True, timeout=15)

    def ns(name, *command):
        """Restrict network operations to this fixture namespace."""
        return run('ip', 'netns', 'exec', name, *command)

    def spawn(name, label, *command):
        """Track each child and its log so failure still releases owned resources."""
        log = (out / (label + '.log')).open('w'); handles.append(log)
        process = subprocess.Popen(['ip', 'netns', 'exec', name, *command], env=env, stdout=log, stderr=subprocess.STDOUT)
        processes.append(process); return process

    def metrics():
        """Read loopback telemetry from the isolated client, never production."""
        return ns(client, sys.executable, '-c', 'import urllib.request; print(urllib.request.urlopen("http://127.0.0.1:29090/metrics",timeout=2).read().decode())').stdout

    # Requests validate echoed bytes; failed new connections during injection are
    # expected. A separate healthy peer is continuously checked for collateral loss.
    requester = r'''import concurrent.futures,socket,time,json
end=time.monotonic()+55;start=time.monotonic()
def worker(peer,port,index):
 while time.monotonic()<end:
  at=time.monotonic()-start
  try:
   with socket.create_connection(('127.0.0.1',port),2) as s:
    s.settimeout(7);payload=('integrity-'+str(index)+'\n').encode();s.sendall(payload);data=b''
    while len(data)<len(payload):
     b=s.recv(len(payload)-len(data))
     if not b:raise RuntimeError('unexpected EOF')
     data+=b
    if data!=payload:raise RuntimeError('corrupted echo')
   result={'peer':peer,'at':at,'done':time.monotonic()-start,'ok':True}
  except Exception as e:result={'peer':peer,'at':at,'done':time.monotonic()-start,'ok':False,'error':str(e)}
  print(json.dumps(result),flush=True);time.sleep(.15)
with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:
 list(pool.map(lambda x:worker(*x),[('broken',28080,i) for i in range(4)]+[('healthy',28081,4)]))
'''
    if args.case in ('established','reverse-established','repeat-established','early-stall','all-established'):
        # Four connections are opened once and kept for the entire workload.
        # No new broken-peer openings can supply recovery-failure evidence.
        requester = r'''import concurrent.futures,socket,time,json
start=time.monotonic();end=start+55
def held(index):
 time.sleep(index*.25)
 try:
  with socket.create_connection(('127.0.0.1',28080),2) as s:
   s.settimeout(25)
   while time.monotonic()<end:
    at=time.monotonic()-start;payload=('held-'+str(index)+'\n').encode();s.sendall(payload);data=b''
    while len(data)<len(payload):
     part=s.recv(len(payload)-len(data))
     if not part:raise RuntimeError('held connection EOF')
     data+=part
    if data!=payload:raise RuntimeError('held payload corruption')
    print(json.dumps({'peer':'broken','held':index,'at':at,'ok':True}),flush=True);time.sleep(.15)
 except Exception as exc:print(json.dumps({'peer':'broken','held':index,'at':time.monotonic()-start,'ok':False,'error':str(exc)}),flush=True)
def healthy():
 while time.monotonic()<end:
  at=time.monotonic()-start
  try:
   with socket.create_connection(('127.0.0.1',28081),2) as s:
    s.settimeout(7);s.sendall(b'healthy');data=b''
    while len(data)<7:
     part=s.recv(7-len(data))
     if not part:raise RuntimeError('healthy EOF')
     data+=part
    if data!=b'healthy':raise RuntimeError('healthy corruption')
   row={'peer':'healthy','at':at,'ok':True}
  except Exception as exc:row={'peer':'healthy','at':at,'ok':False,'error':str(exc)}
  print(json.dumps(row),flush=True);time.sleep(.15)
with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:
 futures=[pool.submit(held,index) for index in range(4)]+[pool.submit(healthy)]
 for future in futures:future.result()
'''
    duration = 90 if args.case == 'whole-peer' else 55
    if args.case == 'whole-peer':
        requester = requester.replace('+55;', '+90;')
    if args.held_payload_bytes:
        if not 1 <= args.held_payload_bytes <= 1048576: parser.error('held-payload-bytes must be 1..1048576')
        requester=requester.replace("('held-'+str(index)+'\\n').encode()", "((b'held-integrity-'+bytes([index]))*"+str((args.held_payload_bytes+14)//15)+")[:"+str(args.held_payload_bytes)+"]")
    if args.sequenced_payloads:
        if args.case not in ('established','reverse-established','repeat-established','early-stall','all-established'): parser.error('sequenced-payloads requires held streams')
        requester=requester.replace(' time.sleep(index*.25)', ' sequence=0;time.sleep(index*.25)')
        requester=requester.replace(";s.sendall(payload);data=b''", ";payload=sequence.to_bytes(8,'big')+payload;sequence+=1;s.sendall(payload);data=b''")
    workload = None
    try:
        for name in (client, router, server):
            run('ip', 'netns', 'add', name); namespaces.append(name); ns(name, 'ip', 'link', 'set', 'lo', 'up')
        ns(server, 'iptables', '-A', 'INPUT', '-p', 'udp', '--dport', '31111', '-j', 'DROP')
        for side, endpoint, iface, bridge_if, mac, address in [('c', client, 'spq-c', 'rc', '02:00:00:00:00:01', '198.18.0.1/24'), ('s', server, 'spq-s', 'rs', '02:00:00:00:00:02', '198.18.0.2/24')]:
            host, peer = f'xc{side}{suffix}', f'xr{side}{suffix}'
            run('ip', 'link', 'add', host, 'type', 'veth', 'peer', 'name', peer)
            run('ip', 'link', 'set', host, 'netns', endpoint); ns(endpoint, 'ip', 'link', 'set', host, 'name', iface)
            run('ip', 'link', 'set', peer, 'netns', router); ns(router, 'ip', 'link', 'set', peer, 'name', bridge_if)
            ns(endpoint, 'ip', 'link', 'set', iface, 'address', mac)
            ns(endpoint, 'ip', 'addr', 'add', address, 'dev', iface); ns(endpoint, 'ip', 'link', 'set', iface, 'up')
        ns(router, 'ip', 'link', 'add', 'br0', 'type', 'bridge'); ns(router, 'ip', 'link', 'set', 'br0', 'up')
        for iface in ('rc', 'rs'):
            ns(router, 'ip', 'link', 'set', iface, 'master', 'br0'); ns(router, 'ip', 'link', 'set', iface, 'up')
            ns(router, 'tc', 'qdisc', 'add', 'dev', iface, 'root', 'netem', 'delay', '40ms', 'limit', '10000')
        for iface in ('rc','rs'):ns(router, 'tc', 'qdisc', 'add', 'dev', iface, 'clsact')
        kcp = {'mode':'manual','sndwnd':4096,'rcvwnd':4096,'mtu':1350,'nodelay':0,'interval':30,'resend':2,'nocongestion':1,'wdelay':True,'acknodelay':False,'small_write_flush':256,'smuxbuf':4194304,'streambuf':2097152,'adaptive_buffers':False,'ack_timestamps':False,'credit_hints':False}
        def endpoint(ip, port, interface, mac, flags, address):
            """Use the deployed null/four-session transport profile and exact flags."""
            return {'address':address,'enc':'null','shared_source':True,'sessions':4,'max_sessions':4,'packet_workers':1,'adaptive':True,'kcp':kcp,'network':{'backend':'packet','interface':interface,'ipv4':{'addr':f'{ip}:{port}','router_mac':mac},'tcp':{'local_flag':[flags],'remote_flag':['PA' if flags=='S' else 'S']}}}
        broken = endpoint('198.18.0.1',29997,'spq-c','02:00:00:00:00:02','S','198.18.0.2:29999')
        broken['path_recovery']={'enabled':True,'stalled_after':f'{args.stall_seconds}s','retry_interval':'15s','probe_timeout':'5s','preserve_connections':args.preserve_connections}
        if not args.shared_source:
            broken['shared_source']=False
            broken['network']['ipv4']['addr']='198.18.0.1:0'
            broken['source_ports']=[29997,29995,29993,29991]
        healthy = endpoint('198.18.0.1',29996,'spq-c','02:00:00:00:00:02','S','198.18.0.2:29999')
        listener = endpoint('198.18.0.2',29999,'spq-s','02:00:00:00:00:01','PA','198.18.0.2:29999')
        listener['packet_workers']=args.packet_workers
        common={'metrics':'127.0.0.1:29090','log':{'level':'debug','interval':'1s','format':'json'},'limits':{'open_timeout':'5s','dial_timeout':'2s'}}
        cfg={**common,'peers':{'broken':broken,'healthy':healthy},'forwards':[{'listen':'127.0.0.1:28080','peer':'broken','target':'127.0.0.1:18080'},{'listen':'127.0.0.1:28081','peer':'healthy','target':'127.0.0.1:18080'}]}
        config.write_text(json.dumps(cfg)); server_config=out/'server.json';server_config.write_text(json.dumps({**common,'listeners':[listener]}))
        echo = r'''import socketserver
class Echo(socketserver.BaseRequestHandler):
 def handle(self):
  while True:
   data=self.request.recv(4096)
   if not data:return
   self.request.sendall(data)
class Server(socketserver.ThreadingTCPServer):
 allow_reuse_address=True
 daemon_threads=True
Server(('127.0.0.1',18080),Echo).serve_forever()
'''
        for namespace,config_path,label,executable in ((client,config,'client',binary),(server,server_config,'server',pathlib.Path(args.server_binary).resolve() if args.server_binary else binary)):
            checked=ns(namespace,str(executable),'config','validate','-c',str(config_path),'--json')
            (out/(label+'-validation.json')).write_text(checked.stdout)
        capture=spawn(client,'wire', 'tcpdump','-i','spq-c','-n','-U','-w',str(out/'wire.pcap'),'tcp port 29999')
        time.sleep(.25)
        spawn(server,'echo',sys.executable,'-c',echo)
        spawn(server,'server',str(pathlib.Path(args.server_binary).resolve()) if args.server_binary else str(binary),'run','-c',str(server_config))
        client_process=spawn(client,'client',str(binary),'run','-c',str(config))
        for _ in range(80):
            try:metrics();break
            except subprocess.CalledProcessError:time.sleep(.1)
        else:raise RuntimeError('client metrics not ready')
        workload=spawn(client,'requests',sys.executable,'-c',requester);started=time.monotonic();injected=False;restored=False;reloaded=False;snapshots=[]
        while workload.poll() is None:
            elapsed=time.monotonic()-started
            if elapsed>5 and not injected:
                before=metrics();(out/'before-metrics.txt').write_text(before)
                if args.case in ('tuple','established','short-loss','repeat-tuple','repeat-established','early-stall'):
                    ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','20','flower','ip_proto','tcp','src_port','29997','dst_port','29999','action','drop')
                elif args.case in ('reverse-tuple','reverse-established'):
                    ns(router,'tc','filter','add','dev','rs','ingress','protocol','ip','pref','20','flower','ip_proto','tcp','src_port','29999','dst_port','29997','action','drop')
                elif args.case in ('all-tuples','all-established'):
                    for pref,port in enumerate((29997,29995,29993,29991),20):
                        ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref',str(pref),'flower','ip_proto','tcp','src_port',str(port),'dst_port','29999','action','drop')
                elif args.case=='whole-peer':
                    ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','10','flower','ip_proto','tcp','src_port','29996','dst_port','29999','action','pass')
                    ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','20','flower','ip_proto','tcp','dst_port','29999','action','drop')
                else:
                    convs=re.findall(r'super_paqet_peer_conversation_id\{peer="broken",session="\d+"\} (\d+)',before)
                    if len(convs)!=4:raise RuntimeError('expected four active conversations before injection')
                    conv=int(convs[0]);network_value=struct.unpack('!I',struct.pack('<I',conv))[0]
                    ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','20','u32','match','ip','protocol','6','0xff','match','u16','29997','0xffff','at','20','match','u32',hex(network_value),'0xffffffff','at','60','action','drop')
                    events.append({'event':'blocked_conversation','conv':conv,'at':elapsed})
                events.append({'event':'injected','at':elapsed});injected=True
            if args.case in ('repeat-tuple','repeat-established','early-stall') and elapsed>(23 if args.case=='early-stall' else 32) and not repeated:
                current=dict(re.findall(r'super_paqet_peer_source_port\{peer="broken",session="(\d+)"\} (\d+)',metrics()))
                port=current['0']
                assert int(port)!=29997,'first recovery did not finish before repeated fault'
                ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','21','flower','ip_proto','tcp','src_port',port,'dst_port','29999','action','drop')
                events.append({'event':'repeated_tuple_drop','port':int(port),'at':elapsed});repeated=True
            if args.case=='short-loss' and elapsed>10 and not restored:
                ns(router,'tc','filter','del','dev','rc','ingress','protocol','ip','pref','20');events.append({'event':'restored','at':elapsed});restored=True
            if args.case=='whole-peer' and elapsed>70 and not restored:
                ns(router,'tc','filter','del','dev','rc','ingress','protocol','ip','pref','20');events.append({'event':'restored','at':elapsed});restored=True
            if elapsed>(82 if args.case=='whole-peer' else 47) and not reloaded:
                # An identical endpoint plus a log edit must retain the effective
                # recovered tuple. Atomic replacement exercises the live reader.
                cfg['log']['interval']='2s';temp=config.with_suffix('.next');temp.write_text(json.dumps(cfg));os.replace(temp,config)
                events.append({'event':'config_reload','at':elapsed});reloaded=True
            snapshots.append({'at':elapsed,'metrics':metrics()})
            if elapsed>duration+13:raise RuntimeError('workload exceeded deadline')
            time.sleep(.5)
        workload.wait(timeout=1)
        rows=[json.loads(line) for line in (out/'requests.log').read_text().splitlines()]
        after=metrics();(out/'after-metrics.txt').write_text(after)
        drops=json.dumps({dev:json.loads(ns(router,'tc','-j','-s','filter','show','dev',dev,'ingress').stdout) for dev in ('rc','rs')});(out/'filters.json').write_text(drops)
        logs=[json.loads(line) for line in (out/'client.log').read_text().splitlines() if line.startswith('{')]
        capture.send_signal(signal.SIGINT);capture.wait(timeout=5)
        wire=check_wire(out/'wire.pcap')
        recovered=[r for r in logs if r.get('msg')=='path.recovered'];failed=[r for r in logs if r.get('msg')=='path.recovery_probe_failed']
        result={'case':args.case,'shared_source':args.shared_source,'preserve_connections':args.preserve_connections,'packet_workers':args.packet_workers,'held_payload_bytes':args.held_payload_bytes,'sequenced_payloads':args.sequenced_payloads,'sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'client_pid':client_process.pid,'events':events,'recovered':recovered,'failed_probes':failed,'healthy_successes':sum(r['ok'] for r in rows if r['peer']=='healthy'),'healthy_errors':[r for r in rows if r['peer']=='healthy' and not r['ok']],'broken_successes_after_45s':sum(r['ok'] for r in rows if r['peer']=='broken' and r['at']>(75 if args.case=='whole-peer' else 45)),'broken_errors':sum(not r['ok'] for r in rows if r['peer']=='broken'),'first_success_after_fault':next((r for r in rows if r['peer']=='broken' and r['ok'] and r['at']>6),None),'snapshots':snapshots,'wire':wire}
        (out/'result.json').write_text(json.dumps(result,indent=2)+'\n')
        result['stall_seconds'] = args.stall_seconds
        assert not result['healthy_errors'],'healthy peer disrupted'
        assert result['broken_successes_after_45s']>10,'path did not recover'
        if args.case in ('tuple','reverse-tuple','established','reverse-established'):assert len(recovered)==1,'single tuple recovery missing or siblings replaced'
        if args.case in ('repeat-tuple','repeat-established','early-stall'):assert len(recovered)==2 and all(r.get('session')==0 for r in recovered),'repeated carrier recovery missing'
        if args.case in ('all-tuples','all-established'):assert len(recovered)==4,'not all four blocked tuples recovered'
        if args.case=='short-loss':assert not recovered,'transient loss rotated source tuple'
        if args.case=='one-lane':
            if args.shared_source:assert not recovered,'progressing shared pool was replaced'
            else:assert len(recovered)<=1 and all(r.get('session')==0 for r in recovered),'conversation fault replaced sibling tuples'
        if args.case=='whole-peer':assert failed,'failed probe not exercised'
        if args.case in ('established','reverse-established','repeat-established','early-stall','all-established'):
            held_errors=[r for r in rows if r['peer']=='broken' and not r['ok']]
            assert len(held_errors)==(0 if args.preserve_connections and not args.server_binary else (4 if args.case=='all-established' else 1)),'established stream continuity failed'
            assert not any(r.get('msg') in ('opening.transport_timeout','opening.syn_timeout') for r in logs),'established test used new-opening failure evidence'
            result['established_streams_preserved']=4-len(held_errors)
        if args.case=='early-stall':
            signals=[r for r in logs if r.get('msg')=='path.migration_early_stall']
            assert len(signals)==1, 'early reblocked carrier warning missing or duplicated'
            result['early_stall_signals']=signals
        assert client_process.poll() is None,'client process exited'
        healthy_before=set(re.findall(r'super_paqet_peer_conversation_id\{peer="healthy",session="\d+"\} (\d+)',before))
        healthy_after=set(re.findall(r'super_paqet_peer_conversation_id\{peer="healthy",session="\d+"\} (\d+)',after))
        assert healthy_before==healthy_after,'healthy peer carriers replaced'
        for snapshot in snapshots:
            assert len(re.findall(r'super_paqet_peer_conversation_id\{peer="broken",',snapshot['metrics']))<=4,'carrier pool exceeded fixed four'
        initial_ports={29997} if args.shared_source else {29997,29995,29993,29991}
        changed=[x for x in snapshots if any(int(p) not in initial_ports for p in re.findall(r'super_paqet_peer_source_port\{peer="broken",session="\d+"\} (\d+)',x['metrics']))]
        if args.case in ('tuple','reverse-tuple','established','reverse-established','all-tuples','repeat-tuple','repeat-established','early-stall','all-established') or (args.case=='one-lane' and recovered):
            assert changed,'effective source did not change'
            port_before_reload=re.findall(r'super_paqet_peer_source_port\{peer="broken",session="\d+"\} (\d+)',next(x['metrics'] for x in snapshots if x['at']>45))
            if args.case not in ('repeat-tuple','repeat-established','early-stall'):assert set(port_before_reload)==set(re.findall(r'super_paqet_peer_source_port\{peer="broken",session="\d+"\} (\d+)',after)),'reload reset recovered source'
            result['source_change_at_seconds']=changed[0]['at']
            result['recovery_after_injection_seconds']=changed[0]['at']-next(e['at'] for e in events if e['event']=='injected')
            if not args.shared_source and args.case!='all-tuples':
                before_map=dict(re.findall(r'super_paqet_peer_source_port\{peer="broken",session="(\d+)"\} (\d+)',before))
                before_conv=dict(re.findall(r'super_paqet_peer_conversation_id\{peer="broken",session="(\d+)"\} (\d+)',before))
                after_conv=dict(re.findall(r'super_paqet_peer_conversation_id\{peer="broken",session="(\d+)"\} (\d+)',after))
                assert all(before_conv[i]==after_conv.get(i) for i,port in before_map.items() if int(port)!=29997),'healthy sibling carrier replaced'
                result['healthy_sibling_carriers_preserved']=True
                if args.preserve_connections and not args.server_binary and args.case in ('established','reverse-established','repeat-established','early-stall','all-established'):
                    assert before_conv==after_conv, 'migration replaced the logical KCP carrier'
                    assert all(r.get('connections_preserved') for r in recovered), 'migration fell back unexpectedly'
                    result['logical_carriers_preserved']=True
        # A slot switch must remove the old tuple's rules while retaining all
        # current sibling/probe-adopted ports. This checks ownership while the
        # client is still running, not just after process-wide teardown.
        rules=ns(client,'iptables-save').stdout
        (out/'live-firewall-after.txt').write_text(rules)
        ports=set(re.findall(r'super_paqet_peer_source_port\{peer="(?:broken|healthy)",session="\d+"\} (\d+)',after))
        for port in ports:
            assert '--dport '+port+' ' in rules and '--sport '+port+' ' in rules,'live carrier rules missing'
        for record in recovered:
            retired=record.get('old_source_port')
            if retired is not None and str(retired) not in ports:
                assert '--dport '+str(retired)+' ' not in rules and '--sport '+str(retired)+' ' not in rules,'retired tuple rules leaked'
        result['live_sibling_rules_preserved']=True
        result['retired_tuple_rules_removed']=True
        result['healthy_carriers_preserved']=True
        result['fixed_four_ceiling_verified']=True
        (out/'result.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps({k:v for k,v in result.items() if k!='snapshots'}),flush=True)
    finally:
        for process in reversed(processes):
            if process.poll() is None:process.terminate()
        for process in reversed(processes):
            try:process.wait(timeout=10)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
        for handle in handles:handle.close()
        cleanup={}
        for name in namespaces:
            if name==router:continue
            rules=ns(name,'iptables-save').stdout
            cleanup[name+'_owned_rules_removed']='SPQ_' not in rules
            if name==server:cleanup['unrelated_rule_preserved']='--dport 31111 -j DROP' in rules
        for name in reversed(namespaces):
            subprocess.run(['ip','netns','delete',name],check=False,capture_output=True)
        remaining=run('ip','netns','list').stdout
        cleanup['namespaces_removed']=all(name not in remaining for name in namespaces)
        (out/'cleanup.json').write_text(json.dumps(cleanup)+'\n')
        assert all(cleanup.values()),'fixture resources or firewall cleanup failed'


if __name__=='__main__':
    main()
