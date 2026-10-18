# Modern repository explorer — local qualification

Verdict: **MERGED / LOCALLY QUALIFIED**, NOT published/deployed.
Runtime/UI source commit: **1237a8ef8c20d7e17e7f8f7e5cd2907502fb06bb**.
The following qualification-only acceptance commit does not change runtime assets.

Official source is an isolated worktree of the authoritative TPA repository:
`/home/eugen/projekte/tpa-modern-repository-ui-20261018`, branch
`integration/modern-repository-ui-20261018`. Main7eafccc and its existing history
remain unchanged. Base198c749 includes the canonical-path documentation. The
original dirty feature/tpa-utils patch/status AND every untracked file were
compared with the starting snapshots and are unchanged. No source was pushed.

## Actual fixtures and browser evidence

Persistent qualification root:
`/home/eugen/projekte/tpa-modern-repository-ui-evidence-20261018/`.

Live LOCAL review: **http://127.0.0.1:46813/representative/**.
The server binds127.0.0.1 only, serves site-final/ only, and does not expose the
qualification signing-key directory. Source fixture/generation scripts are in
that root (generate-preview.py, rebuild-site.py, serve-preview.py).

Actual inputs: three qualified TPA0.8.0~rc1 DEBs, four BLL0.2.0 DEBs, two MEMA
versions0.4.1/0.4.2, and one newly built long/hostile-description fixture. Ten
package identities render as eight grouped versions. Other actual generated
outputs: empty repository, independent utility-v1 browser, and2000 DEBs built by
tpa compose (NOT a mock JSON website or50k performance rerun).

Real Firefox153.4.0esr,1440/390 CSS viewports:

- overview, metadata text safety and complete architecture grouping;
- real keyboard search, architecture/category filtering and all handling;
- actual relative artifact links and full size/hash fields;
- architecture-tabbed modal, real Escape and opener focus restoration;
- empty repository and independent utility JSON schema;
-13 large-list page transitions, hard maximum50 live package cards;
- mobile sticky filter collapse/expansion and no horizontal overflow;
- exact current production-CSP MIRROR: script-disabled static details/download
  links remain present. No production policy change or interactivity claim.

Direct paths to REAL screenshots (screenshots/ under the evidence root):

- overview-1440.png, overview-390.png
- search-1440.png, search-390.png
- filter-1440.png, filter-390.png, category-1440.png, category-390.png
- details-1440.png, details-390.png
- large-1440.png, large-390.png
- empty-1440.png, empty-390.png
- mobile-expanded-filters.png
- current-host-csp-fallback-1440.png and390.png

Final images were visually reviewed. An inherited content-visibility placeholder
issue caught by screenshot review was fixed BEFORE source freeze; current cards
are actually painted rather than blank. The hard50-card bound replaces the old
potentially unbounded observer/Show-more behavior. CSS/JS have no CDN/framework.

## Gates — PASS

Bounded GOMAXPROCS=2, GOFLAGS=-p=2; independent qualification streams capped at2.

- go test -count=1 ./...
- go test -race -count=1 ./...
- go vet ./...
- git diff --check
- tests/qualification.sh: actual DEB/signing/APT install/upgrade/downgrade/lifecycle
- tests/repository-contract-qualification.sh
- tests/ssh-output-qualification.sh
- tests/dependency-qualification.sh
- tests/version-qualification.sh: package/runtime/provenance/tool consistency
- tests/direct-pipeline-qualification.sh: Direct-RAM metadata-only DEB, worker/input
  determinism, signed APT dependencies and browser isolation; no50k measurement
- tests/signed-ssh-repository-qualification.sh: actual BLL/MEMA, ordinary OpenSSH
  SFTP output, independent readback, new-destination guard and Debian12 APT upgrade
- public Go-library importer using tpautils.Inspect: original public signer and
  strict VerifyTPARepositoryTree for BOTH original legacy0.8.0 AND new modern HTML
- shared model/URL/parser/escaping and shipped-JS oracles, including conflicting
  identity/metadata rejection, safe links, both schemas and repeated bounded pages
- benchmark oracle unittest and EMPTY source diff under bench/; historical benchmark
  source/evidence preserved, no50,000-package benchmark rerun
- source index rebuilt without errors

Container tests used an existing ROOTLESS Podman API socket. The default Docker
BuildKit attempt failed because local catatonit is absent; the unchanged test
scripts passed with the supported Docker legacy build API (DOCKER_BUILDKIT=0).
No host package/service was changed to make the qualification pass.

## Actual unchanged TPA.run backend compatibility — PASS

Disposable installed **exact bootstrap2 DEB**, SHA
b7cd8d1fa440b929cd544ef9a3144db3930a16631dda089a64287882f3698c2e,
source2338522, canonical schema1. Its running server was not rebuilt/modified.
The new TPA client uploaded actual modern HTML with unchanged canonical-v1 JSON
and original signed APT bytes; receiver verification/activation, exact full HTTP
readback, signed native amd64 Debian12 APT installation, retained previous
generation and identical-repeat ZERO payload all passed.

Accepted evidence:
`cas-qualification/evidence/installed-3251eea13f73872ba144f93fed7a46c0/`.

The private TEST harness archive changed only its browser selector to understand
new cards (static fallback OR loaded interactive badges) instead of the obsolete
static table. No serving-source or artifact change was made. An initial selector
race and an explicit-signer omission in the standalone strict-verification driver
were diagnosed/fixed in qualification code; mandatory assertions were retained.
The original signed release, original13-file manifest and live production HTML
were checked unchanged. This is local transport/backend compatibility, NOT a new
production publication or a claim that current production CSP permits scripts.

## Remaining work and next phase

The modern UI is available for owner review; no production generation was
replaced. Interactive hosting on current account-origin production remains subject
to its restrictive CSP. The existing isolated RepositoryOrigin capability is the
place to review policy separately; do not make TPA.run render repository HTML.

Uncommitted utility-v2 JSONL streaming, bulk selection, archive/export and optional
local serve remain intentionally unmerged in the preserved utility worktree.
The older direct-json-utils branch is already in main and has no newer unique UI;
tpa_repo_lister has an older generator. No branch/worktree was deleted. No new
remaining duplicate APT parser or live browser implementation was introduced;
the legacy template remains only for strict old-artifact verification.

See [separate main-website assessment](tparun-website-next-phase.md). The proposed
next worktree is `/home/eugen/projekte/tparun-main-website-modernization`, based on
exact deployed2338522, not the broader unapproved webhook/updater branch. No
TPA.run redesign, deployment, account/Nginx mutation or hosting-architecture rebuild
was performed as part of this TPA integration.
