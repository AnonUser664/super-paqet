#!/usr/bin/env python3
"""Reject invalid observer settings before privileged link creation.

Run under with_test_lock.py even though these CLI failures create no traffic.
"""
from pathlib import Path
import subprocess
import sys
import unittest


class MetricsObserverPreflight(unittest.TestCase):
    """A malformed diagnostic cadence must not create an uncontrolled observer."""

    def test_invalid_intervals_fail_preflight(self):
        """Nonfinite, negative and out-of-range cadences fail before root checks."""
        script = Path(__file__).with_name('netns_bench.py')
        for value in ('nan', 'inf', '-1', '0.5', '61'):
            with self.subTest(interval=value):
                result = subprocess.run(
                    [sys.executable, '-B', str(script), '--metrics-interval', value],
                    capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 2)
                self.assertIn('metrics-interval must be', result.stderr)

    def test_legacy_observer_fails_preflight(self):
        """Upstream fixtures lack the enterprise metrics endpoint."""
        result = subprocess.run(
            [sys.executable, '-B', str(Path(__file__).with_name('netns_bench.py')),
             '--legacy', '--metrics-interval', '5'],
            capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 2)
        self.assertIn('metrics timeline requires enterprise', result.stderr)


if __name__ == '__main__':
    unittest.main()
