# Reusable TPA Debian release workflow

TPA provides a tag-driven reusable GitHub Actions workflow at
`.github/workflows/reusable-deb-release.yml`. It builds a project payload using
project-owned build commands, then delegates Debian package initialization and
archive creation to a pinned TPA `init`/`build` binary. It does not publish an
APT repository and does not modify caller repositories.

## Release contract

- Release tags use `vMAJOR.MINOR.PATCH`, optionally followed by
  `-alpha[.N]`, `-beta[.N]`, or `-rc[.N]`. Prereleases map to Debian versions
  such as `v1.4.0-rc.2` → `1.4.0~rc.2`. Unsupported tags fail closed.
- The checked-out source SHA must equal the triggering SHA, and a release tag
  must resolve to that exact commit. The optional configured source-version
  check must also agree with the tag.
- Every declared architecture is built. `native` and `emulated` runtimes must
  run the configured clean-install command. `build-only` is explicit in the
  manifest and is **not** reported as runtime-qualified. An optional upgrade
  command is recorded as `not-configured` unless it runs successfully; use it
  when upgrade qualification is a project release gate.
- Reproducibility mode performs two independent package builds per
  architecture and requires byte-identical `.deb` files. `SOURCE_DATE_EPOCH`
  is the source commit timestamp; project build scripts should normalize
  generated payload timestamps accordingly.
- The bundle contains one `.deb` per architecture, a deterministic source
  archive, `SHA256SUMS.txt`, `SHA256SUMS-GITHUB.txt`, `release-manifest.json`,
  `provenance.json`, and release notes. The second checksum list is attested
  with the bundle and published as `SHA256SUMS.txt`. GitHub normalizes `~` to
  `.` in release asset names, so that list uses the normalized asset filenames;
  Debian versions in package metadata and manifests retain canonical `~`.
  Git submodules and Git LFS pointers are rejected rather than silently omitted
  from the source archive. Package metadata and qualification records are
  independently rechecked before bundling and before publication.
- Tag pushes create a GitHub build-provenance attestation, create/populate a
  draft release, read every uploaded asset back, then publish it. Published
  releases are never overwritten: a repeat must match the tag, source commit,
  source archive, release notes, builder identity, qualification records, and
  artifact hashes. Conflicts fail without deleting or replacing assets.
  Interrupted workflow-owned drafts can be resumed; unrelated or edited drafts
  are not mutated.
- GitHub's release interface may still allow a repository administrator to
  edit or delete a published release. The publisher itself is append-only for
  published releases; protect release tags and repository permissions as an
  additional control.

The workflow does not run for pull requests. Build/test jobs have read-only
contents permissions; only the attestation and publication jobs receive their
respective write permissions. Project build and qualification commands are
repository code and execute on the configured runners. Do not put signing keys,
APT credentials, customer credentials, or other secrets in project build
commands. APT signing and hosted repository publication remain separate
workflows and authority boundaries.

## Calling the workflow

Pin the reusable workflow and the TPA package builder to the same full TPA
commit SHA. Do not use a moving branch or tag for `tpa-ref`.

```yaml
name: Debian release

on:
  push:
    tags: ["v*"]
  workflow_dispatch:

permissions:
  contents: read
  attestations: write
  id-token: write

jobs:
  release:
    permissions:
      contents: write
      attestations: write
      id-token: write
    uses: eugen252009/tpa/.github/workflows/reusable-deb-release.yml@<TPA_FULL_COMMIT_SHA>
    with:
      config: .tpa-release.yml
      tpa-repository: eugen252009/tpa # optional; defaults to the upstream TPA repo
      tpa-ref: <TPA_FULL_COMMIT_SHA>
      tpa-version: "0.7.0"
      go-version: "1.26.3" # omit for non-Go projects
```

`tpa-repository` defaults to `eugen252009/tpa`; set it to a trusted fork when
testing unpublished workflow commits. The exact `tpa-ref` commit must exist in
that repository and is checked out by immutable SHA.

For a project outside TPA, set `tpa-version` to the exact TPA builder version
whose source SHA is pinned. `from-release-tag` makes the builder version equal
to the validated tag version; it is useful when TPA is dogfooding its own
release. A manual `workflow_dispatch` run is qualification-only and never
publishes, even if started on a tag.

Protect `v*` tags with repository rules that restrict who can create or update
tags. The workflow rejects a tag/source mismatch and rechecks the remote tag
before publishing, but no workflow can compensate for an unrestricted release
identity or compromised repository administrator account.

## Project configuration

The complete dogfood configuration is [`.tpa-release.yml`](../.tpa-release.yml).
The schema rejects unknown YAML keys and path traversal. Project-specific
commands receive these environment variables:

