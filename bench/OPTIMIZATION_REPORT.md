# TPA scalability optimization report: Phase A history and Phase B state

**Scope:** TPA package-build orchestration, repository Pack/verification, and
portable generation manifests. The Phase A measurements below are historical;
the authoritative post-Phase-B results and their caveats are in
[`REPORT.md`](REPORT.md), with raw evidence in
`post-phaseB-results/20261002T175545Z/`. No deployment, publication, or push
was performed.

## Executive summary

Phase A bounded package workers (defaulting to `GOMAXPROCS`, capped at 32, with
`-workers` override) reduced signed Pack from a 28.45 s one-worker median to
3.71 s at 16 workers (7.67x). Those figures predate the Phase B direct reader
and are retained as historical measurements. Pool copy and independent package
verification use the same bounded worker count. Ordered inspection has a fixed
`2*workers` in-flight window, preserving input order and bounding pending
results.

Each input `.deb` is SHA-256 hashed while copied to the pool; `Packages` gets
size and hash from those exact published bytes. Repository verification still
independently reopens, sizes, and hashes every published artifact. Reproducible
gzip headers (`gzip -n`) make `Packages.gz` bytes stable across worker counts
and runs.

The v1 generation-manifest contract has aligned TPA and TPA.run limits of
16 MiB and 65,536 files (historically 4 MiB and 8,192). TPA writes manifests
incrementally through a byte-limited writer. Lexical walk order avoids a second
sort and repeated whole-manifest marshalling; retained inventory and
verification maps still scale with file count, subject to the fixed limits.

Phase B subsequently replaced the common per-package `dpkg-deb -f` process with
an in-process control reader for plain, gzip, and xz control archives. Typed
unsupported formats use a bounded `dpkg-deb -f` fallback; malformed supported
archives fail directly. `dpkg-deb` remains used for package construction,
fallback, and oracle/qualification tooling. The fresh 10k oracle matched all
10,000 packages in 4.128 s direct versus 34.589 s for `dpkg-deb -f`; the
instrumented signed Pack sweep measured 5.330/2.940/1.830/1.340/1.260 s at
1/2/4/8/16 workers. Eight workers was near the practical knee. See the current
report for instrumentation distinctions and full qualifications.

## Measurement method and retained artifacts

The Phase A workload was the original 10,000 deterministic Debian meta-packages
(7,760,012 bytes total; 776 bytes/package), generated with
`SOURCE_DATE_EPOCH=1700000000`. Those signed Pack runs used a disposable
OpenPGP key on the shared ext4/NVMe bind mount described by the original
benchmark's software record. `host.txt` and the Phase A result directories
belong to that historical run; fresh Phase B environment metadata is in
`post-phaseB-results/20261002T175545Z/environment-host.txt` and
`environment-container.txt`.

Benchmark-only stage timers compile with `-tags=tpa_bench`; ordinary builds use
a no-op implementation. Paired checks were close: signed Pack was 28.36 s
instrumented versus 28.56 s uninstrumented; the warm instrumented package
build was 70.84 s versus 71.25 s uninstrumented. The earlier first package
build run was slower (73.81 s), so it is retained as run-order/system noise,
not treated as a representative speedup or regression. The raw tagged and
untagged logs are retained.

New raw outputs and scripts are under `bench/phase0-results/`,
`bench/phase1-results/`, `bench/phase3-results/`, `bench/phase4-results/`,
`bench/phase5-results/`, and `bench/final-qualification/`. The original
`bench/results/20261001T181442Z/` corpus, report, manifest-limit observation,
and aptly/reprepro results were not overwritten. `worker-sweep-in-container.sh`,
`build-sweep-in-container.sh`, the summary scripts, and the benchmark-only
`generation-manifest` helper reproduce the new phases in the disposable Debian
container. A disposable private signing key existed only in that container and
the container was removed after qualification.

## Historical Phase A stage profile of the serial baseline

On the signed 10k repository, the paired uninstrumented run took 28.56 s; the
instrumented profile took 28.36 s. Serial stage durations in that profile:

