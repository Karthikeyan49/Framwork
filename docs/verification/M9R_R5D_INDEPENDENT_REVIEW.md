# Independent adversarial review — R5d

Date: 15 October 2026 IST. Reviewed implementation: `88c8528`, based on
`cbf323e`, branch `remediation/m9r-foundation`. Reviewer is a separate agent
from the implementation author. Implementation was frozen throughout this
review. The reviewer edited only regression tests and this report.

Initial result: **PARTIAL — two HIGH findings require implementation fixes and
independent re-verification before R5d completion.** Overall M9R remains PARTIAL;
this review does not authorize production activation, a main merge or M10.

## Findings

1. **HIGH — partial writes omit trusted owner attributes from field decisions.**
   `patch` checks touched field actions before loading the row, with a record ID
   but no owner ID, and never rechecks those decisions using the actual owner.
   An existing supported owner-dependent field-write policy denies replacement
   update with 403, yet the new patch operation updates the protected field and
   returns 200. A complementary policy that allows a field only to its owner
   incorrectly rejects a legitimate owner's patch with 403. Execute the
   `TestIndependentR5dPatchPreservesTrustedFieldOwnerABAC` and
   `TestIndependentR5dPatchAllowsTrustedOwnerFieldPolicy` regressions. Move the
   definitive field action checks after trusted row/owner loading and before
   field-value validation/mutation. Do not use caller-supplied owner attributes.

2. **HIGH — contextless field action decisions can expose owner-restricted
   projections and authorize hidden predicates.** `visibleJSON` evaluates the
   action guard with only entity/field IDs; the query predicate guard similarly
   lacks the actual row/owner. A policy returning an entity-wide SQL scope while
   denying field access to the row's owner is accepted by the current public
   Engine/Guard ports. Point lookup and list projection return the denied value;
   a filter over that value succeeds. The shipped tenant-definition adapter uses
   SQL DataScope for per-record conditions, but that fact cannot silently make
   other accepted central-policy denials ineffective. Preserve SQL-only masking
   and fail closed for predicate scope coverage. A per-page header check alone
   is insufficient for sort/filter admission over an entire visible entity set.
   See the independent point/list/predicate owner regressions.

Three historical regressions also still asserted the superseded whole-request
projection behavior: `TestM5RecordSpecificFieldABACAndReferenceEntityPolicy`,
`TestIndependentM6RFieldScopeBypass` and
`TestM9RCustomSortPagePreservesTiesNullsScopesAndJoins`. The reviewer updated only
those projection assertions to demand retained entity-visible rows, explicit
masks and absence of hidden values. Their existing filter/sort, write and
reference-denial assertions remain intact. The updated checks passed.

## Executed probes

All integration fixtures used `m8Database`, which creates a disposable local
PostgreSQL database, verifies effective driver target/database/role, migrates
through 46 and drops its owned database afterward. No customer database or
production credentials were used. API probes used signed test identities,
live tenant membership and real role definitions where testing native roles.
Owner-sensitive probes exercise the existing public central policy port.

Observed successful boundaries:

- Twenty additional string fields remain patchable when fully readable, both
  with small and with approximately 30 KiB of populated values.
- The same wide schemas with hidden fields reject byte-budget admission
  consistently for paired small/large hidden values. Writable hints are empty;
  denied commands create no receipt, record mutation or outbox fact.
- Field scopes identical to the entity OWNER boundary admit predicates without
  revealing another owner's records. Unrelated hidden projections stay masked.
- A cursor cannot bypass live field revocation; joining through a hidden source
  relationship key is rejected. A plain projection retains that visible row
  while omitting the hidden reference.
- Two concurrent receipted patches using the same expected row version produce
  one 200 and one 409. Hidden values remain intact; exactly one receipt and
  update event commit.
- Numeric projection value/source version track a numeric patch and a later
  unrelated patch. A fault after audit insertion rolls back record version,
  numeric projection, receipt and outbox together.
- Existing strict rule and workflow/worker field-scope denial regressions pass;
  they receive no masked null as an executable input.
- Eight frontend decoder probes preserve legacy/optional rows and reject hidden
  values coexisting with a mask, writable/masked intersections, duplicate keys,
  non-string IDs and access lists exceeding 32 entries.

## Exact commands and outcomes

