# TPA / hosted TPA.run integration contract

The binding ownership rules are in [the architecture contract](../TPA-ARCHITECTURE-CONTRACT.md).
TPA now additionally supports finished client-signed repository publication through
restricted TPA.run SSH v2; see [client-signed-ssh.md](client-signed-ssh.md). That
protocol negotiates fixed1MiB SHA256 chunks and transfers only missing objects. Ordinary SSH/SFTP stays
independent and new-destination-only. Local real-package/signature/sidecar/APT
qualification does not establish compatibility with any deployed TPA.run instance
or production/deployment readiness.

## Ownership boundary

TPA on the owner/developer side owns package construction/parsing, APT indexes,
canonical website generation, local lifecycle changes and signing. Hosted
orchestration owns account/repository authorization, visibility, admission,
quotas/metering, independent verification of received bytes, immutable storage,
generation activation and retention. Signer public trust is server policy;
private signing-key custody remains with the owner, not the host.

Publish only a completed stable candidate. The receiver verifies and activates
exact received content; it must not run a repository builder, repair indexes,
regenerate the website/provenance or replace signatures. Reuse TPA verification
semantics without granting the client direct hosted live-filesystem access.
Legacy server-build/signing `stage` / `publish` workflows are not the target
architecture. Their earlier qualification does not override this ownership.

## Capability negotiation

1. Pin the exact trusted TPA executable and its authorized source commit/hash
   outside the candidate. A compatible version string is not provenance.
2. Run `tpa capabilities`, require `format == "tpa-capabilities"` and
   `version == 1`, then require the exact `repository_format` name/version and
   needed operation identifiers. For an empty repository, require
   `repository.pack-empty.v1` and invoke `tpa pack --empty`. Reject unknown or
   missing required fields conservatively.
3. Require the expected platform/atomic behavior. Linux atomic replacement is
   a local filesystem operation; it is not a remote hosted publication
   primitive. Filesystem support can still make the operation fail.
4. After generation, independently validate the candidate against
   [repository-format-v1.md](repository-format-v1.md), the signed APT metadata,
   package bytes, expected identity, and the bounded transport inventory.
5. Submit through the separately authorized finished-tree SSH receiver using
   `tpa publish`, explicit owner public trust and expected active parent. Only the
   receiver's independently verified generation may become visible. Resolve
   uncertain activation rather than blindly retrying or rebuilding server-side.

Capabilities advertises `repository.publish-client-signed-tparun-ssh.v2` (plus explicit legacy v1),
`publication.client_signed_ssh`, its protocol name/version and explicit
`client_signed_ssh_chunked_cas: true`. These additive schema-v1 fields describe
an outbound client, not a TPA upload server. The standalone transport inbound/
authenticated-upload server flags and hosted-managed-publication flag stay false.
Ordinary `pack` output backends remain local-filesystem / ssh-sftp; the explicit
hosted destination currently belongs to the finished-tree `publish` command.
Local owner-side signing may use the caller's GPG environment; publishing needs
only public verification trust, never private provider signing material.

## Operation model (semantic, not wire format)

