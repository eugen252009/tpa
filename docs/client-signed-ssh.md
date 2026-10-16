# Client-signed SSH publication: first vertical slice

TPA builds/signs; the destination receives finished repository bytes. Generic
OpenSSH remains independent of TPA.run and requires no account, CAS or host key
for APT signing. `pack --output=ssh://...` and `host:path` retain existing behavior:
new remote destination only, with explicit existing-destination refusal.

## Finished repository -> restricted TPA.run SSH

```sh
# Build once on the owner/developer's machine:
tpa pack -in ./artifacts -out ./finished -gpg OWNER_FINGERPRINT
# Export only the public certificate for independent verification:
gpg --export OWNER_FINGERPRINT > owner-public.gpg
# Repository and owner public signer must already be authorized by TPA.run.
tpa publish -in ./finished \
  --output=tparun+ssh://tparun@host:2222/0123456789abcdef0123456789abcdef \
  -ssh-config ./ssh-config -keyring ./owner-public.gpg \
  -fingerprint OWNER_FINGERPRINT
```

The explicit `tparun+ssh` scheme selects the restricted hosted protocol, never
SFTP or a remote shell. This first `publish` command supports that hosted scheme
only; ordinary output retains `pack --output`. A future unified finished-tree
transport entry can preserve both without weakening this distinction. There is
no automatic fallback to another transport or provider signing on failure.

The client verifies signatures/artifacts/canonical website pair with the explicit
owner **public** key, inventories files, then streams from existing files. No
build, signing, temporary package tree, full repository copy or payload-sized RAM
buffer is introduced. A fresh32-hex generation ID is generated unless supplied
with `-generation-id`. Updates require `-parent-generation` matching the current
active generation. Stable/main is the first supported suite/component. Explicit
local signature verification failure stops before any SSH transfer.

Standard OpenSSH config/identity handling, strict known_hosts and batch mode are
used; unknown/changed host keys fail. The service's SSH protocol user is `tparun`.
Local SSH credentials are not APT signing authority. Neither provider nor client
signing secrets are sent to the host. The local source tree is read-only to this
publisher (unlike the existing generic transport's private readback scratch).

## Version1 wire / server boundary

Command: `tparun-v1 generation <32-hex-repository-id>`. One exec/channel/connection;
no shell, PTY, forwarding, SCP or SFTP. Existing identity/key-version/lifecycle
checks and repository ownership/delegation policy authorize admission. Pending,
revoked and read-only keys cannot publish. The same credential is rechecked at
prebuilt activation checkpoints, inside the durable commit transaction, and
monitored throughout the connection.

Server emits bounded JSON `version:1`, `protocol:tparun-prebuilt-ssh`, `state:ready`.
Only after that readiness does the client send a bounded one-line JSON header
(`version:1`, `content_type:multipart/form-data; boundary=...`), followed by the
existing canonical multipart generation inventory and all files. InRelease is
first. It ends with an EOF on the write half of the SSH channel. Server returns
JSON with `version`, HTTP-compatible `status` and the existing activation result.
Malformed headers/version/content types fail closed.

The receiver invokes the **existing prebuilt admission/verifier/activation**, not
the package-ingestion worker, TPA builder or provider signer. The server can run
this handler with no builder executable and an empty signing home. File inventory,
owner-public-key signature, APT checks, capacity, expected parent, publication
slots and activation rollback remain authoritative. Canonical HTML/JSON are
verified for agreement, never silently regenerated as published output.

The original control SSH handshake/wire limits remain for unauthenticated/pending
operations. A live write credential may use the configured publication timeout;
wire allowance increases only after repository authorization. It is bounded
relative to configured MaxUpload; prebuilt HTTP-compatible body limits still
apply. Do not raise limits without matching disk/inode/timeout policy.

**This version is full-tree transfer, not CAS.** It uploads every file, including
an unchanged second generation. Chunked negotiation, persistent content objects,
reference/GC lifecycle, a fresh minimal canonical core schema, huge-file reader
limits and full-process restart qualification remain later gates. This slice
uses isolated fresh instances of the existing customer-security/prebuilt state;
it introduces no historical migration bridges and touches no deployed database.
Legacy builder routes still exist separately; do not use them as fallbacks or
claim that the whole backend is already the final canonical core.

If transport fails after activation, an error can mean activation is unknown.
Inspect the active generation before retrying; never retry with a stale parent
or rebuild the signed content merely to change its identity.

## Qualification

- `tests/signed-ssh-repository-qualification.sh REVIEW_DIRECTORY`: builds a local
  signed complete repository using the six retained BLL/Mema archives, publishes
  to disposable ordinary OpenSSH, compares every file hash including signatures/
  website and performs Debian12 HTTP/APT install/upgrade. Existing destination is
  refused. Existing seven-step SSH failure qualification remains separate.
- Backend `TestRealClientSignedSSHGeneration`, enabled with
  `TPARUN_SSH_QUALIFICATION=IMPLEMENTATION_ROOT` and `TPA_DIR=...`: real HTTP account
  provisioning plus existing approved-key fixtures, real OpenSSH possession,
  first/repeat publish, stale parent/foreign scope/read-only rejection, independent
  signed HTTP verification and Debian12 APT using that same tree. Approved-key
  fixture setup does not qualify browser authenticator enrollment.
- Protocol/header/permission and existing customer SSH/prebuilt tests run without
  the real-artifact opt-in. Local qualification is not deployment qualification.
