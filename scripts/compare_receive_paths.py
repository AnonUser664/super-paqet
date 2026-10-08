#!/usr/bin/env python3
"""Compare receive-path changes serially in disposable Linux namespaces.

Both executables use the same deployed reliability/mux profile, four independent
sources, S/PA flags and CPU budgets. The one-worker bulk case removes fanout as a
confounder; multiworker results retain actual capture distribution for review.
This runner checks correctness and cleanup, not universal performance claims.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]

# Keep transport settings identical: only the compiled receive implementation
# differs. Recovery grace was already qualified separately and is enabled here
# on both versions rather than being bundled into a performance comparison.
KCP = {
    'mode': 'manual', 'sndwnd': 4096, 'rcvwnd': 4096, 'mtu': 1350,
    'nodelay': 0, 'interval': 30, 'resend': 2, 'nocongestion': 1,
    'wdelay': True, 'acknodelay': False, 'small_write_flush': 256,
    'smuxbuf': 4194304, 'streambuf': 2097152, 'adaptive_buffers': False,
    'ack_timestamps': False, 'credit_hints': False, 'smux_recovery_grace': 60,
}

# WAN rates are directional. Middle-bridge shaping models loss before packet
# capture; fixed seeds repeat the fault inputs but do not eliminate scheduling.
CASES = {
    'single-worker-bulk': ['--packet-workers', '1', '--mode', 'bulk', '--iperf', '--iperf-directions', 'upload', 'download'],
    'multiworker-bulk': ['--mode', 'bulk', '--iperf', '--iperf-directions', 'upload', 'download'],
    'churn': ['--mode', 'http-churn', '--workers', '64'],
    'duplex-latency': ['--mode', 'bulk', '--iperf', '--iperf-directions', 'bidirectional', '--duplex-http'],
    'bounded-duplex': ['--mode', 'bulk', '--iperf', '--iperf-directions', 'bidirectional', '--duplex-http', '--iperf-rate-mbit', '1000', '--warmup', '3'],
    'bounded-duplex-one-worker': ['--mode', 'bulk', '--packet-workers', '1', '--iperf', '--iperf-directions', 'bidirectional', '--duplex-http', '--iperf-rate-mbit', '1000', '--warmup', '3'],
    'asymmetric': ['--mode', 'both', '--workers', '16', '--bridge', '--delay-ms', '40', '--loss', '.5', '--reorder', '5', '--rate-mbit', '50', '--down-rate-mbit', '10', '--queue-packets', '2048', '--seed', '804'],
    'high-delay': ['--mode', 'http-churn', '--workers', '16', '--bridge', '--delay-ms', '120', '--rate-mbit', '20', '--queue-packets', '2048', '--seed', '805'],
    'reorder': ['--mode', 'both', '--workers', '16', '--bridge', '--delay-ms', '40', '--jitter-ms', '20', '--reorder', '50', '--rate-mbit', '100', '--queue-packets', '4096', '--seed', '806'],
    'burst': ['--mode', 'both', '--workers', '16', '--bridge', '--delay-ms', '20', '--burst-loss', '.5', '20', '80', '.1', '--rate-mbit', '100', '--queue-packets', '2048', '--seed', '807'],
    'mobile': ['--mode', 'both', '--workers', '4', '--bridge', '--delay-ms', '50', '--loss', '5', '--rate-mbit', '2', '--queue-packets', '512', '--seed', '810'],
    'rate-step': ['--mode', 'bulk', '--workers', '8', '--bridge', '--delay-ms', '10', '--rate-mbit', '50', '--queue-packets', '2048', '--seed', '808', '--schedule', '[{"at":4,"rate_mbit":5},{"at":10,"rate_mbit":50}]'],
    # HTTP/GET bulk travels server-to-client, so this stresses the narrow
    # downlink. Retain the historical case name for existing receipts; use
    # upload-ack-bottleneck below to isolate constrained reverse feedback.
    'ack-bottleneck': ['--mode', 'both', '--workers', '8', '--bridge', '--delay-ms', '20', '--rate-mbit', '100', '--down-rate-mbit', '1', '--queue-packets', '2048', '--seed', '812'],
    'upload-ack-bottleneck': ['--mode', 'bulk', '--iperf', '--iperf-directions', 'upload', '--workers', '8', '--bridge', '--delay-ms', '20', '--rate-mbit', '100', '--down-rate-mbit', '1', '--queue-packets', '2048', '--seed', '812'],
    # Change propagation delay while work is active; the original ceiling and
    # controller remain in force. The runner requires >=12 seconds, and the
    # qualifying invocation uses 30 seconds to observe both transitions.
    'delay-step': ['--mode', 'both', '--workers', '8', '--bridge', '--delay-ms', '10', '--rate-mbit', '50', '--queue-packets', '2048', '--seed', '813', '--schedule', '[{"at":5,"delay_ms":80},{"at":15,"delay_ms":10}]'],
    'tiny-queue': ['--mode', 'both', '--workers', '8', '--bridge', '--delay-ms', '10', '--rate-mbit', '100', '--queue-packets', '32', '--seed', '809'],
    'encrypted': ['--mode', 'bulk', '--packet-workers', '1', '--block', 'aes-128-gcm', '--iperf', '--iperf-directions', 'upload', 'download'],
    'hold-10000': ['--hold', '10000', '--workers', '64'],
    'hold-100000': ['--hold', '100000', '--workers', '64'],
}


def compact_result(directory):
    """Retain workload/resources/capture counters without bulky per-stream data."""
    report = json.loads((directory / 'results.json').read_text())
    cleanup = json.loads((directory / 'cleanup.json').read_text())
    if not cleanup.get('firewall_clean') or not cleanup.get('unrelated_rule_preserved'):
        raise RuntimeError('cleanup or unrelated-rule preservation failed')
    rows = []
    for result in report['results']:
        if result.get('errors', 0):
            raise RuntimeError('workload errors')
        if 'iperf' in result:
            row = {k: result[k] for k in ('iperf', 'method', 'tunnel_cpu_cores', 'idle_wait_seconds') if k in result}
            row['receiver_gbps'] = result['end']['sum_received']['bits_per_second'] / 1e9
            if result['iperf'] == 'bidirectional':
                row['reverse_receiver_gbps'] = result['end']['sum_received_bidir_reverse']['bits_per_second'] / 1e9
            rows.append(row)
        else:
            rows.append(result)
    captures = {}
    populations = {}
    for path in sorted(directory.glob('server-*-metrics.txt')):
        captures[path.name] = [int(x) for x in re.findall(r'super_paqet_listener_capture_packets\{[^}]+\} (\d+)', path.read_text())]
    for path in sorted(directory.glob('client-*-metrics.txt')):
        # Retain the placement evidence needed to interpret shared-carrier
        # latency; cached pressure can leave a lane idle in one run and busy
        # in another. Capture distribution alone does not reveal this.
        populations[path.name] = [line for line in path.read_text().splitlines()
                                  if re.match(r'super_paqet_peer_(streams|pending|send_window|rtt_ms|conversation_id|local_source_port)\{', line)]
    return {'sha256': report['binary_sha256'], 'parameters': report['parameters'],
            'results': rows, 'processes': report['processes'], 'cleanup': cleanup,
            'worker_capture_packets': captures, 'carrier_population_samples': populations}


def main():
    """Alternate versions and retain every completed command/result immediately."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', required=True)
    parser.add_argument('--candidate', required=True)
    parser.add_argument('--output', default='build/receive-comparison')
    parser.add_argument('--cases', choices=list(CASES), nargs='+', default=[x for x in CASES if x not in ('hold-100000', 'delay-step')])
    parser.add_argument('--repetitions', type=int, default=2)
    parser.add_argument('--duration', type=int, default=15)
    parser.add_argument('--warmup', type=int, default=0, help='omitted iperf startup seconds; HTTP/capacity cases are unchanged')
    parser.add_argument('--client-procs', type=int, default=4)
    parser.add_argument('--server-procs', type=int, default=2)
    parser.add_argument('--client-cpus', default='')
    parser.add_argument('--server-cpus', default='')
    parser.add_argument('--workload-cpus', default='')
    parser.add_argument('--profile', action='store_true', help='collect CPU profiles on the first repetition')
    parser.add_argument('--debug', action='store_true', help='sample structured transport diagnostics each second on both versions')
    parser.add_argument('--pinned-lanes', action='store_true', help='pin equal iperf streams to each carrier, leaving non-iperf tests unchanged')
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error('run as root; only owned namespaces are changed')
    if args.duration < 12 or args.warmup < 0 or min(args.repetitions, args.client_procs, args.server_procs) < 1:
        parser.error('duration must be >=12 and repetitions/core budgets positive')
    if len(set(args.cases)) != len(args.cases):
        parser.error('cases must be unique so each receipt has one workload identity')
    if 'delay-step' in args.cases and args.duration < 25:
        parser.error('delay-step needs at least 25 seconds to observe both transitions and recovery')
    binaries = {name: (ROOT / path).resolve() for name, path in [('baseline', args.baseline), ('candidate', args.candidate)]}
    hashes = {name: hashlib.sha256(path.read_bytes()).hexdigest() for name, path in binaries.items()}
    out = ROOT / args.output
    out.mkdir(parents=True, exist_ok=True)
    receipt = out / 'comparison.json'
    if receipt.exists():
        parser.error('choose a new output directory; existing receipts are never overwritten')
    evidence = {'executable_sha256': hashes, 'production_changed': False, 'runs': []}
    for repetition in range(args.repetitions):
        for case in args.cases:
            order = ['baseline', 'candidate'] if repetition % 2 == 0 else ['candidate', 'baseline']
            for name in order:
                label = f'{case}-{name}-{repetition}'
                directory = out / label
                directory.mkdir(exist_ok=False)
                command = [sys.executable, '-B', str(ROOT / 'scripts/netns_bench.py'), '--binary', str(binaries[name]),
                           '--source-ports', '39941', '37475', '54359', '52477', '--duration', str(args.duration),
                           '--client-procs', str(args.client_procs), '--server-procs', str(args.server_procs),
                           '--sessions', '4', '--workers', '32', '--packet-workers', '2', '--conversation-listener',
                           '--path-recovery', '--preserve-connections', '--client-flag', 'S', '--server-flag', 'PA',
                           '--block', 'null', '--kcp-options', json.dumps(KCP), '--output', str(directory), *CASES[case]]
                if args.profile and repetition == 0:
                    command.append('--profile')
                if args.debug:
                    command.append('--debug')
                if args.warmup and '--iperf' in CASES[case]:
                    command.extend(['--warmup', str(args.warmup)])
                if args.pinned_lanes and '--iperf' in CASES[case]:
                    command.append('--pinned-lanes')
                for option in ('client-cpus', 'server-cpus', 'workload-cpus'):
                    cpus = getattr(args, option.replace('-', '_'))
                    if cpus:command.extend(['--'+option, cpus])
                print('START', label, flush=True)
                started = time.monotonic()
                with (directory / 'runner.log').open('w') as log:
                    result = subprocess.run(command, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT)
                row = {'label': label, 'case': case, 'version': name, 'repetition': repetition,
                       'command': command, 'exit_code': result.returncode, 'elapsed_seconds': time.monotonic() - started}
                if result.returncode == 0:
                    try:
                        row.update(compact_result(directory))
                        if row['sha256'] != hashes[name]:
                            raise RuntimeError('executable changed during comparison')
                    except Exception as error:
                        # A successful child exit cannot hide invalid receipts;
                        # preserve this failed gate before stopping the matrix.
                        row['validation_error'] = str(error)
                evidence['runs'].append(row)
                receipt.write_text(json.dumps(evidence, indent=2) + '\n')
                print('END', label, 'rc', result.returncode, 'seconds', round(row['elapsed_seconds'], 1), flush=True)
                if result.returncode or row.get('validation_error'):
                    raise SystemExit('fixture failed; inspect ' + str(directory / 'runner.log'))
    print('Comparison complete:', receipt, flush=True)


if __name__ == '__main__':
    main()
