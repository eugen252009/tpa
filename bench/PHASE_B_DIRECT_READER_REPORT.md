# Phase B: in-process `.deb` control reader

**Historical Phase B baseline:** TPA `5caebcd9c4c28ef0fccaad4c56f01dca24f13af5`; TPA.run `ac8ea6edddc277c894964db4fef7e844dceed1cc`. The original qualification below was written before Phase B was committed; its statement that Phase B was uncommitted describes that earlier report state only. Phase B was subsequently committed as TPA `078b62977e3cdec78fc84191172780f73468750f`. Post-commit results are recorded at the end of this report and in `REPORT.md`. No push, deployment, or publication occurred.

## Outcome

The control reader removes the per-package `dpkg-deb -f` fork/exec for the normal corpus while preserving `dpkg-deb -f` as both an unsupported-format fallback and an independent test oracle. The same 10,000-package signed Pack workflow materially improved at every requested worker count. **Keep the direct reader.** The remaining throughput ceiling is approached around 8 workers; 16 workers is only modestly faster and costs more CPU and RSS.

The project’s existing `ParseControl` remains the only Debian control-field parser. The new code only reads the outer ar archive and control tar, then passes its `control` bytes to `ParseControl`.

## Format audit and behavior

The audit used Debian 13 / dpkg 1.22.22 `deb(5)`, `dpkg-deb --help`, TPA’s package fixtures, and the retained 10k corpus. The corpus and default `dpkg-deb` fixtures use `control.tar.xz`. Current Debian documentation permits plain, gzip, xz, and zstd control archives; its data archive list also includes bzip2 and lzma.

- **Direct:** Debian ar v2.x packages with ordinary ar names and `control.tar`, `control.tar.gz`, or `control.tar.xz`. The required `data.tar` member and Debian member ordering are checked; data members are not decompressed for control metadata.
- **Fallback:** zstd and other control compression suffixes; unknown data compression suffixes; GNU/BSD extended ar names; incompatible Debian major versions; and XZ layouts the bounded in-process decoder does not handle (for example, concatenated streams, omitted compressed-size fields, unsupported filters/checks, or a dictionary requirement above the resource cap). Only a typed “unsupported format” result selects fallback.
- **Malformed supported input:** invalid ar magic/header/numeric fields, truncation, invalid or missing `debian-binary`, missing/duplicate/ambiguous required members, bad ordering, malformed gzip/xz/tar, missing/duplicate/non-regular `control`, unsafe paths, and missing tar end markers are rejected directly. They do not select fallback. When an unsupported format is sent to dpkg, dpkg’s error is returned; it is not ignored.

The reader is bounded: 4,096 ar members (128-byte names), a 4 KiB `debian-binary`, 64 MiB compressed control tar, 128 MiB decompressed control archive, 16 MiB control file/fallback stdout, 4,096 tar entries (4 KiB paths), and a 16 MiB maximum XZ decoder dictionary. It streams archive contents rather than extracting them. XZ block sizes are checked against the XZ index; the decoder view lowers unnecessarily large declared dictionaries to the block’s bounded uncompressed size and recomputes the block-header CRC. The xz reader then verifies decoded block sizes and checksums. Fallback diagnostics are capped at 64 KiB.

The XZ implementation uses pure-Go `github.com/ulikunitz/xz` v0.5.17. `dpkg-deb` remains in package construction, the unsupported-format fallback, and oracle/qualification tests.

## Oracle comparison

`TPA_DEB_CORPUS` ran the direct reader and `dpkg-deb -f` on all **10,000** retained `.deb` files. For each package, tests compared the normalized `Control` value consumed by TPA and the repository control stanza; all matched. The measured loop reported **4.049 s direct** versus **34.728 s dpkg-deb** (about **8.6×** for this sequential read comparison). This is an oracle-test measurement, separate from the signed Pack sweep.

Fixtures cover xz, gzip, uncompressed control tar, and zstd fallback; a minimal/empty-data package; long Description continuations; dependencies/provides; `Architecture: all`; malformed/truncated ar; invalid `debian-binary`; missing control tar/control file; malformed compression/tar; missing tar terminators; and nonzero tar trailers. Fuzz targets ran 703,297 ar-reader executions and 8,035 control-tar executions without a crash.

## Signed Pack benchmark

The final matrix used three trials per worker count, the same immutable corpus and ext4/NVMe bind mount as Phase A, and a disposable signing key. It ran inside Debian 13 with 16 logical CPUs, matching the Phase A container environment. Times are medians; `time -v` supplied CPU and peak RSS. Control-stage durations are **aggregate active worker time**, not Pack wall time.

