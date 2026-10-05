#!/usr/bin/env python3
# Module purpose: Read recorded benchmark outputs only; summarization must not rerun tests or
# imply missing results passed.
"""Summarize saved benchmark artifacts without rerunning a workload."""
import argparse
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('directories',nargs='+')
a = p.parse_args()
for directory in a.directories:
    root=Path(directory)
    data=json.loads((root/'results.json').read_text())
    results=[]
    for result in data['results']:
        if 'iperf' in result:
            rates={key:round(value['bits_per_second']/1e9,4) for key,value in result['end'].items() if key.startswith('sum_')}
            results.append({'iperf':result['iperf'],'gbps':rates,'tunnel_cpu_cores':result.get('tunnel_cpu_cores')})
        else:
            results.append(result)
    cleanup=json.loads((root/'cleanup.json').read_text()) if (root/'cleanup.json').exists() else None
    print(json.dumps({'directory':str(root),'parameters':data['parameters'],'results':results,'processes':data['processes'],'binary_sha256':data.get('binary_sha256'),'cleanup':cleanup},indent=2))
