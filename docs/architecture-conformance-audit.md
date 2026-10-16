# TPA ecosystem: strict architecture conformance audit

Audit date: 2026-10-16. Authority: [TPA-ARCHITECTURE-CONTRACT.md](../TPA-ARCHITECTURE-CONTRACT.md),
recording the owner's binding client-built/client-signed/SSH-published decision.
This is an audit, not implementation of a new backend. Only contract, instructions
and audit documentation changed in isolated product worktrees.

## Executive answer

**Most developer-side package/repository construction already conforms.** TPA
can independently build, sign, verify and generate complete static repositories.
Generic SSH transport is real and reusable, but is new-destination-only and a
newly reproduced inventory-order defect blocks an ordinary real mixed BLL/Mema
repository. It is not yet generally qualified for that corpus.

**TPA.run's standard staged-package publishing path is an ARCHITECTURAL DEVIATION:**
it copies packages, invokes TPA on the server, generates new indexes/websites and
signs with the server's configured key. Calling the shared TPA binary does not
make that responsibility client-side. Existing tests prove the old path works,
not that it conforms. There is no evidence that this path repacks the `.deb`
archives; qualified DEB hashes were preserved. The deviation is repository
construction/signing and orchestration, not invented package-byte rewriting.

**A substantially better-aligned receive path already exists:** client-signed
prebuilt-generation admission verifies and stores original bytes and signatures
with owner-scoped public signer trust, quota checks, parent compare-and-swap and
activation recovery. Reuse it. Its HTTP transport, full re-upload, limits and
lack of CAS/chunk references do not satisfy the required optimized SSH path.

**End-to-end target conformance is not achieved.** Inbound SSH publication,
missing-content negotiation, chunked storage/reference lifecycle and a unified
minimal hosting core remain missing. SSH login and a remote build worker are
not that protocol. No honest single completion percentage captures these
boundaries: the local builder is substantially present, the exact receiver is
partially present, and the essential SSH/CAS join is absent.

The former recommendation to prioritize historical Schema17/20 bridge migrations
is **superseded**: prefer a fresh canonical baseline when verified retention
needs do not justify bridges. Do not reset deployed state during this audit.

## 1. Sources, scope and evidence

Immutable source abbreviations below:

- **T**: TPA feature source `32fc08fbfdf11e2c56a18a223ca2659899038f92`, original
  `/home/eugen/projekte/tpa-direct-utils`; TPA main `ffab7d5`, clean.
- **B**: audited backend `211fd6d29f5ae14111dbeb86b425a3338555423c`, at
  `/home/eugen/projekte/tpa-ecosystem-review-20261016T141403Z/backend-unified`.
  Shipping implementation is f6a5498; final commit only separates test evidence.
  This misleadingly named branch is NOT a consolidated customer-security backend.
- **S**: customer-security source `994cba848c56d530b44d3dd70d4fce1a27fc8ab0`.
  `/home/eugen/projekte/tparun` has unrelated help/package modifications, retained.
- **M**: meter/suite/importer source `1357ec7b30e43ffb519ea8778bb13625176e127b`;
  importer `b615d7e` is its ancestor, not an additional implementation to rebuild.
- **BLL**: original3f08321 with dirty README retained; isolated rollback fix
  `8994da8`, four retained0.2.0 packages.
- **Mema**: release `2715ba6` core0.4.2 and main `d260cf2` core0.4.1. Native dpkg-deb
  builder, not a falsely claimed TPA archive build. Packaging can be adapted later
  without a mandatory payload schema. Original sources remain unchanged.

Current public `/version` GET independently reports0.1.1, Schema11,
`044e0d18a562e1d6974500bf8c957cebb9e5aba6`. It does not reveal accounts, repos or
signing configuration. No protected database, production credentials, private
keys or production state was inspected. Old staging account-count observations
are historical, not proof that live state is disposable today.

Inventory covered main/feature/release/integration worktrees plus older
`coffemaker/integration/tpa` e5e6e81 and `it/target_repos/tpahub`0d85fa8. Their
presence does not supersede current references. The historical UI worktree has
an unavailable gitdir; current website source is embedded in backend templates.
A pre-existing `tests/__pycache__/` is present in the isolated Schema17 snapshot;
it was not removed or mistaken for a source fix. Mema isolated core binaries
have expected build changes. See machine inventory for all branch/status/log/
worktree details. No dirty original was edited.

Audit directory (untracked evidence, not public output):
`/home/eugen/projekte/tpa-architecture-audit-20261016T153420Z/`.

