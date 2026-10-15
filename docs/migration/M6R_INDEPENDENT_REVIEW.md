# M6R independent architecture and security review

Verdict: **PASS for the reviewed M6R foundation; no unresolved BLOCKER or HIGH finding.**

This is an independent review of the current repository implementation, not approval
to begin M7 or a claim that the production ERP framework is finished. The reviewer
read AGENTS.md, the two-agent protocol, V3 architecture/acceptance/migration guidance,
the M0–M6 audit, ADR-020 and relevant retained V2 contracts. V3 controls where it
supersedes V2. Implementation and repository tests were not edited by the reviewer.
Temporary overlays and synthetic harnesses were used; the implementer separately
adopted exercised regressions into repository tests. This report is the reviewer's
sole repository deliverable.

## Confirmed findings and closure

The initial review reproduced the following unsafe behavior. These findings are
closed on the tested source, rather than accepted as risks.

| Severity | Reproduction before correction | Correction and independently executed evidence |
| --- | --- | --- |
| HIGH | A policy allowed actions but returned `NONE` for a contact name field. A joined projection disclosed that field; create returned it; update wrote and returned it. | Joined projected-field scopes are intersected in SQL JOIN predicates. Commands intersect entity, returned and modified field scopes on existing and proposed state. Real normal-role API regression now returns no protected joined value, create 403 and update 404. |
| HIGH | A universal `data.row.read` action denial was ignored by query paths when the SQL scope engine returned `ALL`. | Source and joined-target collection-read decisions remain mandatory alongside SQL DataScope. Real API regression returns 403. Native collection preflight semantics also pass the normal query suite. |
| HIGH | Worker audit INSERT accepted a forged `writer_role` and historical timestamp. The reproduction was rolled back; no forged persistent audit evidence was created. | Forward migration 00012 removes table-level INSERT and grants only permitted evidence columns. Worker provenance-forgery regression rejects the statement. |
| HIGH | Worker/dispatcher production connection bindings bypassed the runtime PostgreSQL URL trust validation. | Both bindings use production TLS/credential validation. Ten transport/credential override negatives pass, including duplicate parameters and password overrides. |
| HIGH | Restore accepted a rehashed bundle whose verification outcome was `FAIL`. | Build and Restore share admission validation. The independent pure bundle harness rejects it; promotion race tests pass. |
| HIGH | Restore accepted a changed source rule alongside unchanged stored IR after recomputing source/artifact hashes. | Full normalized source/application equivalence is validated without recompilation. The independent harness rejects the mismatch. |
| HIGH | Restore accepted altered IR runtime field mappings, including changing an ordinary name field into a platform core ID, while source/application metadata remained unchanged. | Stored runtime entities/fields must exactly match their derivation from the admitted application. The independent harness rejects the altered mapping. |
| HIGH | Credential curation recognized Go-run API/worker commands but missed the built API/worker binary paths, allowing operator credentials into those subprocess environments. | Binary and Go invocation forms are classified. An isolated synthetic-only binary harness confirmed both API and worker lack migration/dispatcher credential bindings; the permanent Node regression also passes. |
| MEDIUM | `RECONCILE_FIRST` allowed retry after unknown financial delivery outcome without reconciled absence. | Retry modes are honored and unknown outcome enums fail closed. The independent pure harness rejects both cases. |
| MEDIUM | Quoted `.env` production configuration bypassed the local role-helper guard and was modified in a synthetic reproduction. | Parsed configuration with environment precedence permits only development/test. The permanent synthetic Node regression confirms production refusal and unchanged file. |
| MEDIUM | Missing optional money under a proposed `balance > 0` scope produced SQL NULL, yielding 503 rather than a definite permission denial. | Proposed permission expressions use `COALESCE(..., false)`. The actual independent test initially failed with 503, then passed with 403 after correction. |
| MEDIUM | Team traversal could silently truncate a deeper hierarchy. | The traversal detects depth overflow, cycles and the team-count limit and returns an explicit error. The source correction was inspected; ordinary parent/child team visibility was independently exercised. Deep/cyclic negative execution was not independently repeated. |

The final worker boundary was also reviewed after migration 00016. Tenant-scoped
IAM/team SELECT policies are restrictive and intersect the routed job scope, rather
than relying only on business-record RLS. The worker regression now gives its actor
a legitimate membership in a second tenant. Both initial and spoofed-tenant-GUC IAM
queries still return zero second-tenant memberships. This strengthened regression
was independently rerun and passed.

Release authorization now receives the explicit tenant, actor, action and artifact
hash. Opaque approval retains its tenant and activation compares the trusted access
tenant. The cross-tenant approval unit negative and actual persisted stage/approval/
activation/CAS/rollback tests pass. Release rollback changes an existing approved
pointer only after explicit storage/external-effect assessment; it does not promise
to reverse real-world effects or destructive migrations.

## Preservation and database boundaries

Old migrations 00001–00008 and the retained legacy preview data are preserved.
Forward migrations introduce generic records, role boundaries, release/outbox
contracts, column privilege sealing and final reconciliation. Source checksums
independently pass for 84 retained V2 and 43 V3 documents.

The initial concern that an uncommitted legacy INSERT could evade migration 00009
was investigated, not confirmed as an exploit. The disposable-database rehearsal
observes the migration waiting on the legacy writer, commits that writer, applies
the remaining chain through 00016 and verifies the row and adapter read equivalence.
It also verifies legacy table **and column** write privileges are revoked and the
normal runtime cannot ALTER the shared record table. Policy DDL in 00009 already
provides locking; this report does not claim a reproduced pre-fix lost-row bug.

