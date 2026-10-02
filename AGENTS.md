# TPA Development Guide

## Purpose

TPA is a Go command-line tool for creating Debian package trees, building and
inspecting `.deb` archives, and deriving verified APT repository trees from
actual package artifacts.

## Repository Facts

- Module: `github.com/eugen252009/tpa`
- Language: Go 1.26.3 (`go.mod`)
- Executable entry point: `main.go`
- Internal implementation: `internals/aptpackage`
- External tools: `dpkg-deb`, `gzip`, and optionally `gpg`
- Atomic replacement: Linux `renameat2(RENAME_EXCHANGE)`
- Release script: `build.sh`
- Release architectures: `amd64`, `arm64`, and `riscv64`
- Generated release packages: `dist/` (ignored)

## Commands

| Command | Behavior |
| --- | --- |
| `init` | Creates `DEBIAN/`, `usr/local/bin/`, control metadata, and executable maintainer scripts. |
| `build` | Validates `DEBIAN/control`, fixes present maintainer-script modes, and invokes `dpkg-deb --root-owner-group --build`. |
| `parse` | Reads plain, gzip, and xz control archives in-process; typed unsupported formats fall back to `dpkg-deb -f`, while malformed supported inputs fail directly. |
| `pack` | Derives and verifies an APT repository from top-level `.deb` files. |
| `json` | Reads configuration from standard input and initializes a package tree; it does not build an archive. |
| `schema` | Prints the TypeScript-style configuration interface. |
| `version` | Prints the TPA program version. |

All successful commands return zero. Invalid invocations and failed operations
return non-zero and write diagnostics to standard error.

## Repository Invariants

- `.deb` artifacts are authoritative. Repository metadata is derived state.
- The source package control stanza is preserved in `Packages`.
- TPA replaces package-provided `Filename`, `Size`, and `SHA256` with values
  derived from the published artifact.
- Canonical identity is `Package + Version + Architecture`.
- Byte-identical duplicate identities are indexed once; conflicting bytes fail.
- Package files are checked against `Packages`; indexes are checked against
  `Release`; signed payload and expected signer are checked for `InRelease`.
- A fresh input set defines a fresh repository snapshot. Historical versions
  remain only when their artifacts remain in that set.
- No persistent package metadata or hash cache is used.
- Supported `.deb` control archive formats (plain, gzip, xz) are read in process;
  only explicitly unsupported formats use the bounded `dpkg-deb -f` fallback.
  Keep malformed-supported-input rejection distinct from fallback behavior.
- Pack worker queues and the ordered inspection window remain bounded; worker
  changes must preserve deterministic indexes, artifact-derived hashes, and
  independent repository verification.

`Pack` supports direct generation. Use `AtomicPack`/`--atomic-publish` when an
existing repository may be read concurrently: TPA builds a sibling staging
tree, verifies it, exchanges it with the live directory, and removes the
replaced tree. Failure before exchange leaves the previous tree unchanged.

## Data Model

`Config` contains package control metadata, maintainer scripts, repository
release metadata, input and output paths, and an optional GPG selector.
`Control` contains known typed fields plus scalar custom metadata. `Scripts`
contains maintainer-script bodies and is the canonical script representation.

`Control.Render` writes known and custom package metadata to `DEBIAN/control`.
`InitPackage` adds `TPA-Version` and `Created-At` unless explicitly supplied;
explicit equivalent values win and normalized duplicates are rejected. Automatic
provenance is enabled by default, can be disabled with `provenance: false`, or
with the CLI `--no-provenance` escape hatch; explicit metadata remains intact.
Keep the CLI flags, JSON tags, TypeScript interface, renderer, parser, README,
and manpage aligned when modeled fields change. Repository indexes preserve the
raw package control stanza so valid unmodeled Debian fields are not discarded.
Legacy `*body` script fields are accepted only as JSON compatibility input and
are normalized into `Config.Scripts`.

## Local Development

```sh
go build -o tpa .
go test -race ./...
go vet ./...
./tests/qualification.sh
./tests/dependency-qualification.sh
```

The signed qualification requires Docker, GPG, `dpkg-deb`, and Go. It verifies
signature acceptance and APT install, upgrade, and downgrade. The dependency
qualification verifies direct and transitive APT dependency resolution.

Manual package smoke test:

```sh
./tpa init -name=example -ver=1.0.0 -arch=all \
  -maintainer='Example <example@example.invalid>' \
  -desc='Example package' -out=/tmp/tpa-example
./tpa build -in=/tmp/tpa-example -out=/tmp/example_1.0.0_all.deb
./tpa parse -in=/tmp/example_1.0.0_all.deb
```

## Release Build

`build.sh` uses `TPA_VERSION` when set and otherwise version `1`. It builds a
static Linux binary for each configured architecture and packages it through
TPA.

```sh
./build.sh
```

Do not commit generated `tpa`, `dist/`, package work directories, or compressed
manpage artifacts.

## Change Guidelines

- Prefer the Go standard library and explicit error handling.
- Keep control files readable (`0644`) and maintainer scripts executable
  (`0755`).
- Run gofmt, race tests, vet, and both qualification scripts after repository
  format or publication changes.
- Treat `.deb` archives and APT metadata as externally consumed formats.
- Do not weaken verification or artifact-derived hashing for performance.
