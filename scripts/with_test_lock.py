#!/usr/bin/env python3
"""Serialize tests across worktrees and reject existing unlocked load generators.

Use outside sudo: python3 scripts/with_test_lock.py -- sudo <test command>.
Busy reservations exit 75; they never terminate another agent's workload.
"""
import fcntl
import os
from pathlib import Path
import subprocess
import sys

LOCK = Path('/home/shayan/.cache/super-paqet-tests/test.lock')
LOADS = {'iperf3', 'spq-bench', 'pytest', 'wrk', 'wrk2', 'ab', 'hey',
         'vegeta', 'stress', 'stress-ng', 'tcpkali'}
RUNNERS = {'netns_bench.py', 'compare_receive_paths.py', 'path_recovery_bench.py',
           'stress_links.py', 'qualify_wan.py', 'live_reload_netns_test.py',
           'systemd_netns_test.py', 'fault_checks.py', 'resume_wan.py',
           'post_backpressure.py'}


def ancestors():
    """Exclude this wrapper and its caller chain, not unrelated agent children."""
    result = set()
    pid = os.getpid()
    while pid > 1 and pid not in result:
        result.add(pid)
        try:
            fields = Path('/proc', str(pid), 'stat').read_text().rsplit(')', 1)[1].split()
            pid = int(fields[1])
        except (OSError, ValueError, IndexError):
            break
    return result


def workloads():
    """Find known test/load processes; report identities without exposing argv."""
    excluded = ancestors()
    found = []
    for proc in Path('/proc').iterdir():
        if not proc.name.isdigit() or int(proc.name) in excluded:
            continue
        try:
            comm = (proc / 'comm').read_text().strip()
            args = [x.decode(errors='replace') for x in (proc / 'cmdline').read_bytes().split(b'\0') if x]
        except OSError:
            continue
        if not args:
            continue
        executable = Path(args[0]).name
        is_load = comm in LOADS or executable in LOADS or comm.endswith('.test') or executable.endswith('.test')
        is_go = executable == 'go' and len(args) > 1 and args[1] in ('test', 'build', 'run')
        # Only the actual Python script/module matters. A wrapper's remaining
        # argv may name a test that has not started yet.
        is_python = executable.startswith('python')
        script = next((x for x in args[1:] if not x.startswith('-')), '') if is_python else ''
        is_runner = is_python and (Path(script).name in RUNNERS or script in ('pytest', 'unittest'))
        if is_load or is_go or is_runner:
            found.append((int(proc.name), comm))
    return sorted(found)


def main():
    """Reserve the machine before authentication, children and cleanup begin."""
    command = sys.argv[1:]
    if command[:1] == ['--']:
        command = command[1:]
    if not command:
        print(__doc__, file=sys.stderr)
        return 2
    LOCK.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    descriptor = os.open(LOCK, os.O_CREAT | os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as reservation:
        try:
            fcntl.flock(reservation, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print('Test reservation busy; no command started.', file=sys.stderr)
            return 75
        active = workloads()
        if active:
            print('Existing test/load processes (PID, name): ' + repr(active) + '; no command started.', file=sys.stderr)
            return 75
        namespaces = Path('/run/netns')
        leftovers = sorted(p.name for p in namespaces.glob('spq-*')) if namespaces.exists() else []
        if leftovers:
            print('Existing test namespaces: ' + repr(leftovers) + '; inspect ownership before testing.', file=sys.stderr)
            return 75
        print('Exclusive test reservation acquired.', flush=True)
        try:
            status = subprocess.call(command)
            return status if status >= 0 else 128 - status
        except OSError as error:
            print('Could not start command: ' + str(error), file=sys.stderr)
            return 127


if __name__ == '__main__':
    raise SystemExit(main())
