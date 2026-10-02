# TPA 10,000 meta-package audit and benchmark

Run data: `results/20261001T181442Z/`. All timed trials used a full-corpus
warm-up followed by three measured trials, on one shared, warm-cache filesystem.
Times below are wall-clock medians and min–max unless stated otherwise. No
production code was changed.

## 1. EXECUTIVE SUMMARY

TPA's repository path is correct and synchronous, not a concurrent pipeline.
`Pack` serially extracts control metadata, hashes each source `.deb`, retains
all records in memory, copies packages, creates/compresses indexes, signs, and
then verifies the repository. On this tiny-package workload it completed a
signed, verified 10,000-package repository in a median **27.64 s**. A separate
TPA API harness built the 10,000 `.deb`s in **71.67 s** median; almost all that
time was in `Build` and its per-package `dpkg-deb` subprocess.

No production optimization is justified by these measurements alone. The
10,000-package repository does expose a correctness/scale ceiling: TPA's
portable generation manifest limit is 8,192 files, so TPA can pack and verify
this corpus but cannot inventory it as a portable generation. The measured
syscall profile and flat `GOMAXPROCS` results point to per-package process and
filesystem-metadata work, not large-payload hashing or compression, as the
likely dominant cost. Repository substage durations were not instrumented, so
that attribution remains a code-structure inference, not a stage timing.

## 2. CURRENT PIPELINE

```text
package spec
  -> Control.Render + control-file/directory creation (synchronous)
  -> aptpackage.Build: parse control, inspect/chmod scripts, run dpkg-deb
  -> .deb

Pack:
  os.ReadDir(input)
  -> serial dpkg-deb -f for every .deb
  -> parse/control-stanza normalization/stat/source SHA-256
  -> retain package slice + identity map
  -> one deterministic package sort; group by architecture
  -> serial pool directory creation and package copies
  -> Packages write, then gzip once per architecture
  -> hash index files and write Release
  -> optional serial GPG signing
  -> verifyRepository: hash Release entries, parse the full Packages index,
     and stat/hash every published .deb again; verify InRelease if signed

Optional manifest:
  serial walk/hash for CreateGenerationManifest
  -> serial second walk/hash for VerifyGenerationManifest
  -> JSON write/sync/rename
```

`AtomicPack` adds a sibling staging tree, permission preparation, verification,
and atomic directory exchange. It was not included: this comparison measures
repository creation, and the other tools do not offer the same TPA directory
exchange operation.

## 3. CONCURRENCY AUDIT

There are no production worker pools, package goroutines, channels, or
`runtime.NumCPU`/`GOMAXPROCS` scheduling in `internals/aptpackage`. Build and
repository phases are serial. A slow operation blocks its caller; this gives
synchronous backpressure, not a queued pipeline. It avoids goroutine explosion
but does not exploit independent package work.

The whole package set is retained as records and an identity map. `os.ReadDir`
retains directory entries; verification reads each Packages index into memory
and constructs paragraph maps. These structures grow with package count.
Package *contents* are streamed through hashing/copying, not loaded wholesale.
There are no expensive global locks. TPA sorts package records once and sorts
the architecture list; index ordering is deterministic. Compression, Release
creation, and signing are naturally final/serial stages.

Source `.deb` control is extracted once for indexing. Source bytes are hashed,
copied, then published bytes are hashed again by repository verification. The
index is also read/parsed for verification. With a manifest, published files
are hashed during both manifest creation and verification. No cache is used.

## 4. ISSUES FOUND

| Location | Observation | Impact / action |
|---|---|---|
| `internals/aptpackage/pack.go` | All package inspection, hashing, copying, and index writing are serial. | Potential throughput limit, but no worker-count experiment was possible without changing production APIs. Not changed. |
| `internals/aptpackage/verify.go` | Published `.deb`s are rehashed after source hashing and copying. | Extra complete read, intentionally protects repository integrity. Kept. |
| `internals/aptpackage/generation.go` | `maxGenerationFiles = 8192`. | 10,000 package files cannot be represented in a portable manifest. Confirmed by the actual error `generation exceeds 8192 files`. Not changed because the matching TPA.run limit is outside this repository/task. |
| Package builder | One `dpkg-deb` subprocess per package; there is no batch/worker API. | Dominates this lightweight creation workload structurally. No production change made. |

