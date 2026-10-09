"""Check durable summaries across counter resets, replay and raw-log rotation."""
import json
import datetime
import os
import pathlib
import tempfile
import unittest
from unittest import mock

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

    def test_extended_rotation_replays_all_retained_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "samples.jsonl.15").write_text(json.dumps(row("01", "a", 10)) + "\n")
            (root / "samples.jsonl").write_text(json.dumps(row("02", "a", 13)) + "\n")
            self.assertEqual(watch.load_summary(root, 15)["counter_deltas"]["errors_total"], 3)

    def test_recovery_counter_summary_does_not_count_process_reset(self):
        summary = {}
        for utc, pid, value in [("01", "a", 8), ("02", "a", 10), ("03", "b", 0), ("04", "b", 1)]:
            sample = row(utc, pid, 0)
            sample["metrics"] += f"super_paqet_path_recovery_attempts_total {value}\n"
            watch.update_summary(summary, sample)
        self.assertEqual(summary["counter_deltas"]["path_recovery_attempts_total"], 3)

    def test_opening_retry_signals_survive_reset_and_older_metrics(self):
        """New opening counters remain optional on legacy runtimes and reset by PID."""
        summary = {}
        previous = None
        reasons = []
        for utc, pid, value in [("01", "a", None), ("02", "a", 8),
                                ("03", "a", 10), ("04", "b", 0), ("05", "b", 1)]:
            sample = row(utc, pid, 0)
            sample["unit"]["ActiveState"] = "active"
            if value is not None:
                for key in ("opening_capacity_retries_total", "opening_reused_targets_total"):
                    sample["metrics"] += f"super_paqet_{key} {value}\n"
            reasons.append(watch.incident(sample, previous))
            watch.update_summary(summary, sample)
            previous = sample
        for key in ("opening_capacity_retries_total", "opening_reused_targets_total"):
            self.assertEqual(summary["counter_deltas"][key], 3)
            self.assertIn(key + "_increased", reasons[2])
        self.assertEqual(reasons[1], [])
        self.assertEqual(reasons[3], ["process_changed"])

    def test_metric_outage_is_retained_without_manufacturing_counter_changes(self):
        summary = {}
        before, after = row("01", "a", 100), row("03", "a", 101)
        outage = {"utc": "02", "unit": {"MainPID": "a", "ActiveState": "active"},
                  "metrics_error": "timeout", "triggers": ["metrics_unavailable"]}
        before["unit"]["ActiveState"] = after["unit"]["ActiveState"] = "active"
        for sample in [before, outage, outage, after]:
            watch.update_summary(summary, sample)
        self.assertEqual(summary["samples"], 3)
        self.assertEqual(summary["samples_without_metrics"], 1)
        self.assertEqual(summary["triggers"]["metrics_unavailable"], 1)
        self.assertEqual(summary["counter_deltas"]["errors_total"], 1)
        self.assertEqual(watch.incident(after, outage), [])


class RecoveryTests(unittest.TestCase):
    """Identify recovery without interpreting a restart or a new slot as filtering."""

    def test_tuple_and_suspect_transitions_require_same_process(self):
        before, after = row("01", "a", 0), row("02", "a", 0)
        for sample, port, suspect in [(before, 1000, 0), (after, 2000, 1)]:
            sample["unit"]["ActiveState"] = "active"
            sample["metrics"] += (f'super_paqet_peer_source_port{{peer="france",session="0"}} {port}\n'
                                  f'super_paqet_peer_carrier_suspect{{peer="france",session="0"}} {suspect}\n')
        self.assertEqual(watch.incident(after, before), ["source_port_changed", "carrier_became_suspect"])
        after["unit"]["MainPID"] = "b"
        self.assertEqual(watch.incident(after, before), ["process_changed"])
        self.assertEqual(watch.incident(after, {**before, "metrics": ""}), ["process_changed"])

    def test_fixed_deadline_does_not_extend_after_restart(self):
        start = datetime.datetime(2026, 10, 7, tzinfo=datetime.timezone.utc)
        deadline = "2026-10-08T00:00:00Z"
        self.assertEqual(watch.remaining_seconds(86400, deadline, start), 86400)
        self.assertEqual(watch.remaining_seconds(86400, deadline, start + datetime.timedelta(hours=23)), 3600)
        self.assertEqual(watch.remaining_seconds(86400, deadline, start + datetime.timedelta(days=2)), 0)
        with self.assertRaises(ValueError):
            watch.remaining_seconds(86400, "2026-10-08T00:00:00", start)


