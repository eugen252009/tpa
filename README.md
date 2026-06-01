# TPA (Tool for Package Automation)

**TPA** is a high-performance, minimalist tool designed for automating the creation of Debian packages (`.deb`) and managing APT-compatible repositories. It is built for efficiency, allowing package definitions via JSON blueprints or direct command-line arguments.

## Installation

*TPA does not require complex dependencies. It is optimized for Debian-based systems and relies on standard Unix utilities.*

**System Dependencies:**

* `libc6`: Standard C library.
* `dpkg`: Required for the packaging process.
* `gpg`: Optional, for signing repositories.
* `gzip`: Used for data compression.

## Synopsis

```bash
tpa [COMMANDS] [OPTIONS]
```

## Commands

* `init`: Initialize a new project structure.
* `build`: Build a .deb package from input files and metadata.
* `parse`: Parse existing package metadata.
* `pack`: Create or update an apt repository index.
* `json`: Generate a JSON blueprint from command-line arguments.
* `schema`: Output the schema definition for JSON blueprints.

## Options

### General Options

* `-in <dir>`: Input directory containing source files (default: `.`)
* `-out <dir>`: Output directory for the generated .deb file (default: `.`)

### Package Metadata

* `-name <string>`: Package name
* `-ver <string>`: Package version
* `-desc <string>`: Short description
* `-maintainer <string>`: Maintainer contact info
* `-section <string>`: Package section (default: `utils`)

### Dependencies & Relationships

* `-depends <string>`: Package dependencies (comma separated)
* `-conflicts <string>`: Conflicting packages

### Repository Options

* `-arch <string>`: Architecture (default: `all`)
* `-gpg <ID>`: GPG Key ID for signing (empty for no signing)

---

## Example Usage (Zero-to-100)

```bash
# 1. Initialize the package structure
tpa init -name=helloDeb -ver=0.1 -desc="Short description" -maintainer="Maintainer" -section=utils -out=hello_deb

# 2. Build the .deb package
tpa build -in=hello_deb -out="hello_deb/hello_deb.deb"

# 3. Bundle into an apt repository (optional: uncomment for GPG signing)
tpa pack -in=hello_deb -out=repo # -gpg=YOUR_GPG_KEY_ID
```

**Expected Output:**

```text
Initializing package 'helloDeb' in: hello_deb
Package structure for './helloDeb' created.
dpkg-deb: building package 'hellodeb' in 'hello_deb/hello_deb.deb'.
Package myNewAPTPackage.deb successfully created!
Repo build complete!
```

## License

MIT @ Coffee Maker Studio
*„Part of the Coffee Maker Studio ecosystem. Built for performance, licensed for everyone.“*
