# TPA — Tool for Package Automation

TPA creates Debian package directory trees, builds and inspects `.deb` files,
and derives small APT repositories from package artifacts.

```text
filesystem input → TPA → filesystem output
```

The `.deb` artifacts are authoritative package state. `Packages`, `Release`,
and `InRelease` are derived repository state; TPA does not require a persistent
package database or metadata cache.

## Requirements

- `dpkg-deb` for package building and fallback inspection of unsupported `.deb` formats
- `gzip` for compressed package indexes
- `gpg` when signing repositories
- Linux and a filesystem supporting `renameat2(RENAME_EXCHANGE)` for replacing
  an existing repository atomically

Build TPA with:

```sh
go build -o tpa .
```

Run `tpa` without a command to print the command synopsis and all flags. It
returns a non-zero status because no command was supplied.

## CLI contract

| Command | Input | Output |
| --- | --- | --- |
| `init` | Package metadata flags | Package root at `-out`, including `DEBIAN/control`, maintainer scripts, and `usr/local/bin` |
| `build` | Package root at `-in` | `.deb` archive or destination directory at `-out`, using `dpkg-deb --root-owner-group --build` |
| `parse` | `.deb` archive at `-in` | Human-readable parsed control summary on standard output |
| `pack` | Top-level `.deb` files in `-in`, or an optional JSON config path | Verified APT repository at `-out`, `--output`, or `--atomic-publish`; optionally a generation inventory |
| `unlist` | Repository at `-in` and exact `-package`, `-ver`, `-arch` identity | Atomically removes that identity from APT metadata and retains its `.deb` |
| `delete` | Repository at `-in` and exact `-package`, `-ver`, `-arch` identity | Confirms, unlists if needed, verifies and publishes metadata, then removes the `.deb` |
| `json` | Configuration JSON on standard input | Initialized package root at JSON `outdir` |
| `schema` | None | TypeScript-style configuration interface on standard output |
| `version` | None | TPA program version on standard output |

The program version command is `tpa version`. It is distinct from
`tpa -version`, which is not an alias for the version command and is rejected
as an invalid command.

`parse` and repository generation normally read control metadata in process for
plain, gzip-compressed, and xz-compressed control archives. Unsupported archive
formats use a bounded `dpkg-deb -f` fallback; malformed supported archives fail
directly rather than being hidden by fallback.

Exit status is part of the process contract:

```text
0        command completed successfully, or delete was cancelled without changes
non-zero command failed or its invocation was invalid
```

Diagnostics are written to standard error. Callers should use the exit status,
not match diagnostic text.

### Important flags

Package flags include `-name`, `-ver`, `-arch`, `-maintainer`, `-desc`,
`-depends`, `-pre-depends`, `-recommends`, `-suggests`, `-provides`,
`-conflicts`, `-breaks`, `-replaces`, `-homepage`, `-section`, `-priority`,
`-built-using`, `-essential`, `-multi-arch`, `-preinst`, `-postinst`, `-prerm`,
and `-postrm`.

Repository flags include `-origin`, `-label`, `-suite`, `-codename`, and
`-components`. Lifecycle commands use `-package`, `-ver`, and `-arch` for the
canonical package identity and `-in` for the existing repository tree. For a
non-default distribution or component, set `-codename` and `-components` to
match that tree. `delete` accepts `--yes` for explicit non-interactive approval;
without it, deletion requires a terminal and accepts only `y` or `yes` (case
insensitive). `-gpg` is required for a signed repository and must select its
signer. The `pack` command's `-workers` option bounds package inspection,
source hashing during pool copy, and published-artifact verification; zero (the
default) derives the worker count from `GOMAXPROCS`, capped at 32. Repository architectures are inferred from the actual `.deb`
artifacts; there is no architecture-list override. `-gpg` selects a signing key.
The general path flags are `-in` and `-out`. For `pack`, `--output` is an alias
for `-out`, while `--atomic-publish` selects atomic replacement; those two
output overrides cannot be used together. For hosted prebuilt publication,
`-generation-manifest`, `-repository-id`, and `-generation-id` write a versioned
inventory after repository verification; `-parent-generation` binds the expected
active parent. Use a server-issued repository ID and a fresh 32-character
lowercase-hex generation ID when publishing to TPA.run.

