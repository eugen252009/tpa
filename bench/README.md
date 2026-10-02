# Meta-package benchmark and implementation audit

This is non-production tooling for the 10,000-package TPA/aptly/reprepro study.
It deliberately uses TPA's existing package and repository implementations; it
is not a proposed production architecture. A report and raw timing files live
beside the corpus after a run.

## TPA data-flow audit (HEAD `ba99e4d`)

```text
package config/tree
  -> InitPackage (synchronous filesystem writes)
  -> Build (synchronous control validation/chmod, one dpkg-deb subprocess)
  -> .deb output

Pack:
  os.ReadDir(input)
    -> serial per-.deb dpkg-deb -f control extraction
    -> serial parsing/stanza normalization/stat/SHA-256
    -> retain all package records + identity map in memory
    -> one deterministic package sort; group by architecture
    -> serial pool directory creation + byte copy
    -> serial Packages write per architecture
    -> one gzip subprocess per architecture
    -> serial SHA-256/size of Packages and Packages.gz into Release
    -> optional serial GPG fingerprint lookup + InRelease signing
    -> verifyRepository:
         hash Release-declared indexes
         read each Packages file wholly and parse all paragraphs into maps
         stat/hash every published .deb again
         optionally invoke GPG to check the signature and payload

Optional generation inventory:
  CreateGenerationManifest: serial tree walk + SHA-256 every regular file
  VerifyGenerationManifest: serial second tree walk + SHA-256 every file
  WriteGenerationManifest: JSON serialization + temp write/sync/rename

AtomicPack wraps repository construction in a sibling staging directory,
prepares permissions, verifies, and atomically exchanges the directory. Its
filesystem walks are serial too.
```

### Concurrency, bounds, and memory

There are no production worker pools, goroutines, channels, `NumCPU` or
`GOMAXPROCS` decisions in `internals/aptpackage`. Package builds and each Pack
phase are serial. A slow operation blocks the caller, so there is no concurrent
queue to fill and no goroutine explosion; this is synchronous backpressure, not
a staged bounded pipeline. `os.ReadDir`, the package slice/identity map, the
whole Packages byte slice, and parsed paragraph maps all grow with corpus size.
Package bytes are streamed through `io.Copy`/hashing rather than loaded wholly
into RAM. No expensive work is under a global mutex. No sync.Pool is warranted
by this implementation.

`Pack` sorts package records once (then sorts the architecture names); input
`ReadDir` is name-sorted by Go. Metadata/control extraction happens once per
source `.deb`; full repository verification parses the generated index and
rehashes copied package bytes. Hashing repeats by design for verification. With
a generation manifest, each published file is additionally hashed once while
creating and once while verifying the inventory. There is no package cache.
Compression occurs once per architecture, after complete index generation;
Release construction and signing are naturally final/serial.

The manifest has a hard `maxGenerationFiles = 8192` limit
(`internals/aptpackage/generation.go`). A 10,000-package repository contains at
least 10,000 pool files, so TPA can build and verify it with `pack`, but cannot
create or verify its portable generation manifest at this count. This is an
observed scale limit, not a reason to silently omit manifest validation or
change the limit in a performance benchmark. Benchmark results must identify
whether generation-manifest mode was available.

### Current likely costs (hypotheses to measure, not conclusions)

* Package construction is a serial TPA API loop with one `dpkg-deb` process per
  package; the process and filesystem metadata overhead may dominate tiny
  meta-packages.
* Repository ingestion is serial and invokes `dpkg-deb -f` once per input,
  hashes each source archive, copies it, then hashes the published copy during
  verification. These duplicate reads are part of the current correctness
  path, not grounds to remove verification absent evidence and a safe design.
* For 10,000 packages, source/output directory entry handling and repeated
  open/stat/hash operations may dominate CPU compression. The benchmark must
  decide this empirically.

## Corpus

`metapackages/` uses TPA's `Control.Render` and `aptpackage.Build` APIs to
create normal `.deb` archives. It omits default maintainer-script stubs and
has an empty data payload (the normal Debian archive still contains the data
archive metadata). Names are `tpa-meta-00000` through `tpa-meta-09999`,
version `1.0.0`, architecture `all`, with a deterministic dependency chain,
capability `Provides`, no external downloads, and no payload files. Set
`SOURCE_DATE_EPOCH=1700000000` for reproducible archive metadata. Updates use
versions `1.0.1` for the first 100 package names.

## Fairness and current environment

The three repository tools receive the same immutable version-1 corpus. Package
construction is a separate TPA-only category; aptly and reprepro do not build
`.deb` files. Repository timing includes each tool's normal local repository
workflow and signing. Update semantics are reported as implemented by each
tool; TPA creates a fresh current-state generation, while aptly/reprepro may
retain both old and new versions unless explicitly pruned.

Run `./bench/run.sh 10000 100`. It builds the TPA binary and corpus helper,
starts an ephemeral Debian 13 container, installs aptly/reprepro, then invokes
`benchmark-in-container.sh`. Output (corpus, three trials, logs, tool state,
validation and report inputs) is retained under `bench/`; the container and
private signing key are removed on exit. The initial and update repositories
are signed with one newly generated disposable unprotected key; only its public
key is retained. All measured tool data lives on the same bind-mounted host
filesystem. No network service or production credentials are used.

The current host has 8 physical/16 logical AMD Ryzen 7 5800X CPUs, 62 GiB RAM,
Debian 13 host, Linux 6.12, Go 1.26.4, and the repository is on ext4 over an
NVMe device (not tmpfs). aptly and reprepro are not installed on the host.

## Optimization study

See [`OPTIMIZATION_REPORT.md`](OPTIMIZATION_REPORT.md) for the additional
stage profile, bounded worker sweeps, manifest-scale qualification, correctness
results, and decisions not to replace dpkg-deb. Its raw outputs are retained in
`phase0-results/`, `phase1-results/`, `phase3-results/`, `phase4-results/`,
`phase5-results/`, and `final-qualification/`; the original baseline under
`results/` is unchanged. Stage metrics are benchmark-only and require
`-tags=tpa_bench`.
