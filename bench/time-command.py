#!/usr/bin/env python3
"""Run one command and record wall, CPU, and peak-RSS metrics."""
import resource
import subprocess
import sys
import time


def main() -> int:
    if len(sys.argv) < 5 or sys.argv[3] != "--":
        raise SystemExit("usage: time-command.py METRICS_FILE OUTPUT_LOG -- COMMAND [ARG ...]")
    metrics_path, output_path = sys.argv[1:3]
    command = sys.argv[4:]
    started = time.perf_counter()
    with open(output_path, "wb") as output:
        result = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, check=False)
    elapsed = time.perf_counter() - started
    usage = resource.getrusage(resource.RUSAGE_CHILDREN)
    with open(metrics_path, "w", encoding="utf-8") as metrics:
        metrics.write(f"Elapsed (wall clock) time (seconds): {elapsed:.6f}\n")
        metrics.write(f"User time (seconds): {usage.ru_utime:.6f}\n")
        metrics.write(f"System time (seconds): {usage.ru_stime:.6f}\n")
        metrics.write(f"Maximum resident set size (kbytes): {usage.ru_maxrss}\n")
        metrics.write(f"Exit status: {result.returncode}\n")
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
