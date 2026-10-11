# M5 independent adversarial review

- Date: 2026-10-11
- Scope: M5 — Dynamic Data Runtime, isolated development/test preview
- Reviewer: independent Codex review agent under `AGENTS.md` and
  `docs/agents/82_TWO_AGENT_PROTOCOL.md`
- Result: **PASS** for the reviewed implementation and independently executed
  checks below. No unresolved BLOCKER/HIGH/MEDIUM finding remains.

The user authorized M4–M6 together. This review covers M5 only; M6 work and later
production activation require their own verification. M2–M4 form the baseline.
The reviewer made no implementation or repository test edits. Temporary integration
overlays lived under `/tmp`; this report is the reviewer-owned repository change.

## Architecture and implementation

Reviewed `internal/runtime/data` models, value validation, store, query builder and
visibility preflight; migration 00008; preview API, central policy resource/audit
composition and environment routing against the data/query/policy specifications,
roadmap, tenant/security architecture and ADR-018.

The runtime accepts private Scope minted from immutable compiler.Program and a
trusted tenant Access. Every SQL operation binds tenant, application, source revision,
IR hash and entity. Preview records combine typed identifiers, actor/time/version
columns with bounded custom JSONB. Tenant-inclusive primary/foreign keys and the
existing forced-RLS installer provide the database boundary. Runtime grants protect
identifier/ownership columns. Production composition does not register preview routes.

Dynamic SQL chooses identifiers, casts and operators from trusted plans/allowlists;
customer field keys and values are parameters. Queries bound projection, filter
depth/nodes, IN lists, page size, cursor size, joins and aggregation. Decimal/money
remain exact strings; integers remain within JavaScript's safe range. Cursor hashes
pin query and dataset but do not confer authorization.

Commands check write and response-read permissions before business mutation. Dataset
counter updates serialize writes before reference checks; row-version CAS and
repeatable-read failures prevent lost updates and reference/delete races. API replies
remain buffered until guard audit, outer audit and transaction commit succeed.
Rejected reference commands may change only the internal coordination counter, as
ADR-018 documents. No event publication, external side effect, customer DDL,
production release or source activation is introduced.

## Findings resolved

1. **HIGH — missing record/owner field authorization context.** A policy denying
   `field.contact.name` on a particular record still received HTTP 200 with its
   synthetic confidential value. A field-write policy denying the known owner also
   allowed UPDATE 200 because OwnerID was absent. Query guards now receive actual
   RecordID/OwnerID for every used field; command field read/write guards receive
   the known owner. Final independent probes return 403 and verify the denied
   update leaves values and row version unchanged.
2. **HIGH — reference target entity authorization omitted.** A contact CREATE
   referring to a separately declared hidden entity succeeded despite an explicit
   target `data.entity.read` denial. Commands now authorize target entity before
   lookup and authorize the target row. The same probe returns 403 and verifies
   that no contact record was inserted.
3. **HIGH — application-scoped preview policy bypass.** A descriptor returned 200
   despite an explicit app-specific `preview.read` denial; the outer decision lacked
   AppID. Preview route app IDs now validate before the decision, and actual AppID
   is supplied to outer preview read/write and capability-hint decisions. The final
   descriptor probe returns 403.
4. **HIGH — filtering before row/field visibility.** Source inspection showed that
   authorization only of filtered results could expose a predicate oracle: an empty
   result versus a denied matching row reveals protected values. The implementation
   now authorizes unfiltered candidates and all used fields in the same repeatable-read
   snapshot before executing customer predicates. Joined targets and lookahead follow
   that proof. This concern was identified from control flow; the independent oracle
   probe first executed after the fix and confirmed both matching and nonmatching
   guesses uniformly return 403. No pre-fix oracle execution is claimed.
5. **MEDIUM — aggregate budget preceded authorization.** A count with two rows,
   limit one and universal row-read denial returned 400 before row policy, exposing
   matching cardinality. Candidate visibility and buffered guards now precede the
   aggregate budget decision. The same independent request returns 403.
6. **MEDIUM — sum over an absent projection.** An intermediate authorization fix
   broadened the used-field set; sum validation mistook a filter field for a projected
   field. Two balances totaling 24.6 returned count 2 and sum 0 with only ID projected.
   A separate actual-projection set now rejects that request with 400.
7. **MEDIUM — date domain incompatible with PostgreSQL.** CREATE accepted
   `0000-01-01`, then date sorting failed with 503 because PostgreSQL has no year zero.
   Date and canonical UTC datetime validation now restrict years to 1–9999. The same
   invalid creation returns 400; subsequent date sorting succeeds. Repository tests
   also check a valid year-one date and sorting recovery.

