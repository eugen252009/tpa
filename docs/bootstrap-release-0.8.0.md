# TPA 0.8.0 bootstrap candidate and release boundary

This is a local preparation plan, not publication or deployment approval. The
public bootstrap remains 0.8.0~rc1 until its signed public distribution is checked.
The candidate version is **0.8.0**, selected before building. It upgrades both
0.8.0~rc1 and 0.8.0~rc2+jsonl1; do not relabel previous qualification packages.
A final version is deliberate: canonical bootstrap CI permits final tags only,
not a prerelease targeting the stable destination.

## Source and licensing

The authoritative module/repository is github.com/eugen252009/tpa. The original
README declared MIT on 1 June 2026 (a86d451), following the 29 May initial commit.
The owner explicitly confirmed that existing license decision and directed the
current attribution to **the tpa.run Team**. LICENSE represents that instruction
with the 2026 origin year; it is not a relicensing of dependencies or TPA.run.
Canonical root LICENSE is GitHub's MIT template with only year/holder substituted.
Remote GitHub detection cannot change before an authorized push.

THIRD_PARTY_NOTICES retains the original Go runtime/standard library, xz, x/sys
and YAML terms, YAML's upstream NOTICE and full Apache-2.0 text. The test-only
check.v1 module is not linked into release tools. External dpkg/GPG/gzip/OpenSSH
are not bundled or relicensed. Browser assets are first-party. Both package build
entrypoints stage license documents under /usr/share/doc/tpa. The mandatory
copyright file includes MIT plus complete dependency notices so Debian slim's
documentation exclusion cannot discard the only copy. Preserved native CI
tool artifacts carry the same notices. TPA.run must be evaluated separately.

## What was missing

The qualified source at 7d9acd7 already has stage/sign and owner-side publication.
The public 0.8.0~rc1 packages predate those commits. No second installer, receiver
redesign or new signing protocol is needed. Build the current complete source
with a selected version through the existing shared payload stager/TPA engine.
The additional packaging correction here is distribution of license notices.

Freeze a signed source commit before building. Pin Go 1.26.3, dependency sums,
SOURCE_DATE_EPOCH to that commit's timestamp and TPA_SOURCE_COMMIT to its full SHA.
Use the existing build.sh or reviewed reusable release workflow, preserve original
archives/manifests, compare independent rebuilds, install the candidate in clean
Debian and qualify exact installed CLI bytes against a disposable current receiver.
Do not rebuild after artifact approval. Hashes identify bytes, not test success.

The realistic staged gate compiles declarative metadata with pinned epoch and
multiple workers, stages exact DEBs alongside a payload package, validates private
staging, interrupted upload/resume, CAS reuse, corrupt and unauthorized requests,
owner-side sign failures/retry, actual receiver SIGKILL/restart, publication CAS,
lost response/idempotency and native APT install by a client without TPA. Use the
existing opt-in receiver gate with TPA_QUALIFIED_BINARY pointing to the CLI copied
from the clean candidate installation, TPA_DIR to these workflow scripts and
TPARUN_STAGE_DOCKER=1. Save its approved manifest, receipt and public repository.

## After local qualification: explicit release authorization is still required

1. Review exact signed source SHA, original DEB SHA256/size/identity manifest,
   toolchain, source archive and executed qualification evidence. Obtain separate
   authorization to merge/push; preserve signed history and protected staging.
2. Reinspect complete live bootstrap inventory and current parent immediately
   before approval. Include every required historical/other bootstrap artifact
   in the next complete snapshot; stages do not merge. Preserve the original
   public signer EDCEE63AA803737C8ABBC1E4A34190A2829D5E54, opaque repository ID
   e47afe7164e1147e5c715e5885d7cee0 and existing Origin/Label/Description.
3. For GitHub CI, configure the restricted TPA_STAGE_SSH_KEY, pinned public trust,
   an isolated owner signing runner/device, and independent reviewer/initiator.
   Self-review prevention/admin-bypass policy must remain enabled. Authorize a
   new immutable v0.8.0 tag only after readiness; a tag runs a new qualified build
   and that run's original bundle must itself be reviewed, never assumed equal.
4. Alternatively authorize existing owner-device stage/sign finalization of the
   locally qualified exact bundle. Inspect the pinned parent and original manifest
   on that trusted device; never export production private keys for convenience.
   This alternative must not be described as GitHub environment approval.
5. After publication, independently verify public InRelease against original
   public trust, every new/historical artifact and sidecar, exact bytes and native
   fresh/upgrade APT installation. Confirm no unintended host updater activation.
6. Only then update website release/version/download examples and remove the
   specific older-bootstrap stage/sign warnings. Frontend deployment requires its
   own authorization and preserves planned Starter/Pro and existing account flows.

## Recovery

Before publication retain the complete prior signed snapshot and inventory,
current generation/parent and public reader tests. On a failed pre-activation
operation the old generation must remain authoritative; inspect/retry the same
stage identity for unknown outcomes, not a new blind publication. A stale parent
requires a new explicitly reviewed complete snapshot, never automatic rebasing.

There is no arbitrary host-side generation reset command or client retained
registry. If rollback is needed after activation, owner-authorize republishing the
retained exact prior signed snapshot using the then-current expected parent and a
new publication identity; verify the public result. Keep new DEBs/receipts for
reconciliation. Metadata rollback does not downgrade clients automatically;
explicit owner-authorized APT downgrade/pinning is separate and must be tested.
Frontend rollback selects the prior immutable frontend, not an older database.
The existing receiver needs no upgrade for this client-only release; never roll
its executable/schema back as a bootstrap or website side effect.

See [bootstrap](bootstrap.md), [staged releases](staged-releases.md), and
[reusable release workflow](reusable-deb-release.md). Per-artifact actual results
and GO/NO-GO decisions belong in the external qualification report, not assumed
from this preparation plan.
