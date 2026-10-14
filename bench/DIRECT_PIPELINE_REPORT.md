# Direct composition and standalone utilities: implementation / qualification

## Scope and source state

Worktree: `/home/eugen/projekte/tpa-direct-utils`, branch
`feature/direct-json-utils`, based on clean `main` at `ffab7d5`.

Independently qualified implementation commits:

- `4757c13e8137142bfd96f8f932e6bae073c61fda`: shared Debian rules and standalone
  library/CLI/static browser. Its isolated staged snapshot passed race tests
  and vet without the compose implementation present.
- `dc267c25958e39ce1170f8cf5b11cc97053a6a26`: direct composition, CLI/capability,
  docs, failure tests, qualification and comparative benchmark harness.
- `f11ccf1`: retained measurements, this report, stronger recursive/script/
  dependency tests and a shipped-JS DOM logic test; no shipping implementation
  changes. A following report-only commit records the remaining compatibility
  qualifications.

The handoff branch is committed and clean after the evidence commit. Original
`/home/eugen/projekte/tpa` remains clean on `main` at `ffab7d5`; no merge, push,
tag, publication, deployment or remote server operation was performed. Existing
released packages were not rebuilt/overwritten. Qualification built only
synthetic fixtures and temporary `9.8.7~completion1` candidates; no host install.
The existing three `0.7.1~dev.20261014190140` local package checksums still verify.

## Repository investigation and design

The actual shared surfaces were the `Control` renderer/checker, strict APT
readers, sorted `buildRepository` writer, selected-key GPG signer,
`VerifyTPARepositoryTree`, and exclusive new-directory activation. These were
reused, not replaced with a database, second signer, generic package model,
persistent cache or hosted-publication architecture.

- Pure metadata rules/diagnostics were extracted into `pkg/debian`; TPA wrappers
  and `pkg/tpautils` call them. Complete custom metadata checks remain shared in
  the renderer. Version ordering is in process and cross-checked against dpkg.
  Overflowing dpkg epochs now fail early, rather than passing validation and
  failing later in dpkg. No valid identity is automatically rewritten.
- `cmd/tpa-utils` independently builds/installs. The facade provides field and
  complete-control validation, Debian version comparison, bounded ordinary APT
  inspection and a static interactive browser. No account/backend required.
- Interactive browser output uses its own versioned convenience JSON, escaped
  text, safe URL handling, local JS/CSS, filtering/search/details/pagination and
  noscript JSON fallback. It refuses existing sidecars. Canonical TPA browser
  files remain unchanged and continue to pass byte-level verification; use a
  separate web output and explicit artifact base URL for those trees.
- `tpa compose` is separate from unchanged `json`/`build`/`pack`. JSON, JSONL and
  sorted recursive directory inputs use one existing-schema normalizer, reject
  ambiguous duplicate keys and unsupported payloads, and enter one definition
  list. Effective identical identities collapse; conflicting content fails.
- Root-owned deterministic Debian ar/tar/gzip archives contain control and
  optional executable scripts plus a valid empty data tar. No per-package tree,
  dpkg-deb process, temporary pre-pool DEB or pool-copy pass is needed. Actual
  written bytes determine size/hash; independent verification rereads them.
- Contiguous deterministic worker ranges write disjoint result slots; the
  shared sorted gather writes indexes, signs with the configured existing key,
  and generates canonical sidecars. Worker scratch and input/metadata bounds
  are separate. Concurrency drops to one when needed; an insufficient budget
  fails before staging, never falls back to package trees.
- Only a new destination is activated after verification. Failed candidates
  are cleaned, cleanup errors reported, and existing paths refused. Existing
  post-activation `Published=true` error semantics are retained.

Detailed input, trust, timestamp and resource contracts:
[`direct-compose.md`](../docs/direct-compose.md) and
[`tpa-utils.md`](../docs/tpa-utils.md).

## End-to-end comparison

Retained raw evidence:
[`measurements.csv`](direct-pipeline/measurements.csv),
[`measurements.json`](direct-pipeline/measurements.json),
[`environment.json`](direct-pipeline/environment.json), with adjacent
[`SHA256SUMS.txt`](direct-pipeline/SHA256SUMS.txt).

Measured shipping implementation: **dc267c2**. Three trials per size/path,
alternating order; medians below. AMD Ryzen 7 5800X (16 logical CPUs), Linux
6.12.111 Debian, Go 1.26.4, dpkg-deb 1.22.22, temporary inputs/output on **tmpfs**.
Both paths use eight requested workers, `GOMAXPROCS=8`,
`DPKG_DEB_THREADS_MAX=1`, epoch 0, identical metadata/four explicit scripts,
no payload and no automatic provenance. Direct scratch budget: 128 MiB.
Signing is excluded from both timed paths and tested separately.

Existing path: shared JSON reader → InitPackage → Build (bounded contiguous
workers, immediately remove each package tree) → Pack → verification.
Direct path: same reader → ComposeDefinitions → verification. Both include
canonical browser generation, final artifact reads and output-size accounting.
Compilation, corpus creation and final run-directory cleanup are outside timing;
legacy intermediate tree/DEB cleanup is inside its path. This is an optimized
practical legacy baseline, not one retaining all package trees unnecessarily.

| Packages | Existing seconds | Direct seconds | Existing/direct | Existing output bytes | Direct output bytes |
|---:|---:|---:|---:|---:|---:|
| 100 | 0.188 | 0.051 | 3.66× | 224,412 | 205,961 |
| 1,000 | 1.886 | 0.432 | 4.37× | 2,226,994 | 2,042,429 |
| 10,000 | 16.776 | 3.458 | 4.85× | 22,244,071 | 20,411,356 |

