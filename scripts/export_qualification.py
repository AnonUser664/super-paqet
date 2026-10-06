#!/usr/bin/env python3
# Module purpose: Validate same-binary workload, scale, service and cleanup evidence before
# publishing a qualification result.
"""Export compact qualification evidence and reject mixed binaries or failures."""
import argparse
import hashlib
import json
from pathlib import Path
from stress_links import PROFILES


# load: Load recorded JSON evidence; absence/malformed data must remain a qualification
# failure.
def load(path):
    return json.loads(Path(path).read_text())


# workloads: Separate actual workload result entries from setup controls and retain the
# workload's reported errors.
def workloads(results):
    summary=[]
    for result in results:
        if result.get('errors',0):raise ValueError('unexpected workload errors')
        if 'iperf' in result:
            end=result['end']
            row={'iperf':result['iperf'],'received_mbit':end['sum_received']['bits_per_second']/1e6,'cpu_cores':result.get('tunnel_cpu_cores')}
            if result['iperf']=='bidirectional':
                if result.get('method')!='concurrent_unidirectional':raise ValueError('invalid legacy duplex method')
                row['reverse_received_mbit']=end['sum_received_bidir_reverse']['bits_per_second']/1e6
                for direction in result['direction_reports']:
                    if any(stream['receiver']['bytes']==0 for stream in direction['end']['streams']):raise ValueError('duplex receiver stream carried no measured bytes')
            summary.append(row)
        else:summary.append(result)
    return summary


# clean: Require successful owned-rule cleanup and preservation of the planted unrelated
# rule.
def clean(report):
    if report.get('firewall_clean') is not True or report.get('unrelated_rule_preserved') is not True:
        raise ValueError('firewall cleanup/preservation failed')


# main: Validate same-binary workload, scale, service and cleanup evidence before publishing
# a qualification result.
def main():
    p=argparse.ArgumentParser()
    p.add_argument('--binary',required=True)
    p.add_argument('--matrices',nargs='+',required=True)
    p.add_argument('--scale',required=True,help='directory containing results.json and cleanup.json')
    p.add_argument('--service',required=True,help='systemd results.json')
    p.add_argument('--checks',required=True,help='JSON describing completed validation commands/exit codes')
    p.add_argument('--output',required=True)
    a=p.parse_args()
    sha=hashlib.sha256(Path(a.binary).read_bytes()).hexdigest()
    links=[]
    for path in a.matrices:
        for row in load(path):
            if row['exit_code'] or row.get('failures'):raise ValueError('link profile failed: '+row['case'])
            results=row['results']
            if results['binary_sha256']!=sha:raise ValueError('mixed link binaries')
            clean(row['cleanup'])
            links.append({'case':row['case'],'seed':row['seed'],'parameters':results['parameters'],'workloads':workloads(results['results']),'resources':results['processes'],'cleanup':row['cleanup'],'artifact':str(Path(results['parameters']['output']))})
    if not set(PROFILES).issubset({row['case'] for row in links}):raise ValueError('full live profile matrix is incomplete')
    clean_rates=[w['received_mbit'] for row in links if row['case']=='clean' for w in row['workloads'] if w.get('iperf') in ('upload','download')]
    if not clean_rates or max(clean_rates)<2000:raise ValueError('separate 2 Gbit/s bulk acceptance failed')
    scale_dir=Path(a.scale)
    scale=load(scale_dir/'results.json')
    if scale['binary_sha256']!=sha:raise ValueError('mixed scale binary')
    established=max((r.get('connections',0) for r in scale['results'] if r.get('phase')=='established'),default=0)
    verified=max((r.get('verified',0) for r in scale['results'] if r.get('phase')=='verified'),default=0)
    if established<100000 or verified!=established or scale['parameters']['duration']<120:raise ValueError('100,000-connection acceptance incomplete or held sockets failed verification')
    scale_cleanup=load(scale_dir/'cleanup.json');clean(scale_cleanup)
    host_settings=load(scale_dir/'host-settings.json') if (scale_dir/'host-settings.json').exists() else {}
    if host_settings and host_settings.get('restored') is not True:raise ValueError('temporary host settings were not restored')
    service=load(a.service)
    if service['binary_sha256']!=sha or service.get('restart_pid_changed') is not True:raise ValueError('service binary/recovery mismatch')
    clean(service)
    checks=load(a.checks)
    if not checks or any(row['exit_code']!=0 for row in checks):raise ValueError('validation checks incomplete/failed')
    report={'status':'Local qualification passed for the recorded binary and workloads; remote deployment qualification is separate.','binary_sha256':sha,'limitations':['Finite local simulations and tests cannot prove universal optimality or arbitrary real-WAN/firewall behavior.','100,000 held connections are mostly idle; mixed active traffic and bulk-only throughput are separate workloads.','Receive goodput includes bytes delivered in the measurement interval; sender/receiver warmup boundaries and queued bytes can differ.'],'links':links,'scale':{'parameters':scale['parameters'],'workloads':workloads(scale['results']),'resources':scale['processes'],'cleanup':scale_cleanup,'artifact':str(scale_dir)},'service':service,'checks':checks}
    report['scale']['host_settings']=host_settings
    Path(a.output).write_text(json.dumps(report,indent=2)+'\n')
    print(a.output+' verified: '+sha+'; '+str(len(links))+' live profiles')

if __name__=='__main__':main()
