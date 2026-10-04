#!/usr/bin/env python3
"""Run the WAN matrix sequentially; retain each command, binary hash and cleanup."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]

def main():
    p=argparse.ArgumentParser()
    p.add_argument('--binary',default='build/super-paqet')
    p.add_argument('--output',default='build/wan-qualification')
    p.add_argument('--duration',type=int,default=20)
    p.add_argument('--cases',nargs='+',choices=['random','burst','jitter-reorder','mobile','asymmetric','high-delay'])
    a=p.parse_args()
    if os.geteuid()!=0: p.error('run as root')
    if a.duration<10: p.error('use at least 10 seconds per workload')
    if not (ROOT/'build/iperf-local/bin/iperf3').is_file(): p.error('build local iperf3 first; see docs/BENCHMARKS.md')
    out=ROOT/a.output
    out.mkdir(parents=True,exist_ok=True)
    cases=[
        ('random',['--rate-mbit','100','--delay-ms','10','--loss','1','--workers','32']),
        ('burst',['--rate-mbit','100','--delay-ms','20','--burst-loss','0.5','20','80','0.1','--workers','16']),
        ('jitter-reorder',['--rate-mbit','100','--delay-ms','25','--jitter-ms','5','--reorder','1','--loss','0.5','--workers','16']),
        ('mobile',['--rate-mbit','1','--delay-ms','50','--loss','5','--workers','4']),
        ('asymmetric',['--rate-mbit','20','--down-rate-mbit','100','--delay-ms','40','--loss','0.5','--iperf','--workers','4']),
        ('high-delay',['--rate-mbit','1000','--delay-ms','50','--iperf','--iperf-directions','upload','--workers','1','--sessions','1']),
    ]
    results=[]
    for name,flags in cases:
        if a.cases and name not in a.cases: continue
        case=out/name
        command=[sys.executable,str(ROOT/'scripts/netns_bench.py'),'--enterprise','--binary',a.binary,'--bridge','--seed','42','--duration',str(a.duration),'--sessions','4','--output',str(case),*flags]
        print('Running '+name,flush=True)
        with (out/(name+'-console.log')).open('w') as log:
            subprocess.run(command,cwd=ROOT,stdout=log,stderr=subprocess.STDOUT,check=True)
        report=json.loads((case/'results.json').read_text())
        cleanup=json.loads((case/'cleanup.json').read_text())
        if not cleanup.get('firewall_clean') or not cleanup.get('unrelated_rule_preserved'):
            raise RuntimeError(name+': cleanup qualification failed')
        if any(result.get('errors',0) for result in report['results']):
            raise RuntimeError(name+': workload errors; inspect artifacts')
        results.append({'case':name,'command':command,'report':report,'cleanup':cleanup})
        (out/'matrix.json').write_text(json.dumps(results,indent=2)+'\n')
    print('Matrix complete: '+str(out/'matrix.json'),flush=True)

if __name__=='__main__': main()
