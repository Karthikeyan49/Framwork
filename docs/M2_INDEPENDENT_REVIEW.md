# M2 independent adversarial review

- Date: 2026-10-11
- Scope: M2 — PostgreSQL RLS / Data Access
- Reviewer: independent Codex review agent under `AGENTS.md` and
  `docs/agents/82_TWO_AGENT_PROTOCOL.md`
- Result: **PASS** for the reviewed implementation and executed checks below.

No unresolved BLOCKER/HIGH finding remains. This report supplies review evidence;
human milestone approval remains a separate gate. The reviewer made no
implementation or test edits. This report is the reviewer-owned change.

## Scope and architectural assessment

Reviewed the M2 diff against the implementation roadmap, data/security architecture,
tenant-isolation risk guidance and verification requirements. Inspected migration
00004, `postgres.WithRuntime`, its repository `Executor`, role checks, identity and
tenant context rebinding, buffered API responses, real PostgreSQL isolation tests,
fresh-database migration/drift checks, ADR-015 and developer instructions.

The change stays within M2. It adds reusable transaction/RLS infrastructure and
hardens the existing IAM paths. Production business tables, metadata schemas,
customer SQL, typed query AST, dynamic CRUD, workflows and AI runtime remain absent.
Business-table CRUD/join/export-equivalent behavior is tested using uniquely named
disposable fixtures, not exposed product endpoints.

`WithRuntime` checks the exact leased connection and owns a bounded repeatable-read
transaction. Its rollback defer runs before connection cleanup, including on panic.
Cleanup uses an independent timeout after request cancellation, clears all four
identity/scope settings, resets role and discards a connection on failure. API
responses remain buffered until audit and the outer commit succeed. There is no
automatic write retry after an uncertain commit. Retained executors cannot execute
after transaction completion.

IAM lookup functions use bounded read-only `SECURITY DEFINER` queries, a fixed
`pg_catalog` search path, qualified IAM objects, revoked PUBLIC execution and the
existing migration-owner read policies. They avoid recursive membership/tenant RLS
without granting superuser/BYPASSRLS. The tenant parser fails closed on malformed
UUIDs. Identity rebind and failed tenant selection invalidate earlier scope.

The RLS installer is invoker-only, migration-owner-only and unavailable to the
normal app/PUBLIC. It requires UUID/non-null tenant scope and tenant-inclusive
primary/unique keys, distinguishes unique key columns from INCLUDE columns and
checks matching tenant-column foreign-key pairs. It quotes relation identifiers,
rejects preexisting policies/direct partition children/global exclusions and
installs symmetric USING/WITH CHECK plus ENABLE/FORCE RLS and CRUD grants.

## Finding and remediation

**HIGH — non-inherited protected owner-role membership escaped the original role
guard.** A non-superuser/non-BYPASSRLS/non-create-role/non-create-database role can
own a protected table. Granting that role with `INHERIT FALSE` leaves effective
table/schema privilege checks false, but can still permit `SET ROLE` and owner DDL.
The original guard checked membership in globally privileged roles/the named
migration role, and checked ownership only for the current role.

An independent disposable-database probe compared the prior predicate and the fixed
guard. It demonstrated that the prior predicate missed owner membership, while the
application role could switch to that owner, disable RLS and read an otherwise
denied fixture row. The updated guard rejected the runtime callback. The fixture
transaction was rolled back; temporary role membership/database/role were cleaned.
No shared data or service was stopped. The temporary helper is not a repository
implementation or a permanent role grant.

The correction checks membership in protected schema/table/function owners, including
non-inherited membership. It also rejects replication and privileged all-data,
server-file and server-program roles. Column-level IAM writes/audit updates and
runtime/control access participate in safety checks, so table-level checks do not
miss column-only grants when RLS is disabled.

The repository regression uses private probe/owner roles in the disposable database
and does not change real `framework_app` memberships. It reproduces owner DDL and
tests non-inherited table, function and schema owner rejection. A smaller
least-privilege gap was also corrected: column-level REFERENCES now participates
in runtime/control unsafe-grant checks and has a fresh-database drift regression.

## Independent execution evidence

Commands were executed from `/workspace/Framwork` and completed with exit code 0.

| Exact command | Result | Evidence |
| --- | --- | --- |
| `make check` | PASS | Final source including owner-membership/column-grant/panic-span corrections: 84 source-document checksums; format, Go vet/module verification, frontend lint/typecheck. |
| `./scripts/go.sh test -race -count=1 ./internal/identity ./internal/tenant ./internal/policy ./internal/config ./internal/platform/postgres ./tests/security` | PASS | Identity/tenant/policy/config regressions and module-boundary checks. Postgres has no direct unit test files; its behavior is exercised by real integration tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM[12]' ./tests/integration` | PASS | Final rerun completed in 4.412 seconds. Real normal-role M1/M2 tests: tenant CRUD/scans, joins/aggregates, key/FK isolation, missing/forged/inactive/switching/rebound context, pooling, rollback/panic/cancellation/audit failure, retained executor lifetime, concurrent tenant requests, unsafe DDL/role rejection and fresh migration/drift recovery, including private non-inherited owner roles and column-only access/REFERENCES. |
| `node scripts/with-env.mjs ./scripts/go.sh run .local/review/m2-owner-role.go` | PASS | Disposable adversarial reproduction: prior predicate missed owner membership; fixed guard rejected callback; owner role could disable RLS/read the denied fixture, proving why that grant is unsafe. |

The ignored reproduction helper was removed after its regression was preserved in
the repository test source. Final integration checks run the permanent private-role
regression rather than requiring this removed helper.

The implementer runs the complete build/test/smoke commands. Full builds, frontend
behavioral tests, the M0 foundation integration suite, live smoke and hosted CI
were not independently executed for this M2 review and are not claimed here.
No failed final independent check is concealed.

## Boundaries and limitations

- PostgreSQL custom settings are trusted application context, not cryptographic
  credentials. A stolen app database password plus arbitrary SQL could forge all
  identity settings. JWT verification remains in trusted Go, and Executor must
  never be exposed to AI/customer metadata. Its SQL methods are not a capability
  sandbox; transaction-control/role-changing SQL is forbidden by the repository
  contract.
- Privileged DDL administrators can change grants, ownership, functions and
  policies. Runtime checks detect selected unsafe drift; they do not constitute
  cryptographic attestation of every schema/function definition.
- Repeatable-read authorization is snapshot-consistent. Revocation takes effect
  in new transactions, without promising cancellation of already authorized reads.
- Query/lock/transaction limits are initial fixed safety budgets. Their performance
  and workload suitability require later measurement; no scale claim is made.
- No cache/file/queue/background-job tenant feature exists to test in M2. Such
  features must establish fresh delegated actor/tenant/policy context when added.
- Security migration rollback intentionally refuses to weaken the policies;
  deployed changes require reviewed forward fixes. M3 remains a separate task.

**M2 REVIEW STATUS: PASS. Stop before M3 pending a separate authorized task.**
