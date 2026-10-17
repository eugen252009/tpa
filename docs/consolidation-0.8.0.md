# Authoritative 0.8.0 client consolidation

The local release line `release/consolidated-client-0.8.0-rc1` is based on
fec57d9 (connected chunk-CAS client), with normal history-preserving merges of
1af2524 (reusable release tooling and 0.7 reproducibility/provenance fixes) and
921b6fc (optional account delegation). Main ffab7d5, direct/utils 32fc08f and
client-signed transport 56a4192 are already ancestors of that baseline.

The account merge keeps BOTH completion dispatch and optional delegation. Shared
staging aligns the two existing release entrypoints, rather than maintaining
parallel payload scripts or adding another DEB engine. Directory builds continue
through dpkg-deb; the intentionally separate bounded metadata-only direct writer
continues to serve compose and shares control/repository validation. The release
orchestrator invokes the normal TPA JSON/build commands. These strategies are not
superseded compatibility layers to delete indiscriminately.

The independent da3e775 --extend prototype is retained but not selected: copying
all old pool artifacts can re-list unlisted identities and needs a separate
retention/locking decision. Existing coffeemaker integration changes predate the
standalone destination/error/positional-config implementation already present;
they are not merged backwards. Earlier release/audit/qualification worktrees
remain historical evidence, never deletion targets for this consolidation.

Qualification uses disposable local instances of exact TPA.run 0.1.5~rc2 source
4c1ffa1c03e2 and its unchanged approved package, native BLL/MEMA signed fixtures,
and ordinary OpenSSH without TPA.run. It must not contact/change the production
server, its repositories, configuration, authorization or signing. Test-only GPG
homes are isolated; no production/bootstrap signing key is generated or used.

Release version 0.8.0~rc1 reflects the new additive compose/utils/chunk-publication
feature set beyond the 0.7 line, not a version-number workaround. Exact commit,
actual toolchain, target execution/install results and artifact SHA256 live with
the qualified local release under ~/projekte/releases/tpa/. No publication, tag,
source push or removal of original branches/worktrees/evidence is authorized.