Retained prior status/continuation reports were read, not overwritten. Their
server-managed-signing integration results are now explicitly classified as
legacy-path qualification. Their source/binary hashes and functional observations
remain useful. All current source claims below have named functions/files;
line numbers refer to the frozen sources, not future HEADs.

## 2. Actual publish pipelines (not an imaginary `tpa publish`)

### Local TPA / ordinary SSH

T `main.go` public command catalog has **no `publish` command**. Actual entry is
`pack` with destination options (and separately metadata-only `compose`). The
requirement names the intended action; lack of that exact spelling is an
interface gap, not justification for inventing another architecture.

```text
tpa init / json -> developer payload staging -> tpa build (dpkg-deb)
existing DEBs -> tpa pack -> buildRepository -> local indexes + browser pair
             -> selected local GPG signing -> independent verification
             -> local output OR SSH transport of finished local candidate
```

T evidence: `main.go:297–308,457+`, `internals/aptpackage/{init,build,pack}.go`,
`repository_destination.go:45+`, `repository_browser.go`, `verify.go`.
Remote pack uses a private local candidate; `PublishRepositoryToSSH` at
`ssh_publish.go:84+` verifies/inventories it, resolves ordinary OpenSSH config,
uses standard SFTP, stages a remote directory, checks bytes, and renames it to
an absent target. It does not call TPA.run, build remotely, negotiate CAS or
require a TPA account. Authentication is standard SSH host/user authentication.

Repeated publication to the SAME remote destination is **refused**, not an
incremental update. New remote generation paths work; generic replacement needs
an explicit safe strategy/operational contract rather than pretending arbitrary
SFTP directory replacement is atomic. Relative `host:path` resolves against
remote home; `ssh://.../absolute` is supported. Transport normalizes served
files to0644/directories0755, not arbitrary original filesystem modes/mtimes.
The signed repository file bytes are preserved; `.deb`-internal modes/links
are unaffected. Repository inventory rejects filesystem links/special entries;
that must not be misrepresented as a ban on payload links inside packages.

Performance/ownership caveat: `verifyRemoteTree`, `ssh_publish.go:415+`, unlinks
local **scratch** candidate files and downloads them back over those paths,
once before activation and once afterward. Thus this is roughly an upload plus
two full readbacks, not missing-content transfer. Bounded RAM and one-candidate
scratch allocation are good; bandwidth and local candidate path mutation must
be explicit. The exported API must not be advertised as read-only publishing
of an arbitrary authoritative local tree. The CLI's private-candidate ownership
contains the destructive path operation; preserve that distinction.

### Legacy TPA.run staged package publication — deviation

```text
tparun stage -> local staged manifest
  -> tparun publish -> HTTP plan/session + buffered multipart DEB batches
  -> authenticated upload -> queued job
  -> copy active DEBs + incoming DEBs into server work directory
  -> server invokes tpa pack (local or remote worker)
  -> server generates indexes/canonical website
  -> server GPG signs Release -> verify -> activate generation -> HTTP
```

B evidence: `cmd/tparun/staged_publish.go:100–240` (batches in bytes.Buffer,
including ALREADY_PRESENT items), `main.go:808+` (individual/multipart uploads),
`main.go:1292–1530` (copy baseline and inputs, runner, sign, verify),
`internal/tpa/tpa.go:194–238` (`pack --atomic-publish`),
`cmd/tparun/publication_signing.go:60–110` (host GPG secret selector/home).

`.deb` identity/hash checks are useful validation, but do not turn server index
regeneration or host signing into compliance. Server work/candidate copies and
namespaces defend a server builder; much of that complexity becomes obsolete
when the host no longer runs it. Do not move this sandbox into ordinary TPA.
GitHub import currently feeds this queued package flow, so it too must not be
retained as a back door to mandatory host building/signing.

### Existing client-signed receive path — reusable partial implementation

```text
local tpa pack -gpg ownerkey -generation-manifest ...
  -> tparun publish-generation --directory tree --manifest inventory
  -> streaming HTTP multipart (InRelease first)
  -> repository/account authorization + registered public signer check
  -> inventory/hash/signature/index/package/browser verification
  -> rename exact incoming tree into generations + durable parent-CAS activation
  -> unchanged file bytes served through HTTP
```

B evidence: `prebuilt_client.go:16–149` (`io.Pipe`, streamed files),
`prebuilt_generation.go:23–450,487–614`, `generation_manifest.go`,
`apt_signers.go`, `store` publication-slot/parent transactions.
`preparePrebuiltGeneration` changes serving permissions, not file contents.
Server reconstruction of canonical browser bytes **for comparison** is
verification, not regeneration of the submitted output. Do not remove integrity
checks merely because their verification code renders expected bytes in memory.

