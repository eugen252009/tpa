# Separate next phase: TPA.run main website (assessment only)

No TPA.run implementation, production repository, account, service or Nginx policy
was changed for this assessment. Read-only current public pages and exact deployed
source23385223c534bc640ef312b3ac917dc0b74e3a34 were inspected after TPA UI work.
Authenticated dashboards were inspected in source, not with exported production
cookies. Earlier360/1440 Firefox evidence is historical, not a new authenticated
mobile qualification. The public page observations are retained in the TPA UI
qualification directory as tparun-public-assessment.json.

| Area | Actual finding | Focus for the subsequent UI phase |
| --- | --- | --- |
| Landing | Existing typography, trust messaging, four-step cards and responsive layout; says bootstrap repository is inactive | Link the LIVE canonical tparun/bootstrap inventory and distinguish TPA availability from still-unpublished tparun runtime packages. Do not advertise apt install tparun merely because TPA is now available. |
| Documentation/how-to | Docs say TPA.run signs; publishing examples use legacy tparun stage/status/publish package workflow | Update COPY/examples to client-owned TPA build/pack/sign and finished-tree SSH-CAS publication. Keep hosted account help separate from independent local builds. No server builder redesign is needed. |
| Navigation | Product, Docs, Pricing, Nano, Account; docs/how-to repeat installation warnings; /dashboard404, actual customer entry /account | Use clear canonical repository/install links, task-oriented docs and correct account routes. Preserve customer/operator navigation separation. |
| Operator | /ops requires existing operator passkey; source has service dashboard and global-bootstrap controls | Modernize visual hierarchy, capacity/audit readability, bound-repository status and dangerous legacy-action distinction. Retain real passkey/origin/CSRF boundaries; do not expose customer authority as operator authority. |
| Customer | Existing account view mixes repository/usage, CLI examples, entitlements and optional billing controls | Prioritize repositories, exact canonical links, usage and owner SSH publishing. Keep optional billing visually secondary; do not add Stripe or change quota logic. |
| Visual consistency | Main site uses beige/teal system-font design; explorer uses teal accents, cards, grouped details and blue-gray surfaces | Align spacing, focus/touch targets, typography and colors without making account pages a copy of repository metadata UI or introducing a frontend framework. |
| Mobile | Responsive structures exist; repository explorer is now actually checked at390; no new authenticated mobile main-site claim | Requalify actual generated landing/docs/account/ops at390/1440 with real interactions/labels, overflow and destructive-action feedback, retaining existing fixtures and explicit hardware-passkey scope. |

## Separate hosting-policy follow-up, not a new architecture

Current repository CSP intentionally disables scripts/fetch and isolates untrusted
uploaded HTML from account authority. The client-generated modern explorer has a
qualified static fallback; interactive deployment needs a separately authorized
repository-origin/CSP security review. The existing bootstrap2 implementation
already has an isolated RepositoryOrigin boundary (cookie stripping, repository-
only routing and a script-capable CSP); review/adopt that existing capability
rather than building a second hosting mechanism. Do not simply allow untrusted scripts and
same-origin account cookies, change production Nginx in this phase, or move the
repository renderer to TPA.run. No new package/signing/protocol authority is needed.

## Appropriate next worktree

Proposed (NOT created or implemented by this task):

- Path: `/home/eugen/projekte/tparun-main-website-modernization`.
- Branch: `feature/main-website-modernization`.
- Base: exact deployed source **23385223c534bc640ef312b3ac917dc0b74e3a34**,
  package0.1.5~rc2+bootstrap2/schema1.

Start from the deployed narrow branch rather than silently merging the broader
webhook/updater development branch. First do landing/docs/navigation copy and
shared visual tokens, then fixture-authenticated customer/operator presentation
and local browser qualification. No production update until its own exact artifact
is reviewed/approved. This is the NEXT task after the TPA repository UI review,
not an invitation to rebuild repository hosting, authentication or storage.