## Package creation

Relationship flags include `-depends`, `-pre-depends`, `-recommends`,
`-suggests`, `-provides`, `-conflicts`, `-breaks`, and `-replaces`.
Maintainer scripts are separate from control metadata. CLI script flags are
`-preinst`, `-postinst`, `-prerm`, and `-postrm`; their values are script
bodies, not paths to script files.

```sh
tpa init \
  -name=hello-tpa -ver=1.0.0 -arch=all \
  -maintainer='Example <example@example.invalid>' \
  -desc='Example package' -depends='dependency-package' \
  -out=build/hello-tpa

tpa build -in=build/hello-tpa -out=dist/hello-tpa_1.0.0_all.deb
```

`tpa json` performs the same initialization from standard input. Omitted fields
retain the CLI defaults. The JSON uses the field names printed by `tpa schema`.
The `json` command initializes a package tree only; it does not build a `.deb`.

The preferred JSON structure separates control metadata from maintainer scripts:

```json
{
  "control": {
    "name": "example",
    "version": "1.0.0",
    "architecture": "all",
    "maintainer": "Example",
    "description": "Example",
    "packageType": "backup",
    "memaService": "example",
    "memaSchema": 1
  },
  "scripts": {
    "postinst": "echo installed"
  },
  "outdir": "build/example"
}
```

Known Debian fields remain typed. Additional scalar control fields are accepted
without a TPA code change and are converted generically: `packageType` becomes
`Package-Type`, `memaService` becomes `Mema-Service`, and `memaSchema` becomes
`Mema-Schema`. Strings, numbers, and booleans are supported; arrays, objects,
nulls, unsafe names, control characters, and field-name collisions are rejected.

TPA adds `TPA-Version: <version>` and a UTC RFC3339 `Created-At` field when
those fields are not supplied explicitly. Explicit equivalent metadata values
win and are emitted once. Use `tpa json --no-provenance` (or the equivalent
package-definition command) to disable only automatic generation; explicit
provenance-shaped metadata and all other custom fields are preserved. The
configuration property `provenance: false` provides the persistent equivalent.
TPA does not assign semantics to arbitrary custom fields; it transports them as
Debian control metadata.

For compatibility, legacy `preinstbody`, `postinstbody`, `prermbody`, and
`postrmbody` fields are accepted and normalized into `scripts`. They are never
emitted as control fields.

```sh
printf '%s\n' '{
  "control": {
    "name": "hello-tpa",
    "version": "1.0.0",
    "architecture": "all",
    "maintainer": "Example <example@example.invalid>",
    "description": "Example package"
  },
  "outdir": "build/hello-tpa"
}' | tpa json
```

## Repository generation

TPA reads the actual control stanza from every input `.deb` and preserves its
Debian metadata in `Packages`. It removes any package-provided `Filename`,
`Size`, and `SHA256` fields and appends values derived from the actual published
artifact. Custom control fields therefore remain visible to APT consumers and
metadata inspectors.

```sh
tpa pack -in=dist -out=repo
```

For the default codename and component, output has this form:

```text
repo/
├── dists/stable/Release
├── dists/stable/InRelease                 # only when signed
├── dists/stable/main/binary-<arch>/Packages
├── dists/stable/main/binary-<arch>/Packages.gz
└── pool/main/<initial>/<package>/<original-archive-name>.deb
```

`-out` and `--output` select direct, non-atomic output. Use a new or empty path
when the result must be an exact snapshot. Direct output remains useful for
manual generation where no concurrent reader observes the destination.

For a completed repository, `-generation-manifest=<path>` writes a deterministic
versioned file inventory outside the repository tree. It requires
`-repository-id` and `-generation-id`; `-parent-generation` is optional. TPA
hashes every regular file after repository generation and verifies the inventory
against the final tree. The v1 contract bounds manifests to 16 MiB, 65,536 files,
4,096-byte/64-component paths, and 65,536 directories. The per-file inventory
and verification maps scale with file count but remain bounded by these fixed
limits. This inventory is transport metadata, not a replacement for APT's
signed Release metadata.