No unbounded goroutine creation, payload-sized RAM buffering, or expensive work
under a global mutex was found.

## 5. BENCHMARK HOST

- CPU: AMD Ryzen 7 5800X, 8 physical / 16 logical CPUs.
- RAM: 62 GiB.
- OS/kernel: Debian 13 (trixie), Linux 6.12.107.
- Filesystem/storage: ext4 on local Intenso NVMe; benchmark data was on the
  bind-mounted host filesystem, not tmpfs or network storage.
- Go: 1.26.4; TPA commit: `ba99e4daeda7ad653b1178d1067de96ea3bf08d5`.
- Container tools: aptly 1.6.1+ds1-3; reprepro binary reports 5.3.1 (Debian
  package 5.4.6+really5.3.2-1+deb13u1); dpkg-deb 1.22.22; GnuPG 2.4.7.
- Docker server: 29.5.2. Tool versions and host details are recorded in the
  run directory.

## 6. 10K CORPUS

The deterministic helper uses TPA `Control.Render` and `aptpackage.Build` to
make ordinary `.deb` archives. It omits maintainer-script stubs and has an
empty data payload. Names are `tpa-meta-00000` … `tpa-meta-09999`, version
`1.0.0`, architecture `all`; a deterministic Depends chain and synthetic
Provides avoid external downloads. `SOURCE_DATE_EPOCH=1700000000` is set.
The update set replaces the first 100 names with version `1.0.1` and keeps the
other 9,900 source archives unchanged.

- Package count / unique filenames: 10,000 / 10,000.
- Corpus bytes: 7,760,012; average: 776.0 bytes per package.
- Every archive was inspected with `dpkg-deb --info`; `--info` and `--contents`
  samples are retained for four packages.

## 7. TPA PACKAGE-CREATION BENCHMARK

TPA-only API harness; aptly and reprepro do not create `.deb`s.

| Tool | Packages | Median time (min–max) | Packages/s | Peak RSS (median) | Output |
|---|---:|---:|---:|---:|---:|
| TPA | 10,000 | 71.67 s (69.79–72.06) | 139.5 | 11.1 MiB | 7,760,012 B |
| aptly | N/A | N/A | N/A | N/A | N/A |
| reprepro | N/A | N/A | N/A | N/A | N/A |

The harness calls TPA's metadata renderer and `Build` API in a serial loop; it
does not include launching 10,000 separate TPA CLI processes or `InitPackage`
script stubs. `Build` itself invokes `dpkg-deb` once per package.

## 8. COMMON REPOSITORY BENCHMARK

All tools consumed the same immutable 10,000-file corpus. All repositories were
signed with one disposable RSA benchmark key. Each tool had a full-corpus
warm-up and three independent clean-state measured trials. TPA's operation was
signed `tpa pack` (which includes its own full verification). Aptly used
`repo create`, `repo add`, snapshot creation, and signed publish with default
Contents-index behavior. Reprepro used one `includedeb` invocation with all
files, which exports and signs the distribution.

| Tool | Packages | Median time (min–max) | Packages/s | Peak RSS (median) |
|---|---:|---:|---:|---:|
| TPA | 10,000 | 27.64 s (27.27–27.66) | 361.8 | 62.3 MiB |
| aptly | 10,000 | 307.74 s (306.28–311.39) | 32.5 | 102.7 MiB |
| reprepro | 10,000 | 16.19 s (14.92–18.11) | 617.7 | 18.8 MiB |

These are operation timings, not a universal ranking. Aptly performs its
repository/database/snapshot/publish workflow and default Contents generation;
reprepro imports into and exports a distribution; TPA copies a fresh repository
tree and verifies every indexed artifact. Their output and storage models
aren't identical.

## 9. UPDATE BENCHMARK