| Stage | Calls | Aggregate duration |
| --- | ---: | ---: |
| `dpkg-deb -f` control extraction | 10,000 | 26.98 s |
| metadata parse/stanza preparation | 10,000 | 0.130 s |
| source `stat` | 10,000 | 0.050 s |
| source SHA-256 pass | 10,000 | 0.156 s |
| pool directory creation | 10,000 | 0.368 s |
| pool copy | 10,000 | 0.320 s |
| Packages generation | 1 | 0.029 s |
| gzip | 1 | 0.043 s |
| package artifact verification (inclusive) | 1 | 0.189 s |
| of which verification hashes | 10,002 | 0.146 s |
| Packages index read/parse | 1 | 0.019 s |
| signing and signature verification | 1 each | about 0.005–0.007 s each |

The aggregate verification-artifact timer includes its nested hash timer. These
serial baseline stages are comparable to elapsed time; **parallel stage values
are sums of per-worker active durations, not wall time**, and must not be added
together to estimate Pack latency.

The separate package-build profile measured 10,000 calls to `Build`: dpkg-deb
startup was 1.17 s aggregate and waiting for dpkg-deb was 70.15 s. Generator
tree creation, control rendering, and control writes totaled 0.86 s; remaining
Build validation, maintainer-script, and output-directory work totaled about
0.38 s. The existing historical 3-trial package-build median was 71.67 s. The
build sweep below is a benchmark-only bounded orchestration of these same
`aptpackage.Build` calls; single-package `tpa build` continues to use dpkg-deb.

## Historical Phase A bounded Pack worker sweep (before the direct reader)

Signed Pack runs used the original immutable input set and distinct output
paths. Worker counts 1, 8, and 16 have three measured trials; 2 and 4 have one
sweep trial. Times are medians for repeated counts. RSS is GNU `time` peak RSS.

| Workers | Trials | Median Pack wall | Speedup vs 1 | Median CPU (user+sys) | Median peak RSS |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 3 | 28.45 s | 1.00x | 35.73 s | 53.8 MiB |
| 2 | 1 | 14.60 s | 1.95x | 35.98 s | 54.1 MiB |
| 4 | 1 | 7.61 s | 3.74x | 36.22 s | 54.7 MiB |
| 8 | 3 | 4.59 s | 6.20x | 41.21 s | 52.0 MiB |
| 16 | 3 | 3.71 s | 7.67x | 45.79 s | 60.0 MiB |

The 16-worker improvement is primarily overlapping dpkg-deb startup/control
extraction. It spends more CPU and can increase peak RSS; users can select
`-workers=1..32`. Zero (the default) uses `GOMAXPROCS`, capped at 32. The
worker pool has bounded job/result channels; inspection is gathered in input
name order with at most `2*workers` indexes in its reorder window. Pool copies
write disjoint artifact paths and gather by stable package index. Architecture
and package sorting still define output order.

After integrating source hash with copy and bounding the inspection reorder
window, an additional signed run measured 28.14 s at one worker and 3.75 s at
16 workers. Peak RSS was 56.3 MiB and 51.0 MiB respectively. Independent
verification still performs 10,002 hash checks (10,000 pool artifacts plus the
two Release-declared indexes); after parallelizing artifact verification, its
16-worker wall stage was 0.059 s. A final signed Pack plus manifest run took
4.35 s at 16 workers, including manifest creation/verification; it used
73.5 MiB peak RSS. That final run is not mixed into the plain-Pack table.

The source-read optimization removed one complete source-file read/hash pass.
In serial stage sums, source hash plus copy fell from about 0.48 s to 0.36 s;
at 16 workers, the corresponding sums fell from about 0.71 s to 0.48 s. Total
Pack wall time did not materially move from this I/O change alone because
control extraction remains dominant. Verification still independently hashes
the published pool files before success.

## Package-build orchestration sweep

The benchmark-only generator defaults to one worker to preserve the historical
baseline semantics; it now accepts bounded `--workers=1..32` and invokes the
unchanged TPA `Build` API for independent package roots. One trial was measured
for 2/4/8/16 workers and three for 1/32. The 1- and 32-worker medians use their
three trials.