Repository regressions cover contextual field/reference policy and
visibility/app/aggregate/date cases in `data_runtime_test.go`. The independent probes
also exercise owner-denial atomicity, reference-denial atomicity, candidate bounds and
keyset behavior. All reported fixes were inspected and reverified.

## Independently executed commands

Commands ran from `/workspace/Framwork`. The final race and integration suites ran
after the fixes. Initial observation probes returned zero while logging real defects;
that exit status did not mean their unsafe behavior was correct.

| Command | Result | Evidence |
| --- | --- | --- |
| `./scripts/go.sh test -race -count=1 ./internal/runtime/data ./internal/policy ./internal/platform/httpserver ./tests/security` | PASS | Initial existing unit/security suite, which did not detect the findings above. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM5' ./tests/integration` | PASS | Initial five existing M5 roots, package result 3.142s; adversarial gaps were subsequently reproduced independently. |
| `./scripts/go.sh test -race -count=1 ./internal/compiler ./internal/runtime/data ./internal/policy ./internal/platform/httpserver ./tests/security` | PASS | Final compiler/runtime/policy/HTTP/security regression checks. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^Test(M5\|M1FreshDatabaseMigrations$)' ./tests/integration` | PASS | Final seven M5 roots plus fresh migration 00001–00008/repeatability/cleanup and prior disposable-database boundary tests; package result 4.259s. |
| `./scripts/go.sh test -fuzz=FuzzTypedFilterNeverEmitsCallerSQL -fuzztime=5s -parallel=2 ./internal/runtime/data` | PASS | 65,572 executions, 6.028s; filter builder unchanged by subsequent visibility fixes. |
| `node scripts/with-env.mjs ./scripts/go.sh test -overlay=/tmp/m5-review-overlay.json -race -tags=integration -count=1 -run '^TestIndependentM5' -v ./tests/integration` | PASS | Executed initially and after fixes. Final seven independent probes: 4.572s, denial atomicity, field/reference/app policy, aggregate guards, date recovery, uniform protected predicate denial, null/descending pagination, numeric money sorting, cursor query mismatch and candidate ceiling. |
| `node scripts/with-env.mjs ./scripts/go.sh test -overlay=/tmp/m5-review-overlay.json -race -tags=integration -count=1 -run '^TestIndependentM5DataReadiness$' -v ./tests/integration` | PASS | Normal-role M5 data readiness contract against the local migrated database. |
| `./scripts/go.sh vet ./cmd/... ./internal/... ./db/... ./contracts/... ./tests/security/...` | PASS | Final Go static checks. |
| `./scripts/go.sh vet -tags=integration ./tests/integration` | PASS | Integration source static checks. |
| `./scripts/go.sh mod verify` | PASS | Managed module contents verified. |
| `node scripts/verify-docs.mjs` | PASS | All 84 supplied architecture source checksums intact. |
| `git diff --check` | PASS | No whitespace errors in the working tracked diff. |

The full overlay command includes probes also run individually during diagnosis;
individual subsets used `^TestIndependentM5Aggregate`, `^TestIndependentM5DateDomain$`,
`^TestIndependentM5FilteredFieldOracle$` and `^TestIndependentM5(Null|Candidate)`.
The 1,001-candidate test inserted only synthetic records in a uniquely created IAM/app
fixture through the normal role and M2 trusted transaction context. A no-match filter
and an unfiltered query both returned 400, proving that customer predicates do not
reduce the proof budget. No existing data was modified or deleted.

The reviewer did not rerun the complete frontend build/tests, full application smoke
or all prior-milestone integration roots. M6 frontend work was in progress, and those
checks remain separate implementer/M6 evidence. No executed source check failed;
reproduced behavioral defects and their resolution are disclosed above.

## Practical limits

This runtime is deliberately limited to development/test previews. Query visibility
requires all relevant unfiltered candidates to be readable: any denied candidate
rejects the whole query, regardless of filter/cursor. The ceiling is 1,000 candidates
plus authorized lookahead. Larger datasets need a trusted policy-predicate adapter;
per-field audit and the three-second transaction deadline can reject a query below
that ceiling. No throughput or production suitability claim is made.

Pagination does not freeze a dataset across separate requests. Concurrent edits may
change later pages, while row CAS and dataset serialization protect writes. Aggregates
must fit the requested page budget and require the summed field projected. Shared
local integration fixtures retain synthetic metadata/audit/preview evidence after IAM
cleanup; they are not production seeds.

Later milestones still own rules, durable workflows, tools, transactional outbox,
real sandbox lifecycle and approved immutable production releases. M6 must preserve
these preview/authorization/type boundaries and receive its own independent review.
