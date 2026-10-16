# TPA Development Guide

## Binding architecture — read before changes

Before changing publishing, repository generation, signing, storage, repository
websites or backend integration, read and follow
[`TPA-ARCHITECTURE-CONTRACT.md`](TPA-ARCHITECTURE-CONTRACT.md).
It overrides conflicting historical implementation notes or qualification plans:
TPA builds/signs locally; ordinary SSH copies; TPA.run receives/verifies/stores/
meters/serves finished content. No host builder or mandatory host signing.
Do not infer conformance from passing legacy ingestion tests. Do not introduce
schema bridges without a demonstrated durable-data retention requirement.

## Purpose

TPA is a Go command-line tool for creating Debian package trees, building and
inspecting `.deb` archives, and deriving verified APT repository trees from
actual package artifacts.

## Repository Facts

- Module: `github.com/eugen252009/tpa`
- Language: Go 1.26.3 (`go.mod`)
- Executable entry point: `main.go`
- Internal implementation: `internals/aptpackage`
- Standalone utilities: `cmd/tpa-utils`, public facade `pkg/tpautils`, pure shared rules `pkg/debian` (see `docs/tpa-utils.md`)
- Browser formats: utilities use a distinct convenience schema and never overwrite canonical TPA v1 sidecars
- External tools: `dpkg-deb`, `gzip`, OpenSSH `ssh`/`sftp` for SSH transport, and optionally `gpg`
- Atomic replacement: Linux `renameat2(RENAME_EXCHANGE)`
- Release script: `build.sh`
- Release architectures: `amd64`, `arm64`, and `riscv64`
- Generated release packages: `dist/` (ignored)

## Commands

| Command | Behavior |
| --- | --- |
| `init` | Creates `DEBIAN/`, control metadata, and executable maintainer scripts; payload directories are user-managed. |
| `check` | Read-only package-tree metadata checks with aggregated, linked human/JSON diagnostics. |
| `build` | Validates `DEBIAN/control`, fixes present maintainer-script modes, and invokes `dpkg-deb --root-owner-group --build`. |
| `parse` | Reads plain, gzip, and xz control archives in-process; typed unsupported formats fall back to `dpkg-deb -f`, while malformed supported inputs fail directly. |
| `compose` | Builds metadata-only definitions (JSON/JSONL/directory) directly into reproducible DEBs and a verified, optionally signed new local APT tree; see `docs/direct-compose.md`. |
| `pack` | Derives and verifies an APT repository from top-level `.deb` files; `--empty` is the canonical empty workflow. Local and SSH/SFTP output are supported; remote output is new-destination-only. |
| `capabilities` | Prints the versioned machine-readable TPA capability/repository-format contract. |
| `inspect` | Reads local, HTTP(S), or SSH repository metadata/indexes without mutation; supports JSON and exact identity lookup. |
| `verify` | Verifies Release/index hashes, public-key InRelease signatures, artifact bytes, and package identity. |
| `unlist` | Removes one exact `Package + Version + Architecture` identity from APT metadata, verifies and atomically publishes metadata, and retains the `.deb`. |
| `delete` | Requires terminal `y`/`yes` or explicit `--yes`, unlists if needed, verifies and publishes metadata, then removes the `.deb`. |
| `json` | Reads configuration from standard input and initializes a package tree; it does not build an archive. |
| `schema` | Prints the TypeScript-style configuration interface. |
| `version` | Prints the canonical runtime version; `--version` prints the human-readable identity. |

All successful commands return zero. Invalid invocations and failed operations
return non-zero and write diagnostics to standard error.

## Repository Invariants

- `.deb` artifacts are authoritative. Repository metadata is derived state.
- The source package control stanza is preserved in `Packages`.
- TPA replaces package-provided `Filename`, `Size`, and `SHA256` with values
  derived from the published artifact.
- Canonical identity is `Package + Version + Architecture`.
- Byte-identical duplicate identities are indexed once; conflicting bytes fail.
- Package files are checked against `Packages`; indexes and `Packages.gz`
  correspondence are checked against `Release`; signed payload and expected
  signer are checked for `InRelease`.
