#!/usr/bin/env python3
# Module purpose: Resume only successful matching stages and finish scale/service/fuzz/checks
# with an explicitly selected binary.
"""Finish qualification sequentially, recording actual exit status of each stage."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from stress_links import PROFILES

ROOT=Path(__file__).resolve().parents[1]

# main: Resume only successful matching stages and finish scale/service/fuzz/checks with an
# explicitly selected binary.
def main():
    p=argparse.ArgumentParser()
    p.add_argument('--binary',required=True)
    p.add_argument('--matrix',required=True)
    p.add_argument('--output',required=True)
    p.add_argument('--hold-seconds',type=int,default=600)
    p.add_argument('--check-source',default=str(ROOT),help='source checkout for full checks (allows an isolated candidate checkout)')
    p.add_argument('--resume',action='store_true',help='reuse successful recorded stages with identical commands')
    p.add_argument('--host-backlog',type=int,default=0,help='temporary host backlog override for the scale soak only')
    a=p.parse_args()
    if os.geteuid()!=0:p.error('run as root')
    matrix=json.loads((ROOT/a.matrix).read_text())
    sha=hashlib.sha256((ROOT/a.binary).read_bytes()).hexdigest()
    if set(PROFILES)-{r['case'] for r in matrix}:raise RuntimeError('matrix incomplete')
    if any(r['exit_code'] or r['failures'] or r['results']['binary_sha256']!=sha for r in matrix):raise RuntimeError('matrix failed or mixed binaries')
    out=ROOT/a.output;out.mkdir(parents=True,exist_ok=True)
    checks=json.loads((out/'checks.json').read_text()) if a.resume and (out/'checks.json').exists() else []
    # stage: Persist each command's actual outcome and reuse only identical successful stages
    # during resume.
    def stage(name,command,cwd=ROOT):
        previous=next((row for row in checks if row['name']==name),None)
        if previous and previous['exit_code']==0:
            if previous['command']!=command:raise RuntimeError('resume command differs for '+name)
            print('Reusing completed '+name,flush=True)
            return
        checks[:]=[row for row in checks if row['name']!=name]
        print('Starting '+name,flush=True)
        started=time.monotonic()
        with (out/(name+'.log')).open('w') as log:
            result=subprocess.run(command,cwd=cwd,stdout=log,stderr=subprocess.STDOUT)
        row={'name':name,'command':command,'exit_code':result.returncode,'elapsed_seconds':time.monotonic()-started,'log':str(out/(name+'.log'))}
        checks.append(row)
        (out/'checks.json').write_text(json.dumps(checks,indent=2)+'\n')
        if result.returncode:raise RuntimeError(name+' failed; inspect '+str(out/(name+'.log')))
    stage('full-checks',['make','vet','test'],cwd=Path(a.check_source).resolve())
    scale_command=[sys.executable,'scripts/netns_bench.py','--enterprise','--binary',a.binary,'--hold','100000','--mixed','--duration',str(a.hold_seconds),'--sessions','8','--workers','64','--debug','--output',str(out/'scale')]
    if a.host_backlog:scale_command+=['--host-backlog',str(a.host_backlog)]
    stage('scale-soak',scale_command)
    stage('service',[sys.executable,'scripts/systemd_netns_test.py','--binary',a.binary])
    stage('fuzz-control',['go','test','./internal/protocol','-run','^$','-fuzz','FuzzControlRead','-fuzztime','60s','-parallel','4'])
    stage('fuzz-frame',['go','test','./internal/socket','-run','^$','-fuzz','FuzzDecodeFrame','-fuzztime','60s','-parallel','4'])
    stage('extra-seeds',[sys.executable,'scripts/stress_links.py','--binary',a.binary,'--output',str(out/'seeds'),'--duration','30','--profile','--cases','asymmetric','reorder','harsh','mixed-ack','mixed-asymmetric','outage','--seeds','7','313'])
    service=json.loads((out/'service.log').read_text())
    stage('export',[sys.executable,'scripts/export_qualification.py','--binary',a.binary,'--matrices',a.matrix,str(out/'seeds/matrix.json'),'--scale',str(out/'scale'),'--service',str(Path(service['output'])/'results.json'),'--checks',str(out/'checks.json'),'--output','docs/step1-qualification.json'])
    print('Sequential qualification completed: '+str(out/'checks.json'),flush=True)

if __name__=='__main__':main()
