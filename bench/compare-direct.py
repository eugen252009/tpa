#!/usr/bin/env python3
"""End-to-end metadata-only benchmark; requires Linux /proc, Go, dpkg and gzip.
No signing keys, publishing, strace, installation, or production paths are used.
Peak summed RSS is sampled (shared pages may be counted more than once).
wait4 ru_maxrss is also retained; it is not a summed process-tree high-water mark.
"""
import argparse
import csv
import json
import os
import pathlib
import platform
import statistics
import subprocess
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]


def rss_tree(pid):
    total = 0
    try:
        for line in pathlib.Path(f"/proc/{pid}/status").read_text().splitlines():
            if line.startswith("VmRSS:"):
                total += int(line.split()[1])
        children = pathlib.Path(f"/proc/{pid}/task/{pid}/children").read_text().split()
        for child in children:
            total += rss_tree(int(child))
    except (FileNotFoundError, ProcessLookupError, PermissionError):
        pass
    return total


def measure(command, env, log):
    start = time.monotonic()
    peak = 0
    with log.open("wb") as stream:
        proc = subprocess.Popen(command, stdout=stream, stderr=stream, env=env)
        while True:
            peak = max(peak, rss_tree(proc.pid))
            done, status, usage = os.wait4(proc.pid, os.WNOHANG)
            if done:
                proc.returncode = os.waitstatus_to_exitcode(status)
                break
            time.sleep(0.01)
    elapsed = time.monotonic() - start
    if proc.returncode:
        raise RuntimeError(f"benchmark failed ({proc.returncode}): {log.read_text()[-4000:]}")
    result = json.loads(log.read_text().splitlines()[-1])
    result.update(wall_seconds=elapsed, peak_summed_rss_kib=peak, wait4_maxrss_kib=usage.ru_maxrss)
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--counts", default="100,1000,10000")
    parser.add_argument("--repetitions", type=int, default=3)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOMAXPROCS="8", SOURCE_DATE_EPOCH="0", DPKG_DEB_THREADS_MAX="1")
    with tempfile.TemporaryDirectory(prefix="tpa-direct-benchmark-") as tmp:
        tmp = pathlib.Path(tmp)
        binary = tmp / "harness"
        subprocess.run(["go", "build", "-o", str(binary), "./bench/direct-pipeline"], cwd=ROOT, env=env, check=True)
        results = []
        for count in map(int, args.counts.split(",")):
            source = tmp / f"definitions-{count}.jsonl"
            with source.open("w") as stream:
                for n in range(count):
                    script = "#!/bin/sh\nset -e\n"
                    record = {"control": {"name": f"meta-{n:05d}", "version": "1.0", "architecture": "all", "maintainer": "Fixture <fixture@example.invalid>", "description": "Metadata-only comparative benchmark", "depends": "dpkg (>= 1.0)"}, "provenance": False, "scripts": {key: script for key in ("preinst", "postinst", "prerm", "postrm")}}
                    stream.write(json.dumps(record, separators=(",", ":")) + "\n")
            for trial in range(args.repetitions):
                # Alternate run order to reduce systematic cache/order bias.
                modes = ("existing", "direct") if trial % 2 == 0 else ("direct", "existing")
                for mode in modes:
                    with tempfile.TemporaryDirectory(prefix=f"{mode}-", dir=tmp) as run:
                        result = measure([str(binary), "-mode", mode, "-in", str(source), "-out", str(pathlib.Path(run) / "repo"), "-workers", "8"], env, args.output / f"{count}-{trial}-{mode}.log")
                    result["trial"] = trial + 1
                    results.append(result)
                    (args.output / "measurements.json").write_text(json.dumps(results, indent=2) + "\n")
                    print(json.dumps(result), flush=True)
        with (args.output / "measurements.csv").open("w") as stream:
            writer = csv.DictWriter(stream, fieldnames=list(results[0]))
            writer.writeheader()
            writer.writerows(results)
        metadata = {"platform": platform.platform(), "cpu_count": os.cpu_count(), "go": subprocess.check_output(["go", "version"], text=True).strip(), "dpkg_deb": subprocess.check_output(["dpkg-deb", "--version"], text=True).splitlines()[0], "workers": 8, "memory_budget_mib": 128, "source_date_epoch": 0, "sampling_interval_seconds": 0.01, "filesystem_syscall_counts": "not measured (strace unavailable); package_trees and dpkg_deb_processes are explicit path counts", "signing": "unsigned for both; signed compatibility tested separately", "compression": "existing dpkg defaults (xz); direct Go gzip; identical metadata and four scripts, no payload"}
        (args.output / "environment.json").write_text(json.dumps(metadata, indent=2) + "\n")
        for count in map(int, args.counts.split(",")):
            medians = {mode: statistics.median(r["wall_seconds"] for r in results if r["packages"] == count and r["mode"] == mode) for mode in ("existing", "direct")}
            print(count, medians, "existing/direct ratio", medians["existing"] / medians["direct"])


if __name__ == "__main__":
    main()