- `inspect` and `verify` share bounded repository readers and strict Release/Packages
  parsing across local, HTTP(S), and SSH sources. They are read-only; signatures
  are cryptographically verified only against explicitly supplied public keys.
- `unlist` and `delete` target exactly `Package + Version + Architecture`.
  Metadata is verified and atomically published before artifact removal; a
  cancelled or failed pre-publication delete leaves the repository unchanged.
  Cleanup failure after unlisting must leave the safe unlisted state.
- TPA has no retained-generation registry. Retention and rollback references
  are owned by orchestration such as TPA.run; lifecycle operations affect only
  the repository tree path they receive.
- A fresh input set defines a fresh repository snapshot. Historical versions
  remain only when their artifacts remain in that set.
- No persistent package metadata or hash cache is used.
- Direct composition never builds per-package filesystem trees or falls back to them. Worker scratch and definition input are bounded separately. It uses the shared renderer, repository writer, selected signer and independent verification, and refuses existing output paths.
- Supported `.deb` control archive formats (plain, gzip, xz) are read in process;
  only explicitly unsupported formats use the bounded `dpkg-deb -f` fallback.
  Keep malformed-supported-input rejection distinct from fallback behavior.
- Pack worker queues and the ordered inspection window remain bounded; worker
  changes must preserve deterministic indexes, artifact-derived hashes, and
  independent repository verification.
- TPA repository format v1 requires paired root `index.html` and `repository.json`
  sidecars generated from APT indexes; these are convenience metadata, not signed
  APT trust data. `VerifyTPARepositoryTree` validates them; generic `verify`
  remains compatible with ordinary APT repositories without them.
- SSH/SFTP output builds and verifies a local candidate, uploads through OpenSSH SFTP, reads the remote tree back, verifies it, and activates only a new remote path. Existing remote replacement is unsupported. The TPA sibling lock serializes cooperating TPA publishers only; the remote parent must exclude out-of-band writers. Activation atomicity is SFTP-server-dependent and must be reported honestly.
- `tpa capabilities` is the versioned machine-readable integration contract and
  must report standalone transport and hosted-publication boundaries accurately.

`Pack` supports direct generation and the canonical `pack --empty` initialization
mode. Use `AtomicPack`/`--atomic-publish` for
local replacement when an existing repository may be read concurrently; remote
`--output` does not support replacement. TPA verifies any existing live
TPA tree (including its signature with the selected current signer when signed),
builds a sibling staging tree, verifies it, exchanges it with the live
directory, and removes the replaced tree. Failure before exchange leaves the
previous tree unchanged; post-activation errors must report `Published=true`.

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
./tests/repository-contract-qualification.sh
./tests/ssh-output-qualification.sh
./tests/dependency-qualification.sh
./tests/version-qualification.sh
```

The signed qualification requires Docker, GPG, `dpkg-deb`, and Go. It verifies
signature acceptance, APT install/upgrade/downgrade, signed unlist/delete, and
that an already-installed package survives unlisting. The dependency
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

`internal/version.Version` is the sole runtime/provenance version source and
defaults to `dev`. `build.sh` injects `TPA_VERSION` (default `0.0.0~dev`,
validated by dpkg) into its host and target binaries, and uses that same value
as Debian `Version`. It produces static Linux binaries for the configured
architectures and packages them through TPA. Run
`tests/version-qualification.sh` to check package/runtime/provenance/help
consistency.

```sh
./build.sh
```

Do not commit generated `tpa`, `dist/`, package work directories, or compressed
manpage artifacts.

## Change Guidelines

- Prefer the Go standard library and explicit error handling.
- Keep control files readable (`0644`) and maintainer scripts executable
  (`0755`).
- Run gofmt, race tests, vet, and the relevant qualification scripts after changes.
- Treat `.deb` archives and APT metadata as externally consumed formats.
- Do not weaken verification or artifact-derived hashing for performance.