class PipeProcess:
    """Expose a real nonblocking pipe while replacing only journalctl itself."""

    def __init__(self):
        read_fd, self.write_fd = os.pipe()
        self.stdout = os.fdopen(read_fd, "rb")
        self.returncode = None

    def poll(self):
        return self.returncode

    def terminate(self):
        self.returncode = 0
        os.close(self.write_fd)

    def wait(self, timeout):
        return self.returncode


class JournalTests(unittest.TestCase):
    """Keep bounded journal spools resumable without running a live service."""

    def record(self, cursor="c1"):
        return json.dumps({"__CURSOR": cursor, "__REALTIME_TIMESTAMP": "1791396000000000",
                           "MESSAGE": "path.recovered", "PRIORITY": "4", "UNRELATED": "discard"}).encode() + b"\n"

    def test_partial_record_and_cursor_resume(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            process = PipeProcess()
            with mock.patch.object(watch.subprocess, "Popen", return_value=process) as spawn:
                recorder = watch.JournalRecorder(root, "2026-10-07 18:28:00 UTC")
                record = self.record()
                os.write(process.write_fd, record[:20])
                self.assertEqual(recorder.drain()["records"], 0)
                self.assertFalse((root / "journal-state.json").exists())
                os.write(process.write_fd, record[20:])
                self.assertEqual(recorder.drain()["records"], 1)
                saved = json.loads((root / "tunnel.journal.jsonl").read_text())
                self.assertNotIn("UNRELATED", saved)
                self.assertEqual(saved["MESSAGE"], "path.recovered")
                recorder.close()
            replacement = PipeProcess()
            with mock.patch.object(watch.subprocess, "Popen", return_value=replacement) as spawn:
                resumed = watch.JournalRecorder(root, "different start")
                self.assertIn("--after-cursor=c1", spawn.call_args.args[0])
                self.assertEqual(resumed.drain()["records"], 1)
                resumed.close()

    def test_vacuumed_cursor_falls_back_to_saved_timestamp(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "journal-state.json").write_text(json.dumps({"cursor": "gone", "realtime_usec": "1000000"}))
            first, second = PipeProcess(), PipeProcess()
            with mock.patch.object(watch.subprocess, "Popen", side_effect=[first, second]) as spawn:
                recorder = watch.JournalRecorder(root, "start")
                first.terminate()
                first.returncode = 1
                self.assertIn("error", recorder.drain())
                self.assertIn("--since=@1.0", spawn.call_args.args[0])
                recorder.close()

    def test_failed_write_does_not_advance_cursor(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            first, second = PipeProcess(), PipeProcess()
            with mock.patch.object(watch.subprocess, "Popen", side_effect=[first, second]) as spawn:
                recorder = watch.JournalRecorder(root, "start")
                os.write(first.write_fd, self.record())
                with mock.patch.object(recorder.handler, "emit", side_effect=OSError("disk full")):
                    self.assertIn("disk full", recorder.drain()["error"])
                self.assertNotIn("cursor", recorder.state)
                self.assertIn("--since=start", spawn.call_args.args[0])
                os.write(second.write_fd, self.record())
                self.assertEqual(recorder.drain()["records"], 1)
                recorder.close()

    def test_journal_rotation_retains_latest_cursor(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            process = PipeProcess()
            with mock.patch.object(watch.subprocess, "Popen", return_value=process):
                recorder = watch.JournalRecorder(root, "start")
                recorder.handler.maxBytes, recorder.handler.backupCount = 180, 2
                for index in range(10):
                    os.write(process.write_fd, self.record("c" + str(index)))
                    recorder.drain()
                self.assertLessEqual(len(list(root.glob("tunnel.journal.jsonl*"))), 3)
                self.assertEqual(json.loads((root / "journal-state.json").read_text())["cursor"], "c9")
                self.assertEqual(json.loads((root / "tunnel.journal.jsonl").read_text())["__CURSOR"], "c9")
                recorder.close()


if __name__ == "__main__":
    unittest.main()
