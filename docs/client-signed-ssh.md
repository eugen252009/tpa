# Client-signed chunk-CAS SSH publication

TPA builds/signs; TPA.run receives, verifies, stores and serves original files.
Ordinary `pack --output=ssh://...` / `host:path` stays independent of TPA.run,
CAS and hosted accounts; it still requires a new remote destination.

```sh
# Owner-side construction/signing, before transport:
tpa pack -in ./artifacts -out ./finished -gpg OWNER_FINGERPRINT
gpg --export OWNER_FINGERPRINT > owner-public.gpg
# Repository ownership and this public signer must already be authorized:
tpa publish -in ./finished \
  --output=tparun+ssh://tparun@host:2222/0123456789abcdef0123456789abcdef \
  -ssh-config ./ssh-config -keyring ./owner-public.gpg \
  -fingerprint OWNER_FINGERPRINT -json
# Updates add: -parent-generation CURRENT_GENERATION
```

The explicit hosted destination uses restricted OpenSSH, pinned known_hosts,
batch mode and ordinary client identities. No general shell/SFTP/forwarding,
provider signature or automatic full-tree fallback. `publish` needs only public
APT trust, not a signing secret. It verifies the Stable/main finished tree and
canonical website pair locally; publication never builds/signs/changes that tree.
Generation IDs are lifecycle references, not new package/release identities.

## Protocol2

Exec: `tparun-v1 generation-cas REPOSITORY_ID`. The command namespace stays stable;
the handshake explicitly identifies `tparun-chunk-ssh`, version2 and
`fixed-sha256-1m-v1`. Authorization precedes ready and object negotiation. Existing
live key/account/repository policy, publication slot and transactional credential/
expected-parent checks apply. Hash knowledge grants no permission. The initial
reuse scope is **within one repository**: namespaces are separate across repos,
so another customer's object existence cannot be queried/referenced this way.

After the bounded JSON ready line, messages use a4-byte big-endian length and
strict JSON, at most16MiB each. Inventory contains version/algorithm, repository,
generation/expected parent, sorted unique relative file paths, original lengths/
SHA256 and ordered chunk SHA256/length extents. Fixed1MiB boundaries reset for
each file; only the last chunk may be shorter. Paths use existing traversal/
duplicate/prefix/depth validation. Maximum131,072 extents and65,536 files are
explicit metadata bounds, not whole-file payload buffers.

Server returns distinct missing chunks. Client sends each requested object's
exact raw bytes in request order; server verifies length/SHA256 and durably
inserts/leases it before acknowledging its digest. Existing verified objects are
leased/reused. A repeated identical publication transfers **zero payload chunks**.
The client rejects requests for undeclared, duplicated or incorrectly sized
objects. It reads existing files with one1MiB hashing buffer, then streams only
requested file sections, without a repository copy or whole-DEB allocation.

Final frames report activation identity, original signer and transfer/storage
counters. `-json` exposes verified uploaded bytes, reused unique bytes, logical
bytes and unique CAS payload storage. Client also measures SSH application-stream
bytes (both directions) and control bytes excluding payload. These are not TCP/
encrypted SSH wire bytes. Failed/uncertain activation must be independently
resolved against current generation authority before retrying.

## Verification, storage and lifetime

The server reconstructs exact file streams into the existing production prebuilt
receiver/verifier. One **transient** full verification tree is needed by the
current path-based verifier. Signature, artifact identities/hashes, indexes and
canonical sidecars are verified; no repair/regeneration or provider signing.
After quota checks, durable CAS retention precedes activation. Verification files
are removed and replaced by a small private extent descriptor; active and retained
generations do **not** keep additional persistent full repository payload trees.
Original file content is streamed/seeked directly from immutable chunks over
HTTP, retaining common range/HEAD/conditional/egress behavior. Private descriptors
and raw object paths are never downloadable repository files.

Repository HTTP readers pin reachability with the existing repository lock for
the response; activation/retention for that repo waits for such readers. Other
repositories and chunk reception continue independently. This deliberately simple
pinning policy can delay activation behind slow downloads.

Chunks committed before interruption remain leased for24hours. Reconnect and
resubmit inventory; verified remaining content is negotiated again. There is no
opaque browser token or second upload service. A failed generation that already
registered an immutable job ID may require a fresh generation ID on retry.

Owner-only `GET /v1/repositories/ID/cas-usage` exposes logical current generation,
unique CAS payload bytes/objects, last-generation uploaded/reused bytes, retained
counts and observed HTTP egress. Egress reuses the existing persistent daily
ledger, without double-counting strict accounting. Owner-authorized
`POST /v1/repositories/ID/cas-gc` explicitly reuses conservative collection:
active/retained generations and live leases protect objects; uncertain jobs remain
protected. Interrupted/failed candidate cleanup never guesses an active root dead.
No new background collector or billing integration is introduced.

## Server mode and qualification boundaries

`TPARUN_RECEIVE_ONLY=true` skips builder preflight/worker and rejects legacy
individual-package publishing, server metadata mutation and provider-key routes.
Use the self-hosted/no-billing profile and existing local policy settings. No
Stripe or dashboard is needed for SSH/HTTP operation. This mode does not reset
existing state or create a new account system.

The isolated implementation currently composes the existing customer/prebuilt
application store and content schema1 with conservative retain-before-activation
coordination. Fresh canonical hosting schema consolidation remains a separate
reviewable phase; do not invent historical Schema17/20 bridges or reset deployed
state. Existing artifact-size/time limits and full crash/restore qualification
still apply; streaming does not certify arbitrary-size inputs.

V1 full-tree receiving remains available to explicit Go callers using
`HostedPublishOptions.LegacyFullTree`; it is never an automatic downgrade.
Capabilities advertises protocol2 and `client_signed_ssh_chunked_cas: true`;
standalone inbound-server and hosted-managed-publication flags remain false.

The opt-in backend real-artifact gate exercises the unchanged14-file BLL/Mema
fixture, zero-payload repeat, scoped authority, concurrent expected-parent
competition, storage reopen, real changed16MiB package, interrupted/reused chunks,
corruption rejection, activation rollback/reference cleanup, Firefox and signed
Debian12 APT. It also kills/restarts the actual receive-only serve executable
while a chunk is incomplete and resumes against the original active repository.
These tests do not certify hardware enrollment, every crash point, canonical
schema cutover, package/systemd deployment or production readiness.