| Packages | Existing wait4 max RSS MiB | Direct wait4 max RSS MiB | Existing sampled summed RSS MiB | Direct sampled summed RSS MiB |
|---:|---:|---:|---:|---:|
| 100 | 18.29 | 28.18 | 25.73 | 24.99 |
| 1,000 | 22.50 | 33.93 | 29.18 | 34.19 |
| 10,000 | 81.76 | 89.67 | 97.55 | 90.45 |

`wait4.ru_maxrss` is the Linux largest-process high-water metric, **not** a
summed concurrent process-tree limit. Summed parent/descendant RSS is sampled
every 10 ms and can miss short peaks/double-count shared pages. Polling also
adds up to about one interval to measured completion time, material at 100
packages. Small-size timing/RSS conclusions should therefore be conservative.
Direct composition is **not always lower-RSS**: its Go gzip work uses more
in-process heap, while the existing path delegates compression to children.
The configurable scratch budget is not a hard OS total-RSS ceiling.

Filesystem work: syscall counts were **not measured** (strace unavailable).
Exact successful-path counts in the harness show **N package trees and N
per-package dpkg-deb processes versus zero**. With these four-script fixtures,
legacy initialization structurally creates 5N package-tree files and later
removes them, writes N temporary DEBs and copies N to the pool. Direct does
none of those per-package intermediates, but still writes N authoritative pool
DEBs, pool directories and normal repository metadata. These are path counts,
not invented syscall measurements. Both final outputs have N+5 regular files.

Caveats: existing dpkg compression defaults to xz versus Go gzip in direct
archives; output size/CPU differences are not solely scheduling. Results are
metadata-only, warm local runs on tmpfs, three samples, not disk throughput,
payload packaging, signed throughput, universal speedups or confidence bounds.
The ~8% smaller output is specific to this small corpus. No verification or
trust checks were removed for speed.

Reproduce (requires Go, Python, dpkg-deb and gzip; no install/publication):

```sh
python3 bench/compare-direct.py \
  --output bench/direct-pipeline-results/my-run \
  --counts 100,1000,10000 --repetitions 3
```

## Qualification results

Passed on the handoff source:

- `GOMAXPROCS=4 go test -race -p=4 ./...` and `go vet -p=4 ./...`;
- standalone library/CLI tests, individual/aggregate metadata diagnostics and
  a version-pair matrix against `dpkg --compare-versions`;
- ar acceptance, control/script tar ownership/modes/times, valid empty data,
  custom fields/epoch identity preservation, byte reproducibility and direct
  TPA parsing; worker-1/worker-8 full-tree equivalence with mixed architectures;
- JSON/JSONL/recursive directory normalization, duplicate/case-conflicting keys,
  size/symlink/unsupported-input rejection, effective identical/conflicting
  package identities, missing script interpreter, short/write error, actual
  filesystem failure/partial worker cleanup, memory fallback, cancellation,
  existing-output refusal and signed public-key verification;
- `tests/direct-pipeline-qualification.sh`: independent dpkg inspection; equal
  directory/JSONL trees; explicit signer/public-key TPA verification; signed
  APT installation with direct **and transitive** generated dependencies, script
  execution and upgrade 1.0 → 2.0 in disposable Debian; both versions retained;
  tampered artifact and missing-signer rejection; browser generation does not
  change any source-tree bytes;
- `tests/qualification.sh`: legacy signed APT install/upgrade/downgrade and
  unlist/delete retention behavior;
- `tests/repository-contract-qualification.sh`: legacy empty/signed initialization,
  first-artifact transition, sidecar contract and APT acceptance;
- `tests/version-qualification.sh`: temporary synthetic `9.8.7~test1`
  amd64/arm64/riscv64 packages agree on Debian/runtime/provenance versions,
  help identity and packaged completion files; existing release artifacts untouched;
- `tests/dependency-qualification.sh`: legacy artifact-derived direct/transitive
  dependency metadata and independent APT resolution;
- `tests/ssh-output-qualification.sh`: all seven stages passed on a disposable
  loopback OpenSSH/SFTP server, including APT consumption, strict host trust,
  existing-path protection, concurrent writers and injected failure boundaries.
  This qualifies existing pack transport, not remote compose support;
- utility HTTP serving of all four generated files and shipped-JS Node DOM-mock
  tests: search including metadata, architecture filter, pagination, empty
  inventory, plain-text hostile metadata, traversal-link rejection, unsafe URL
  and unsupported-schema rejection. The JS oracle ran, not skipped here.

Not claimed: exhaustive Debian Policy/Lintian compliance; visual or real-browser
rendering qualification; non-Linux activation qualification; reproducible GPG
signature time/bytes; cancellation interrupting existing external gzip/GPG calls;
remote direct composition/replacement; total RSS hard-capping; production or
hosted Git-to-APT qualification. No hosted publication feature was added.

## Fish investigation, kept separate

The prior failure is **not reproducible at the supplied baseline**. On original
clean `main`/ffab7d5 with Fish 4.0.2:

```sh
go test -race -count=1 -run '^TestFishCompletionBehavior$' -v .
# PASS (not skipped)
```

The baseline already contains the non-file option fix (`-x`, not `-r`) and normal
Fish startup for packaged vendor discovery. This feature does not redo or hide
that fix. It adds compose catalog entries/regression cases and teaches the
parser/catalog consistency test to recognize Int64 flags. Unit tests explicitly
source completion under `--no-config`; vendor-discovery qualification correctly
uses normal startup instead.

`tests/completion-qualification.sh` also passed installation, automatic Bash/Zsh/
Fish discovery, removal/reinstallation and unchanged profile checks in disposable
Debian, using temporary synthetic 9.8.7~completion1 packages only.
