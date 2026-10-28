# Persistent CAS stages and owner-side signed releases

**Upload many → approve once → owner-side TPA sign → SSH-CAS atomic publish.**

TPA.run retains validated package bytes in its existing repository-scoped CAS.
It never creates APT indexes, constructs DEBs, signs metadata or holds owner
private keys. Owner-side `tpa sign` composes the existing Pack, GPG, verification
and finished-tree SSH-CAS publisher. Direct `tpa publish` remains supported.

## Commands

Use an existing authorized repository and its configured owner public signer:

```sh
destination=tparun+ssh://tparun@host:2222/0123456789abcdef0123456789abcdef
stage=$(python3 -c 'import secrets; print(secrets.token_hex(16))')

tpa stage create --output="$destination" -stage="$stage" \
  -manifest=release-manifest.json -ssh-config=owner-ssh.conf

# Independent jobs share the repository, stage ID and authenticated account.
tpa stage upload --output="$destination" -stage="$stage" \
  -in=core_1.2_all.deb -ssh-config=owner-ssh.conf
# Upload every expected artifact, then inspect the complete ready manifest.
tpa stage inspect --output="$destination" -stage="$stage" -ssh-config=owner-ssh.conf

# Owner machine / independently approved owner-side runner:
GNUPGHOME=/owner/gnupg tpa sign "$destination" -stage="$stage" \
  -manifest=release-manifest.json -ssh-config=owner-ssh.conf \
  -gpg=OWNER_FINGERPRINT -keyring=owner-public.gpg -fingerprint=OWNER_FINGERPRINT

# Only unpublished stages can be cancelled:
tpa stage cancel --output="$destination" -stage="$stage" -ssh-config=owner-ssh.conf
```

The existing release bundle's `release-manifest.json` is accepted directly.
A standalone manifest can contain just `{"artifacts":[...]}`. Each entry has
`filename`, `package`, `version`, `architecture`, `size`, and lowercase `sha256`.
Hashes identify qualified archive bytes, not repacked DEBs. Filenames and package
identities must be unique. Safe leaf filenames are at most 255 bytes; artifact
count (4096), logical size, extents, and control frames are bounded. At most 64
open/finalizing stages per owner are admitted within existing capacity limits.

Create binds the original manifest SHA256, artifact projection, repository,
creating publisher, repository owner and base generation. Default
`-parent-generation=auto` snapshots the current generation transactionally;
explicit parents pin an already-inspected generation. An exact create retry
cannot rebind inputs. Retain the explicit stage ID for recovery.

**The manifest describes a complete new snapshot**, not an implicit merge with
the base. Include all versions/packages that the next snapshot should retain.
The base is a concurrency precondition. Different stages never merge implicitly.
Preserve the repository's existing `-origin` and `-label` client policy values.
The current receiver uses Stable/main; channels use distinct repository URLs.

## Lifecycle and security

Stage states: `OPEN → FINALIZING → PUBLISHED`, or explicit `CANCELLED`.
Per-artifact state: expected/uploading/ready. No generalized workflow engine.

- Restricted SSH checks live credentials, repository publication policy and owner
  quota before admission. The creator or repository owner may access the stage.
  Existing Write permission is reused; this is **not** a new upload-only key
  scope. Signing authority remains the separate owner GPG identity/public trust.
- Different artifacts upload concurrently. An in-flight duplicate can conflict;
  retry after it finishes. Sequential identical repeats send zero payload bytes.
  Hash knowledge never grants cross-repository content access.
- Private, bounded verification scratch checks whole-file SHA256 and real Debian
  identity before ready status commits. Immutable objects, leases, reservations
  and byte receipts use the existing canonical hosting DB/CAS.
- Disconnect/startup resets interrupted upload claims, retaining committed chunks
  and immutable manifests. Persistent stage leases protect pending input from GC
  until explicit cancellation or successful publication.
- Finalization requires every expected upload ready, then freezes contents. Owner
  retrieval checks approved hashes/identities again. Pack derives only metadata
  and signatures, using the existing DEBs without invoking package builds.
- The receiver independently verifies the complete signed tree and its exact
  frozen DEB set. Stage receipt, activation, CAS settlement and existing activation
  events share the canonical transaction. Existing publication slots and CAS
  parent checks serialize competing activations.
- Signing failure leaves the old tree and a frozen, retriable stage. A failed or
  recovered receiver attempt can receive a new attempt ID; concurrent finalizers
  share one attempt. A completed stage always returns its original receipt after
  a lost response/retry, even if a newer release has since become active.
- Stale parents fail closed, without rebasing. Cancel and create a newly reviewed
  stage. Cancel stages before ownership transfer: owner changes invalidate the
  immutable stage ownership binding.
- Cancellation refuses active uploads/publications and published stages. Retry
  after disconnect/settlement. Repository deletion cannot race an active upload.

Staged artifacts have no ordinary APT/reader URL. Existing private readers,
JSONL/fixed viewers, one-domain routing, native services and retention remain
unchanged. Canonical receive-only hosting is required; legacy managed ingestion
and server-side signing are not used.

## Existing GitHub Actions integration

