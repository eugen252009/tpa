# Consolidated repository explorer

## Source selection

The official repository's main and feature/tpa-utils both started at
7eafccc53d5755712f4bb8f87d0a4800b64ed727. Integration starts from its descendant
198c7498127c44111a3b48709eaa0a3dd50bef02, retaining the canonical-bootstrap docs.
No main history was replaced, source was pushed or production was published.

The committed standalone browser was introduced by4757c13 and is already in main;
feature/direct-json-utils32fc08f is already an ancestor. All relevant modern
committed utility assets have the same Git blob as main. tpa_repo_lister22a50dc
has an older static generator, not a newer UI. The deployed0.8.0 generator uses
the legacy noninteractive table, despite already containing the utility browser.

The most developed UI was UNCOMMITTED in /home/eugen/projekte/tpa-utils: grouped
versions/architectures, category and architecture filters, all-architecture option,
architecture-tabbed details, responsive cards and sticky/collapsible mobile search.
The generated /home/eugen/projekte/debian-browser is an earlier streamed variant;
/tmp/debian-repo-browser matches the newer dirty JS. Those generated previews and
the source/diffs were inspected rather than treating branch names as evidence.
The original dirty worktree, branch and untracked files remain unchanged. The
integration selectively reuses presentation, not its experimental archive server,
repository export, bulk-selection protocol or unrelated APT operations changes.

## One shared implementation, no circular imports

Public facade: `github.com/eugen252009/tpa/pkg/tpautils` remains independently
importable and both CLIs remain independently buildable. It already calls the
same bounded APT readers/parsers as TPA, not a second package database. Pure Debian
validation/version comparison already share `pkg/debian`.

Directly importing tpautils from internals/aptpackage would create a REAL cycle:
`aptpackage -> tpautils -> aptpackage`. Calling the utility executable would add
a false runtime dependency. Neither is introduced.

Instead, genuine shared presentation moved to the dependency-free
`github.com/eugen252009/tpa/pkg/repositoryweb` leaf:

- one embedded HTML/CSS/JS explorer, not two divergent browsers;
- one pure package-report model, with the existing RepositoryPackage API retained
  as a Go type alias (same fields/JSON tags);
- shared safe pool URL encoding and artifact-base validation;
- one escaped static-preview renderer, used by BOTH generators.

The canonical browser also reuses the existing strict Deb822 paragraph reader
instead of a separate bufio metadata parser. Its historical first-line trimming
is retained explicitly so canonical v1 JSON bytes do not drift. No package archive
parser, signer, reader cache, executable wrapper or external runtime dependency
was added. Models and UI do not import APT processing.

## UI features and actual bounds

The shared explorer implements search over package/metadata values, architecture
and category filters, inclusion of architecture-independent packages, grouped
package versions, architecture download badges, full metadata/size/SHA256 detail
dialogs, keyboard architecture tabs, native Escape/focus restoration, accessible
labels/status, skip navigation, responsive light/dark styling and mobile sticky
filter expansion that does not hide a focused control.

Exact duplicate identities collapse; conflicting artifact OR metadata identities
fail closed. Descriptions and arbitrary fields remain text, not executable HTML.
Unsafe filenames never become links. All artifact links are encoded and relative
to the inventory's actual artifact host.

Pagination replaces the current **50 cards**, never infinitely appends cards or
retains a detached DOM node per visited package. Details are constructed only
when opened. The old source's content-visibility alone was not a hard DOM bound;
its infinite Show-more/observer loop could grow without limit. Content-visibility
was removed after real Firefox captures revealed blank offscreen placeholders;
50-card paging provides the relevant bound without this visual defect. This is
NOT a claim of full data virtualization: canonical JSON is loaded within existing
repository-size limits and an in-memory search index is retained.

No50,000-package performance benchmark was rerun. Actual2000-DEB repository
qualification traverses13 page transitions at1440/390 and keeps at most50 cards.
Existing benchmark drivers, evidence and historical source records are unchanged.

