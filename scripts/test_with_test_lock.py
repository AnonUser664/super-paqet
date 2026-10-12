#!/usr/bin/env python3
"""Check actual process detection without starting network traffic or nested locks.

Run this suite through with_test_lock.py like every other repository test.
The dummy sleeps belong to this test and are stopped before its reservation ends.
"""
import subprocess
import unittest

import with_test_lock


class ArchivedWorkloadDetection(unittest.TestCase):
    """Archived helper aliases must not evade the manual-workload guard."""

    def test_known_helpers_with_custom_argv(self):
        """Observe argv[0] independently of comm using harmless owned sleeps."""
        for name in ('iperf3', 'iperf-local', 'fixes-iperf-local',
                     'spq-bench', 'fixes-spq-bench'):
            with self.subTest(name=name):
                process = subprocess.Popen([name, '30'], executable='/usr/bin/sleep')
                try:
                    self.assertIn(process.pid, [pid for pid, _ in with_test_lock.workloads()])
                finally:
                    process.terminate()
                    process.wait(timeout=3)


if __name__ == '__main__':
    unittest.main()
