# Canonical public bootstrap repository

The single canonical public bootstrap URL is:

**https://tpa.run/r/tparun/bootstrap**

Keep this existing path for all future bootstrap releases unless the owner
explicitly changes that decision. Do not create `/r/tpa/bootstrap` as a
replacement or parallel bootstrap repository.

Repository identity does not determine package names. This repository can contain
qualified TPA, TPA.run, MEMA, BLL and other bootstrap dependencies. A release of
`Package: tpa` does not require renaming the repository.

## Trust and publication

The original bootstrap signing fingerprint is:

```text
EDCEE63AA803737C8ABBC1E4A34190A2829D5E54
```

Signing remains client-owned. Build the complete repository with local TPA and
sign exclusively on the owner's computer using this existing identity. TPA.run
receives, verifies, deduplicates, stores and serves the original uploaded bytes;
it does not build packages, generate replacement indexes or sign repositories.
The host and any optional updater need public trust material, never a private
signing key. Do not substitute a test key or silently rotate trust.

Before replacing a generation, inspect its owner, authorized SSH identity,
current generation, complete package inventory, installer aliases and any updater
subscription. Preserve necessary already-qualified bootstrap artifacts; a new
TPA-only input directory must not silently remove other clients' dependencies.
A public HTTP 404 is not evidence that the repository is empty or unowned.

The HTTP URL is not itself a supported `tpa publish` destination. Resolve the
existing repository's opaque ID and use the actual hosted transport:

```sh
# All paths and IDs below must be selected from the reviewed installation.
FINGERPRINT=EDCEE63AA803737C8ABBC1E4A34190A2829D5E54
GNUPGHOME="$OWNER_GPG_HOME" tpa pack -in="$COMPLETE_QUALIFIED_INPUT" \
  -out="$NEW_FINISHED_TREE" -gpg="$FINGERPRINT" \
  -suite=stable -codename=stable -components=main

tpa verify --require-signed -keyring="$BOOTSTRAP_PUBLIC_KEYRING" \
  -fingerprint="$FINGERPRINT" "$NEW_FINISHED_TREE"

tpa publish -in="$NEW_FINISHED_TREE" \
  --output="tparun+ssh://tparun@tpa.run:2222/$EXISTING_REPOSITORY_ID" \
  -ssh-config="$PINNED_SSH_CONFIG" -keyring="$BOOTSTRAP_PUBLIC_KEYRING" \
  -fingerprint="$FINGERPRINT" -parent-generation="$CURRENT_GENERATION" -json
```

These are templates, not proof that this account, SSH port, ID, current generation
or local signing capability is available. Use the existing authorized connection;
do not create a second repository to work around missing authority. For an
actually empty repository, follow the existing CLI's initial-publication contract
instead of inventing a parent. See [client-signed SSH publication](client-signed-ssh.md).

Preview the actual TPA-generated `index.html` over local HTTP before activation.
An unsigned tree may be used for preview only and must never replace the public
signed repository. Qualify its relative links, `repository.json`, all package
hashes and both desktop/mobile Firefox layouts. The qualified 0.8.0~rc1 CLI has
no documented `Valid-Until` option; do not patch it or invent a flag for a release.

After activation, verify public package hashes, the intended InRelease signer,
a disposable Debian client's APT installation and the hosted website. Retain the
previous generation and confirm its recovery procedure. Reuse the exact finished
signed tree for a zero-payload repeat-upload check; do not regenerate signatures
or timestamps between uploads.

Publishing permission is not OS-update authorization. Check any configured
consumer's fixed package/source/signing policy before publication; a bootstrap
update must not cause an unapproved TPA.run runtime installation. This document
does not enable an updater, configure `.tpa` project defaults or authorize backend
changes.

## Availability versus canonical identity

The canonical URL is a binding path decision, not a claim that a release is live.
Use the publication report and current HTTP/APT checks to establish availability.
The TPA 0.8.0~rc1 publication evidence is staged under
`~/projekte/releases/tpa/0.8.0~rc1/publication/`; its exact packages must not be
rebuilt or modified for publication.