## Metadata and compatibility

Canonical generation still emits ONLY paired `index.html` and `repository.json`.
Canonical JSON remains `tpa-repository-index` version1, derived from Release's APT
indexes with complete metadata and artifact hashes. The new canonical HTML embeds
first-party assets; there are NO extra asset paths, backend API or JSONL package
database. Release, InRelease, package identities and signing remain unchanged.

Utility generation still emits its DISTINCT `tpa-utils-repository-browser`
version1 report wrapper, base_url and external assets. It still refuses existing
sidecars. The shared JS explicitly adapts the two shapes, never relabels them.
Uncommitted utility-v2 JSONL streaming/archive/selection remains separate work,
not a silent canonical-format migration. No format supported by main was removed.

Strict verification accepts the EXACT known legacy table OR the EXACT current
modern HTML, always with byte-identical JSON derived from APT. Tampered or arbitrary
HTML and JSON still fail. New clients can therefore verify published0.8.0 trees;
older strict template-only verifiers may reject new HTML. Generic APT verification,
package consumers, ordinary SSH and tested TPA.run protocol2 remain compatible.

No JavaScript / restrictive host policy: the first50 package records retain native
HTML details and relative downloads, with the complete inventory linked as JSON.
Controls remain disabled until the interactive inventory loads. Unknown schemas,
unsafe base URLs and unsupported native dialogs leave this fallback intact.
This replaces the old unbounded no-JS table with an explicitly bounded preview;
it does not pretend to display the entire large inventory without scripts.

## IMPORTANT: current TPA.run hosting policy

Current production sends:

```
Content-Security-Policy: sandbox; default-src 'none'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'
```

That policy blocks JavaScript AND inventory fetches. The new UI was tested with
an EXACT local policy mirror: static previews/details remain usable, interactive
filters intentionally remain disabled. An exact disposable bootstrap2 backend
accepted and byte-served the modern HTML over real SSH-CAS, including signed APT
and zero-payload repeat, WITHOUT any backend/policy change. Transport compatibility
is NOT a claim that production's policy enables interactivity.

Before authorizing a modern interactive production generation, separately review
repository-origin isolation/CSP. Do NOT simply grant untrusted owner HTML scripts
and same-origin account authority. TPA.run must continue serving original client
files, never building/replacing their website. This deployment-policy question is
separate from the subsequent landing/docs/dashboard modernization, not a request
to rebuild the hosting architecture. No production policy changed in this task.

## Qualification and review

Actual generator output reuses qualified TPA/BLL/MEMA DEBs, two MEMA versions,
multiple architectures, a new long/hostile-description fixture, an empty tree,
a2000-package direct-compose tree, and an independent utility browser.
The compatibility tree retains the original signed APT bytes locally and replaces
ONLY its unsigned convenience HTML with actual newly generated HTML; the frozen
production artifact is never modified or re-signed.

Reproduce the Firefox interaction gate against an HTTP-served local qualification
site containing representative/, utility/, empty/, large/ and a local sandbox/
policy mirror:

```sh
python3 tests/repository-browser-firefox.py \
  --base-url http://127.0.0.1:PORT --evidence /private/firefox-evidence
```

The dependency-free Marionette client is reused from existing project ecosystem
tests. Firefox and Node are qualification tools, not CLI/runtime dependencies.
Persistent reports, actual screenshots and fixture generation commands:
`/home/eugen/projekte/tpa-modern-repository-ui-evidence-20261018/REPORT.md`.

The original feature/tpa-utils dirty work and historical branches are NOT deleted
or claimed wholesale merged. Main's committed utility history already exists in
the integration ancestry. Remaining independent work: v2 streaming JSONL input,
selection/streamed archives and the optional local utility server, which require
their own coherent review rather than becoming TPA.run rendering requirements.
