# tpa-utils: standalone Debian/APT utilities

Build/install separately from the TPA executable (no account, TPA.run service,
configuration database, or credentials required):

```sh
go build -o /tmp/tpa-utils ./cmd/tpa-utils
# or: go install github.com/eugen252009/tpa/cmd/tpa-utils@<reviewed-ref>
/tmp/tpa-utils check package my-package
/tmp/tpa-utils check package myPackage                  # nonzero, linked diagnostic
/tmp/tpa-utils check dependency 'libc6 (>= 2.36)'
/tmp/tpa-utils check version '1.0~beta'
/tmp/tpa-utils check control package.json
/tmp/tpa-utils version compare '1.0~beta' '1.0'         # -1
/tmp/tpa-utils repo inspect --codename stable /path/to/repository
```

`check control` reads one definition using the existing `{"control": {...}}`
JSON configuration shape. Required fields, relationships, custom scalar fields,
reserved-field conflicts and normalized duplicate metadata follow TPA's renderer.
Individual field checks support package/name, version, architecture, maintainer,
description, priority, essential, multi-arch and modeled relationship fields.
Validation is supported binary metadata checking, **not Lintian or an exhaustive
Debian Policy audit**. It never silently repairs identity or trust fields.

Version comparison returns -1/0/1 (exit status zero for all three), compares
epoch/upstream/revision, handles tilde and missing revision, and avoids numeric
run overflow. Invalid versions are rejected, including overflowing dpkg epochs.
Tests cross-check a matrix of version pairs against `dpkg --compare-versions`.

## Library

```go
import "github.com/eugen252009/tpa/pkg/tpautils"

err := tpautils.ValidateField("package", "my-package")
err = tpautils.ValidateControl(tpautils.Control{
    Name: "my-package", Version: "1.0", Architecture: "all",
    Maintainer: "Example <example@example.invalid>", Description: "Example",
})
comparison, err := tpautils.CompareVersions("1:1.0-1", "1.0-9")
```

The pure metadata implementation is `pkg/debian`; TPA calls it rather than
maintaining a second validator. The facade also retains complete custom-metadata
checks in the shared renderer. Neither validation nor comparison shells out.
`Inspect(context, locator, RepositoryOptions)` returns the same bounded,
strictly parsed report as `tpa inspect`. Local paths and HTTP(S)/SSH readers are
shared, not a TPA-specific database protocol. Debian tools are not needed for
ordinary unsigned APT inspection; SSH reads require OpenSSH. Signature checking
uses GPG and explicit **public** key material only.

## Static interactive browser

```sh
tpa-utils repo html /path/to/ordinary-apt-repository
# Existing website or canonical TPA tree: never overwrite its sidecars.
tpa-utils repo html --out ./web \
  --base-url https://packages.example.invalid/repository/ /path/to/repository
# Readers also accept remote repositories; generation is always to local files.
tpa-utils repo html --out ./web \
  --base-url https://packages.example.invalid/repository/ \
  https://packages.example.invalid/repository/
```

Options precede the repository locator. `--codename` defaults to `stable`.
`--keyring public.gpg --fingerprint FULL_FINGERPRINT --require-signed` can require
an explicitly trusted signer for either inspect or html. As with `tpa inspect`,
unsigned/keyless inspection does **not** claim signature verification, and
inspection does not download/verify all artifact bytes (use `tpa verify`).

Generated files:

- `index.html`: controls, accessible status, noscript link to JSON;
- `repository.json`: `format: "tpa-utils-repository-browser"`, `version: 1`,
  `base_url`, and a shared `RepositoryReport` under `repository`;
- `assets/app.js`: search, architecture filter, expandable package metadata,
  size/hash information, encoded artifact links and bounded 200-row pagination;
- `assets/style.css`: local system-font styling, light/dark support.

No CDN, external font, framework, API account or backend is needed. Serve the
files with ordinary HTTP(S), e.g. `python3 -m http.server --directory web`.
Browsers generally disallow fetching JSON from `file://`. JSON alone remains
usable without JavaScript. The page and its artifact host must be reachable;
there are no third-party asset dependencies. Separate output requires an
explicit artifact base URL. Relative URLs work when deploying alongside APT
metadata; absolute HTTP(S) URLs work for separately hosted pages.

Package text is rendered with `textContent`, never as executable markup; artifact
path segments are encoded. Invalid/unsupported schemas and unsafe URL schemes
fail closed in the page. HTML generation refuses existing `index.html`,
`repository.json`, or `assets` rather than overwriting arbitrary content. On a
write failure it removes only files it created. It never changes `Packages`,
`Release`, `InRelease`, `.deb` files, or signing keys.

**Format separation:** this browser JSON is deliberately distinct from canonical
TPA format-v1 `repository.json`. Canonical TPA sidecars are verified byte-for-byte
by the existing repository verifier and remain unchanged. Use separate output
for TPA repositories, not a compatibility bypass. All browser files are
convenience presentation, not additional signed APT trust data.
