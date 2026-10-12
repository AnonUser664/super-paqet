#!/usr/bin/env python3
"""Check actual process detection without starting network traffic or nested locks.

Run this suite through with_test_lock.py like every other repository test.
The dummy sleeps belong to this test and are stopped before its reservation ends.
"""
import subprocess
import unittest
from unittest import mock

import with_test_lock


class ArchivedWorkloadDetection(unittest.TestCase):
    """Archived helper aliases must not evade the manual-workload guard."""


    def test_interrupt_keeps_waiting_for_owned_cleanup(self):
        """A terminal interrupt must not kill a child's finally block."""
        child = mock.Mock()
        child.wait.side_effect = [KeyboardInterrupt(), KeyboardInterrupt(), 130]
        with mock.patch.object(with_test_lock.sys, 'stderr'):
            self.assertEqual(with_test_lock.wait_for_owned_child(child), 130)
        self.assertEqual(child.wait.call_count, 3)
        child.kill.assert_not_called()
        child.terminate.assert_not_called()

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
