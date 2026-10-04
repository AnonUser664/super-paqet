#!/usr/bin/env python3
"""Expanded sequential link qualification. Never runs two measured cases together."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT=Path(__file__).resolve().parents[1]
PROFILES={
    'clean':['--iperf','--workers','8','--sessions','8'],
    'wan100':['--iperf','--rate-mbit','1000','--delay-ms','50','--workers','1','--sessions','1'],
    'satellite':['--iperf','--rate-mbit','100','--delay-ms','300','--workers','4'],
    'asymmetric':['--iperf','--rate-mbit','20','--down-rate-mbit','100','--delay-ms','40','--loss','0.5','--workers','4'],
    'ack-limited':['--iperf','--rate-mbit','1','--down-rate-mbit','100','--delay-ms','25','--workers','4'],
    'random':['--rate-mbit','100','--delay-ms','10','--loss','1','--workers','32'],
    'loss5':['--rate-mbit','100','--delay-ms','40','--loss','5','--workers','16'],
    'loss20':['--rate-mbit','20','--delay-ms','50','--loss','20','--workers','8'],
    'burst':['--rate-mbit','100','--delay-ms','20','--burst-loss','0.5','20','80','0.1','--workers','16'],
    'reorder':['--rate-mbit','100','--delay-ms','25','--jitter-ms','5','--reorder','5','--workers','16'],
    'jitter':['--rate-mbit','100','--delay-ms','25','--jitter-ms','10','--loss','0.5','--workers','16'],
    'mobile':['--rate-mbit','1','--delay-ms','50','--loss','5','--workers','4'],
    'tiny-queue':['--rate-mbit','100','--delay-ms','10','--queue-packets','32','--workers','8'],
    'mtu576':['--mtu','576','--rate-mbit','20','--delay-ms','10','--workers','8'],
    'ipv6':['--ipv6','--rate-mbit','100','--delay-ms','10','--workers','16'],
    'ipv6-mtu1280':['--ipv6','--mtu','1280','--rate-mbit','100','--delay-ms','10','--loss','1','--workers','8'],
    'rate-step':['--mode','bulk','--rate-mbit','100','--delay-ms','10','--workers','8','--schedule',json.dumps([{'at':4,'rate_mbit':10},{'at':10,'rate_mbit':100}])],
    'delay-step':['--mode','bulk','--rate-mbit','100','--delay-ms','10','--workers','8','--schedule',json.dumps([{'at':4,'delay_ms':100},{'at':10,'delay_ms':10}])],
    'outage':['--mode','bulk','--rate-mbit','100','--delay-ms','10','--workers','8','--schedule',json.dumps([{'at':4,'loss':100},{'at':7,'loss':0}])],
    'restart':['--functional','--restart','--capture','--workers','4','--sessions','1'],
}

def main():
    p=argparse.ArgumentParser()
    p.add_argument('--binary',default='build/super-paqet')
    p.add_argument('--output',default='build/expanded-links')
    p.add_argument('--cases',nargs='+',choices=list(PROFILES),default=list(PROFILES))
    p.add_argument('--seeds',nargs='+',type=int,default=[42])
    p.add_argument('--duration',type=int,default=20)
    p.add_argument('--profile',action='store_true')
    a=p.parse_args()
    if os.geteuid()!=0:p.error('run as root')
    if a.duration<12:p.error('use >=12 seconds so scheduled events execute within the workload')
    out=ROOT/a.output;out.mkdir(parents=True,exist_ok=True)
    evidence=[]
    for case in a.cases:
        for seed in a.seeds:
            directory=out/(case+'-seed-'+str(seed))
            flags=PROFILES[case]
            command=[sys.executable,str(ROOT/'scripts/netns_bench.py'),'--enterprise','--binary',a.binary,'--debug','--bridge','--seed',str(seed),'--duration',str(a.duration),'--sessions','4','--output',str(directory),*flags]
            if '--iperf' in flags:command+=['--warmup','8']
            if a.profile:command+=['--profile']
            print('Starting '+case+' seed '+str(seed),flush=True)
            with (out/(case+'-'+str(seed)+'-console.log')).open('w') as log:
                result=subprocess.run(command,cwd=ROOT,stdout=log,stderr=subprocess.STDOUT)
            row={'case':case,'seed':seed,'command':command,'exit_code':result.returncode}
            for name in ('results','cleanup'):
                path=directory/(name+'.json')
                if path.exists():row[name]=json.loads(path.read_text())
            failures=[]
            if result.returncode:failures.append('harness failed')
            if row.get('cleanup',{}).get('firewall_clean') is not True:failures.append('firewall cleanup failed')
            for workload in row.get('results',{}).get('results',[]):
                if workload.get('errors',0):failures.append('workload errors: '+str(workload['errors']))
            row['failures']=failures;evidence.append(row)
            (out/'matrix.json').write_text(json.dumps(evidence,indent=2)+'\n')
            if failures:raise SystemExit(case+' failed: '+', '.join(failures)+'; inspect '+str(directory))
    print('All selected profiles passed: '+str(out/'matrix.json'),flush=True)

if __name__=='__main__':main()