| Operation | Request meaning | Success result | Important failure boundary |
| --- | --- | --- | --- |
| `repository.compose-metadata.v1` | `tpa compose -in` metadata-only JSON/JSONL/directory, `-out` new local destination, explicit repository metadata/signer and resource/epoch options | Independently verified v1 tree containing in-process metadata-only DEBs and canonical sidecars | New destination only; no payload files, remote activation, hosted account, managed key creation or replacement; see `direct-compose.md` |
| `repository.pack-empty.v1` | `tpa pack --empty`, no package input, repository Release metadata (defaults: suite/codename `stable`, component `main`), optional architecture list and signer | Verified empty v1 tree with empty indexes and paired sidecars | Refuses every pre-existing destination; no live tree is replaced |
| `repository.pack.v1` | Normal `tpa pack` with a fresh `.deb` artifact set and repository metadata | Verified snapshot derived from artifacts | Empty artifact input fails; duplicate identity with conflicting bytes fails |
| `repository.publish-client-signed-tparun-ssh.v2` | `tpa publish` with completed owner-signed Stable/main tree, public trust and explicit `tparun+ssh://` repository | Missing chunks verified/leased; exact full files verified; CAS-backed generation activated and served | No provider signing/building; no automatic v1 downgrade; authorization and expected parent checked durably |
| `repository.publish-client-signed-tparun-ssh.v1` | Explicit legacy Go option `LegacyFullTree` | Original full-tree receiver remains available | Not the default CLI or negotiated fallback |
| `repository.publish-ssh-new.v1` | Verified local candidate plus an SSH/SFTP destination | Remote tree read back and verified, then renamed to a new path | Existing remote destinations are refused; activation semantics depend on the SFTP server and the remote parent must exclude out-of-band writers |
| `repository.inspect.v1` | Read-only locator and optional exact identity | Bounded metadata report; does not fetch package bytes | Read-only; signature may be present but untrusted without an explicit key |
| `repository.verify.v1` | Read-only locator and optional public key/fingerprint policy | Verified indexes, indexed artifact bytes, identities, and signature status | Generic APT verification does not require TPA browser sidecars or enforce an exact tree inventory |
| `repository.unlist.v1` | Local tree plus exact Package/Version/Architecture and signer if signed | New metadata generation with artifact retained | Verifies and atomically activates metadata before success |
| `repository.delete.v1` | Local tree plus exact identity and explicit destructive approval | Verified unlist if needed, then artifact removal | Cancellation is a no-op; cleanup failure after unlisting leaves the safe unlisted state |

The table names semantic operations, not proposed HTTP paths or SSH messages.
Path/destination flags such as `-in`, `-out`, `--output`, and
`--atomic-publish` are not hosted identifiers or remote authorization. A hosted API must bind each operation to
an authenticated account/repository, authorization scope, capacity reservation,
request/job identifier, immutable candidate identity, and expected active
parent according to its own versioned policy. These values must be checked
server-side and must not be inferred from untrusted candidate metadata.

## Error and activation semantics

Callers use process exit status or typed Go errors, not diagnostic-string
matching. Failures before atomic activation leave the prior live tree unchanged.
`AtomicPublishError` has `Published == true` for failures after local activation;
its stable `Stage` is `sync-parent` or `cleanup-replaced-tree`. A sync failure means
durability of the activation rename is uncertain; cleanup failure leaves the new tree active and
the old tree at its staging path. Callers must not blindly retry: independently
inspect/verify the live tree and resolve the reported post-activation state. A
hosted staging adapter must treat candidate creation, upload, server-side
validation, and active-generation publication as separate states; a failed
stage must never be mistaken for a published candidate.

Direct local `Pack` is for a new or empty output path and is not atomic. SSH/SFTP
output is new-destination-only and is not a hosted activation primitive.
`AtomicPack` verifies an existing local destination as a complete TPA v1 tree (and verifies its
current signature with the selected `-gpg` signer when signed) before
replacement; key rotation requires a separate migration, and arbitrary paths are
refused.
Use it for a local tree read concurrently. Hosted systems must not use
`AtomicPack` against the hosted live path; they stage an immutable candidate and
use the hosted activation mechanism.

## SSH and deployment boundary

TPA has three distinct outbound SSH uses: read-only repository inspection,
ordinary SFTP publication to a new remote path, and explicit finished-tree
TPA.run SSH publishing. Ordinary output does not become a hosted API, replace
existing destinations or require a TPA.run account/daemon/CAS. Its same-parent
rename remains server-dependent and the parent must exclude out-of-band writers.

The separate hosted protocol is restricted and repository-authorized, not a
shell/SFTP endpoint. SSH key possession alone is insufficient: account/key
liveness, current write permission, repository ownership/policy, quota, exact
content/signature and expected parent must also pass. The TPA client itself
implements no inbound upload listener. Its sibling server implementation and
client are locally qualified for chunk transfer, direct CAS HTTP, interrupted
reconnect, actual receive-only process restart and explicit conservative GC.
Canonical core schema consolidation, exhaustive crash/restore and release
qualification remain separate; see the implementation report. Never equate a local handler test with deployment qualification.

This document and the standalone TPA qualification do not establish TPA.run
production compatibility, quota enforcement, authorization correctness,
sandboxing, availability, or deployment readiness. Those remain separately
qualified and approved hosted-system responsibilities.