`pack` also accepts one positional JSON config file. `--output` and
`--atomic-publish` explicitly override the output path from that file. The
configuration file supplies the same package/repository fields as the CLI;
package artifacts are still read from the configured input directory.

## Repository semantics

The input artifact set defines the desired repository snapshot:

```text
artifact included in input → indexed in the generated repository
artifact omitted from input → absent from a fresh generated repository
```

TPA does not independently retain package history. To retain an older version,
keep its `.deb` in the desired input set.

Canonical package identity is:

```text
Package + Version + Architecture
```

- Same identity and byte-identical files are accepted idempotently and indexed
  once.
- Same identity and different bytes are rejected.

### Unlist and delete

`unlist` removes exactly one `Package + Version + Architecture` identity from
its architecture's `Packages` index, regenerates `Packages.gz` and `Release`,
re-signs `InRelease` when the repository is signed, verifies the complete
candidate, and atomically publishes it. The `.deb` remains in `pool` and may
still be directly downloaded by URL. Unlisting does not uninstall a package
from existing client systems; it only changes what fresh APT index updates
advertise. A later fresh `pack` from an artifact input that includes the `.deb`
will list it again.

`delete` is the confirmed destructive counterpart. It automatically performs
the same verified unlist transition when the exact identity is listed; when
already unlisted, it skips metadata mutation. Only after the new metadata tree
is verified and atomically active does TPA remove the artifact. The default
answer to the interactive prompt is no; only `y` or `yes` approves. Automation
must pass `--yes`. If metadata generation, signing, verification, or publication
fails, the artifact is retained and the active repository remains valid. If
artifact cleanup fails after unlisting, the command reports that safe partial
state: unlisted, artifact retained. TPA never deliberately leaves active
metadata referencing a missing artifact.

Lifecycle mutation uses a persistent hidden sibling lock file shared with
`pack --atomic-publish` and publishes a complete sibling candidate with the
existing Linux atomic directory exchange. Do not remove that lock file while
TPA writers may be active. TPA tracks no historical repository generations;
retention and rollback generations are owned by orchestration such as TPA.run.
`delete` only knows the repository tree path passed to it; an orchestrator must
ensure an artifact is not still needed by a separately retained generation.

```sh
tpa unlist -in=repo -package=foo -ver=1.2.3 -arch=amd64 \
  -gpg=FULL_SIGNING_FINGERPRINT

tpa delete -in=repo -package=foo -ver=1.2.3 -arch=amd64 \
  -gpg=FULL_SIGNING_FINGERPRINT --yes
```

Omit `-gpg` for an unsigned repository. `unlist` fails if that exact identity
is not listed. `delete` fails if its exact artifact cannot be found, and never
selects a package by filename alone.

## Verification

Before reporting repository-generation success, TPA verifies:

```text
.deb bytes
   │ Size + SHA256
   ▼
Packages
   │ index Size + SHA256
   ▼
Release
   │ signed payload
   ▼
InRelease
```

For every `Packages` entry, the referenced `Filename` must exist and its actual
size and SHA-256 must match. `Packages` and `Packages.gz` must match the size and
SHA-256 recorded in `Release`.

For signed repositories, the `InRelease` signature must be valid, its signer
must match the selected full fingerprint, and its signed payload must exactly
match `Release`.

## Signing

Use `-gpg` with a GPG selector; a full fingerprint is recommended:

```sh
tpa pack -in=dist -out=repo -gpg=FULL_SIGNING_FINGERPRINT
```

TPA invokes the installed `gpg` and uses the caller's GPG environment, including
`GNUPGHOME`. It selects one primary secret key, signs `Release`, and verifies the
result. It rejects expired, revoked, invalid, or ambiguous signature status and
requires the InRelease signature block to end the file. Key creation, storage,
expiration, and rotation remain GPG concerns.

## Exporting a hosted generation