| Command actually executed | Outcome |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS; formatting only reviewer test changes |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestIndependentR5d.*\|TestIndependentM6RFieldScopeBypass\|TestM5RecordSpecificFieldABACAndReferenceEntityPolicy\|TestM9RCustomSortPagePreservesTiesNullsScopesAndJoins)$' ./tests/integration` | Initial FAIL 9.778s on three historical projection assertions; initial three new independent probes passed. After updating assertions and adding owner probes, FAIL 17.142s only on the three owner-related probes |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestIndependentR5dPatchPreservesTrustedFieldOwnerABAC$' ./tests/integration` | FAIL 2.804s; protected patch returned 200 |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestIndependentR5d.*\|TestIndependentM6RFieldScopeBypass\|TestM5RecordSpecificFieldABACAndReferenceEntityPolicy\|TestM9RCustomSortPagePreservesTiesNullsScopesAndJoins\|TestM7PreviewRuleAuthorizationIsolationAndAuditFailClosed\|TestM9FieldScopeDeniesStartSnapshotAndWorkerResume)$' ./tests/integration` | FAIL 25.516s only on the three owner-related probes; wide admission, predicates/revocation/joins, concurrent CAS, numeric rollback, historical projection checks and strict rule/workflow checks passed |
| `npm --prefix apps/web test -- --run src/preview/contracts.review.test.ts` | PASS 8/8, 1.47s |

The additional general-list and predicate owner probes were executed separately;
their outcome and any subsequent fixes/re-verification follow below. Full
`make check test build` and browser E2E are the implementation author's final
verification gate and are not claimed as executed by this independent review.

## Threat model and limits

This verifies API-mediated policy, SQL projection and transactional behavior
under trusted application composition, including an adversarial central-policy
adapter. It does not prove timing/physical noninterference, arbitrary policy
correctness, protection from a compromised application database credential or
production delivery readiness. CONDITION partial writes and conservative hidden
size reservations remain explicit ADR037 restrictions. Create/delete/replacement
update remain whole-record operations. R3b–R3d, remaining R5 administration and
identity UX, R6 and R7 are separate open work.

Additional initial-source evidence:

`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestIndependentR5dVisible(List|Predicate)HonorsTrustedFieldOwnerABAC$' ./tests/integration`

**FAIL 3.027s**. The general list omitted the owner denial, and the hidden-value
filter returned 200. Both fixtures were populated, including another visible
owner's record in the list case. These are regression tests for finding 2;
implementation and re-verification remain pending at this review checkpoint.

## First fix checkpoint — `c96c534`

The implementation author added trusted row/owner field actions for opaque
interactive queries, SQL ID-array masks, a bounded complete predicate universe,
a concrete Native/Defined optimization proof and a closed WithRuntime stable
snapshot proof. Patch checks now use the loaded owner before validation.
Source review found those changes consistent with the intended fixes. One
executed wrapper embedding Native correctly remained opaque and its denied
balance was masked by the interactive endpoint. Full independent closure of the
original regressions is deferred until the following alternate path is fixed.

3. **HIGH — strict rule/workflow inputs bypass the same owner-sensitive field
   action denial.** The strict `Store.Query` port still checks fields without
   actual row/owner context. With a Native-embedding policy denying read access
   to the owner's balance and returning SQL scope ALL, the interactive endpoint
   correctly masks that balance, but the rule evaluation endpoint returns 200
   and its threshold result. A corresponding owner-denied name can still enter
   a workflow snapshot; workflow start returns 200. This is an alternate value
   inference/capture path and prevents R5d closure even though the new visible
   port itself is corrected. Strict point inputs must obtain trusted header
   context and enforce the field/row actions before consuming record values.
   Preserve the private worker source adapter; do not substitute generic worker
   table access or masked nulls.

`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestIndependentR5dStrict(Rule|Workflow)Cannot' ./tests/integration`

**FAIL 3.951s**: both the rule and workflow returned 200 for owner-denied source
fields. Both probes use populated disposable databases and central policy ports.
No source edits or unrelated test workloads ran during the review. The reviewer
added the two independent strict-input regressions. Status remains **PARTIAL**
pending a source fix and re-verification, including the original owner probes,
joined target checks, bounded universes and executor-isolation negatives.

A follow-up evidence assertion queried durable workflow bindings:

`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestIndependentR5dStrictWorkflowCannot' ./tests/integration`

**FAIL 2.934s**: workflow start returned 200 and one workflow binding persisted.
