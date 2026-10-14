# Direct metadata-only JSON → verified APT repository

This is a separate opt-in path. Existing `json` still initializes a package
tree; `build` still uses dpkg-deb; `pack` still indexes existing DEBs.

```sh
go build -o /tmp/tpa .
/tmp/tpa compose -in definitions.jsonl -out ./new-repository \
  -gpg FULL_SIGNING_FINGERPRINT -epoch 1700000000 -workers 8 -memory-mib 128
```

No key is generated, imported or selected implicitly. The existing GPG selector
and caller's `GNUPGHOME` are reused; an empty selector deliberately produces an
unsigned repository. Explicitly supply `-gpg` for signed APT output. Full signing
fingerprints are recommended. No signing key/token is embedded in inputs,
packages, browser assets or reports.

## Definitions and input modes

Each package uses the **existing TPA configuration schema**:

```json
{
  "control": {
    "name": "example-meta",
    "version": "1.0",
    "architecture": "all",
    "maintainer": "Example <example@example.invalid>",
    "description": "Metadata-only package",
    "depends": "libc6 (>= 2.36)",
    "memaType": "backup"
  },
  "scripts": {
    "postinst": "#!/bin/sh\nset -e\necho configured\n"
  }
}
```

- `-in package.json`: exactly one object.
- `-in definitions.jsonl`: one complete object on **every** line; no blank lines
  or multiline records; an ordinary final newline is allowed.
- `-in directory`: recursively gather regular `.json` files, sort by full
  relative pathname and normalize in that order. No intermediate JSONL file is
  created. Symlink definition files are rejected and symlink directories are
  not traversed. Non-JSON files are ignored.

The directory and JSONL readers feed the same normalized definition list.
There is no new universal package schema, external payload format or file
manifest. Unknown top-level fields and duplicate/case-conflicting JSON keys are rejected. Unknown control fields remain
supported scalar Debian metadata. The existing legacy `*body` script aliases
are accepted through the shared JSON decoder.

Only `control`, `scripts` and `provenance` carry per-package content; empty
legacy repository/path fields are tolerated, but nonempty `repo`, `indir`,
`outdir` or `gpg` values are rejected rather than ignored. Repository metadata,
destination and trust belong on the compose command. Package-specific CLI
options, remote output, replacement, generation manifests and `--empty` are
not supported by this command. Use `pack` for existing payload-bearing DEBs or
empty/replacement workflows.

Scripts are optional. Present scripts must have an explicit `#!` interpreter
and contain no NUL. Their supplied bodies are preserved, not silently prefixed
or rewritten, and stored mode 0755. Unlike package-tree initialization, compose
does not synthesize four no-op scripts when scripts are absent. Script bodies
are installation code: only use trusted definitions; parsing/building does not
execute them, installing does.

Automatic `TPA-Version` and `Created-At` use the existing renderer/provenance
rules. `Created-At` is pinned to the chosen epoch, not the wall clock. To disable
automatic fields, put `"provenance": false` in each definition. Explicit fields
remain intact. `compose --no-provenance` is rejected to avoid an ineffective
per-command override of per-record policy.

## DEB format and artifact authority

Each final artifact is written in process as Debian ar 2.0:

1. `debian-binary`: `2.0\n`;
2. `control.tar.gz`: `./control` (0644), then present preinst/postinst/prerm/postrm
   (0755), root UID/GID 0, explicit USTAR header times;
3. `data.tar.gz`: a valid empty tar archive.

Gzip headers, member ordering, ar padding and timestamps are deterministic.
There is no per-package dpkg-deb process, package directory, payload tree, or
temporary DEB followed by a pool copy. One bounded compressed control archive
and tiny empty data archive are buffered per active worker; the final DEB is
streamed once to the candidate pool while hashing/counting the actual bytes.
The finished repository is independently reread and verified, including each
DEB, before activation. DEBs remain authoritative artifacts, not database rows.

