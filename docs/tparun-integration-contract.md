# TPA / hosted TPA.run integration contract

This is a transport-independent boundary for a future hosted adapter. It does
not add a hosted API, authenticated hosted-upload protocol, authorization
mechanism, or deployment. Standalone TPA separately supports outbound SSH/SFTP
publication to a new remote path; that is not hosted publication.
Compatibility with any deployed or checked-out TPA.run build is **NOT VERIFIED**;
in particular, an adapter must prove that its candidate verifier accepts TPA's
paired root `index.html` and `repository.json` sidecars before use.

## Ownership boundary

TPA owns Debian package parsing, APT repository generation, local lifecycle
semantics, candidate verification, and optional local GPG signing. Hosted
orchestration owns account/repository authorization, visibility, quotas and
admission, trusted binary/source selection, sandbox/resource limits, independent
candidate validation, signing policy/key custody, generation activation,
rollback, and retention references.

A hosted caller may invoke TPA as a subprocess in a fresh isolated work area or
use the exported Go repository operations. Stop the builder and its descendants
before validation, and validate a server-owned immutable copy rather than a
concurrently writable work tree. It must not duplicate TPA's package
index, artifact-hash, or lifecycle semantics. It must also not give standalone
TPA direct write access to hosted live storage. Publication uses TPA.run's
authorized `stage` / `status` / `publish` workflow only; staging and publication
must be separately qualified against the intended server baseline.

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
5. Only then submit the candidate through the separately authorized hosted
   staging API. Query its status and publish only the staged candidate that
   passed independent checks. Do not write directly into generation storage.

The capabilities document explicitly reports that this standalone TPA has no
hosted managed publication, authenticated upload, or inbound SSH upload. It
supports local operations, read-only HTTP(S)/SSH repository inspection,
outbound SSH/SFTP publication to a new remote path, and optional GPG `InRelease`
signing using the caller's GPG environment. A hosted
managed signer must remain outside the package-build process; never pass hosted
private signing material to TPA.

## Operation model (semantic, not wire format)

| Operation | Request meaning | Success result | Important failure boundary |
| --- | --- | --- | --- |
| `repository.compose-metadata.v1` | `tpa compose -in` metadata-only JSON/JSONL/directory, `-out` new local destination, explicit repository metadata/signer and resource/epoch options | Independently verified v1 tree containing in-process metadata-only DEBs and canonical sidecars | New destination only; no payload files, remote activation, hosted account, managed key creation or replacement; see `direct-compose.md` |
| `repository.pack-empty.v1` | `tpa pack --empty`, no package input, repository Release metadata (defaults: suite/codename `stable`, component `main`), optional architecture list and signer | Verified empty v1 tree with empty indexes and paired sidecars | Refuses every pre-existing destination; no live tree is replaced |
| `repository.pack.v1` | Normal `tpa pack` with a fresh `.deb` artifact set and repository metadata | Verified snapshot derived from artifacts | Empty artifact input fails; duplicate identity with conflicting bytes fails |
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

TPA has two distinct outbound SSH uses: read-only repository inspection through
the system `ssh` client, and SFTP publication of a verified repository tree to a
new remote path. SSH output does not replace an existing tree, execute remote
shell commands, authenticate a hosted publication request, or activate hosted
generations. Its same-parent rename behavior is server-dependent, and the remote
parent must exclude out-of-band writers. It is not an upload server or hosted
publication API. SSH-key login to a hosted account is not itself repository
publication authorization. Any inbound SSH protocol requires a separate
restricted server design and qualification; this contract intentionally does
not invent one.

This document and the standalone TPA qualification do not establish TPA.run
production compatibility, quota enforcement, authorization correctness,
sandboxing, availability, or deployment readiness. Those remain separately
qualified and approved hosted-system responsibilities.
