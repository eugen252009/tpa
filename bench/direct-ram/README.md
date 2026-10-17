# Direct-RAM: metadata-only Debian package generation

**Current accepted measurement: 50,000 metadata-only packages, three trials per path.**
TPA built and verified the packages and their repository in a median
**22.248 seconds**, versus **133.624 seconds** through directory staging:
**6.01×** faster in this measured workload. All six individual measurements,
archive-validation results and environment records are public below. Earlier
historical results remain separately documented, without rewriting their values.

## What is Direct-RAM?

` tpa compose ` reads declarative JSON, JSONL or a directory of definitions and
builds Debian control/script archives with bounded worker scratch, writing the
final `.deb` files directly into a new repository pool. It avoids per-package
staging directories, per-package `dpkg-deb` processes and a second pool-copy pass.
It still writes package/repository files, hashes their actual bytes, generates
indexes and browser metadata, and independently verifies the finished repository.
**“Direct-RAM” does not mean the entire operation is diskless or its RSS is capped.**

This optional path is for **metadata-only packages**. Normal applications with
payload trees continue to use the first-class `tpa init` → `build` → `pack` workflow.
Both paths build locally; optional signing is local. TPA.run is not part of this
benchmark and remains a receiver/host for completed repositories.

Implementation: [`direct.go`](../../internals/aptpackage/direct.go),
[`definitions.go`](../../internals/aptpackage/definitions.go),
[`input and resource contract`](../../docs/direct-compose.md).
This is **not** the separate in-process Debian control-reader benchmark.

## Current 50,000-package result (2026-10-17)

Measured clean source: **`ba7b5095935e00874738833fa06f8462b6f46854`**.
The implementation and benchmark code are unchanged by this results publication.

| Pipeline | Trial 1 s | Trial 2 s | Trial 3 s | Median s |
|---|---:|---:|---:|---:|
| Conventional | 134.552 | 133.624 | 133.530 | **133.624** |
| Direct-RAM | 22.248 | 22.612 | 21.608 | **22.248** |

The speedup is the ratio of unrounded medians: 6.006089…, rounded to **6.01×**.
All six successful runs validated all 50,000 generated archives. Their semantic
digests match: `cc2c404fab05b1395eb55b6cac236f63c5cb89624dd33a905dbe50a15baed136`.
Control fields, dependencies, architecture, script bodies/modes, root ownership
and empty payload were checked independently; deterministic first/middle/last
archives also passed dpkg-deb inspection. Both timed paths verify the repository.

> **Measurement note:** The benchmark was conducted on a workstation with concurrent background activity. A CPU-intensive `ccc` process was observed, and recorded system load ranged from 4.3 to 11.3. Results reflect the measured environment and should not be interpreted as fully isolated performance measurements.

Recorded environment: AMD Ryzen 7 5800X (8 cores/16 logical CPUs), Debian 13.7,
Linux 6.12.111, glibc 2.41, Go 1.26.4, dpkg-deb 1.22.22, tmpfs `/tmp`.
Four workers and GOMAXPROCS=4, DPKG_DEB_THREADS_MAX=1, SOURCE_DATE_EPOCH=0,
128 MiB direct-worker scratch budget. Recorded MemTotal was 65,203,024 KiB and
MemAvailable at start was 34,106,096 KiB. This is a Go API harness build (`dev`),
not a rebuilt release package. Source and input hashes are retained.

The input generator, metadata, four scripts and conventional/direct paths are
as described below, scaled to 50,000 packages. Three repetitions alternate path
order. Signing is excluded from both timings. Compilation, corpus generation,
final run-directory cleanup and the additional archive oracle are outside timing;
repository generation/verification and conventional intermediate cleanup are inside.

| Median metric | Conventional | Direct-RAM |
|---|---:|---:|
| Sampled summed process RSS, KiB | 394,460 | 416,908 |
| wait4 largest-process maxrss, KiB | 423,424 | 449,980 |
| Sampled allocated workspace bytes | 483,594,240 | 278,827,008 |
| Total package bytes | 37,217,424 | 28,045,404 |
| Complete repository bytes | 111,204,524 | 102,061,675 |

Direct is not lower-RSS in this run. Workspace sampling includes intermediates
and final output, not merely temporary overhead; it misses brief peaks and adds
traversal overhead. RSS and disk sampling limitations below apply. Compression
is conventional xz versus direct gzip: this is logical equivalence, not identical
archive bytes or compression work. No exact effect is attributed to background activity.