The original shared control renderer/validator and strict index readers remain
in use. Custom metadata and relationships survive into the control archive and
Packages stanza. Filename/Size/SHA256 are artifact-derived, never accepted from
definitions as overrides. Names, versions, architectures and dependencies are
not silently fixed. Epoch colons become underscores **only in artifact
basenames** for portability; the package's Version field and exact identity
remain unchanged.

## Scatter/gather and memory

Definitions are validated, normalized and de-duplicated before building.
Identity is exact Package + Version + Architecture. Equal effective control and
script content at a pinned epoch yields byte-identical artifacts and is indexed
once; conflicting content for one identity fails and requires a new version.

Workers get deterministic contiguous index ranges:
`[N*i/W, N*(i+1)/W)`. Each writes disjoint artifact paths and its corresponding
result slot. Gather uses the existing sorted repository writer for Packages,
Packages.gz, Release, signing and canonical browser sidecars, not worker finish
order. Existing pack's bounded ordered inspection path is unchanged.

Bounds:

- one serialized/normalized definition: 1 MiB;
- total serialized input and total normalized content: each at most 64 MiB;
- at most 100,000 definitions and existing repository-reader limits;
- `-workers` obeys the existing bounded worker limit (0 uses GOMAXPROCS);
- `-memory-mib` (default 64, CLI 1..4096) is an **archive-worker scratch budget**:
  the planner reserves 8 MiB + four times the largest normalized content size
  per active worker, then lowers concurrency, down to one;
- if even one archive worker cannot fit, fail before creating a candidate;
  there is **no temporary package-tree fallback**.

This is not a hard total-process RSS limit: retained definitions, metadata,
indexes, Go runtime/GC and independent verification are separately bounded
overhead. Increasing workers beyond the budget does not create unbounded
archive buffers. The supported case always uses the direct path.

## Reproducibility, activation and failure

`-epoch` pins ar/tar/provenance and Release time; it defaults to
`SOURCE_DATE_EPOCH` if present, otherwise Unix zero. An explicit flag wins.
Accepted range is 0..4294967295. Same normalized input, epoch, TPA runtime version
and toolchain produces identical DEBs and unsigned repository files, regardless
of worker count/input adapter. Release signing uses existing GPG behavior:
InRelease signature bytes/time are **not** promised reproducible. Explicit
nondeterministic custom fields are the caller's responsibility.

The output must **not exist**, even as an empty directory or symlink. A sibling
lock serializes cooperating writers. A private sibling candidate is built and
verified, permissions prepared, and exclusively activated with the existing
new-directory publication primitive. Unsupported platforms fail closed, as for
empty initialization. No existing repository is replaced or mutated. Prior to
activation, any validation, worker/filesystem, signing or verification error
removes the candidate; the destination remains absent. Cleanup errors are
joined with the original failure; a hidden staging directory can remain only
when cleanup itself fails, and is never treated as the activated destination. Parent directories and
the cooperating-writer lock file may remain. A post-activation parent-sync
failure retains the existing typed `AtomicPublishError{Published:true}`
semantics; do not retry blindly. Context cancellation is checked between records
and phases, but existing external gzip/GPG calls are not preemptively cancelled.
This is not a general crash-recovery or hostile concurrent-writer guarantee.

## Qualification and measurements

`tests/direct-pipeline-qualification.sh` generates disposable keys and fixtures,
checks dpkg/TPA parsing, compares directory/JSONL outputs across worker counts,
verifies the public-key InRelease, and installs/upgrades versions 1.0 → 2.0 in
a disposable Debian Docker container. Both versions remain indexed. It also
checks direct/transitive dependency installation, script execution, browser isolation, tamper rejection and failed-signing
cleanup. It never installs on the host or publishes/deploys.

Unit/race tests cover archive modes/ownership, deterministic bytes, custom
metadata/epochs, individual and aggregate validation, malformed/duplicate JSON,
size/symlink limits, duplicate identities, short/write failure, filesystem
failure, memory fallback, cancellation, output refusal and signed verification.

See [the benchmark report](../bench/DIRECT_PIPELINE_REPORT.md) for reproducible
100/1,000/10,000-package comparisons, including the compression/memory caveats.
