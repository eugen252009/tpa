# TPA 0.8.0 — local final-version bootstrap candidate

This additive minor release consolidates the implemented client features without
redesigning the TPA/TPA.run boundary. Candidate version: **0.8.0**, selected before
building. Public bootstrap remains **0.8.0~rc1** until independently verified
publication. No publication, tag, source push or deployment is implied here.

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

- Persistent private stages and owner-side `tpa sign`: qualified source c4fd4bd
  and canonical bootstrap/owner-agent wiring 7d9acd7. Upload exact qualified DEBs,
  review the original manifest on the owner device, sign APT metadata locally,
  and activate via authenticated expected-parent SSH-CAS. No post-approval rebuild
  or server signing. CI requires explicit credentials and independent approval.
- Established MIT license representation by the tpa.run Team, with 2026 origin
  year, complete dependency notices, and notices retained in the mandatory Debian
  copyright file even when slim images exclude optional documentation.

Both build.sh and reusable self-release configuration share payload staging,
including tpa-utils, tpa-release and Bash/Zsh/Fish completions. Static Linux target
builds are bounded and path-independent. All binaries are packaged with the SAME
source's TPA init/build host bootstrap; ambient installed TPA is not used. Existing
Created-At/TPA-Version semantics and explicit overrides remain. Optional Git-Commit
and Repository-URL document source; completed DEB SHA256 is the only artifact hash.

## Compatibility and qualification boundaries

The receiver contract is protocol2 `tparun-chunk-ssh`, fixed-sha256-1m-v1, against
the existing TPA.run receiver. Direct publication remains compatible with the
previous RC2 baseline; staged finalization requires the stage-capable receiver
**0.1.5~rc9+stages.8462183**, canonical `tparun-hosting / 1`. Local real SSH,
restart/interruption/resume, signatures and HTTP/APT fixtures qualify exact client
artifacts. Public version/repository reads check distribution state; production
is never mutated by local qualification. Native and emulated scope is recorded
per artifact, not inferred from cross-compilation.

Ordinary ssh:// output remains an independent OpenSSH/SFTP transport and supports
only new destinations; repeated existing-path publication is rejected safely.
Retained payload/input files define repository snapshots. No persistent hash cache,
provider signing, server-side build, mandatory dashboard or billing is introduced.

The old independent --extend experiment is not merged: it copies an existing pool,
can resurrect deliberately unlisted artifacts and lacks current atomic selection
semantics. Its source/history is retained for an explicit later feature decision.
Older destination/error fixes are superseded by the existing qualified
standalone destination/parser/error implementation, not blindly merged backwards.

See the local consolidation report/inventory and release qualification directory
for exact source commit, target execution methods, installed upgrade results,
artifact hashes and cleanup recommendations. Cross-compiled metadata alone is NOT
architecture qualification. Where QEMU is used, it is explicitly labeled emulated,
not native hardware. No old worktree/evidence/branch is deleted by consolidation.
