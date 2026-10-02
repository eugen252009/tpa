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

- `dpkg-deb` for package building and inspection
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
| `json` | Configuration JSON on standard input | Initialized package root at JSON `outdir` |
| `schema` | None | TypeScript-style configuration interface on standard output |
| `version` | None | TPA program version on standard output |

The program version command is `tpa version`. It is distinct from
`tpa -version`, which is not an alias for the version command and is rejected
as an invalid command.

Exit status is part of the process contract:

```text
0        command completed successfully
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
`-components`. The `pack` command's `-workers` option bounds package inspection,
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
go test -race ./...
go vet ./...
./tests/qualification.sh
./tests/dependency-qualification.sh
```

The signed qualification covers signature verification and APT install,
upgrade, and downgrade. The dependency qualification proves that relationship
metadata survives repository generation and APT resolves both direct and
transitive dependencies automatically.

## Optional future work

The following are optional repository-format improvements, not baseline
requirements:

- APT by-hash indexes
- reproducible gzip timestamps
- reproducible `Release` dates
- detached `Release.gpg` output

## License

MIT @ Coffee Maker Studio