| Workers | Trials | Median wall | Speedup vs 1 | Median CPU (user+sys) | Median peak RSS |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 3 | 69.71 s | 1.00x | 59.95 s | 13.0 MiB |
| 2 | 1 | 38.39 s | 1.82x | 63.02 s | 12.4 MiB |
| 4 | 1 | 22.55 s | 3.09x | 66.87 s | 12.9 MiB |
| 8 | 1 | 13.71 s | 5.08x | 73.44 s | 14.1 MiB |
| 16 | 1 | 12.75 s | 5.47x | 92.77 s | 15.3 MiB |
| 32 | 3 | 7.10 s | 9.82x | 100.30 s | 15.2 MiB |

This is a throughput result for callers that can build independent package
trees concurrently, not a claim that a single dpkg-deb invocation is faster.
The generator's one-worker and every parallel output matched the original
corpus names and per-file SHA-256 inventory. The six 10k runs' inventories
matched one another. The 32-worker case used about 1.67x the one-worker CPU
for 9.82x lower wall time; RSS remained about 15 MiB.

## Manifest scalability and memory

The v1 JSON shape and path/hash checks are unchanged. Both TPA and TPA.run now
accept at most 65,536 files and a 16 MiB canonical manifest; path limits
(4,096 bytes/64 components) and the 65,536-directory limit remain. These are
hard resource bounds, not constant-memory streaming of the inventory: TPA
retains one record per file and verification maps scale with that count. TPA's
writer emits the canonical JSON incrementally (one field/file at a time) to a
limited writer, avoiding a second whole-manifest encoded buffer. TPA.run uses
`bytes.Reader`/`bytes.Equal` instead of copying the input into strings for
canonical decoding/comparison.

On a real signed 10k repository, inventory creation, writing, and independent
inventory verification succeeded for **10,004 files**. The manifest was
1,880,793 bytes. The final bounded-writer helper measured 235 ms create,
113 ms write, and 236 ms verify (0.58 s end-to-end); peak RSS was 17.1 MiB. The
previous JSON-writer run was 0.52 s / 20.1 MiB; it is retained separately. The
new writer trades about 94 ms for a lower and more predictable serialization
working set. The emitted bytes matched the canonical `encoding/json` output
and the earlier manifest byte-for-byte.

`filepath.WalkDir` already yields lexical path order. TPA now checks this
invariant while walking rather than sorting the completed inventory. Limit
validation no longer marshals the same complete manifest before writing it.
The existing original baseline's `generation exceeds 8192 files` observation
remains documented in historical artifacts; it is now fixed rather than
silently bypassed.

## Compatibility and correctness

- Signed Pack outputs at worker counts 1/2/4/8/16 had identical `Packages`
  and deterministic `Packages.gz`; each `InRelease` verified with GPG.
- Gzip now suppresses filename/mtime metadata. A focused test confirms output
  is stable across input modification times. Debian Bookworm APT accepted the
  gzip/index format.
- Final current-code Pack produced 10,000 package entries, a signed Release,
  and a verified 10,004-file manifest. Stock APT `update` succeeded and
  downloaded `tpa-meta-05000=1.0.0`; the downloaded `.deb` matched the input.
- `tests/qualification.sh` passed signed APT install, upgrade, and downgrade.
  `tests/dependency-qualification.sh` passed direct and transitive dependency
  installation.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and
  `go test -tags=tpa_bench ./...` passed in TPA. TPA.run's full tests, race
  tests, vet, and diff check passed. The benchmark package builder also passed
  a 200-package, 16-worker race-instrumented smoke run.
- The inventory is transport metadata, not a signature. The existing signed
  Release verification, APT signer trust, atomic publication, and independent
  repository/file verification remain in place.

## Deliberately unchanged

Phase A added no native dpkg replacement, cache, reuse layer, persistent
metadata, or alternate repository viewer. Phase B later added the pure-Go
`github.com/ulikunitz/xz` dependency for supported xz control archives; it did
not replace dpkg for package construction or remove the bounded fallback for
unsupported formats. Package index parsing still materializes the Packages
file/paragraph maps as before; bounded task queues do not imply constant memory
as repository size grows.

No aptly/reprepro timings or update semantics were changed. Historical
benchmarks remain as recorded, with the original distinction between TPA's
fresh current-state rebuild and the other tools' update/retention behavior.