| Tool | Median time (min–max) | Peak RSS (median) | Active package entries after update | Semantics |
|---|---:|---:|---:|---|
| TPA | 27.29 s (27.24–27.32) | 61.3 MiB | 10,000 | Rebuilds a fresh current-state tree from 9,900 old + 100 new `.deb`s. |
| aptly | 4.42 s (3.89–4.56) | 120.8 MiB | 10,100 | Adds 100 new versions, snapshots and switches publication; old versions remain in the snapshot. |
| reprepro | 0.28 s (0.28–0.31) | 14.3 MiB | 10,000 | `includedeb` replaces the active version for those package names in this configuration. |

The workflows are intentionally not described as identical. Timings include
signing/publication where the workflow does so. Reprepro's count and updated
package availability were checked from its resulting index and APT client.

## 10. CPU SCALING

One signed TPA Pack observation per runtime limit (baseline trials above are
three runs). No meaningful scaling was observed, consistent with the serial
implementation.

| GOMAXPROCS | Wall time | Peak RSS |
|---:|---:|---:|
| 1 | 27.37 s | 62.4 MiB |
| 2 | 27.32 s | 55.5 MiB |
| 4 | 27.25 s | 52.5 MiB |
| 8 | 27.48 s | 62.1 MiB |
| 16 | 27.23 s | 61.5 MiB |

## 11. TPA STAGE BREAKDOWN

The package generator's separately timed API stages were:

| Stage | Median | Approx. share of package-build wall time |
|---|---:|---:|
| Control render + package-tree setup | 0.829 s | 1.2% |
| `aptpackage.Build` (validation/chmod + `dpkg-deb`) | 69.768 s | 97.3% |

The remaining difference is process/measurement overhead. This confirms that
package-tree metadata rendering is not the dominant cost here.

TPA's production repository function does not expose stage hooks. Inspection,
hashing, copying, indexing, compression, Release writing, signing, and
verification were measured together as `Pack`; separate percentages would be
fabricated. The one architecture `all` means gzip compression was one sequential
subprocess. Signing was enabled in every repository timing but not timed alone.

## 12. MEMORY AND I/O

Median peak RSS: package construction 11.1 MiB; TPA Pack 62.3 MiB; aptly initial
102.7 MiB; reprepro initial 18.8 MiB. Update peaks are in the update table.

A separate, unsigned `strace -f -c` diagnostic of TPA Pack recorded 3,352,701
file/descriptor-related syscalls for 10,000 packages, including 290,019
`openat`, 170,019 `newfstatat`, 20,000 `copy_file_range`, 20,008 `mkdirat`, and
90,002 `execve` calls (60,000 failed PATH candidates). This was an instrumented
diagnostic, not a timed result. GNU `time -v` raw filesystem input/output
counters were saved, but they are operation counters rather than reliable byte
volumes; exact bytes read/written and Go allocation/GC profiles were not
measured.

The evidence points to process-launch and filesystem-metadata pressure for tiny
files. It does not indicate storage-bandwidth or large-payload hash pressure.

## 13. STORAGE MODEL

The source corpus is shared and excluded from tool storage totals. `du` reports
below distinguish logical apparent bytes from allocated filesystem bytes;
allocated sizes are large because 10,000 tiny files and many pool directories
consume blocks/inodes.

| Tree | Apparent bytes | Allocated bytes | Notes |
|---|---:|---:|---|
| Shared `.deb` corpus | 7,760,012 | 41,500,672 | Each tool reads this same source. |
| TPA initial repository | 13,126,883 | 87,674,880 | One published tree. |
| TPA update repository | 13,126,847 | 87,670,784 | Separate fresh tree. |
| aptly state + publication after update | 33,336,275 median | 183,877,632 median | Includes aptly DB, pools, snapshots/publication. |
| reprepro repository after update | 26,246,256 | 100,831,232 | Includes repository and internal state. |

Initial aptly/reprepro apparent sizes were about 33.09 MB / 26.22 MB. Storage
is tool-specific; hardlinks and database representation make simple apparent
size an incomplete measure. The retained benchmark directory occupies about
378 MB apparent / 1.96 GB allocated across corpus, three trials and outputs.

