"""Check durable summaries across counter resets, replay and raw-log rotation."""
import json
import pathlib
import tempfile
import unittest

import production_watch as watch


def row(utc, pid, errors, active=1, rss=1):
    """Build metadata-only observations without contacting a live service."""
    return {"utc": utc, "unit": {"MainPID": pid}, "metrics": f"super_paqet_errors_total {errors}\nsuper_paqet_active_connections {active}\n", "proc_status": f"VmRSS:\t{rss} kB\n", "fds": active + 5}


class SummaryTests(unittest.TestCase):
    """Keep full-window evidence accurate when raw history is bounded."""

    def test_process_reset_and_duplicate_replay(self):
        summary = {}
        for observation in [row("01", "a", 100, 8, 10), row("02", "a", 105, 12, 20), row("03", "b", 1, 2, 9), row("04", "b", 4, 4, 8)]:
            watch.update_summary(summary, observation)
        watch.update_summary(summary, row("02", "a", 105, 12, 20))
        self.assertEqual(summary["counter_deltas"]["errors_total"], 8)
        self.assertEqual(summary["samples"], 4)
        self.assertEqual(summary["peak_active"], 12)
        self.assertEqual(summary["peak_rss_kib"], 20)
        self.assertEqual(summary["observed_processes"], 2)

    def test_summary_survives_raw_history_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            first = row("01", "a", 100, 8, 20)
            (root / "samples.jsonl").write_text(json.dumps(first) + "\n")
            summary = watch.load_summary(root)
            watch.write_json_atomic(root / "summary.json", summary)
            (root / "samples.jsonl").unlink()
            resumed = watch.load_summary(root)
            watch.update_summary(resumed, row("02", "a", 108, 2, 5))
            self.assertEqual(resumed["counter_deltas"]["errors_total"], 8)
            self.assertEqual(resumed["peak_rss_kib"], 20)
            self.assertEqual(resumed["first_utc"], "01")

    def test_run_details_remain_bounded_without_losing_global_totals(self):
        summary = {}
        for index in range(40):
            pid = str(index)
            watch.update_summary(summary, row(f"{index:03}a", pid, 0))
            watch.update_summary(summary, row(f"{index:03}b", pid, 2))
        self.assertEqual(len(summary["runs"]), 32)
        self.assertEqual(summary["evicted_runs"], 8)
        self.assertEqual(summary["counter_deltas"]["errors_total"], 80)


if __name__ == "__main__":
    unittest.main()
