# TPA

TPA (Tool for Package Automation) creates Debian packages and generates the metadata for a small APT repository. It wraps standard Debian tooling so package metadata and maintainer scripts can be supplied through command-line flags or a JSON document.

## Requirements

- Go 1.26.3 or later to build TPA
- `dpkg-deb` to build and inspect `.deb` archives
- `gzip` to create compressed APT package indexes
- `gpg` only when signing a repository with `-gpg`

Build the executable:

```bash
go build -o tpa .
```

## Commands

| Command | Purpose |
| --- | --- |
| `init` | Creates a Debian package directory with `DEBIAN/control`, maintainer scripts, and `usr/local/bin`. |
| `build` | Runs `dpkg-deb --root-owner-group --build` for a package directory. |
| `parse` | Reads a `.deb` archive's control metadata and prints it. |
| `pack` | Creates APT `Packages`, `Packages.gz`, and `Release` metadata from `.deb` files. |
| `json` | Reads a JSON configuration from standard input and initializes a package directory. |
| `schema` | Prints the TypeScript-style interface describing the JSON input. |

Run `tpa` without a command to see every supported flag and its default value.

## Create A Package

`init` creates the package root, its control file, empty executable maintainer scripts, and `/usr/local/bin` within that root. Add your package files under the package root before building it.

```bash
./tpa init \
  -name=hello-tpa \
  -ver=1.0.0 \
  -arch=amd64 \
  -maintainer="Example Maintainer <maintainer@example.com>" \
  -desc="A package created with TPA" \
  -out=build/hello-tpa

install -m 0755 hello build/hello-tpa/usr/local/bin/hello
./tpa build -in=build/hello-tpa -out=dist/hello-tpa_1.0.0_amd64.deb
```

The package metadata defaults to `myNewAPTPackage`, version `0.0.1`, architecture `all`, section `utils`, and priority `optional`. The required Debian control fields are package name, version, architecture, maintainer, and description.

The metadata flags include `-depends`, `-homepage`, `-section`, `-priority`, `-pre-depends`, `-recommends`, `-suggests`, `-breaks`, `-conflicts`, `-replaces`, `-provides`, `-built-using`, `-essential`, and `-multi-arch`. The `-preinst`, `-postinst`, `-prerm`, and `-postrm` values are written directly as script content; they are not file paths.

## JSON Input

Pass a complete configuration to `json` through standard input. JSON field names follow the struct tags used by the program.

```bash
printf '%s\n' '{
  "control": {
    "name": "hello-tpa",
    "version": "1.0.0",
    "architecture": "amd64",
    "maintainer": "Example Maintainer <maintainer@example.com>",
    "description": "A package created with TPA"
  },
  "outdir": "build/hello-tpa"
}' | ./tpa json
```

For JSON input, all five required control fields must be present. `json` performs initialization only; run `build` separately to produce the `.deb` archive.

## Generate A Repository

Place `.deb` archives in an input directory, then generate repository metadata:

```bash
./tpa pack -in=dist -out=repo
```

TPA writes `dists/stable/Release` plus a `Packages` and `Packages.gz` pair for every architecture found in the input archives. Use `-gpg=KEY_ID` to write a clear-signed `InRelease` file.

Current repository behavior has important limitations: `pack` currently uses fixed repository values (`TPA-Repo`, `stable`, and `main`) instead of the `-origin`, `-label`, `-suite`, `-components`, and `-codename` flags. It also copies `.deb` archives into `pool/` only when `-gpg` is supplied. An unsigned repository therefore contains indexes but not package files and is not installable as-is.

## Build Release Packages

`build.sh` builds the TPA executable and packages it for `amd64`, `arm64`, and `riscv64`. It writes the resulting Debian packages to `dist/`.

```bash
./build.sh
```

The script requires a Linux Go toolchain capable of cross-compiling those targets, plus `dpkg-deb` and `gzip`.

## Project Layout

| Path | Purpose |
| --- | --- |
| `main.go` | CLI flags and command dispatch. |
| `internals/aptpackage/` | Package initialization, build, parsing, JSON input, and repository generation. |
| `build.sh` | Multi-architecture release package build script. |
| `manpage/` | Installed manpage source and compressed artifact. |
| `description.txt` | Long Debian package description used by `build.sh`. |

See [`AGENTS.md`](AGENTS.md) for contributor and automation guidance.
