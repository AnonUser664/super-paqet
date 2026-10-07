#!/usr/bin/env python3
"""Collect bounded, finite production diagnostics without generating customer traffic.

Run separately on each host so samples survive SSH loss. Capture ten-second
counter/resource snapshots and incident journals; optional loopback Go profiles
require profiling to be explicitly enabled in the tunnel configuration. No
customer configurations or payloads are collected; metrics include configured
peer endpoints.
"""
import argparse
import datetime
import json
import logging
import logging.handlers
import os
import pathlib
import subprocess
import time
import urllib.error
import urllib.request


def command(args):
    """Bound subprocess time and output; tolerate older kernels and vanished PIDs."""
    try:
        result = subprocess.run(args, capture_output=True, text=True, timeout=3)
        return {"rc": result.returncode, "out": result.stdout[-131072:], "err": result.stderr[-1024:]}
    except (OSError, subprocess.TimeoutExpired) as error:
        return {"error": str(error)}


def read(path):
    """Read resource metadata without failing the observer on process exit."""
    try:
        return pathlib.Path(path).read_text()[:131072]
    except OSError:
        return ""


def snapshot():
    """Fetch bounded local metrics and process/cgroup/host counters."""
    row = {"utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "monotonic": time.monotonic()}
    result = command(["systemctl", "show", "super-paqet", "-p", "MainPID,NRestarts,ActiveState,Result,ControlGroup"])
    row["unit"] = dict(line.split("=", 1) for line in result.get("out", "").splitlines() if "=" in line)
    base = pathlib.Path("/proc") / row["unit"].get("MainPID", "0")
    row["proc_status"] = read(base / "status")
    row["proc_stat"] = read(base / "stat")
    try:
        row["fds"] = sum(1 for _ in (base / "fd").iterdir())
    except OSError:
        row["fds"] = None
    group = pathlib.Path("/sys/fs/cgroup") / row["unit"].get("ControlGroup", "").lstrip("/")
    row["cgroup"] = {name: read(group / name) for name in ["memory.events", "memory.current", "cpu.stat", "pids.events"]}
    row["host"] = {name: read("/proc/" + name) for name in ["loadavg", "meminfo", "net/dev", "pressure/cpu", "pressure/memory"]}
    try:
        with urllib.request.urlopen("http://127.0.0.1:29090/metrics", timeout=3) as response:
            row["metrics"] = response.read(262144).decode()
    except (OSError, urllib.error.URLError) as error:
        row["metrics_error"] = str(error)
    return row


def counters(row):
    """Extract unlabelled totals for within-process incident comparisons."""
    result = {}
    for line in row.get("metrics", "").splitlines():
        fields = line.split()
        if len(fields) == 2 and "{" not in fields[0]:
            try:
                result[fields[0]] = float(fields[1])
            except ValueError:
                pass
    return result


def incident(row, previous):
    """Detect resets separately from growing failure counters."""
    reasons = []
    if "metrics_error" in row:
        reasons.append("metrics_unavailable")
    if row["unit"].get("ActiveState") != "active":
        reasons.append("service_inactive")
    if previous:
        if row["unit"].get("MainPID") != previous["unit"].get("MainPID"):
            reasons.append("process_changed")
        else:
            now, old = counters(row), counters(previous)
            for key in ["errors_total", "rejected_total", "config_reload_rejected_total"]:
                name = "super_paqet_" + key
                if now.get(name, 0) - old.get(name, 0) >= (10 if key == "errors_total" else 1):
                    reasons.append(key + "_increased")
    if previous:
        current_drops = {k: v for k, v in counters_with_labels(row).items() if "tx_queue_drops" in k or "capture_drops" in k}
        old_drops = counters_with_labels(previous)
        if any(v - old_drops.get(k, v) >= 10 for k, v in current_drops.items()):
            reasons.append("packet_drops_increased")
    return reasons


def counters_with_labels(row):
    """Retain endpoint labels when comparing packet-driver counters."""
    result = {}
    for line in row.get("metrics", "").splitlines():
        fields = line.rsplit(" ", 1)
        if len(fields) == 2 and fields[0].startswith("super_paqet_"):
            try:
                result[fields[0]] = float(fields[1])
            except ValueError:
                pass
    return result


def capture(directory, count):
    """Rotate twelve profile slots; later incidents retain capture coverage.

    Disabled profiling produces no artifact. Remove each old slot artifact first
    so a failed request cannot make a previous dump look like the new incident.
    """
    artifacts = {}
    # Aggregated stacks remain complete when a detailed many-thousand-stream
    # dump hits the byte ceiling. Capture them first, before CPU sampling delays.
    for name, endpoint, timeout in [("goroutines-summary.txt", "goroutine?debug=1", 4), ("goroutines.txt", "goroutine?debug=2", 4), ("cpu.pprof", "profile?seconds=5", 8)]:
        target = directory / (str(count % 12) + "-" + name)
        target.unlink(missing_ok=True)
        try:
            with urllib.request.urlopen("http://127.0.0.1:29090/debug/pprof/" + endpoint, timeout=timeout) as response:
                data = response.read(4 * 1024 * 1024)
            target.write_bytes(data)
            target.chmod(0o600)
            artifacts[name] = len(data)
        except (OSError, urllib.error.URLError):
            pass
    return artifacts


def update_summary(summary, row):
    """Retain full-window peaks/counter deltas even after raw samples rotate.

    Process-local counters are compared only within one PID. The bounded run
    list keeps recent process detail while global deltas/peaks survive eviction.
    A timestamp guard makes startup replay and observer restarts idempotent.
    """
    utc = row.get("utc", "")
    if "metrics" not in row or utc <= summary.get("last_utc", ""):
        return
    summary.setdefault("first_utc", utc)
    summary["last_utc"] = utc
    summary["samples"] = summary.get("samples", 0) + 1
    values = counters(row)
    status = dict(line.split(":", 1) for line in row.get("proc_status", "").splitlines() if ":" in line)
    rss = int(status.get("VmRSS", "0").strip().split()[0])
    for name, value in [("active", values.get("super_paqet_active_connections", 0)), ("rss_kib", rss), ("fds", row.get("fds") or 0)]:
        key = "peak_" + name
        summary[key] = max(summary.get(key, 0), value)
    pid = row.get("unit", {}).get("MainPID", "0")
    runs = summary.setdefault("runs", {})
    if pid not in runs:
        if len(runs) >= 32:
            del runs[next(iter(runs))]
            summary["evicted_runs"] = summary.get("evicted_runs", 0) + 1
        runs[pid] = {"first_utc": utc, "last_counters": {}}
        summary["observed_processes"] = summary.get("observed_processes", 0) + 1
    run = runs[pid]
    run["last_utc"] = utc
    for suffix in ["errors_total", "rejected_total", "opening_transport_retries_total", "config_reload_rejected_total"]:
        value = values.get("super_paqet_" + suffix)
        if value is None:
            continue
        previous = run["last_counters"].get(suffix)
        if previous is not None:
            deltas = summary.setdefault("counter_deltas", {})
            deltas[suffix] = deltas.get(suffix, 0) + max(0, value - previous)
        run["last_counters"][suffix] = value
    triggers = summary.setdefault("triggers", {})
    for reason in row.get("triggers", []):
        triggers[reason] = triggers.get(reason, 0) + 1


def load_summary(directory):
    """Resume durable aggregates, or seed them from the retained raw history."""
    try:
        return json.loads((directory / "summary.json").read_text())
    except (OSError, ValueError):
        summary = {}
        for name in ["samples.jsonl.3", "samples.jsonl.2", "samples.jsonl.1", "samples.jsonl"]:
            try:
                with (directory / name).open() as source:
                    for line in source:
                        try:
                            update_summary(summary, json.loads(line))
                        except (ValueError, TypeError, KeyError):
                            continue
            except OSError:
                pass
        return summary


def write_json_atomic(path, value):
    """Publish a complete private snapshot to independent readers."""
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value))
    os.replace(temporary, path)