Public evidence: [raw JSON](results/2026-10-17-50000/measurements.json),
[CSV](results/2026-10-17-50000/measurements.csv),
[computed summary](results/2026-10-17-50000/summary.json),
[environment](results/2026-10-17-50000/environment.json),
[run-start provenance](results/2026-10-17-50000/run-start.json),
[recorded host context](results/2026-10-17-50000/host-context.json),
[checksums and per-run logs](results/2026-10-17-50000/SHA256SUMS.txt).
Host context is a compact projection of the retained monitor record, not invented
samples. The earlier large run was superseded by this requested rerun; these six
trials are the explicitly accepted publication set. No benchmark was rerun to
prepare this documentation.

## Exact historical measurement baseline

- Measured implementation: **`dc267c25958e39ce1170f8cf5b11cc97053a6a26`**.
- Raw evidence retained by `f11ccf1`, final original qualification report `32fc08f`.
- Already integrated into consolidated `2d30f6ad8b1ea733228d74825263ee9f97fb2263`.
  The direct writer itself is unchanged between the measured and consolidated revisions.
- OS: Debian13, Linux6.12.111, glibc2.41; AMD Ryzen7 5800X, 8 cores/16 logical CPUs.
- Go1.26.4; dpkg-deb1.22.22. Harness directly calls Go APIs, without an injected
  release version (`dev`); these are not measurements of the 0.8.0~rc1 DEB binary.
- Temporary corpus and output: tmpfs (`/tmp`). Eight workers/GOMAXPROCS8,
  DPKG_DEB_THREADS_MAX1; direct scratch budget128MiB; SOURCE_DATE_EPOCH0.
- Historical total/available RAM and machine load were **not recorded**. Current
  host memory must not be presented as a historical observation.
- Three repetitions per size/path, alternating path order. No cache flushing.
- Corpus generated by the script: names `meta-00000` onward, Version1.0,
  Architectureall, Depends `dpkg (>= 1.0)`, identical maintainer/description,
  four explicit `#!/bin/sh\nset -e\n` maintainer scripts, no payload, provenance off.

Conventional path: shared input reader → InitPackage → Build → Pack → verification.
Each staging tree is removed immediately after its build, rather than artificially
retaining all trees. Direct path: same reader → ComposeDefinitions → verification.
Both include repository HTML/JSON generation and final output-size accounting.
Compilation/corpus generation and final temporary-directory cleanup are outside
timing; conventional intermediate cleanup is inside its path.

Signing is excluded from **both** timed paths and independently qualified separately.
Conventional dpkg compression defaults to xz; direct uses Go gzip. Consequently
these are equivalent logical workloads, **not identical compression algorithms**;
the speedup cannot be attributed exclusively to RAM or avoiding filesystem work.

## Results: medians of three historical trials

Packages/s is count divided by median wall time; speedup is the ratio of medians.
Peak RAM columns are median sampled summed process-tree RSS, not hard limits.

| Packages | Conventional s | Direct-RAM s | Speedup | Conventional pkg/s | Direct pkg/s | Conventional peak MiB | Direct peak MiB |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 100 | 0.188 | 0.051 | 3.66× | 532 | 1,946 | 25.73 | 24.99 |
| 1,000 | 1.886 | 0.432 | 4.37× | 530 | 2,316 | 29.18 | 34.19 |
| 10,000 | 16.776 | 3.458 | 4.85× | 596 | 2,892 | 97.55 | 90.45 |

50,000 packages were not measured in that historical series; the current result
above uses a different worker count and instrumented driver and is kept separate.

| Packages | Conventional complete repository bytes | Direct complete repository bytes | Conventional wait4 maxrss MiB | Direct wait4 maxrss MiB |
|---:|---:|---:|---:|---:|
| 100 | 224,412 | 205,961 | 18.29 | 28.18 |
| 1,000 | 2,226,994 | 2,042,429 | 22.50 | 33.93 |
| 10,000 | 22,244,071 | 20,411,356 | 81.76 | 89.67 |

Sizes are **whole repositories**, including indexes/browser files, not individual
archive sizes. Historical per-DEB size summaries and temporary disk high-water
measurements were not retained. The current harness adds both; no historical values
are estimated. Historical successful-path materialization counts are exact from
the harness: **N package trees + N dpkg-deb invocations versus zero**. For the four
scripts this means 5N intermediate control/script files avoided, plus N intermediate
DEBs and their pool copies. These are structural counts, **not measured syscalls**.
Both paths still write N+5 final regular files.

RSS sampling is nominally every10ms and may miss short-lived child peaks or count
shared pages twice. wait4 maxrss is the largest-process high-water metric, not the
summed process-tree peak. Polling overhead matters particularly at100 packages.
Direct is **not consistently lower-memory**, as the 1,000-package result shows.

