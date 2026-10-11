# ADR-019: Versioned React renderer for compiled preview IR

- Status: accepted
- Date: 2026-10-11
- Scope: M6

A built-in version1.0.0 component registry renders compiler1.0.0 IR only, obtained
from the authenticated preview descriptor. Runtime decoding verifies tenant-request
selection via the authenticated client, app/revision, supported compiler/IR/storage
versions, stable references, field compatibility and bounded entity/component trees.
The browser does not compile source or evaluate rules/workflows/scripts. Unknown
components and versions fail closed. Metadata is rendered as React text; no arbitrary
HTML, script, custom import, network destination or executable props are accepted.

Approved primitives: section, tabs (toggle buttons with aria-pressed), form, table,
detail, text/number/money/date inputs, select, relation picker and status badge. File,
chart/KPI and governed action buttons remain unsupported by compiler and renderer
until storage/report/tool capabilities exist. Built-in form buttons dispatch only
M5 typed CRUD commands, never metadata-supplied URLs/actions. References offer20
same-scope choices plus an explicit UUID entry; server validates target visibility and
existence. Table projections are capped32 fields; larger entities reject instead of
silently hiding fields. No bespoke customer ERP React pages are introduced.

Workspace selection is verified before mounting previews. Tokens remain in memory;
requests use Authorization headers, credentials:omit, bounded timeouts and explicit
paths. Selection changes abort requests and unmount the old page. Stale descriptor/
data completions cannot replace another selection. An aborted write can have committed
server-side: reload before deciding to repeat it. No automatic mutation retry exists.
Pages show source revision/hash and development preview status. Production routes are
not registered; this does not implement M11 sandbox lifecycle or M12 activation.

Forms preserve exact decimal/money strings, enum/bool/reference types and immutable
core IDs. Datetime input preserves exact RFC3339 text/nanoseconds and requires an
explicit timezone; Go validates the calendar and canonicalizes UTC. The browser
never repairs invalid dates or treats stored UTC as local time. Update/
delete sends expected row version;409 requires explicit reload. Delete requires a
visible confirmation. Viewer capability hints disable edits; every operation still
passes server policy. Fields denied by policy fail the whole query; masking is future
policy-adapter work, not claimed by the UI.

Semantic labels, required/disabled controls, table header scopes, keyboard buttons,
status/alert messages, overflow tables and responsive shell form the baseline.
Full accessibility certification, advanced editors, large relationship search,
field masking and performance tuning remain future work.

Playwright1.64.0 is the only major new development dependency, for real browser/API/
PostgreSQL verification. Production dependencies are unchanged. CI installs managed
Chromium; cloud tests use an existing /usr/bin/chromium when available, otherwise the
supported managed browser install. Browser snapshots/traces/video are disabled to
avoid capturing bearer tokens; all generated artifacts remain under ignored.local.
E2E creates unique operator-provisioned development fixtures without changing prior
customers/data. Fresh fixtures remain isolated, not destructive test cleanup.

Independent review refinements: compiler and renderer both reject nested forms and
missing/incompatible status badges; an exposed UUID field alias is optional (record
IDs are always available in the typed envelope). Empty entities reject. Unknown write
outcomes/5xx and row conflicts disable further mutations until a successful explicit
reload; reload remains available without a table. Playwright credential input and
authenticated fixture request errors are replaced with safe errors without raw causes,
because even password inputs/request headers can appear in its failure logs.
Synthetic-marker browser regressions verify redaction; mobile overflow is also checked.
