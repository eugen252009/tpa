# TPA 0.8.0 — consolidated local release candidate

This additive minor release consolidates the already implemented client features;
it does not redesign the TPA/TPA.run boundary. Local candidate version:
**0.8.0~rc1**. No publication/tag/push is implied by a successful local build.

## Included history

- Main ffab7d5: diagnostics, standalone publishing, completions, release guard.
- Direct/utils 32fc08f: metadata-only JSON/JSONL/directory compose and shared pure
  Debian utilities/static browser, without replacing payload directory builds.
- Client-signed SSH 56a4192 and chunk client fec57d9: original signed repository
  streaming, fixed 1MiB SHA256 negotiation, zero-payload repeats and retry reuse.
- Reusable-release 1af2524: release orchestration, source-epoch provenance and
  reproducible build fixes. This orchestration calls normal TPA JSON/build; it
  introduces no second package/repository/signing engine.
- Customer delegation 921b6fc: optional register/login forwarding to the existing
  pinned `tparun customer-ssh` client, not a hosted dependency for standalone TPA.

Both build.sh and reusable self-release configuration share payload staging,
including tpa-utils, tpa-release and Bash/Zsh/Fish completions. Static Linux target
builds are bounded and path-independent. All binaries are packaged with the SAME
source's TPA init/build host bootstrap; ambient installed TPA is not used. Existing
Created-At/TPA-Version semantics and explicit overrides remain. Optional Git-Commit
and Repository-URL document source; completed DEB SHA256 is the only artifact hash.

## Compatibility and qualification boundaries

The receiver contract is protocol2 `tparun-chunk-ssh`, fixed-sha256-1m-v1, against
unchanged TPA.run **0.1.5~rc2**, canonical `tparun-hosting / 1`. Local real SSH,
actual installed RC2, restart/interruption/resume, signatures and HTTP/APT fixtures
qualify compatibility. Production is not contacted or changed for this release.

Ordinary ssh:// output remains an independent OpenSSH/SFTP transport and supports
only new destinations; repeated existing-path publication is rejected safely.
Retained payload/input files define repository snapshots. No persistent hash cache,
provider signing, server-side build, mandatory dashboard or billing is introduced.

The old independent --extend experiment is not merged: it copies an existing pool,
can resurrect deliberately unlisted artifacts and lacks current atomic selection
semantics. Its source/history is retained for an explicit later feature decision.
Older coffeemaker destination/error fixes are superseded by the existing qualified
standalone destination/parser/error implementation, not blindly merged backwards.

See the local consolidation report/inventory and release qualification directory
for exact source commit, target execution methods, installed upgrade results,
artifact hashes and cleanup recommendations. Cross-compiled metadata alone is NOT
architecture qualification. Where QEMU is used, it is explicitly labeled emulated,
not native hardware. No old worktree/evidence/branch is deleted by consolidation.