| Workers | Pack wall | User CPU | System CPU | User + system | Peak RSS | Control extraction stage (aggregate) | In-process reads | dpkg fallback reads |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 5.27 s | 3.54 s | 3.04 s | 6.62 s | 59.3 MiB | 3.87 s | 10,000 | 0 |
| 2 | 2.80 s | 3.62 s | 3.00 s | 6.61 s | 64.2 MiB | 3.99 s | 10,000 | 0 |
| 4 | 1.71 s | 3.93 s | 3.35 s | 7.08 s | 63.7 MiB | 4.31 s | 10,000 | 0 |
| 8 | 1.26 s | 4.20 s | 4.07 s | 8.27 s | 66.5 MiB | 5.31 s | 10,000 | 0 |
| 16 | 1.22 s | 4.53 s | 4.44 s | 8.97 s | 72.0 MiB | 7.18 s | 10,000 | 0 |

Phase A’s same-worker signed Pack medians were 28.45 s (1 worker), 4.59 s (8), and 3.71 s (16). Direct reading is therefore **5.40×**, **3.64×**, and **3.04×** faster respectively. The original serial `dpkg-deb -f` stage was 26.98 s; the new one-worker control extraction stage was 3.87 s, about an **86% reduction**. Phase A’s corresponding user+system CPU medians were 35.73 s, 41.21 s, and 45.79 s; the direct-reader runs used 6.62 s, 8.27 s, and 8.97 s. Peak RSS rose from 53.8/52.0/60.0 MiB to 59.3/66.5/72.0 MiB at those worker counts.

Scaling from 8 to 16 workers saved only about 0.04 s while increasing CPU and RSS. The stage profile still shows substantial aggregate per-package control reading and pool-directory work; verification hashing remains independent. The measurements point to CPU/filesystem metadata contention as the next limit, but stage sums are not wall-time attribution. Eight workers is near the throughput knee; the existing explicit worker override remains available.

The metrics recorded 10,000 direct reads and zero `dpkg-deb` fallback reads in every Pack run. `strace` was unavailable, so no syscall/total-execve diagnostic was collected; the fallback counter specifically confirms zero `dpkg-deb -f` launches. The pre-existing gzip/GPG work remains.

Raw trials, stage JSON, signatures, and timing files are under `bench/phaseB-results/20261002T171411Z-container-final/`. The sweep script is `bench/direct-reader-sweep.sh`; `bench/time-command.py` is its fallback timer when GNU `time` is unavailable.

## Compatibility qualification

- Every signed output’s `InRelease` verified. `Packages` and deterministic `Packages.gz` matched the committed Phase A worker-1 output byte-for-byte across the full matrix.
- Stock Debian APT update succeeded on the direct-reader repository and downloaded `tpa-meta-05000=1.0.0`; its SHA-256 matched the input (`004a5710e6cf31d4cbfcc27d00915f4a035a0c66005ead0a2e5d25b072e2dce7`).
- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go test -tags=tpa_bench ./...`, and `git diff --check` passed. Linux static cross-builds passed for amd64, arm64, and riscv64.
- `tests/qualification.sh` passed signed APT install, upgrade, and downgrade; `tests/dependency-qualification.sh` passed direct and transitive dependency resolution.

## Decision

The parser is correct against the 10k dpkg oracle and qualified APT outputs, retains dpkg compatibility fallback, rejects malformed supported inputs without fallback, and materially reduces both Pack wall time and CPU. The measured RSS increase is bounded and recorded. The in-process reader was retained; no further optimization phase was started at the time of this report.

## Post-commit remeasurement

A fresh benchmark measured the exact committed Phase B tree, TPA
`078b62977e3cdec78fc84191172780f73468750f`, in
`bench/post-phaseB-results/20261002T175545Z/`. The reader oracle matched all
10,000 packages with zero mismatches: 4.128 s direct versus 34.589 s for
`dpkg-deb -f` (8.38x). A separate three-trial signed Pack sweep, with
benchmark-only instrumentation and generation manifests, measured medians of
5.330/2.940/1.830/1.340/1.260 s at 1/2/4/8/16 workers. Every run made 10,000
direct reads and zero fallbacks. Eight workers remained near the practical
knee; 16 workers saved 6.0% wall time over 8 while using 2.0% more CPU and
10.1% more peak RSS. These fresh results supersede no historical measurement;
they are a separate dataset and differ from the original report's trials.