The prebuilt path does NOT require the hosting provider's signing secret; only
registered owner public key material is needed by its verifier. Server GPG
configuration is required by the managed-signing route, not intrinsically every
prebuilt upload. Whole server startup still checks a TPA runner (`main.go:293+`),
an unnecessary builder coupling to adapt in the minimal host. Distinguish GPG
verification/public-key processing from signing authority.

### Three different SSH uses must not be confused

1. T outbound ordinary SFTP: finished repository copying; reusable.
2. S inbound `customer_ssh.go:271–372`: registration/confirmation, login and
   account/repository reads; default rejects unknown commands. No publish/file/
   chunk operation. Login issues a token for HTTP use; no optimized SSH upload.
3. B `internal/tpa/ssh.go`: outbound **server-to-build-worker** framed archive
   protocol invoking a remote TPA runner (`:254–258`). It buffers archive payloads
   (`:412,492,638`), and is neither generic SSH hosting nor inbound CAS. Retire
   this role from the hosting core; do not rename it into conformance.

`ParentCAS` in generation tests means **compare-and-swap**, not content-addressable
storage. Neither SHA256 columns nor `ALREADY_PRESENT` plan labels prove CAS.

## 3. Architecture conformance matrix

Classifications apply to the named responsibility, not a blanket project grade.

