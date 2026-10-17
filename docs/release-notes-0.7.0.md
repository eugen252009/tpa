# TPA 0.7.0 release notes

**Status: release candidate; not published.** Package builds use the normal
`TPA_VERSION` injection path. Supported release architectures are amd64, arm64,
and riscv64.

## Added

- `tpa pack --empty --output DEST` creates and verifies an empty TPA v1 APT
  repository. This is the sole public empty-repository initialization interface;
  it refuses existing destinations. The machine-readable capability is
  `repository.pack-empty.v1`.
- `pack --output` supports outbound SSH/SFTP destinations using SCP-style
  shorthand or `ssh://` URLs. SSH publication verifies a local candidate,
  uploads and reads back the remote tree, and publishes only to a new remote
  destination.
- `tpa capabilities` reports the repository format, output backends, and
  standalone-versus-hosted publication boundaries.

## Improved

- Strict TPA v1 tree verification checks repository metadata, package identity,
  hashes, and the paired `index.html` / `repository.json` browser sidecars.
- SSH/SFTP uses strict host-key checking, bounded two-minute SFTP batches, and
  best-effort cleanup of up to 20 seconds after cancellation or timeout.
  Publication errors report whether activation did not occur, did occur, or is
  uncertain.
- Local atomic publication classifies post-activation errors so callers can
  distinguish an active candidate from a pre-activation failure.
- Package provenance and Debian archive timestamps honor a pinned
  `SOURCE_DATE_EPOCH`, enabling repeatable release builds from the same source.

## Compatibility and limitations

- Normal `pack` still requires package artifacts; empty input is not implicitly
  treated as an empty repository.
- SSH/SFTP publication deliberately refuses every existing remote destination.
  Same-parent rename behavior depends on the SFTP server, and the remote parent
  must exclude out-of-band writers. SSH output is not hosted authorization,
  managed signing, or TPA.run publication.
- The `repo-init` spelling existed only in unreleased development work and was
  removed before the 0.7.0 release candidate; it was not part of the released
  0.6.0 interface.
- TPA does not provide a hosted publication API. TPA.run compatibility and
  production publication are separate gates and are not claimed by these
  release notes.
