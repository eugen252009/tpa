# Checking packages and repositories

Use `tpa check -in=package-root` (or `tpa check --json package-root`) before
building. Check is read-only and returns nonzero on errors. Each diagnostic has
an optional field name, a message explaining the failure, and a documentation
URL. URLs are help text only, not sources of trusted metadata.

Supported shared binary-control checks include:

- Package name syntax and uppercase letters.
- Debian version syntax, including epochs and revisions.
- Binary architecture syntax, common non-Debian spellings, and source wildcards.
  Syntax checks do not establish that an architecture exists or matches binaries.
- Required Maintainer and Description fields (not email deliverability).
- Control-character/field injection, malformed control syntax and duplicate fields.
- Priority, Essential, Multi-Arch values and `all`/`same` incompatibility.
- Binary dependency relationship syntax, including permitted version operators,
  alternative restrictions, versioned Provides, and exact Built-Using versions.
- Custom metadata collisions, reserved fields and unsupported values on creation.
- Non-regular maintainer scripts in package trees. Script executable modes are
  handled automatically by build, so missing executable bits are not errors.

Structured multiline descriptions are serialized with Debian continuation
indentation and indented dots for blank lines. Raw control files must already
use valid continuation syntax. Names, versions, dependencies, architectures and
trust settings are never silently guessed or rewritten.

The shared validator is rerun by init/JSON creation, build, parse, pack and
repository verification. This catches edits made after initialization. Check
cannot prove a later operation will succeed: permissions, available tools,
signing keys, transport and repository state can change. Existing publication
candidate verification and atomic-publish safeguards remain authoritative.
There is no separate hosted publishing CLI here: TPA.run is another service;
this change does not deploy or modify it.

`verify --json` retains its existing `valid` and `error` fields and adds
`diagnostics`, `skipped`, and `artifactsChecked`. Independent package-index and
artifact failures are collected deterministically. Signature/Release parsing
failures stop dependent checks. If any index is unusable, artifact checks are
skipped; indexes that passed do not make that repository valid. Within a corrupt
stanza or artifact, later checks may depend on the first failure and are not
claimed as completed. Cancellation and resource bounds still apply. Generic
verify remains compatible with ordinary APT repositories and does not require
TPA's browser sidecars; strict TPA-tree verification checks those separately.

This is not an exhaustive Debian Policy linter: check does not resolve/install
dependencies, validate script runtime behavior, establish reproducibility, or
inspect all payload policy requirements. Use Lintian and disposable APT clients
for broader qualification. Unsupported/corrupt structures may prevent further
analysis; no tool can promise every possible error in one run.

## References

- [Debian control fields](https://www.debian.org/doc/debian-policy/ch-controlfields.html)
- [Dependency relationships](https://www.debian.org/doc/debian-policy/ch-relationships.html)
- [Maintainer scripts](https://www.debian.org/doc/debian-policy/ch-maintainerscripts.html)
- [APT repository format](repository-format-v1.md)
- [GnuPG documentation](https://www.gnupg.org/documentation/)

For operational failures, read the underlying filesystem/tool/SSH diagnostic.
Do not disable signature verification, use `trusted=yes`, change source policy,
or replace conflicting package bytes to suppress an error. Different content
under an existing package/version/architecture requires a new version.