| Component | Target responsibility | Current implementation | Classification | Evidence | Required action |
|---|---|---|---|---|---|
| TPA CLI | Local builder + two destination strategies | init/json/build/check/parse/pack/compose/inspect/verify, ordinary output; no tpa publish/enhanced dispatch | PARTIAL; REUSABLE | T main.go command catalog/pack switch | Add destination-selected enhanced transport after protocol exists; keep existing interfaces |
| Package builder | Prepared tree or supported JSON, native semantics | Project staging + dpkg-deb; optional in-process metadata-only compose | CONFORMANT; REUSABLE | T init.go/build.go/json.go/direct.go; actual BLL outputs | Preserve payload links/hardlinks and provenance; no payload schema framework |
| Repository builder | Complete local indexes/pool/browser | pack derives actual artifact indexes, signs locally | CONFORMANT; REUSABLE | T pack.go/repository_browser.go; audit three signed real repos | Retain locally; fix inventory defect separately |
| Local signing | Owner-selected key before transport | cfg.GPG + caller GNUPGHOME | CONFORMANT; REUSABLE | T verify/sign code; independent local audit signatures | Keep secret authority on client |
| Legacy hosted generation/signing | Receive, not build/sign | Copies DEBs, server pack, host GPG signing | ARCHITECTURAL DEVIATION | B main.go1292+; publication_signing.go | Replace standard path with finished-tree receiver; eventually remove builder workers |
| Ordinary SSH | Standard server, no CAS/daemon dependency | OpenSSH SFTP finished candidate, readback/rename, new paths only | PARTIAL; REUSABLE | T ssh_publish.go; fresh signed Mema-only experiment | Fix inventory, clarify scratch ownership, qualify repeated publication strategy |
| TPA.run SSH | Restricted identity + repo-scoped receive | Customer SSH auth/reads only; remote-build SSH is different | PARTIAL authentication; MISSING publishing | S customer_ssh.go; B internal/tpa/ssh.go | Reuse live principal/repo policy, add receive protocol, not shell/builder |
| Capability negotiation | Identify enhanced upload without changing content | Ordinary destination parser + unrelated build-worker protocol/version | MISSING for target | T destination.go; S SSH dispatch | Explicit versioned SSH capabilities with safe bounds |
| Prebuilt receiver | Preserve already-signed exact files | Streaming HTTP inventory/signature admission + activation | PARTIAL; REUSABLE | B prebuilt_client/prebuilt_generation; fresh targeted test | Adapt entry transport to authenticated SSH, keep verifier/activation |
| Chunked CAS | Request missing chunks, verify/reassemble | No chunk store, negotiation or extent references located | MISSING | B source/schema searches + fixture/schema inspection | Implement measured bounded chunk protocol/storage, no new artifact identity |
| Whole-file reuse | Initial optional transfer/storage optimization | Package same-identity hash classification; full upload/copies still occur | PARTIAL recognition; MISSING CAS reuse | B publication_plan.go129; staged_publish.go160+ | Retain comparisons but don't call them physical dedup/missing-object upload |
| Storage | Immutable content and references, physical/logical bounds | Per-repository whole generation trees, reservations and retention | PARTIAL; REUSABLE lifecycle | B prebuilt_generation/physical_capacity/cleanup | Add objects/extents/reachability; avoid full candidate duplication |
| Generation activation | Complete exact generation, concurrency/recovery | Public signer checks, parent-CAS, slots, sync/rename/pointer/SQLite recovery | CONFORMANT mechanisms; REUSABLE | B prebuilt_generation.go290–450; targeted failure test | Keep semantics under new receiver/state; qualify restart with CAS |
| HTTP serving | Original artifacts, signed indexes, static browser | Active-generation resolver, ranges and exact content delivery | CONFORMANT behavior; REUSABLE | B delivery.go/files(); prior hashes and Firefox; cache fix f6a5498 | Keep no-date304 for sidecars; qualify chosen future content backend |
| Repository website | Generated by TPA, host static files | T generator exists; prebuilt serves it; legacy builds it on host | CONFORMANT local/prebuilt; legacy ARCHITECTURAL DEVIATION | T repository_browser.go; B prebuilt verify; legacy pack invocation | Use local pair; no management-UI package-data authority |
| Optional browser utils | Convenience client-side presentation | Distinct utility JSON/JS/CSS and output guard | REUSABLE optional | T pkg/tpautils; docs/tpa-utils.md | Keep distinct from canonical v1 pair; not a mandatory host dependency |
| Management API | Authenticated optional-client interface | Account/repo/usage/signer/lifecycle APIs exist; some builder-centric | PARTIAL; REUSABLE | B routes/main.go; S security routes | Keep stable core reads/auth/quota, adapt publication; no browser requirement |
| Authentication | SSH identity; reuse customer credentials | S restricted SSH, passkeys/recovery; B session expiry fix; generic SSH independent | PARTIAL; REUSABLE | S customer_ssh/customer_security; B store session tests | Integrate required rules without requiring dashboard or bridges by default |
| Authorization | Repository authority distinct from known hashes | Repo policy/delegations/credential flags exist; no CAS access model yet | PARTIAL; REUSABLE | B repository_policy/prebuilt authorization; S SSH principal | Apply same policy to negotiation/object reuse/commit; hashes grant no access |
| Metering | Core persistent usage independent of Stripe | Usage ledger/rollups and physical admission; logical gauge is active DEB payload only | PARTIAL; REUSABLE | B internal/metering; store usage; physical_capacity | Add full logical generation + dedup physical measures, keep policy separate |
| Stripe boundary | Optional adapter, not core dependency | BillingNone/local provisioning works; hosted service optional | CONFORMANT configuration; PARTIAL separation | B hosted_billing.go40+; prior real flow used local policy/no Stripe | Retain port/none mode; don't copy commerce schema into minimal baseline |
| Dashboard boundary | Optional consumer, same core | Embedded templates/routes always part of current binary/server | PARTIAL | B web.go/main routes | API can serve CLI; explicit headless assembly/removal gate still needed |
| Self-hosting | Same minimal SSH/HTTP core, optional dashboard | Portable runtime/package/volumes docs exist, builder startup coupling remains | PARTIAL; REUSABLE ops | B run()/self-hosted docs; historical qualification | Remove builder requirement, qualify fresh install of selected baseline |
| Canonical database | Only necessary authority/ref/usage state | Divergent17/20 tables; no object/extents; many legacy billing/import/build tables | ARCHITECTURAL DEVIATION to retain wholesale; PARTIAL reusable constraints | B/S schema.sql + customerSQL; no CAS tables | Design fresh baseline; retain durable state only if actually found |

## 4. CAS readiness and measured opportunities

| Feature | Implementation status for target | What exists / what does not |
|---|---|---|
| SSH authentication | IMPLEMENTED in S, not consolidated in B | Real restricted SSH principal/revocation/host pinning code |
| SSH upload negotiation | MISSING | Auth/reads do not accept content or transfer sessions |
| Content hashing | IMPLEMENTED whole files | SHA256 inventories and actual DEB/hash checks |
| Chunking | MISSING | No production fixed-size/CDC chunker located |
| Missing-object discovery | MISSING | ALREADY_PRESENT is package plan display, not missing-files/chunks exchange |
| Upload resumption | PARTIALLY IMPLEMENTED unrelated path | HTTP DEB batch receipts/idempotency; incomplete sessions discarded on startup, not durable chunk resume |
| Received content verification | IMPLEMENTED prebuilt files | Hash/size/signature/inventory exact verification; no chunk verifier because no chunk path |
| Content-addressable object store | MISSING | Generation directories, not digest-keyed file/chunk store |
| Cross-generation physical dedup | MISSING in audited host | Copies/whole independent generations; no production object references |
| Storage reference tracking | PARTIALLY IMPLEMENTED generations | Active generation/retention and slots exist; object/extents/reachability absent |
| Garbage collection | PARTIALLY IMPLEMENTED | Whole-generation cleanup/retry exists; not shared-chunk GC |
| Exact generation activation | IMPLEMENTED for prebuilt HTTP | Parent-CAS, immutable while retained, failure rollback |
| Crash/restart | PARTIALLY IMPLEMENTED | Existing publication/staging cleanup; no CAS/session object recovery |

