# TPA repository format v1

This document defines the repository tree emitted by TPA `pack`, including
`pack --empty`. It is a candidate interchange contract, not an APT trust format.
The repository's APT trust chain remains `InRelease` → `Release` → `Packages`
indexes → `.deb` bytes.

## Version discovery

Run `tpa capabilities` and parse its JSON document. Its top-level `format` is
`tpa-capabilities` and `version` is `1`. The
`repository_format.name` / `version` pair declares `tpa-apt-repository` v1;
`browser_index` / `browser_index_version` declares the paired browser sidecars.
Consumers must reject an unknown capabilities schema or repository format
version, and must independently approve the exact TPA binary/version they run.
The runtime version alone is not source provenance.

A repository tree identifies this format through its mandatory root
`repository.json` sidecar (`format: "tpa-repository-index"`, `version: 1`). In
v1, this browser-index marker maps to the enclosing `tpa-apt-repository` v1
contract; the capabilities document advertises both names and versions. The
sidecar pair is a format marker and convenience view, **not** a signature or
APT trust anchor. Generic APT repositories need not have these files.

## Tree and placement

For one distribution, one component, and one or more binary architectures:

```text
<root>/
├── index.html
├── repository.json
├── dists/<codename>/
│   ├── Release
│   ├── InRelease                         # optional; present when signed
│   └── <component>/
│       └── binary-<architecture>/
│           ├── Packages
│           └── Packages.gz
└── pool/<component>/
    └── <first-byte-of-package>/<package>/<original-input-basename>.deb
```

The canonical empty workflow is `tpa pack --empty`. It creates
`pool/<component>` and empty `Packages` and `Packages.gz` files for every
architecture. `pack --empty` defaults suite and codename to `stable`, component
to `main`, and architectures to `all`; these values may be overridden. Empty
initialization refuses every existing destination. Safe local initialization
uses an exclusive publication primitive
on Linux, Darwin, and Windows; other platforms report it unsupported in
`tpa capabilities`. SSH/SFTP output is supported to a new remote destination.
Normal `pack` infers architectures from actual `.deb` control stanzas and rejects
an empty artifact set; it does not convert an empty input directory into an
empty repository. To add packages to an existing local empty tree, use
`pack --atomic-publish` with the same suite, codename, component, and signing
policy. SSH/SFTP output instead publishes a complete package set only to a new
remote destination.

Paths are relative slash paths. Codename, component, architecture, and package
path segments must be safe canonical tokens; symlinks and special files are not
part of a TPA candidate. Each indexed `Filename` is under `pool/` and points to
the original artifact basename. Fresh `pack` output contains only artifacts in
its input set. `unlist` intentionally retains the removed `.deb` in `pool`, so a
lifecycle-mutated tree can have pool artifacts not referenced by current APT
indexes or `repository.json`; a hosted consumer must explicitly apply its
retention policy to such files rather than silently treating them as indexed.

TPA currently emits one component per repository. Multi-component output is not
part of v1. Architectures and package entries are sorted deterministically.

## APT metadata and trust

`Release` declares `Suite`, `Codename`, `Components`, sorted `Architectures`,
`Date`, and SHA256/size records for every plain and gzip `Packages` index.
`Packages.gz` must decompress byte-for-byte to `Packages`. Each package stanza
preserves the source `.deb` control metadata, except `Filename`, `Size`, and
`SHA256`, which TPA derives from the published artifact bytes. The canonical
identity is `Package + Version + Architecture`; byte-identical duplicates are
indexed once and conflicting bytes for one identity fail.

For every indexed package, a consumer must safely resolve `Filename` inside the
candidate tree, require a regular file, compare the actual size and SHA256 with
`Packages`, and parse the `.deb` control stanza to confirm the exact identity.
The Release SHA256/size records must match every index. If `InRelease` is
present, verify its clear-signature and exact signed `Release` payload against
an explicitly trusted public key/fingerprint. Never infer trust from the
browser files. TPA uses GPG from its caller's environment when `-gpg` is
provided; it does not embed signing secrets in the tree.

## Browser sidecars

`index.html` and `repository.json` are an inseparable pair at the repository
root. TPA generates both from the APT `Packages` indexes named by `Release`.
They contain no credentials or additional package authority. New `index.html`
embeds the shared first-party CSS/JavaScript explorer; no extra canonical files,
server renderer, API or JSON format is introduced. All package text is escaped
in the static preview and assigned with textContent in the interactive view.
These files are not included in the `Release` checksum section and are not
covered by `InRelease`; continue serving them as UNTRUSTED display metadata.
See [hosting-policy and backwards-compatibility notes](repository-ui.md).

`repository.json` is UTF-8 JSON with this v1 shape:

```json
{
  "format": "tpa-repository-index",
  "version": 1,
  "packages": [
    {
      "metadata": {
        "Package": "example",
        "Version": "1.0",
        "Architecture": "all"
      },
      "artifact": {
        "filename": "pool/main/e/example/example_1.0_all.deb",
        "size": 1234,
        "sha256": "<64 lowercase hexadecimal characters>"
      }
    }
  ]
}
```

`metadata` is the complete preserved package control stanza represented as a
string-to-string object, excluding `Filename`, `Size`, and `SHA256`. `artifact`
contains those derived values. Entries are sorted by Package, Version,
Architecture, then Filename. Empty repositories use `"packages": []`. JSON
object key order and indentation are stable in TPA output but consumers must
parse JSON rather than depend on whitespace. This JSON shape and its exact
canonical derivation are unchanged. The HTML uses repository-relative links and
escaping; its first-50-record preview and native details work without scripts.
The interactive explorer requires JavaScript and inventory fetch permission.

`VerifyTPARepositoryTree` checks the APT trust chain, package identities, and
paired sidecars byte-for-byte against current APT indexes. It accepts the exact
modern rendering OR the exact legacy static rendering (including published0.8.0
repositories); arbitrary/tampered HTML is still rejected. Old strict template-only
verifiers may reject modern HTML; generic APT verification remains compatible.
Its `Config.Repo`
values must describe the expected Release metadata, and `Config.GPG` selects the
expected signer when set. It also enforces the
v1 directory/file allowlist, canonical distribution/component/index layout,
safe pool placement, regular files, and bounded file/directory/package counts.
Pool-only `.deb` files retained by `unlist` must still parse and match their
package directory and declared architecture; they have no current APT index
hash and remain subject to the consumer's retention/inventory policy. Generic
`tpa verify` intentionally remains compatible with ordinary APT repositories
that have no TPA sidecars; it does not establish the strict v1 tree contract or
reject arbitrary extra files.

## Determinism and inventory

`Packages`, `Packages.gz` (gzip `-n`), and browser sidecars are deterministic
for the same package bytes and repository metadata. By default `Release.Date`
uses the current UTC time. Set a non-negative `SOURCE_DATE_EPOCH` to make that
field reproducible. OpenPGP signatures may still vary. A generation manifest is
kept outside the repository root and hashes the complete file inventory; it is
transport metadata and does not replace APT signature or semantic validation.

A hosted candidate consumer must compare its bounded transport inventory with
the actual tree, reject path traversal, symlinks, special files, unexpected
files, duplicate paths, and resource-limit violations, then independently
validate this format and the APT trust chain. Do not treat a valid inventory as
proof that the repository is valid.
