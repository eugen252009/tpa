# Reusable TPA Debian release workflow

TPA provides a tag-driven reusable GitHub Actions workflow at
`.github/workflows/reusable-deb-release.yml`. It builds a project payload using
project-owned build commands, then delegates Debian package initialization and
archive creation to a pinned TPA `init`/`build` binary. It does not publish an
APT repository by default and does not modify caller repositories. Optional
owner-side APT publication is described in [staged releases](staged-releases.md).

## Release contract

- Release tags use `vMAJOR.MINOR.PATCH`, optionally followed by
  `-alpha[.N]`, `-beta[.N]`, or `-rc[.N]`. Prereleases map to Debian versions
  such as `v1.4.0-rc.2` → `1.4.0~rc.2`. Unsupported tags fail closed.
- The checked-out source SHA must equal the triggering SHA, and a release tag
  must resolve to that exact commit. The optional configured source-version
  check must also agree with the tag. When `release.source_branch` is set, the
  tag commit must be contained in `origin/<source_branch>`. A complete remote
  checkout is required; a same-named local branch is not sufficient. This is
  rechecked before publication. Staging may advance normally while a candidate
  awaits approval; deleting or rewriting it to exclude the candidate fails closed.
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
- The qualified bundle is uploaded before approval. Its immutable artifact ID,
  archive SHA-256, manifest SHA-256, version and source commit appear in the job
  summary. Attestation and publication download that artifact ID, not a mutable
  artifact name. Publication also checks the manifest against the bundle-job
  output and independently verifies the complete bundle; it does not rebuild
  packages. Missing/expired artifacts fail rather than triggering a rebuild.
- When `release.publish_environment` is set, the environment must already exist,
  require at least one user/team reviewer, prevent self-review, and disallow
  administrator bypass. Missing, inaccessible or insufficient policy fails
  closed before builds and is checked again before any release mutation. GitHub
  gates the publication job on that environment; the read-only policy check is
  not itself an approval receipt. Without this optional configuration, the
  reusable workflow preserves automatic tag publication, using an environment
  named `release` (whose protection, if any, still applies).
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
      actions: read # protected environment policy lookup
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

## TPA's staging and approval flow

The existing [TPA caller](../.github/workflows/tpa-release.yml) uses the local
reusable workflow and pins the builder to the triggering commit, so TPA builds
itself with its own package engine. Its configuration now requires:

```yaml
release:
  notes_file: docs/release-notes-0.8.0.md
  source_branch: staging
  publish_environment: tpa-release
```

Before pushing any release tag, a repository administrator must:

1. Create/protect the `staging` branch, require reviewed merges and passing CI.
   The normal package qualification workflow runs on both `main` and `staging`
   pushes and on pull requests, without publication credentials.
2. Create the GitHub environment `tpa-release` with required reviewers, enable
   **Prevent self-review**, and disable **Allow administrators to bypass
   configured protection rules**. Restrict deployments to protected release
   tags. The reviewer must be different from the actor starting the run.
3. Protect `v*` tag creation/update/deletion and restrict release permissions.
   Grant the caller `actions: read` for environment-policy lookup; the provided
   TPA caller already does so.

Then merge to staging and tag the selected staging commit using the existing
tag grammar. Merely merging does not publish a release. Tag builds run source
tests, all-architecture package/install checks and byte-identical rebuilds, then
upload a reviewable candidate. Review the summary, manifest, qualification
records and attestations before approving the environment deployment. The
publisher downloads and verifies that same candidate and publishes it to GitHub.
Rejecting the deployment leaves the qualified artifact available until its
retention expires and does not publish a GitHub Release.

Choose the Debian version **before building**. For example, a `v1.4.0`
candidate already contains Debian version `1.4.0`, so its tested bytes can be
published unchanged after approval. An RC contains `1.4.0~rc.N`; it cannot be
turned into `1.4.0` by renaming or retagging its DEB. A final-version build must
receive its own qualification and approval. Do not move a release tag to change
the candidate under review.

**Scope:** the review bundle is a GitHub workflow artifact. With explicit
`apt-destination` configuration, independent upload jobs populate a private CAS
stage, a complete-manifest barrier precedes approval, and owner-side `tpa sign`
publishes the finished signed tree through the existing receiver. See
[configuration, keys and recovery](staged-releases.md). This does not create branches/environments,
push tags, configure signing credentials, change installed TPA or enable
automatic updates. GitHub policy and reviewed workflow/configuration source are
trusted enforcement boundaries; repository administrators can change them.
Local fake-API tests cannot establish that a real GitHub deployment was reviewed.

For APT self-updates, configure explicit opt-in staging and stable repository
inputs/variables and public trust keys. The owner-side runner uses existing TPA
metadata generation, signing and finished-tree publication. Promotion must use the already
qualified DEBs and verify their manifest hashes, not rerun package builds;
channel-specific APT indexes/signatures may differ. Keep signing/publication
credentials out of build/test jobs. Stable clients must not enable the staging
source or automatic staging upgrades. Updates remain ordinary administrator-
authorized APT operations, not a self-replacing TPA executable.

TPA currently records upgrade qualification as `not-configured`: its dogfood
install check tests a clean disposable installation, not an upgrade from a
previous TPA version. Before treating the complete APT update path as qualified,
configure a trusted previous-version fixture and an `upgrade_command`, and
exercise install/upgrade through the signed staging channel. No existing
bootstrap repository, customer repository, APT source, or signing key is changed
by this workflow.

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