Do not confuse SQL `BLOB` fields or webhook replay dedup with content storage.
Broader legacy copies inspected (`it/target_repos/tpahub`, older integration TPA)
did not reveal a hidden enhanced SSH/chunk-storage path. This absence finding is
scoped to discovered repositories and source searches, not all possible machines.

Fresh measurement (`experiment/measure-reuse.py`, SHA256, streaming reads <=4MiB):
actual four BLL0.2.0 packages and Mema0.4.1/0.4.2, complete locally signed TPA
repositories. Fixed chunks reset at file boundaries. No new chunker was added
to product code; this is an offline opportunity estimate, not server throughput.

| Change | New logical bytes | Whole-file reusable bytes | Extra reuse at64KiB /1MiB /4MiB |
|---|---:|---:|---:|
| Replace0.4.1 with0.4.2, keep BLL | 5,243,983 | 9,506 | 0 /0 /0 |
| Retain0.4.1 and add0.4.2, keep BLL | 10,480,841 | 5,243,838 | 0 /0 /0 |

Whole-file reuse would avoid about half of the retained-history snapshot's
transfer; current prebuilt receiver does not exploit even that. Additional fixed
chunk reuse **was not useful in this small compressed-DEB corpus**. This neither
removes the chunk-level requirement nor proves CDC useless. Compression and
binary/source changes can destroy fixed-boundary reuse; CDC was NOT implemented
or measured. Before selecting fixed versus CDC, measure real large model/data/
similar-version payloads, shifted content, compressed vs uncompressed packaging,
CPU/RSS/index costs, network resume and dedup scope. Do not synthesize a favorable
result and label it representative. Hundreds-of-GB payload tests remain absent.

Current scale conflicts: B prebuilt uploads default512MiB total, manifest16MiB /
65,536 files (`generation_manifest.go`, configurable upload bound). T generic
repository verifier caps one artifact at16GiB (`repository_operations.go:30,707`).
These prevent the tens/hundreds-of-GB target as currently bounded, even though
streamed paths are reusable. The legacy staged client buffers a whole multipart
batch; server-to-worker SSH buffers the whole archive. Simply raising their
limits would not make them safely streaming.

## 5. Ordinary SSH experiment and newly confirmed defect

Reason to rerun a focused gate: prior seven-step SSH qualification used a small
unsigned fixture, while the contract asks for the SAME finished signed repository.
Prior failure/concurrency coverage was retained, not unnecessarily rerun wholesale.

Fresh developer-only disposable key signed three actual BLL/Mema snapshots;
independent local TPA verification passed for all. **Full six-package ordinary
SSH publication failed before transfer** at manifest creation:

```
inventory local repository candidate: generation walk did not produce sorted unique paths
```

T `generation.go:68–100` assumes depth-first `filepath.WalkDir` order is globally
sorted by full relative path. It is not when directory prefixes contain `-`:
`pool/main/b/bll/...` is visited before `pool/main/b/bll-bash/...`, but `-` sorts
before `/`. The same assumption also affects generation-manifest export used by
prebuilt publication. This is a concrete correctness defect, not a symlink or
security abstraction concern. Suggested future patch: collect bounded metadata,
sort full relative file paths before serializing, retain duplicate/path/size
checks and streaming file hashes. **No implementation patch applied in audit.**

A separate, explicitly smaller real Mema-only two-version repository passed:
complete signed local output -> exported production SFTP API -> ordinary Debian
OpenSSH container -> independent readback. All8 files including original
InRelease, index.html and repository.json had identical SHA256; signature verified
again; second publication to the same path was rejected. No TPA.run or CAS software
was installed on that server. Existing SSH permission/failure guarantees are
qualified by prior logs; no provider signing key was installed remotely.

The experiment helper uses a private hardlink scratch tree because transport
verification unlinks/re-downloads scratch files. It neither hands the authoritative
local tree to that mutable API nor introduces full-payload copying as a product
requirement. The source experiment retains the failure and reduced-corpus pass
separately; do not generalize the smaller pass to the six-package failure.