def main():
    """Expire after a day and rotate output to bound observer overhead."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", default="/var/log/super-paqet-watch")
    parser.add_argument("--duration", type=float, default=86400)
    parser.add_argument("--interval", type=float, default=10)
    args = parser.parse_args()
    if args.duration <= 0 or args.interval < 5:
        parser.error("duration must be positive and interval at least five seconds")
    os.umask(0o077)
    directory = pathlib.Path(args.directory)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    logger = logging.getLogger("production-watch")
    logger.setLevel(logging.INFO)
    handler = logging.handlers.RotatingFileHandler(directory / "samples.jsonl", maxBytes=16 * 1024 * 1024, backupCount=3)
    handler.setFormatter(logging.Formatter("%(message)s"))
    logger.addHandler(handler)
    summary = load_summary(directory)
    summary["completed"] = False
    end, previous, last_capture = time.monotonic() + args.duration, None, -float("inf")
    captures = summary.get("captures", 0)
    while time.monotonic() < end:
        start = time.monotonic()
        try:
            row = snapshot()
            reasons = incident(row, previous)
            row["triggers"] = reasons
            if reasons and start - last_capture >= 300:
                row["journal"] = command(["journalctl", "-u", "super-paqet", "--since", "-5min", "-n", "120", "--no-pager", "-o", "cat"])
                row["kernel"] = command(["journalctl", "-k", "-p", "warning", "--since", "-5min", "-n", "50", "--no-pager", "-o", "cat"])
                # Injection drops may come from a shared qdisc rather than CPU
                # or NIC capacity. Retain bounded kernel/queue metadata at the
                # incident, without inspecting application payloads or changing
                # queue policy. Missing tools remain diagnostic errors only.
                row["qdisc"] = command(["tc", "-s", "qdisc", "show"])
                row["sockets"] = command(["ss", "-s"])
                row["netstat"] = read("/proc/net/netstat")
                row["softnet"] = read("/proc/net/softnet_stat")
                last_capture = start
                artifacts = capture(directory, captures)
                write_json_atomic(directory / ("capture-" + str(captures % 12) + ".json"), {"utc": row["utc"], "pid": row["unit"].get("MainPID"), "sequence": captures, "reasons": reasons, "artifacts": artifacts})
                captures += 1
                summary["captures"] = captures
            logger.info(json.dumps(row, separators=(",", ":")))
            write_json_atomic(directory / "latest.json", row)
            update_summary(summary, row)
            write_json_atomic(directory / "summary.json", summary)
            previous = row
        except Exception as error:
            logger.info(json.dumps({"observer_error": str(error), "utc": datetime.datetime.now(datetime.timezone.utc).isoformat()}))
        time.sleep(max(0, min(args.interval - (time.monotonic() - start), end - time.monotonic())))
    logger.info(json.dumps({"observer_completed": True, "duration_seconds": args.duration}))
    summary["completed"] = True
    write_json_atomic(directory / "summary.json", summary)


if __name__ == "__main__":
    main()
