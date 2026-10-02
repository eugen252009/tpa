# TPA benchmark methodology and current implementation

This directory contains benchmark harnesses and retained reports for TPA. The
current implementation baseline is TPA commit
`078b62977e3cdec78fc84191172780f73468750f` (Phase B). The authoritative
post-Phase-B report is [`REPORT.md`](REPORT.md); Phase A and Phase B reports
retain their original measurements and are explicitly historical where they
predate this baseline.

## Current Pack and control-reader path

`Pack` reads top-level `.deb` artifacts in bounded workers, preserves input-order
processing with a `2*workers` inspection window, hashes source bytes while
copying them into the pool, writes deterministic indexes (`gzip -n`), and
independently verifies the published package files and index hashes. Package
verification and pool copies also use bounded workers. The default worker count
is `GOMAXPROCS`, capped at 32; `-workers=1..32` overrides it. Stage timers are
benchmark-only (`-tags=tpa_bench`), and parallel stage durations are aggregate
worker time, not wall-clock time.

The control reader handles plain `control.tar`, `control.tar.gz`, and
`control.tar.xz` in process. Typed unsupported formats use a bounded
`dpkg-deb -f` fallback; malformed supported formats fail directly. The normal
10k corpus uses xz and does not invoke the fallback. `dpkg-deb` remains required
for package construction and unsupported-format fallback.

Generation manifests are incrementally written and bounded to 16 MiB and
65,536 files. The inventory and verification maps still scale with the file
count; TPA does not claim constant memory as repositories grow.

## Current 10,000-package evidence

Fresh results were collected on the exact Phase B commit in
`post-phaseB-results/20261002T175545Z/`. The container was Debian 13 (trixie),
Linux `6.12.107+deb13-amd64`, dpkg 1.22.22, APT 3.0.3, and GPG 2.4.7. The host
was an AMD Ryzen 7 5800X (8 cores/16 logical CPUs), Go 1.26.4, Docker 29.5.2,
with the workspace on an ext4 NVMe bind mount. Full metadata is retained in
`environment-host.txt` and `environment-container.txt`.

The control-reader oracle compared all 10,000 packages with `dpkg-deb -f`:
4.128 s direct versus 34.589 s oracle (8.38x), zero mismatches. This sequential
oracle test is separate from Pack timing.

The signed Pack worker sweep used benchmark-only instrumentation, complete
generation manifests, and three trials at every worker count:

| Workers | Median wall | Median CPU (user+sys) | Median peak RSS | Direct reads | Fallbacks |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 5.330 s | 6.740 s | 64.7 MiB | 10,000 | 0 |
| 2 | 2.940 s | 6.880 s | 61.8 MiB | 10,000 | 0 |
| 4 | 1.830 s | 7.500 s | 62.1 MiB | 10,000 | 0 |
| 8 | 1.340 s | 8.460 s | 68.5 MiB | 10,000 | 0 |
| 16 | 1.260 s | 8.630 s | 75.4 MiB | 10,000 | 0 |

Eight workers is the practical throughput knee for this run: 16 workers saved
6.0% median wall time over 8 while CPU rose 2.0% and peak RSS 10.1%. This is
workload-specific evidence, not a change to the default worker policy. The
standard, uninstrumented repository-comparison run used TPA's default worker
count and measured initial generation at 1.110 s; it is a distinct workload and
must not be compared directly with the explicitly one-worker or instrumented
sweep runs.

Repository comparison medians were:

| Scenario | TPA | aptly | reprepro |
| --- | ---: | ---: | ---: |
| Initial generation | 1.110 s | 339.170 s | 16.390 s |
| Update | 1.160 s | 4.710 s | 0.290 s |

These update operations are **not equivalent**. aptly retains old package
versions (10,100 indexed entries after the update), while TPA and reprepro
reported 10,000. TPA creates the fresh snapshot defined by its input set; do not
present these update timings as like-for-like replacements.

The benchmark-only concurrent `aptpackage.Build` orchestration sweep measured
1/2/4/8/16/32 workers at 85.38/36.34/19.58/11.33/12.62/9.55 s. Only the 1- and
32-worker endpoints had three trials; intermediate worker counts were single
observations. Artifact hashes matched across runs. These observations do not
establish a precise optimum and do not imply that an individual `tpa build`
command internally builds multiple packages concurrently.

Correctness and qualification: `Packages` and `Packages.gz` matched across the
worker sweep; the 10,004-path signed manifest verified; signed APT update and
package download, signed install/upgrade/downgrade, and dependency resolution
passed. The unsigned repository correctly contains 10,003 files because it has
no `InRelease`; `bench/validate-results.sh` nevertheless expects 10,004 paths
for that unsigned inventory and exits nonzero at that final assertion. The
unsigned inventory was independently checked as 10,003 unique paths matching
the repository. The validator and benchmark results were not changed to conceal
this mismatch.

## Reproducing measurements safely

The qualified comparison harness is fixed to 10,000 packages and 100 updates:

```sh
./bench/run.sh 10000 100
```

It requires Docker and installs aptly, reprepro, GnuPG, GNU `time`, `strace`,
and Python in a disposable Debian container. **Run it in a disposable clean
checkout or worktree:** it deletes and regenerates `bench/corpus/`, writes
`bench/host.txt`, and places new timestamped results under `bench/results/`.
Copy or otherwise preserve any corpus/results you need before running it. The
historical `bench/host.txt` is original baseline metadata; the fresh environment
records belong with the timestamped result directory.

The reader/worker sweep uses a `tpa_bench` build and disposable signing key;
`direct-reader-sweep.sh` runs three trials at each of 1/2/4/8/16 workers. The
package-build sweep script similarly records the 1/2/4/8/16/32 worker matrix.
Raw post-Phase-B inputs, logs, summaries, and qualification records are retained
under `post-phaseB-results/20261002T175545Z/`. Never overwrite historical
result directories or compare results from different environments without
noting the distinction.

## Historical reports

- [`OPTIMIZATION_REPORT.md`](OPTIMIZATION_REPORT.md) preserves Phase A results
  and contrasts them with the later in-process reader.
- [`PHASE_B_DIRECT_READER_REPORT.md`](PHASE_B_DIRECT_READER_REPORT.md) preserves
  Phase B design and qualification observations; its original uncommitted-state
  note has since been superseded by the post-commit measurements above.
- [`REPORT.md`](REPORT.md) includes the original pre-optimization baseline as a
  clearly labeled historical snapshot, alongside the current post-Phase-B
  measurements.