Evidence: `evidence/local-and-ordinary-ssh.log` (failure),
`local-and-ordinary-ssh-mema-only.log`, `ordinary-ssh-result-mema-only.json` (all8
file hashes), disposable **public** verification certificates. Private test keys
and SSH credentials were removed by cleanup. Full signed snapshots are retained
only as local audit evidence, not released/published externally.

## 6. Code disposition (no deletion in this audit)

| Subsystem | Disposition | Reason / preservation boundary |
|---|---|---|
| TPA init/render/build/check/parse/provenance | KEEP | Correct owner and flexible staged tree/JSON inputs; retain explicit overrides |
| TPA pack/sign/index/browser generator | KEEP | Authoritative client-side repository constructor |
| TPA destination parser/OpenSSH SFTP | ADAPT | Reuse normal transport; inventory fix, explicit private scratch and repeat-update contract |
| TPA generation inventory | ADAPT | Existing file SHA256 transport inventory, not a new package identity; fix ordering/scale |
| TPA optional utils/browser | KEEP optional | No host data authority; distinct schema is intentional |
| Prebuilt client streaming/inventory | ADAPT | Reuse byte-preserving transfer; move entry into TPA destination strategy, support SSH/missing content |
| Prebuilt verifier/signer public trust | KEEP/ADAPT | Exact validation, no host secret; maintain repo-scoped owner authority |
| Prebuilt slots/parent-CAS/activation recovery | KEEP/ADAPT | Useful storage lifecycle, fit new canonical baseline/object refs |
| Managed individual-DEB upload/worker | REPLACE, then REMOVE from standard core | Wrong repository-building responsibility; preserve tests as historical evidence until receiver replacement qualifies |
| Server pack runner/remote build worker/namespaces | REMOVE from hosting role after replacement | Exists to run host builds, not receiving finished repos; generic verification/resource bounds remain useful |
| Host GPG signing/public key endpoint for host key | REMOVE mandatory path; ADAPT owner-key discovery | Owner signing must not be replaced; public signer API remains useful |
| Server unlist/delete that regenerates/signs metadata | REPLACE | Owner uses local TPA to publish a new signed generation; management may request mutation, host must not silently re-sign |
| GitHub individual-release import builder | DEFER/ADAPT optional integration | Could deliver a finished owner-signed repository; do not let it reintroduce core building |
| HTTP static delivery/ranges/cache fix | KEEP | Serves original bytes, independent from build method; preserve partial-byte accounting |
| SSH credentials/live permission checks | KEEP/ADAPT | Reuse repository authority in transfer, not blanket general shell or browser-login requirement |
| Customer passkeys/recovery/session expiry | KEEP/ADAPT selected account features | Real rules worth integrating; optional browser frontend, no new authority layers solely to defend a builder |
| Repository policy/delegations/reader auth | KEEP/ADAPT | Distinct authentication/repository authorization; CAS access must use it |
| Core usage ledger/local provisioning/admission | KEEP/ADAPT | Payment-independent, persistent; extend physical dedup vs logical whole-generation accounting |
| Hostedbilling/Stripe code | DEFER outside core | Existing optional port useful; not required canonical schema/runtime, no new Stripe work |
| Embedded dashboard | ADAPT optional consumer | Same API/backend, no forked selfhost server; explicit headless assembly gate needed |
| Historical schema bridges/import/build tables | DEFER/REMOVE from new baseline if unnecessary | Retention evidence, not historical effort, determines need |
| Selfhost package/volume/service tools | ADAPT | Reuse portable runtime/ops; remove host builder/key prerequisites and qualify fresh baseline |
| BLL rollback recovery fix | KEEP | Actual producer-side reliability fix, unrelated to wrong host responsibilities |
| Existing Mema native builder | KEEP working, ADAPT optional TPA adoption later | Do not disguise native pipeline as TPA; project staging remains valid |

No successful legacy test is to be discarded as useless: preserve DEB hashes,
APT compatibility, quota/authorization, HTTP cache/range, generation activation
and recovery assertions; change the expected builder/signer owner in future
end-to-end tests. Do not delete qualified code until replacements pass acceptance.

## 7. Fresh database baseline proposal (NOT implemented)

Preference **B: fresh canonical baseline**, conditional on actual retained-state
inventory. A complex historical bridge is not intrinsically needed because
customer17 and importer20 collide. Preserve useful application rules and
constraints without importing whole historical schemas to avoid merge effort.

Proposed logical entities; names are illustrative, not executable DDL or a fixed
new framework:

