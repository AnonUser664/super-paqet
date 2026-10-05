#!/usr/bin/env python3
"""Exercise the shipped service restrictions in disposable network namespaces."""
import argparse
import configparser
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]

def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='build/super-paqet')
    args = parser.parse_args()
    if os.geteuid() != 0: raise SystemExit('run as root')
    ident = str(os.getpid())
    work = Path('/run')/('spq-service-test-'+ident)
    out = ROOT/'build'/('service-test-'+ident)
    out.mkdir(parents=True, exist_ok=True)
    work.mkdir(mode=0o700)
    binary = work/'super-paqet'
    shutil.copy2(ROOT/args.binary, binary)
    c,s = 'spq-svc-c-'+ident,'spq-svc-s-'+ident
    namespaces,units,processes = [],[],[]
    def ns(n,*args): return run('ip','netns','exec',n,*args)
    def pid(unit): return int(run('systemctl','show',unit,'--property=MainPID','--value').stdout.strip())
    template = configparser.ConfigParser(interpolation=None)
    template.optionxform = str
    template.read(ROOT/'deploy/super-paqet.service')
    def start(n,side):
        unit = 'spq-service-test-'+ident+'-'+side
        args = ['systemd-run','--quiet','--collect','--unit='+unit,'--property=NetworkNamespacePath=/run/netns/'+n]
        for key,value in template['Service'].items():
            if key not in ('ExecStart','ExecStopPost','EnvironmentFile'):
                args += ['--property='+key+'='+value]
        args += ['--property=ExecStopPost='+str(binary)+' firewall-cleanup',str(binary),'run','-c',str(work/(side+'.yaml'))]
        units.append(unit)
        run(*args)
        return unit
    def verify(): return json.loads(ns(c,str(ROOT/'build/spq-bench'),'-mode','verify','-addr','127.0.0.1:28080').stdout)
    report = {'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest()}
    try:
        for n in (c,s):
            run('ip','netns','add',n); namespaces.append(n)
            ns(n,'ip','link','set','lo','up')
        hc,hs='svc'+ident,'svs'+ident
        run('ip','link','add',hc,'type','veth','peer','name',hs)
        for n,old,iface,addr,mac in ((c,hc,'c0','198.19.0.1/24','02:00:00:19:00:01'),(s,hs,'s0','198.19.0.2/24','02:00:00:19:00:02')):
            run('ip','link','set',old,'netns',n)
            ns(n,'ip','link','set',old,'name',iface)
            ns(n,'ip','link','set',iface,'address',mac)
            ns(n,'ip','addr','add',addr,'dev',iface)
            ns(n,'ip','link','set',iface,'up')
        (work/'server.yaml').write_text('listeners:\n  - address: 198.19.0.2:29999\n    key: benchmark-only-key\n    network: {interface: s0, ipv4: {addr: "198.19.0.2:29999", router_mac: "02:00:00:19:00:01"}}\n')
        (work/'client.yaml').write_text('peers:\n  remote:\n    address: 198.19.0.2:29999\n    key: benchmark-only-key\n    sessions: 1\n    network: {interface: c0, ipv4: {addr: "198.19.0.1:0", router_mac: "02:00:00:19:00:02"}}\nforwards:\n  - {listen: "127.0.0.1:28080", peer: remote, target: "127.0.0.1:18080"}\n')
        for n in (c,s): ns(n,'iptables','-A','INPUT','-p','udp','--dport','31111','-j','DROP')
        processes.append(subprocess.Popen(['ip','netns','exec',s,str(ROOT/'build/spq-bench'),'-mode','serve','-addr',':18080'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL))
        server,client=start(s,'server'),start(c,'client')
        time.sleep(3)
        report['initial_verification']=verify()
        oldpid=pid(server)
        if oldpid==0: raise RuntimeError('service failed to start')
        report['server_properties']=run('systemctl','show',server,'--property=CapabilityBoundingSet,NoNewPrivileges,ProtectHome,ProtectSystem,LimitNOFILE,NetworkNamespacePath').stdout
        run('systemctl','kill','--signal=SIGKILL','--kill-whom=main',server)
        deadline=time.monotonic()+40
        while time.monotonic()<deadline:
            time.sleep(.25)
            newpid=pid(server)
            if newpid and newpid!=oldpid: break
        else: raise RuntimeError('systemd did not restart server')
        while True:
            try:
                report['restart_verification']=verify(); break
            except subprocess.CalledProcessError:
                if time.monotonic()>deadline: raise RuntimeError('forward did not recover after service restart')
        report['restart_pid_changed']=True
    finally:
        for unit in reversed(units):
            subprocess.run(['systemctl','stop',unit],check=False,capture_output=True)
            (out/(unit+'.log')).write_text(subprocess.run(['journalctl','--no-pager','-u',unit],capture_output=True,text=True).stdout)
        for proc in processes:
            proc.terminate(); proc.wait(timeout=10)
        try:
            clean=True
            for n in namespaces:
                rules=ns(n,'iptables-save').stdout
                (out/(n+'.rules')).write_text(rules)
                clean=clean and 'SPQ_' not in rules
                ns(n,'iptables','-C','INPUT','-p','udp','--dport','31111','-j','DROP')
            report['firewall_clean']=clean
            report['unrelated_rule_preserved']=True
            if not clean: raise RuntimeError('service leaked owned firewall rules')
        finally:
            for n in reversed(namespaces): subprocess.run(['ip','netns','delete',n],check=False)
            shutil.rmtree(work)
            (out/'results.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({'output':str(out),**report},indent=2))

if __name__=='__main__': main()
