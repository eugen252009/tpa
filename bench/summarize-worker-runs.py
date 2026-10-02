#!/usr/bin/env python3
import csv
import statistics
import sys
from pathlib import Path

root = Path(sys.argv[1])

def seconds(value: str) -> float:
    pieces = value.split(":")
    total = 0.0
    for piece in pieces:
        total = total * 60 + float(piece)
    return total

def metric(path: Path, label: str) -> str:
    for line in path.read_text().splitlines():
        line = line.strip()
        if line.startswith(label) and ": " in line:
            return line.rsplit(": ", 1)[1].strip()
    raise SystemExit(f"missing {label} in {path}")

rows = []
for workers in (1, 2, 4, 8, 16):
    trials = [1, 2, 3] if workers in (1, 8, 16) else [1]
    for trial in trials:
        directory = root / (f"workers-{workers}" if trial == 1 else f"trial-{trial}/workers-{workers}")
        timing = directory / "time.txt"
        stages = directory / "stages.json"
        import json
        stage_data = json.loads(stages.read_text())
        rows.append({
            "workers": workers,
            "trial": trial,
            "elapsed_s": seconds(metric(timing, "Elapsed (wall clock) time")),
            "user_s": float(metric(timing, "User time (seconds)")),
            "system_s": float(metric(timing, "System time (seconds)")),
            "maxrss_kb": int(metric(timing, "Maximum resident set size (kbytes)")),
            "inspection_process_ns": stage_data["package_control_extraction"]["duration_ns"],
            "pool_copy_ns": stage_data["pool_copy"]["duration_ns"],
        })

summary = Path(sys.argv[2]) if len(sys.argv) > 2 else root / "summary.csv"
with summary.open("w", newline="") as file:
    writer = csv.DictWriter(file, fieldnames=rows[0].keys())
    writer.writeheader()
    writer.writerows(rows)

base = statistics.median(row["elapsed_s"] for row in rows if row["workers"] == 1)
print("workers trials median_wall_s speedup median_cpu_s median_maxrss_MiB")
for workers in (1, 2, 4, 8, 16):
    group = [row for row in rows if row["workers"] == workers]
    wall = statistics.median(row["elapsed_s"] for row in group)
    cpu = statistics.median(row["user_s"] + row["system_s"] for row in group)
    rss = statistics.median(row["maxrss_kb"] for row in group) / 1024
    print(f"{workers:7d} {len(group):6d} {wall:13.2f} {base / wall:7.2f} {cpu:13.2f} {rss:17.1f}")