| Entity | Authoritative responsibility / essential constraint |
|---|---|
| Principals and credentials | Identity; unique SSH key fingerprint/credential binding, revocation, live allowed operations; optional passkeys/recovery only if supported |
| Repositories | Owner principal + stable repository ID, visibility/config; names unique in owner scope |
| Repository authorization | Principal/repo/operation grants; explicit owner/delegation rules, separate from object hashes |
| Generations | Repo-scoped ID, parent, verification/activation state, immutable while retained; one authoritative active generation per repo |
| Generation files | Unique relative path per generation, original size + whole-file SHA256, validated path/mode metadata; exact owner-built tree |
| Storage objects | SHA256 + byte length/location/completeness, immutable verified chunk/file objects; no package identity replacement |
| File extents | Ordered object references/offsets covering exact file length; whole-file object can initially be one extent |
| Transfer sessions / expected inventory | Bound principal+repo+parent+limits, lifetime/state, missing/received objects and retry idempotency; activation requires complete verified closure |
| Temporary object leases / reachability | In-flight ownership/expiry protects uncommitted objects; committed generation references protect retained content |
| Quotas and reservations | Server-owned logical/physical/traffic limits; atomic resource settlement, prevent concurrent overcommit |
| Usage ledger and bounded rollups | Persist idempotent physical/logical/delivery events and period aggregates independently from Stripe; do not duplicate current repository authority |
| Necessary audit/recovery records | Minimal evidence for permission changes, activation/recovery and resource settlement, not a second package metadata database |

File hash remains the original file SHA256; chunk object hashes are internal.
Generation IDs are lifecycle references, not clean/release hashes. A generation
inventory is transfer completeness information, not mandatory additional signed
provenance. Derive current reachability/refcounts from canonical references;
if cached counts are used, make reconciliation/transaction ownership explicit.
GC cannot delete active/retained or leased content. Do not add a retained
package registry merely to count references. Existing indexes remain authoritative
package listings, not duplicated server-generated package state.

Consistency boundaries to specify/test before implementation:

- Successful reception verifies object bytes before marking available; a known
  hash never grants access. Authorize every negotiation/reference/commit.
- Complete file extents reconstruct original hashes; reject mismatched input,
  never fix indexes/signatures. Bounded reads do not require full files in RAM.
- Publish verifies complete generation, persists references and switches expected
  parent atomically/coordinately with filesystem activation, with crash recovery
  to one observable generation. Reuse existing tested slot/activation principles.
- Physical bytes count unique stored objects and reservations; logical bytes count
  repository content per configured policy; deletion/retention settle references
  and usage without double counting shared storage.
- Integrations consume authenticated usage/entitlement ports; optional billing
  tables live only in optional integrations, not minimal storage authority.

### Reset feasibility / actual state

The owner reports no customer data requiring migration. Public version inspection
cannot confirm account counts or inventory live repositories, keys or operational
configuration. This audit did not read protected deployed databases or private
credentials. Therefore:

- Disposable local fixtures: fresh initialization is appropriate; no bridge need.
- New canonical development baseline: preferred; integrate required account and
  repository behavior by tests, not by retaining obsolete tables.
- Production reset: **UNVERIFIED and not authorized**. No reset executed/planned
  as an automatic audit step. Before a future authorized change, obtain sanitized
  read-only counts of identities/repositories/generations/active paths and public
  signer references; inventory configuration/keys by existence/ownership/fingerprint
  without secret contents; identify repositories/bootstrap/service dependencies;
  retain tested backups and a matching-binary recovery procedure as needed.
- If durable repo files/keys/config exist without users, retain them explicitly.
  Empty account count alone does not authorize deleting them. Only demonstrated
  retention needs justify migration/import tooling; never assume a bridge graph.

## 8. Minimal compliant implementation sequence and acceptance gates

1. **Freeze architecture.** Commit this contract + small AGENTS references in
   isolated TPA/backend branches. Gate: agents distinguish ordinary SSH, customer
   SSH auth, remote build workers and prebuilt receipt; no host-signing standard.
2. **Local completion.** Keep TPA builder; fix bounded manifest sorting in a
   separate future patch. Qualify real BLL/Mema complete signed snapshots, explicit
   provenance and canonical website pair; extend large-artifact verification
   bounds/streaming based on supported target. Gate **A** without TPA.run.
3. **Ordinary SSH.** Reuse SFTP, fix inventory and private scratch/readback contract,
   document new-path vs repeated publication. Qualify unchanged bytes/signatures,
   subpaths/permissions, failures and independent HTTP/APT. Gate **B**, no CAS or
   hosted account required. Don't replace the protocol to solve inventory order.
4. **Minimal TPA.run hosting.** Adapt prebuilt verifier/activation to a restricted
   authenticated SSH receiver and TPA destination dispatch. Initially stream
   completed files; no packages/remote pack/host signing. Use existing repo policy,
   owner public keys and quotas. Gate exact contents + scoped authority; do not
   claim optimized **C** yet if all content is uploaded every time.
