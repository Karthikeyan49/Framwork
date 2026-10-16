# R3b — Governed text and civil-date indexes

16 October 2026 (Asia/Calcutta). Implementation checkpoint; final acceptance
pending. Starting commit `06882b2`, branch `remediation/m9r-foundation`.
Full R3b/M9R remains PARTIAL. No M10, main merge or production activation.

## Scope

ADR040 was written before code and independently reviewed as a read-only design.
This slice adds fixed shared string/civil-date projections and preserves existing
numeric contracts, immutable IR, source JSONB and preview dataset identities.
The authenticated index GET/register/build routes now dispatch from the compiled
field type. Their existing checkpoint-only response stays compatible.

Forward migration 47 adds `runtime.scalar_indexes`/`scalar_projections`, forced RLS,
NOLOGIN ownership, SELECT-only app access, no generic worker access, typed conversion,
private management/build functions and database-owned record maintenance. The
new fixed indexes cover ascending/descending text/date and overflow lookup.
Numeric/scalar registration shares the existing exclusive epoch gate and combined
eight-field/entity, 32-field/physical-namespace quotas. Nothing introduces a
per-dataset writer counter or tenant DDL. Existing numeric migrations are unchanged.

Copy and verification remain independently checkpointed at 1000 rows each,
source SHARE locks and registry NO KEY UPDATE, with atomic audit/checkpoint and
source-version maintenance. New valid writes/deletes maintain projections in
one transaction. Unready indexes fall back; unsafe witnesses fail closed.

Text up to 512 UTF-8 bytes uses the ordered projection. Existing valid values
through 4096 bytes remain valid: overflow markers omit the large B-tree key and
force the field's existing JSONB query path. Null and overflow are distinct.
No value is truncated or silently omitted. Default database collation is retained,
including legitimate NULL version witnesses; registry/reported/actual drift
rejects. Overflow admission requires the exact transaction-bound RuntimeSnapshot
proof plus stable isolation; wrappers/autocommit pools use the old query path.
All field/row policy, filters, cursor and version predicates remain before LIMIT.

Civil dates use explicit Gregorian components/years 1–9999, independent of
DateStyle/time zone. A discovered existing pagination mismatch rejected valid
long text cursors: Go limited decoded JSON to 1024 bytes and encoded requests to
2048 characters, and the frontend also capped at 2048. Bounded decoded 32KiB and
encoded 43691 now cover JSON escaping of 4096-byte values; old cursors/fingerprints
stay readable. Limits are checked before base64 allocation and remain finite.

## Evidence so far

| Exact command | Result |
| --- | --- |
| `./scripts/go.sh test -race ./internal/runtime/data ./internal/platform/access` | PASS baseline 1.695s and implementation 1.704s; access has no unit test files. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RNumericIndex' ./tests/integration` | PASS baseline 30.549s. |
| `make migrate migration-status` | PASS, additive47 applied; existing1–46 retained. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarIndex' ./tests/integration` | PASS first23.287s and expanded40.001s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -timeout=5m -count=1 -run '^TestM9RScalarIndexPerformanceFloor$' -v ./tests/integration` | RUNNING initial capacity gate. |
| Independent executable adversarial review | NOT RUN yet; stable-code handoff required. |
| Final full checks/tests/build, migrations, browser and existing numeric/write gates | NOT RUN yet. |

Executed cases include 1206-row copy/verification/replay, Unicode/default-order
ASC/DESC/null/tie/cursor equivalence, patch/delete consistency, maximum escaped
text cursors, UTF-8 overflow/re-entry, civil-date/typed rejection, collation witness
fault, field/tenant/manager/worker denial, audit rollback/corrupt coverage, combined
quota race, opaque fallback, concurrent overflow snapshot, old-RR epoch fencing,
routine/trigger drift, populated owner scope, hidden sort and adopted index identity.
Independent review must extend adversarial faults rather than approve these labels.

## Limits and recovery

Fast text capacity requires no overflow anywhere in that physical field. Unindexed,
overflow, selective-policy and deep-page behavior need their own stated evidence.
No general full-text search, enum/reference/instant index, uniqueness, expanded
aggregate planner, automatic index inheritance, production release activation,
external provider certification or complete M9R resolution is implied.
No new major dependency. Existing R7 connection/worker capacity findings stay open.

Retain records, old numeric facts and checkpoint evidence. Unknown build COMMIT
uses authenticated GET before another request. Broken trigger/ownership/collation
requires reviewed forward repair; never restore broad grants or destructive down
migrations. A real collation-library upgrade/reindex drill is not yet executed.
Secrets, keys, logs, binaries, dependencies and data stay ignored. Commit only
source/migrations/tests/docs on the designated remediation branch.
