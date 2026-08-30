# TPA Development Guide

## Purpose

TPA is a Go command-line tool for creating Debian package directory trees, building `.deb` archives, inspecting package metadata, and generating a basic APT repository layout.

## Repository Facts

- Module: `github.com/eugen252009/tpa`
- Language: Go 1.26.3 (`go.mod`)
- Executable entry point: `main.go`
- Internal package: `internals/aptpackage`
- External runtime tools: `dpkg-deb`, `gzip`, and optionally `gpg`
- Release script: `build.sh`
- Supported release architectures: `amd64`, `arm64`, and `riscv64`
- Output directory for release `.deb` files: `dist/`

No automated tests currently exist. Use `go test ./...`, `go vet ./...`, and a manual package build when changing behavior.

## Command Flow

`main.go` defines shared flags, parses flags after the command, and dispatches on the first argument.

| Command | Implementation | Behavior |
| --- | --- | --- |
| `init` | `InitPackage` | Creates `DEBIAN/`, `usr/local/bin/`, control metadata, and executable maintainer scripts. |
| `build` | `Build` | Parses `DEBIAN/control`, ensures present maintainer scripts are executable, then calls `dpkg-deb --root-owner-group --build`. |
| `parse` | `ParsePackage` | Calls `dpkg-deb -f` and parses the resulting control fields. |
| `pack` | `Pack` | Scans the input directory for `.deb` files and writes APT indexes and release metadata. |
| `json` | `JSONBuild` | Decodes standard input into `Config` and calls `InitPackage`; it does not build an archive. |
| `schema` | `JSONSCHEMA` | Prints a TypeScript-style interface for the JSON data model. |

## Data Model

`Config` contains:

- `control`: Debian metadata represented by `Control`
- `repo`: repository metadata represented by `RepoConfig`
- `indir`: input package directory or input archive directory
- `outdir`: output package path, package root, or repository root, depending on command
- `gpg`: optional GPG key ID

Required control values for JSON initialization and package parsing are `name`, `version`, `architecture`, `maintainer`, and `description`. `Control.Render` serializes the metadata into `DEBIAN/control`; add new Debian fields there as well as to `Control`, CLI flags, parsing, and `JSONSCHEMA` when appropriate.

Maintainer script values (`preinstbody`, `postinstbody`, `prermbody`, and `postrmbody`) are raw script bodies. `InitPackage` writes them to executable files beneath `DEBIAN/`; it does not read a script from the value as a path.

## Local Development

```bash
go build -o tpa .
go test ./...
go vet ./...
./tpa
```

Manual smoke test:

```bash
./tpa init -name=example -ver=1.0.0 -arch=all -maintainer='Example <example@example.com>' -desc='Example package' -out=/tmp/tpa-example
./tpa build -in=/tmp/tpa-example -out=/tmp/example_1.0.0_all.deb
./tpa parse -in=/tmp/example_1.0.0_all.deb
```

The build command delegates output naming to `dpkg-deb`; pass the desired archive path through `-out`.

## Release Build

`build.sh` builds the host `tpa` binary if it is absent, compresses the manpage if needed, cross-compiles a static Linux executable for each configured architecture, and packages each result with TPA. It uses version `1` and reads the long description from `description.txt`.

Run it from the repository root:

```bash
./build.sh
```

Do not commit generated `tpa`, `dist/`, or `manpage/usr/share/man/man1/tpa.1.gz` artifacts; they are ignored.

## Repository Caveats

Documented command behavior must follow the implementation, including these current constraints in `Pack`:

- Repository CLI fields in `RepoConfig` are not used when writing `Release`; it hard-codes origin and label to `TPA-Repo`, suite and codename to `stable`, and component to `main`.
- Archive architecture values are inferred from parsed `.deb` files; `-archs` is unused.
- Package files are copied to `pool/` only after a repository is GPG-signed. Without `-gpg`, the metadata is generated but archive files are absent from the repository.
- Package index filenames are constructed from the package name, not the original archive filename.

Preserve or deliberately fix these behaviors with corresponding README and manpage updates. The manpage currently contains stale command descriptions, so update it when changing user-facing CLI behavior.

## Change Guidelines

- Keep CLI flags, JSON tags, `JSONSCHEMA`, control rendering, parsing, README, and manpage aligned whenever metadata fields change.
- Keep Debian maintainer scripts executable (`0755`) and control files readable (`0644`).
- Prefer standard-library Go and explicit error handling. Do not add dependencies without a concrete need.
- Run `gofmt` on changed Go files and `go test ./...` before submitting Go changes.
- Treat package and repository output as externally consumed formats; validate with `dpkg-deb` and APT tooling after format changes.