TPA.run accepts complete repository trees built and signed by TPA. Register the
APT public key for the target repository with TPA.run, then build a fresh
repository tree and inventory:

```sh
gpg --armor --export "$APT_FINGERPRINT" > apt-signing-public.asc
tparun signer add "$REPOSITORY_ID" apt-signing-public.asc
tpa pack -in=artifacts -out=repo-generation -gpg="$APT_FINGERPRINT" \
  -generation-manifest=generation.json \
  -repository-id="$REPOSITORY_ID" \
  -generation-id="$(openssl rand -hex 16)" \
  -parent-generation="$ACTIVE_GENERATION"
tparun publish-generation "$REPOSITORY_ID" \
  --directory repo-generation --manifest generation.json
```

Omit `-parent-generation` when the repository has no active generation. Keep
the inventory beside, not inside, the generation tree. TPA.run authenticates
the publisher, checks the inventory and signed APT metadata independently,
then stores and activates the immutable generation. Its existing managed-signing
`tparun publish` flow remains available.

## Atomic publication

Use `--atomic-publish` when replacing a repository that may be read
concurrently:

```sh
tpa pack -in=dist -atomic-publish=/srv/apt/example \
  -gpg=FULL_SIGNING_FINGERPRINT
```

On Linux, TPA performs:

```text
fresh sibling staging tree
→ complete generation
→ repository verification
→ atomic rename exchange
→ replaced-tree cleanup
```

Failure before the exchange removes staging and leaves the previous repository
unchanged. Repository directories are published as `0755` and files as `0644`.
GPG key material is never copied into the repository tree.

## Qualification

```sh
go test ./...
go test -race ./...
go vet ./...
./tests/qualification.sh
./tests/dependency-qualification.sh
```

The signed qualification covers signature verification, APT install, upgrade,
and downgrade, plus signed unlist/delete against a live client that retains an
installed package. The dependency qualification proves that relationship
metadata survives repository generation and APT resolves both direct and
transitive dependencies automatically.

## 10,000-package benchmark snapshot

The authoritative post-Phase-B measurements were collected on the exact
committed reader at `078b62977e3cdec78fc84191172780f73468750f`. On Debian 13,
Linux 6.12.107, a Ryzen 7 5800X (8 cores/16 logical CPUs), the 10,000-package
control-reader oracle matched `dpkg-deb -f` with zero mismatches: 4.128 s
in-process versus 34.589 s for the oracle loop (8.38x).

A separate signed Pack sweep used benchmark-only stage instrumentation and a
complete generation manifest; each worker count had three trials. Median wall
times at 1/2/4/8/16 workers were 5.330/2.940/1.830/1.340/1.260 s. All 15 runs
read 10,000 packages directly and had zero fallbacks. Eight workers is the
practical throughput knee: 16 workers improved median wall time by 6.0% over 8,
with 2.0% more CPU and 10.1% more peak RSS. This instrumented sweep is distinct
from the standard, uninstrumented repository-comparison run; do not compare the
two as identical workloads.

The fresh repository comparison measured standard TPA initial generation at
1.110 s, aptly at 339.170 s, and reprepro at 16.390 s. Update medians were
1.160/4.710/0.290 s respectively, but update semantics differ: aptly retained
10,100 indexed entries while TPA and reprepro each reported 10,000. The package-build
orchestration sweep measured 85.38/36.34/19.58/11.33/12.62/9.55 s at
1/2/4/8/16/32 workers; intermediate counts were single observations, so no
precise optimum is established.

These figures are workload- and environment-specific. Full methodology,
validation status, caveats, and raw evidence locations are in
[`bench/REPORT.md`](bench/REPORT.md) and [`bench/README.md`](bench/README.md).
The corrected validator distinguishes the unsigned manifest-scale fixture
from signed repository checks: the unsigned inventory has no signing artifact,
while signed cases require and verify `InRelease`. The complete recheck passed
against an isolated copy of the retained comparison results.

## Optional future work

The following are optional repository-format improvements, not baseline
requirements:

- APT by-hash indexes
- reproducible `Release` dates
- detached `Release.gpg` output

## License

MIT @ Coffee Maker Studio