### Public raw evidence

- [All measurements, CSV](../direct-pipeline/measurements.csv)
- [All measurements, JSON](../direct-pipeline/measurements.json)
- [Historical environment](../direct-pipeline/environment.json)
- [Checksums](../direct-pipeline/SHA256SUMS.txt)
- [Frozen historical Python driver](../direct-pipeline/compare-direct.py.snapshot)
- [Original detailed qualification report](../DIRECT_PIPELINE_REPORT.md)

The frozen driver preserves the previously checksummed version (its sole change
from dc267c2 is CSV LF line endings). It is evidence, not a second supported entrypoint.
The checksum file now names this frozen snapshot rather than the evolved driver.
No historical measurement values were rewritten.

## Correctness, not just speed

The original qualification included root-owned ar/tar/gzip correctness, executable
script modes, field/custom-metadata preservation, dependencies, reproducibility,
JSON/JSONL/directory equivalence, worker determinism, bounds/failure cleanup and
signed repository verification. Disposable Debian APT tests verified direct and
transitive dependencies, actual script execution and install/upgrade; conventional
payload-building regressions remain supported.

Current reproduction adds [an untimed independent Python archive oracle](validate.py):
**every archive** must have equivalent control fields, the same four script bodies
and executable modes, root ownership and empty payload. A canonical semantic digest
must match between both paths and across repetitions. Boundary/middle samples also
pass independent `dpkg-deb -f` and `--contents`. Both timed paths already run TPA's
repository verification. Timestamps/compression are excluded from semantic equality;
no byte-identity claim is made. Small oracle regression tests are included.

Earlier contended attempts at 1,000/10,000 packages were interrupted/time-limited
with load around 40 on 16 logical CPUs; those attempts remain excluded. Subsequent
runs were completed, including the accepted 50,000-package rerun above. Historical
and current datasets are not pooled or presented as one identical-methodology
matrix. See [publication checks](QUALIFICATION.md).

## Reproduce without private fixtures

Linux prerequisites: Go≥1.26.3, Python3 standard library, dpkg/dpkg-deb, gzip, Git,
`findmnt` (util-linux), `/proc`; enough free RAM/tmpfs/disk. No credentials, service,
network publisher or production key required. The corpus is generated automatically;
no generated DEBs or large input datasets belong in Git.

Current harness (choose a new, absent output path; run on an otherwise quiet host):

```sh
git clone https://github.com/eugen252009/tpa.git
cd tpa
git checkout --detach ba7b5095935e00874738833fa06f8462b6f46854
export GOMAXPROCS=4 GOFLAGS=-p=4
./bench/direct-ram/bench.sh --output /tmp/tpa-direct-results \
  --counts 50000 --repetitions 3 --workers 4 --tmpdir /tmp
python3 -m unittest discover -s bench/direct-ram -p 'test_*.py'
```

The current driver bounds compilation, records Git revision/dirty state, CPU/OS/Go,
RAM/load context, corpus hashes, output/package sizes and sampled allocated workspace
disk blocks. Disk sampling traverses the workspace every≥250ms: it adds overhead,
can miss brief peaks and delays RSS sampling. Thus current instrumented timings and
four-worker defaults are **not apples-to-apples with the historical eight-worker
non-disk-sampled driver**. Interrupted runs retain provenance/completed rows, not a
false completed verdict. The command above reproduces the accepted 50,000-package
workload; `--counts 1000,10000` can be used for smaller independent runs.

To reproduce the exact historical strategy instead (separate checkout):

```sh
git worktree add --detach ../tpa-direct-historical dc267c25958e39ce1170f8cf5b11cc97053a6a26
cd ../tpa-direct-historical
python3 bench/compare-direct.py --output /tmp/tpa-direct-historical-results \
  --counts 100,1000,10000 --repetitions 3
```

That driver uses eight workers and the historical methodology described above.
Hardware, OS/compiler changes and host contention affect results. To verify retained
evidence without running a performance workload:

```sh
(cd bench/direct-pipeline && sha256sum --check SHA256SUMS.txt)
```

For separate signed APT correctness tests, Docker and GPG are required:
`./tests/direct-pipeline-qualification.sh` and `./tests/qualification.sh`.
They use disposable fixture trust, never a production signing key.

## Scope of the claim

This is a measured improvement for small **metadata-only packages** on tmpfs.
It is not a claim about large payload applications, disk throughput, signed throughput,
hosted publishing throughput, hard total-memory caps or universal speedups. Existing
normal package staging is not deprecated or replaced.
