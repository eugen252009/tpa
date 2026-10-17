# Direct-RAM public benchmark qualification

## Accepted 50,000-package publication update

Measured source: `ba7b5095935e00874738833fa06f8462b6f46854`, clean at measurement.
Three trials per path completed, six total; every run independently checked
50,000 archives and returned the same semantic digest. Conventional median
133.62358221499017 s / direct median 22.248018918995513 s = 6.006089…, published
as **133.624 s / 22.248 s / 6.01×**. See the
[raw records](results/2026-10-17-50000/measurements.json) and
[environment](results/2026-10-17-50000/environment.json).

The owner explicitly accepted background activity (`ccc` observed at startup;
recorded load 4.3–11.3). No attribution of the precise performance effect is made.
This update contains documentation and retained evidence only: no implementation,
harness or test changes, no benchmark rerun. Existing plain/race/vet and correctness
gates below remain applicable to the unchanged code. Focused publication checks
recompute medians/digests from all six rows, compare copied records byte-for-byte
with retained originals, verify SHA256 manifests, review Markdown links and source
references, and check secrets/large files and `git diff --check`.

## Source consolidation (prior publication)

The publication line starts from the clean, qualified 0.8.0~rc1 source
`2d30f6ad8b1ea733228d74825263ee9f97fb2263`, already containing Direct-RAM.
No package-builder redesign or new feature merge was necessary. Main ffab7d5 is
an ancestor, so the official main branch can be updated by ordinary fast-forward,
without rewriting published history. No release tag or APT publication is involved.

Included existing development histories:

- `4757c13`: shared Debian utilities; `dc267c2`: direct JSON composition.
- `f11ccf1`, `32fc08f`: retained comparative evidence and qualification.
- `4077c03`, `3131356`: binding architecture and conformance documentation.
- `5482a06` through `fec57d9`: client-signed SSH and connected chunk-CAS work.
- Merge `81bce56`: reusable release line through `1af2524`, including reproducible
  package provenance fixes through `0532e68`.
- Merge `20c3592`: optional account delegation `921b6fc`, retaining completions.
- `1e6154a`, `d4b3a23`, `3d252a7`, `2d30f6a`: shared release payload staging,
  consolidation docs, installer-test quoting and minimal GPG-agent dependency.

New benchmark work:

- `eaa5f4b`: existing driver improvements, archive semantic oracle, wrapper/tests.
- `b16ba07`: durable run-start provenance for interrupted measurements.
- Subsequent documentation/evidence commit: public historical results, root README
  discovery, frozen historical driver checksum target and qualification record.

All 21 previously inventoried TPA module roots were rechecked clean; all existing
accessible registered TPA worktrees were clean before this work. No unrelated dirty
worktree was staged. Unique old `feature/extend` and coffeemaker experiments remain
preserved/deferred as documented in [the consolidation](../../docs/consolidation-0.8.0.md),
not silently discarded or represented as completed new work. New changes are confined
to the isolated publication worktree. No generated packages, private keys, credentials,
databases, production backups or large corpora are included.

## Verification executed

On the publication code based on `b16ba07` (subsequent edits are benchmark/reporting
only), serial low-priority checks used GOMAXPROCS2, GOFLAGS=-p=2, nice10 to limit
additional load:

- `go test -count=1 ./...` — PASS.
- `go test -race -count=1 ./...` — PASS.
- `go vet ./...` — PASS.
- `python3 -m unittest discover -s bench/direct-ram -p 'test_*.py'` — PASS.
- `tests/direct-pipeline-qualification.sh` — PASS: JSONL/directory determinism,
  dpkg validation, direct/transitive APT dependencies, scripts/install/upgrade,
  signed verification, tamper rejection and browser output isolation.
- `tests/qualification.sh` — PASS: conventional signed APT/lifecycle regressions.
- `tests/dependency-qualification.sh` — PASS.
- `tests/repository-contract-qualification.sh` — PASS.
- Historical artifact checksums — PASS, including frozen historical driver.
- Recalculation of all medians/ratios from all18 historical records — PASS.
- Relative Markdown links in new/changed benchmark documentation — checked.
- `git diff --check` — PASS.

Public Go/oracle logs and gate hashes are in [results](results/qualification.json).
The historical performance source is dc267c2, not b16ba07. The accepted current
50,000-package source is ba7b509; differences from b16ba07 are documentation and
evidence, not implementation/harness/test code. Do not confuse these two datasets.

## Earlier attempts and historical selection (superseded publication scope)

A10-package smoke run completed both paths, with matching whole-corpus semantic
digests. Later contended runs completed all-archive semantic checks at1,000 and
10,000 packages. An initial three-repeat attempt exceeded the600-second execution
limit; the retry was interrupted. Host load was approximately40 on16 logical CPUs.
Neither attempt is published as a completed matrix or used for headline speedups.
The owner explicitly selected the existing qualified historical figures instead.

The historical driver has no disk sampler; the improved current driver does.
Consequently the attempts are not substitutes for the historical methodology even
without contention. At that earlier publication no 50,000-package result was
claimed. The subsequent accepted rerun is now published separately above; no
historical temporary-disk peak is invented. See the [main report](README.md) for
limitations and both reproduction procedures. The historical raw JSON/CSV is unchanged.

## Boundaries

No bootstrap signing investigation resumed, no key transferred, no TPM configured,
no production service touched, no APT repository published, no release package
rebuilt or deployed, and no personal website modified. Correctness tests used only
disposable local fixtures and test signing identities. Git publication is separate
from APT publication. Existing CI may perform its normal non-release qualification
when main advances; no workflow dispatch, release tag or production deployment is
requested here.