Optional reusable inputs: `apt-destination`, `apt-channel` (`staging` or `stable`),
`apt-known-hosts`, `apt-public-key`, `apt-signing-fingerprint`. Preserve existing
APT client policy with `apt-origin`, `apt-label`, and `apt-description` (including
when moving a repository into CI). These extra inputs are for `workflow_call`;
manual dispatch remains qualification-only.
Without a destination, GitHub-only releases remain supported.

APT publication requires `release.source_branch: staging` and
`release.publish_environment: tpa-release`. That existing environment must require
independent reviewers, prevent self-review and disallow administrator bypass.
Policy is checked before builds and again before signing. Repository rules,
reviewed source, protected tags and GitHub's deployment scheduler remain trusted
external controls; local tests do not claim a real GitHub review occurred.

1. Build/test/install/rebuild qualification and bundle verification finish first.
2. Native TPA/release tools are preserved by immutable artifact ID and hashes.
3. Stage creation binds the qualified manifest; one upload job per filename
   verifies and uploads its exact qualified artifact.
4. A complete-ready-manifest barrier must succeed before publication approval.
5. The protected job downloads the original bundle/tools, rechecks hashes and
   policy, signs on the owner-side runner, and invokes SSH-CAS. No Go setup/build
   or DEB construction occurs after approval.
6. GitHub release publication uses that same approved bundle.

Supply caller secret `stage-ssh-key` only to stage/publication jobs, never package
build/test jobs. Supply existing owner key material only in protected environment
`tpa-release`: `APT_STAGING_SIGNING_PRIVATE_KEY` / `APT_STABLE_SIGNING_PRIVATE_KEY`
and optional corresponding `APT_*_SIGNING_PASSPHRASE`. The hosted-runner adapter
imports into a private temporary owner-side GPG home and removes it/its agent on
exit. Nothing transfers that key to TPA.run. Local CLI use may instead use an
existing owner agent/hardware identity without exporting its secret. The reusable
workflow also accepts `apt-publication-runner` (default `ubuntu-24.04`) and
`apt-signing-home`: on a protected owner-side Linux amd64 runner, a nonempty
absolute existing home reuses that agent without importing/exporting keys,
killing the agent, or deleting owner state, including on failure. No private-key
secret is required in this mode. The runner must enforce approved-workflow/run
isolation; do not expose this signing home to arbitrary public-repository or PR
jobs. Merely assigning a runner label is not such isolation.

The TPA caller pins final tags to the existing canonical bootstrap repository,
its original fingerprint, and its existing Origin/Label/Description. It uses:

- `TPA_APT_STAGING_DESTINATION` (explicit opt-in; cannot equal bootstrap)
- `TPA_APT_STAGING_PUBLIC_KEY`, `TPA_APT_STABLE_PUBLIC_KEY`
- `TPA_APT_STAGING_FINGERPRINT` (the bootstrap fingerprint is pinned in source)
- `TPA_APT_KNOWN_HOSTS`, plus caller secret `TPA_STAGE_SSH_KEY`
- Optional `TPA_APT_PUBLICATION_RUNNER`, `TPA_APT_SIGNING_HOME` for the isolated
  protected owner-side runner described above.

Prerelease tags select staging; final tags select stable. Missing staging config
never falls through to stable. Unset staging destinations disable staging APT
publication. Final TPA tags cannot silently fall back to GitHub-only publication;
missing bootstrap credentials or receiver support fail closed. Both use the
current Stable/main tree contract at distinct URLs, not a new channel service. Stable clients must not enable the opt-in staging
source. Promotion is explicit: stage the approved DEBs to the stable repository
and obtain independent approval. RC-to-final still requires a new version/build
and qualification; filenames cannot change Debian control metadata.

## Local qualification and rollout

```sh
cd ../tparun-staged-releases-local
TPA_DIR=/path/to/tpa TPARUN_STAGE_QUALIFICATION=1 TPARUN_STAGE_DOCKER=1 \
  go test -race -count=1 -timeout=5m ./cmd/tparun -run '^TestFivePackageOwnerStageSSH$' -v
```

This uses actual SSH/GPG/CAS and the actual CI adapter in all four modes, five
independent upload processes, malformed/checksum-mismatched wire input, denied
credentials, interruption/resume, a SIGKILL/restart of the real receive-only
executable, concurrent finalizers, activation rollback, lost response/retry,
stale competing stages and a real signed Debian APT client. A wrapper refuses
post-qualification `dpkg-deb --build`. Every downloaded archive hash is checked.

Requires Go, OpenSSH, GPG, dpkg-deb, Docker and `debian:trixie-slim`.
`TPARUN_STAGE_EVIDENCE` retains only public manifests, receipts, metadata and APT
logs. `TPARUN_DOCKER_HOST_ADDRESS` optionally enables a temporary GET-only fixture
proxy for remote/rootless Docker; no production endpoint or key is used.

No deployment, credential provisioning, branch/environment creation or APT-source
change is implicit. Before rollout, back up stopped canonical state and retain
the matching prior executable. Strict validation permits the known retained
pre-stage schema to be extended transactionally; existing credentials, CAS bytes,
events, identities and generations remain. An old binary may reject new tables:
rollback restores its matching full stopped-state backup, never resets a live DB.