| Variable | Meaning |
| --- | --- |
| `TPA_RELEASE_PHASE` | `test`, `build`, `qualify`, `install`, or `upgrade` |
| `TPA_RELEASE_PROJECT`, `TPA_RELEASE_PACKAGE` | Project and Debian package names |
| `TPA_RELEASE_VERSION`, `TPA_RELEASE_TAG`, `TPA_RELEASE_SOURCE_COMMIT` | Validated release identity |
| `TPA_RELEASE_ARCH`, `DEB_HOST_ARCH` | Debian target architecture |
| `TPA_RELEASE_STAGE_ROOT` | Empty, project-contained payload staging directory |
| `TPA_RELEASE_PACKAGE_FILE` | Built package path for package/install/upgrade qualification |
| `TPA_RELEASE_REBUILD` | Independent rebuild number (1 or 2) |
| `SOURCE_DATE_EPOCH` | Commit-derived reproducibility timestamp |
| `TPA_RELEASE_BUILD_FLAGS` | JSON array copied from `build.flags` |

The package builder owns `DEBIAN/control`, generated provenance, and `.deb`
creation. Project build commands must write only payload files into
`TPA_RELEASE_STAGE_ROOT` (and the configured generated metadata file); tracked
source changes and other untracked outputs are rejected before packaging.
Maintainer scripts and source-version inputs are read from the exact committed
source. Supplying `DEBIAN/control` is rejected. Static extra
control fields use `package.control_fields`; a JSON object generated during the
project build can be supplied using `package.metadata_file`. Field values may
use `${version}`, `${tag}`, `${commit}`, `${architecture}`,
`${source_date_epoch}`, `${tpa_version}`, and `${builder_commit}`. The release
manifest records the release-notes SHA-256 plus builder, test, and qualification
identity.

Runtime commands run on the architecture's configured GitHub runner. Use
`emulated` only when the workflow's QEMU-enabled Docker runner can execute and
install the target architecture. For install and upgrade commands, isolate
system changes in disposable containers or VMs. A command that exits zero is
recorded as passed; the workflow cannot infer whether a project-specific test
actually checked the intended behavior.

## TPA.run adoption example

This is an example for a TPA.run repository; it is documentation only and does
not change TPA.run. Preserve its existing Go build, embedded version/revision,
schema checks, and service packaging logic, but refactor the payload phase so it
writes into the configured stage root instead of invoking `dpkg-deb` directly.
Keep hosted APT publication and its signing authority separate from GitHub
Release downloads.

```yaml
schema_version: 1
project:
  name: tparun
  test_command: go test ./...
build:
  command: bash ci/build-tparun-payload.sh "$TPA_RELEASE_STAGE_ROOT" "$TPA_RELEASE_ARCH" "$TPA_RELEASE_VERSION"
package:
  name: tparun
  description: Hosted Debian package repository service
  maintainer: TPA.run <support@tpa.run>
  depends: libc6, ca-certificates
  stage_root: .release-stage
  required_files:
    - usr/local/bin/tparun
    - lib/systemd/system/tparun.service
  architectures:
    amd64:
      runner: ubuntu-24.04
      runtime: native
  qualification:
    install_command: bash ci/qualify-tparun-package.sh
    # Add only when a disposable old-version/data fixture is available:
    # upgrade_command: bash ci/qualify-tparun-upgrade.sh
release:
  notes_file: docs/release-notes.md
reproducibility: true
```

`ci/build-tparun-payload.sh` is a project-owned adapter to implement: it should
preserve TPA.run's version/revision embedding and schema checks, install the
binary and service into the supplied payload root, and leave package metadata
and `.deb` creation to TPA. `ci/qualify-tparun-package.sh` should install the
package in an isolated amd64 Debian container, check the installed version and
schema command, and verify the service/config files without contacting
production services. Add more architectures only after build, install, and
runtime qualification succeed for each one.

## MEMA adoption example

This example likewise does not modify MEMA. Keep MEMA's language build and
configuration/schema generation project-owned; replace direct `.deb` and APT
repository creation with a payload adapter and TPA package construction.
Preserve `Mema-Schema` as package control metadata and verify that the schema in
the built binary, configuration, and package metadata agree.

```yaml
schema_version: 1
project:
  name: mema
  test_command: cd mema-go && go test ./...
build:
  command: bash ci/build-mema-payload.sh "$TPA_RELEASE_STAGE_ROOT" "$TPA_RELEASE_ARCH" "$TPA_RELEASE_VERSION"
package:
  name: mema
  description: MEMA monitoring and automation service
  maintainer: MEMA Project <maintainers@example.invalid>
  depends: libc6, ca-certificates
  stage_root: .release-stage
  metadata_file: .release-control-fields.json
  required_files:
    - usr/local/bin/mema
    - lib/systemd/system/mema.service
  architectures:
    amd64:
      runner: ubuntu-24.04
      runtime: native
    riscv64:
      runner: ubuntu-24.04
      runtime: emulated
  qualification:
    install_command: bash ci/qualify-mema-package.sh
release:
  notes_file: docs/release-notes.md
reproducibility: true
```

The payload adapter can emit `.release-control-fields.json` after generating
MEMA's schema, for example `{"Mema-Schema": 11}`. The qualification command
should install the package in disposable target-architecture containers,
compare `Mema-Schema` against the binary/configuration, and test service
startup without using production credentials or services. Keep `arm64` out of
the release matrix until MEMA's source build and package runtime have been
qualified there. Supply an `upgrade_command` with a disposable previous-version
fixture before treating upgrade behavior as qualified.
