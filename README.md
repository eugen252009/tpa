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
| `pack` | Top-level `.deb` files in `-in`, or an optional JSON config path | Verified APT repository at `-out`, `--output`, or `--atomic-publish` |
| `json` | Configuration JSON on standard input | Initialized package root at JSON `outdir` |
| `schema` | None | TypeScript-style configuration interface on standard output |

Exit status is part of the process contract:

```text
0        command completed successfully
non-zero command failed or its invocation was invalid
```

Diagnostics are written to standard error. Callers should use the exit status,
not match diagnostic text.

## Package creation

Relationship flags include `-depends`, `-pre-depends`, `-recommends`,
`-suggests`, `-provides`, `-conflicts`, `-breaks`, and `-replaces`.
Maintainer-script values are script bodies, not paths to script files.

```sh
tpa init \
  -name=hello-tpa -ver=1.0.0 -arch=all \
  -maintainer='Example <example@example.invalid>' \
  -desc='Example package' -depends='dependency-package' \
  -out=build/hello-tpa

tpa build -in=build/hello-tpa -out=dist/hello-tpa_1.0.0_all.deb
```

`tpa json` performs the same initialization from standard input. Omitted fields
retain the CLI defaults.

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
artifact.

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

`pack` also accepts one positional JSON config file. `--output` and
`--atomic-publish` explicitly override the output path from that file.

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
result. Key creation, storage, expiration, and rotation remain GPG concerns.

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
