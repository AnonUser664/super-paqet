#!/usr/bin/env python3
"""Exercise verified tuple recovery on owned Linux namespaces and a middle bridge.

Drops occur before the backend packet socket: INPUT firewall rules would be an
invalid emulator because AF_PACKET receives packets before kernel TCP filtering.
Only fixture namespaces/interfaces/rules are changed; customer hosts are unused.
"""
import argparse, concurrent.futures, hashlib, json, os, pathlib, re, signal, socket, struct, subprocess, sys, time

ROOT = pathlib.Path(__file__).resolve().parents[1]


def main():
    """Own fixture startup, fault injection, verification and bounded teardown."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True)
    parser.add_argument('--case', choices=['tuple', 'whole-peer', 'one-lane'], default='tuple')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error('requires root for disposable namespaces')
    binary = pathlib.Path(args.binary).resolve()
    out = pathlib.Path(args.output).resolve(); out.mkdir(parents=True, exist_ok=True)
    suffix = str(os.getpid()); client, router, server = [f'spq-rec-{x}-{suffix}' for x in ('c', 'r', 's')]
    namespaces, processes, handles, rows, events = [], [], [], [], []
    config = out / 'client.json'; env = os.environ.copy()

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
    duration = 90 if args.case == 'whole-peer' else 55
    if args.case == 'whole-peer':
        requester = requester.replace('+55;', '+90;')
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
        ns(router, 'tc', 'qdisc', 'add', 'dev', 'rc', 'clsact')
        kcp = {'mode':'manual','sndwnd':4096,'rcvwnd':4096,'mtu':1350,'nodelay':0,'interval':30,'resend':2,'nocongestion':1,'wdelay':True,'acknodelay':False,'small_write_flush':256,'smuxbuf':4194304,'streambuf':2097152,'adaptive_buffers':False,'ack_timestamps':False,'credit_hints':False}
        def endpoint(ip, port, interface, mac, flags, address):
            """Use the deployed null/four-session transport profile and exact flags."""
            return {'address':address,'enc':'null','shared_source':True,'sessions':4,'max_sessions':4,'packet_workers':1,'adaptive':True,'kcp':kcp,'network':{'backend':'packet','interface':interface,'ipv4':{'addr':f'{ip}:{port}','router_mac':mac},'tcp':{'local_flag':[flags],'remote_flag':['PA']}}}
        broken = endpoint('198.18.0.1',29997,'spq-c','02:00:00:00:00:02','S','198.18.0.2:29999')
        broken['path_recovery']={'enabled':True,'stalled_after':'30s','retry_interval':'10s','probe_timeout':'3s'}
        healthy = endpoint('198.18.0.1',29996,'spq-c','02:00:00:00:00:02','S','198.18.0.2:29999')
        listener = endpoint('198.18.0.2',29999,'spq-s','02:00:00:00:00:01','PA','198.18.0.2:29999')
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
        spawn(server,'echo',sys.executable,'-c',echo)
        spawn(server,'server',str(binary),'run','-c',str(server_config))
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
                if args.case=='tuple':
                    ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','20','flower','ip_proto','tcp','src_port','29997','dst_port','29999','action','drop')
                elif args.case=='whole-peer':
                    ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','10','flower','ip_proto','tcp','src_port','29996','dst_port','29999','action','pass')
                    ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','20','flower','ip_proto','tcp','dst_port','29999','action','drop')
                else:
                    convs=re.findall(r'super_paqet_peer_conversation_id\{peer="broken",session="\d+"\} (\d+)',before)
                    if len(convs)!=4:raise RuntimeError('expected four active shared conversations before injection')
                    conv=int(convs[0]);network_value=struct.unpack('!I',struct.pack('<I',conv))[0]
                    ns(router,'tc','filter','add','dev','rc','ingress','protocol','ip','pref','20','u32','match','ip','protocol','6','0xff','match','u16','29997','0xffff','at','20','match','u32',hex(network_value),'0xffffffff','at','60','action','drop')
                    events.append({'event':'blocked_conversation','conv':conv,'at':elapsed})
                events.append({'event':'injected','at':elapsed});injected=True
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
        drops=ns(router,'tc','-j','-s','filter','show','dev','rc','ingress').stdout;(out/'filters.json').write_text(drops)
        logs=[json.loads(line) for line in (out/'client.log').read_text().splitlines() if line.startswith('{')]
        recovered=[r for r in logs if r.get('msg')=='path.recovered'];failed=[r for r in logs if r.get('msg')=='path.recovery_probe_failed']
        result={'case':args.case,'sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'client_pid':client_process.pid,'events':events,'recovered':recovered,'failed_probes':failed,'healthy_successes':sum(r['ok'] for r in rows if r['peer']=='healthy'),'healthy_errors':[r for r in rows if r['peer']=='healthy' and not r['ok']],'broken_successes_after_45s':sum(r['ok'] for r in rows if r['peer']=='broken' and r['at']>(75 if args.case=='whole-peer' else 45)),'broken_errors':sum(not r['ok'] for r in rows if r['peer']=='broken'),'first_success_after_fault':next((r for r in rows if r['peer']=='broken' and r['ok'] and r['at']>6),None),'snapshots':snapshots}
        (out/'result.json').write_text(json.dumps(result,indent=2)+'\n')
        assert not result['healthy_errors'],'healthy peer disrupted'
        assert result['broken_successes_after_45s']>10,'path did not recover'
        if args.case=='tuple':assert len(recovered)==1 and recovered[0]['local']!='198.18.0.1:29997','fresh tuple recovery missing'
        if args.case=='one-lane':assert not recovered,'progressing sibling carriers were replaced'
        if args.case=='whole-peer':assert failed,'failed probe not exercised'
        assert client_process.poll() is None,'client process exited'
        healthy_before=set(re.findall(r'super_paqet_peer_conversation_id\{peer="healthy",session="\d+"\} (\d+)',before))
        healthy_after=set(re.findall(r'super_paqet_peer_conversation_id\{peer="healthy",session="\d+"\} (\d+)',after))
        assert healthy_before==healthy_after,'healthy peer carriers replaced'
        for snapshot in snapshots:
            assert len(re.findall(r'super_paqet_peer_conversation_id\{peer="broken",',snapshot['metrics']))<=4,'carrier pool exceeded fixed four'
        changed=[x for x in snapshots if any(int(p)!=29997 for p in re.findall(r'super_paqet_peer_source_port\{peer="broken",session="\d+"\} (\d+)',x['metrics']))]
        if args.case=='tuple':
            assert changed,'effective source did not change'
            port_before_reload=re.findall(r'super_paqet_peer_source_port\{peer="broken",session="\d+"\} (\d+)',next(x['metrics'] for x in snapshots if x['at']>45))
            assert set(port_before_reload)==set(re.findall(r'super_paqet_peer_source_port\{peer="broken",session="\d+"\} (\d+)',after)),'reload reset recovered source'
            result['source_change_at_seconds']=changed[0]['at']
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