5. **CAS optimization.** Implement missing-file then measured chunk reuse with
   bounded streaming, immutable object verification, leases/references/GC and
   interruption/resume. Whole-file stage is not a redefinition excluding chunks.
   Measure fixed vs CDC on representative large inputs. Gate **C/D**, hashes grant
   no read/write authority; no new signed metadata/artifact identity.
6. **Unified server state.** Materialize the canonical necessary baseline under
   these already-explicit boundaries; do not postpone storage consistency design
   until after coding transfer. Fresh isolated state preferred; owner-approved
   retained-state import only if needed. Gate fresh install, restore/restart,
   quota/metering reconciliation and safe reference GC; no audit-time reset.
7. **Management integrations.** Stable auth/permission/repo/generation/usage/quota
   interface, optional headless dashboard adapter and later Stripe port. Gate
   **F**: core SSH/HTTP still works with both removed; no duplicate hosting logic.
8. **Complete qualification.** SAME finished owner-signed real repository through
   generic SSH and TPA.run; exact file/signature/website hashes, signed APT
   install/upgrade, authorization/owner isolation, logical/physical usage,
   repeated-content transfer savings, interruption/bad chunks/stale parent,
   retention/GC, restart/crash recovery and fresh selfhost. Gate **A–G**. Only
   then retire legacy builder/signer path and prepare an authorized rollout.

No phase authorizes production deployment, shared publication or deletion.
During transition, clearly label legacy entry points; do not silently route a
finished-tree failure into managed signing.

## 9. Tests / evidence / limitations

New audit-only experiments (no production implementation edits):

- Independent local pack/sign/verify for real old/new/retained BLL/Mema repos:
  **PASS**; original input package hashes retained.
- Full six-package SSH inventory: **FAIL**, concrete ordering defect above.
- Smaller real Mema-only signed ordinary OpenSSH publication: **PASS**, eight
  exact output file hashes, unchanged InRelease/signature/browser pair; repeated
  destination refused. No TPA.run/database/server key requirement.
- B existing `TestPrebuiltSignedGenerationAdmissionAndParentCAS`, race-enabled,
  run from isolated audit backend: **PASS**. Confirms exact prebuilt bytes,
  concurrent repo-slot behavior and failure rollback/public-key validation.
  This is an existing fixture/seeded-auth test, not new end-to-end SSH auth/CAS.
- Fixed chunk offline measurement: **PASS as measurement**, zero extra reuse
  beyond whole files for this corpus. No CDC or huge-file inference.
- Read-only public /version confirms previously observed Schema11/runtime.
- Documentation link/diff/contract checks: recorded with final commit evidence.

Prior functional qualification was not repeated without reason: BLL Makefile /
Debian12 lifecycle, real authenticated managed-signing BLL/Mema ingestion/upgrade,
Firefox public/repository layout, session expiry, cache fix and full race/vet
logs remain under the existing ecosystem checkpoint. They are not proof of
owner-signed SSH/CAS publication. No new production/staging qualification claim.

Audit evidence/script hashes and Git snapshots are under the audit directory's
`evidence/` and `SHA256SUMS.txt`; documentation commits contain no generated
packages, signing keys, compiled binaries or private runtime state.

## 10. Genuine remaining owner/environment decisions

The architecture itself is settled; these details are not invitations to change it:

- Verified retained-state inventory and later authorization for any deployed
  reset/cutover. Owner says no customer data; repos/keys/config still unverified.
- Initial CAS reuse scope (within repo/account or cross-account) and corresponding
  visibility/physical allocation policy. Minimum compliant progress can start
  repository-scoped; a hash alone must never broaden authority.
- Generic repeated-publication hosting convention (versioned directories / an
  explicitly controlled activation pointer vs platform-specific replacement).
  Do not claim generic SFTP can atomically replace any populated directory.
- Supported huge-file limits and representative large payload corpus for fixed/
  CDC choice, not whether chunk reuse remains a requirement.
- Which optional existing account recovery/dashboard/import adapters are retained
  in first canonical-core release. This must not make ordinary SSH depend on them.

**Bottom line:** reuse the developer builder/signature/browser output, ordinary
SFTP machinery, exact prebuilt verifier/activation, identity/repository policy,
HTTP delivery and payment-independent metering. Replace the standard hosted
package-build/sign path, add the missing inbound SSH/CAS connection, and prefer
a fresh necessary baseline. Do not optimize the obsolete host builder as the
route to architectural compliance.