An additional independent disposable-database overlay deliberately creates a newer
legacy row inside a transaction, replays the final 00015 barrier, and requires the
explicit divergence error. It then rolls back the injected drift and verifies the
destination version was not overwritten. That test passes. A separate read-only
checkpoint query verifies all existing legacy identifiers, values, versions, creators
and timestamps match their V3 copies; no business row contents were printed.

Cutover still requires the documented operator procedure: quiesce legacy writers,
take a reviewed backup/checkpoint, apply the forward chain and resolve any detected
divergence deliberately. This is not an online arbitrary legacy-write merge.

Runtime role checks and local bootstrap drift checks were inspected for superuser,
BYPASSRLS, replication, schema/table/function ownership memberships, privileged
built-in roles and cross-membership between API, worker, dispatcher and audit roles.
No shared role memberships were changed for this review. Healthy role operation,
dispatcher payload denial, routed-worker tenant isolation, stale ticket rejection
and persistent session-context cleanup were exercised against PostgreSQL. Full
negative simulation of every new bootstrap/runtime role drift was not independently
performed.

## Independent executed checks

Commands below were actually executed by the reviewer. The final integration root
suite passed in 8.657s; the later strengthened dual-membership worker test passed in
1.569s. Race checks use the existing writable cache through `scripts/go.sh`.

| Command | Result |
| --- | --- |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^Test(M6R\|IndependentM6R)' -v ./tests/integration` | PASS: field scopes, action denial, audit provenance, nullable scope, worker/dispatcher boundaries, migrations, persisted release transitions, audit/outbox rollback and ownership/team/share/condition SQL queries. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestIndependentM6RWorkerRoutingAndPool$' -v ./tests/integration` | PASS after legitimate second-tenant membership was added. |
| `./scripts/go.sh test -race -count=1 ./internal/compiler ./internal/config ./internal/policy ./internal/tools ./internal/promotion ./internal/intent ./internal/erp ./internal/sandbox ./internal/platform/jobs ./internal/runtime/data` | PASS. Jobs has no unit-test files; its execution contract was exercised by integration tests. |
| `./scripts/go.sh test -race -count=1 -run '^TestIndependentM6RWorkerProductionTrust$' -v ./internal/config` | PASS: worker and dispatcher trust negatives. |
| `./scripts/go.sh run ./.local/m6r-review` | PASS: failed verification, source/IR mismatch, altered runtime mapping and unreconciled/unknown retry outcomes rejected. |
| `node --test scripts/runtime-env.test.mjs` | PASS: 2 synthetic credential/provisioning tests. |
| `node scripts/verify-docs.mjs` | PASS: 84 V2 and 43 V3 checksums. |
| `git diff --check` | PASS. |
| `make release-check` | PASS: operator-only active-release IR compatibility gate. Real integration also rejects unsupported IR and runtime-role invocation. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m6r-review-divergence-overlay.json -run '^TestM6RLegacyLateCommitMigrationReadEquivalenceAndWriteSeal$' -v ./tests/integration` | PASS: disposable-only divergence refusal and no overwrite, 1.880s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m6r-review-equivalence-overlay.json -run '^TestIndependentM6RLegacyCheckpointEquivalence$' -v ./tests/integration` | PASS: existing legacy checkpoint equality, 1.169s. |

Earlier independent overlays exercised the field, audit, row-action, worker,
production-binding and nullable-scope cases before they were adopted into repository
tests. They are retained as temporary review artifacts, not a second source of truth.
A late attempt to rerun the old config overlay **failed to compile** because the
implementer had already adopted its identically named test. The repository test was
then run directly and passed; this was a stale review-harness duplication, not a
hidden product failure. The initial nullable-scope regression **failed** with actual
503 before the implementation correction, and its corrected repository regression
was rerun successfully. Other initial harnesses established unsafe behavior rather
than certifying it as safe.

## Remaining observations and limits

There is no independently confirmed unresolved MEDIUM defect in this reviewed
architecture/security scope. The following LOW verification gaps and explicit limits
should remain visible:

- The reviewer inspected explicit hierarchy depth/cycle/count rejection but did not
  separately run all three negative hierarchy fixtures. Supported team resolution
  is bounded to depth 8 and 64 teams; overflow must fail explicitly.
- Every privileged-role drift permutation was not independently simulated. The
  actual normal worker/dispatcher boundaries and context cleanup were exercised;
  the additional drift predicates were source-reviewed.
- This report is not a fresh browser, accessibility or complete-component-registry
  certification. The prior M6 completeness findings are recorded in the migration
  audit and addressed by the implementer's frontend remediation/checks. Broad
  frontend/E2E results must be reported as implementer evidence unless independently
  run by another review.
- Global release compatibility is an operator deployment gate. It must be part of
  deployment, not inferred from a tenant connection's potentially empty RLS view.

M6R adds enforceable foundation contracts and tested storage transitions. Intent/
acceptance and verification bundles are requirements and evidence admission, not
execution of independently generated ERP acceptance tests. Public production
promotion services, rule/workflow/tool execution, inbox/dispatch, sandbox orchestration,
AI building, accounting posting and external delivery remain later V3 milestones.
No external side effects were executed or real production release activated by this
review. It does not authorize M7.