## 14. CORRECTNESS

- Every generated package passed `dpkg-deb --info`; representative `--contents`
  checks passed.
- All three initial repositories had 10,000 Packages entries. All signed
  Releases were accepted by stock Debian 13 `apt-get update`; representative
  packages downloaded successfully.
- Updated TPA repositories had 10,000 entries; aptly had 10,100; reprepro had
  10,000. Updated signed repositories passed `gpg --verify` and stock APT
  update; the `1.0.1` updated package downloaded from each.
- TPA uses `binary-all` / `Architectures: all`; aptly and reprepro use
  `binary-amd64` / `Architectures: amd64`. Stock APT accepted each.
- No package installation was attempted: the synthetic dependency chain is
  intentionally for index correctness, not an installation benchmark.
- Portable TPA manifest creation at 10,000 packages fails explicitly at the
  8,192-file limit; it does not produce a misleading partial manifest.

The first monolithic driver attempt exited on a stale reprepro expected-count
assertion. Its timings were retained; initial correctness checks had completed
before update. The corrected expected semantics were then independently
rechecked across all three trials with `validate-results.sh`, including stock
APT, signature, package-count, updated-version download, and the manifest-limit
check. Thus timings were not rerun after that assertion-only correction.

## 15. WHOLE-FILE REUSE ESTIMATE

Comparing TPA's complete initial and updated trees by SHA-256:

- New generation: 10,004 files, 13,126,847 bytes.
- Already present by whole-file SHA-256: **9,900 files / 7,682,428 bytes**.
- New/changed files requiring upload: **104 files / 5,444,419 bytes**.
- Estimated whole-file reuse savings: **58.52%** of full-generation bytes.
- 100 old versioned paths removed and 100 new versioned paths added; Packages,
  Packages.gz, Release, and InRelease are changed.

This estimate has no chunking or binary delta. It indicates whole-file reuse
helps, but index/signature files dominate the remaining transfer for these
very small archives.

## 16. PIPELINE ASSESSMENT

- **Does current TPA resemble bounded builders → bounded collectors → bounded
  hashers → deterministic finalization?** No. It is a serial, whole-set pipeline
  with deterministic finalization.
- **Where does it differ?** There are no stage queues/workers. Metadata records
  and index paragraphs are retained in memory; package bytes stream.
- **Is that a measured performance problem?** It limits parallelism; the flat
  `GOMAXPROCS` results confirm there is no runtime scaling. The 10k trace shows
  substantial per-package process and filesystem work. Whether a bounded
  worker pool improves end-to-end time was not tested.
- **Would dedicated worker counts help?** Not established. There is no public
  worker-count control, and this workload is dominated by tiny files and process
  setup rather than bulk hashing. Do not hardcode worker counts from this run.
- **Measured bottleneck?** Package build is dominated by `Build`/`dpkg-deb`
  subprocess work; repository processing exhibits high file/process syscall
  counts. Stage-level repository timings are unavailable.
- **Does performance scale with CPU count?** No observable scaling from
  `GOMAXPROCS=1` to 16.
- **Does hashing/finalization dominate once package creation is cheap?** Not
  demonstrated. The archives are only 7.76 MB total; hashing repeats are real,
  but the benchmark did not isolate hash time. Per-package metadata/process
  work is the stronger evidence-based hypothesis.

## 17. CHANGES MADE

**Production:** none.

**Benchmark-only:** deterministic TPA package generator, host/container driver,
validation helper, generation hash-reuse calculator, audit/methodology notes,
raw results, and this report. No worker knobs or special package format were
added.

## 18. TEST RESULTS

Passed after benchmark tooling and result collection:

```text
go test ./...
go test -race ./...
go vet ./...
git diff --check
bash -n bench/run.sh bench/benchmark-in-container.sh bench/validate-results.sh
```

The signed/stock-APT repository checks are recorded under the run directory.
The existing qualification scripts were not run; production code was unchanged.

## 19. GIT STATUS

```text
 M .gitignore
?? bench/
```

The `.gitignore` modification pre-dated this task and remains untouched. No
commits, pushes, or deployments were made.
