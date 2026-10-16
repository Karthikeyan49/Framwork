# M12 database layer — independent adversarial review

## Progress

- 2026-10-15T20:20Z started on `review/m12-db-final` (base fb52ad3); stopped by a
  session limit, resumed 2026-10-16T05:30Z.
- DONE: live catalog inventory (roles, database/schema ACLs, default ACLs, full
  table/column privilege matrix, every policy, every SECURITY DEFINER function,
  triggers, FK actions) on the MAIN stack.
- DONE: in-transaction (always rolled back) port probes as `framework_app`
  (probe `TestZZReviewM12DatabasePort`, PASS, findings F1-F3 below).
- DONE: operator/runtime-role immutability probes, worker drift-guard coverage,
  coordinator leads (a)-(c) (F4, F6, lead c).
- DONE: contract-honesty table, final status. Probe file deleted before the
  final commit; MAIN stack re-checked: 0 M12 rows, bundles/approvals/active
  counts and worker ACL entries unchanged (12/12/6/16).

## Scope and commit

Reviewed commit `fb52ad3` ("docs(m12): record full integration result and
decider wording"). Database: MAIN stack (`framework` database, port 5432,
goose version 48), every command through `/home/user/bin/dbrun`; all M12 write
probes run inside transactions that are rolled back. `m12Setup` (which creates a
disposable database) was deliberately NOT used.

## Method

1. Static read of the scope files (ADR038 incl. amendments, appendix, migrations
   11/45/46/47/48, promotion store, release routes, M12 integration test).
2. Live catalog queries as `framework_migrator` (read only): `pg_roles`,
   `pg_auth_members`, `pg_database.datacl`, `pg_namespace.nspacl`, `pg_default_acl`,
   `pg_parameter_acl`, `has_table/column/sequence_privilege` for every relation in
   control/runtime/iam/audit x {app, worker, dispatcher, PUBLIC}, `aclexplode` of
   every `attacl`, `pg_policies`, `pg_proc` (definer/owner/config/ACL/prosrc), `pg_trigger`,
   `pg_constraint`.
3. Port probes: temporary `tests/integration/zz_review_m12db_test.go` (deleted).
   It commits only the `newIAM` identity fixture (cleaned up by newIAM), then runs
   the full source -> sandbox -> freeze -> requirements -> acceptance -> BuildCandidate
   flow and every attack inside ONE `framework_app` REPEATABLE READ transaction
   (savepoint per attack) that is rolled back. `m12Setup` was not used because it
   creates a database.
4. psql probes per role (`framework_app`, `framework_worker`, `framework_dispatcher`,
   `framework_migrator`), each in BEGIN ... ROLLBACK, after checking
   `current_database()`/`session_user`.

Repro snippet (F1/F2 core, inside a rolled-back framework_app transaction bound to
the requester):

```sql
SELECT control.stage_candidate($app, encode(sha256($b),'hex'), $b);
-- $b = server-built HIGH body with its final '}' replaced by
-- ,"HighRisk":false,"Risk":{"Level":"LOW","PolicyVersion":"release-risk-v1"}}
INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash) VALUES($t,$app,$h); -- requester: allowed
INSERT INTO control.active_releases(tenant_id,app_id,artifact_hash,generation) VALUES($t,$app,$h,1); -- allowed
```

## Commands

| Command (all via `/home/user/bin/dbrun node scripts/with-env.mjs ...`) | Result | Duration |
|---|---|---|
| psql whoami per DSN (app/worker/dispatcher/migrator) | PASS (db `framework`, expected roles, goose 48) | <1 s each |
| psql `roles.sql`, `matrix.sql`, `colacl.sql`, `policies.sql`, `definers.sql`, `triggers.sql`, `workerwi.sql` | PASS (inventory) | <2 s each |
| `./scripts/go.sh vet -tags=integration ./tests/integration/` | PASS | ~1 min |
| `./scripts/go.sh test -tags=integration -count=1 -run TestZZReviewM12DatabasePort -v ./tests/integration` (4 runs, last with all sections) | PASS (probe; findings in log) | 3.1 s test, 19 s wall |
| psql `operator.sql` (migrator, rolled back) | PASS (see residual) | <1 s |
| psql `runtime_roles.sql` x4 roles (rolled back) | PASS | <1 s each |
| psql `guard.sql` (migrator, rolled back, 635 grant forms) | PASS (F5) | ~2 s |
| psql `after.sql` (post-run residue check) | PASS (no residue) | <1 s |
| Full integration suite / `m12_candidate_test.go` | NOT RUN (brief forbids full suite; M12 tests create databases) | - |
| Migrations onto an empty database | NOT RUN (no database creation allowed); static ordering review only | - |

## Live inventory (MAIN stack, PostgreSQL 18.6)

Roles: no runtime role is superuser, has BYPASSRLS, CREATEROLE or CREATEDB.
Only membership: `framework_migrator` in `framework_schema_owner` (INHERIT, SET).
Database ACL: PUBLIC has no CONNECT/TEMP; `framework_app`, `framework_worker`,
`framework_dispatcher` have CONNECT only (no TEMP, so no pg_temp objects).
Schemas control/runtime/iam/audit: owner `framework_schema_owner`; USAGE only
for runtime roles; no PUBLIC USAGE; no CREATE for any runtime role. `pg_default_acl`
is empty. `session_replication_role` is superuser-only; no parameter ACL grants it.

### Privilege matrix (effective, `has_*_privilege`, membership-aware)

PUBLIC: no privilege on any table, view, sequence or function in the four
schemas. `framework_dispatcher`: SELECT `control.job_routes` only.

| Object | framework_app | framework_worker |
|---|---|---|
| control.requirement_sets / acceptance_specs / release_candidates (M48) | SELECT | none |
| control.release_bundles | SELECT (table INSERT and all column INSERT revoked) | none |
| control.release_approvals | SELECT, INSERT(tenant_id,app_id,artifact_hash) | none |
| control.active_releases | SELECT, INSERT (table), UPDATE(artifact_hash,generation) | none |
| runtime.sandboxes / sandbox_evidence (M46) | SELECT | none |
| runtime.tool_invocations / tool_attempts (M45) | SELECT | SELECT |
| control.metadata_versions | SELECT, INSERT(tenant_id,app_id,revision,document,source_hash) | none |
| runtime.datasets | SELECT, INSERT(9 cols incl. environment), UPDATE(state,active_binding,epoch,...) | none |

No runtime role holds DELETE/TRUNCATE/REFERENCES/TRIGGER on any evidence table.

### SECURITY DEFINER inventory (72 functions, whole database)

All 72 are owned by `framework_schema_owner`, pin `search_path=pg_catalog, pg_temp`,
grant no PUBLIC EXECUTE and contain no dynamic SQL (`EXECUTE`/`format`) — from
`pg_proc` (`prosecdef`, `proconfig`, `proacl`, `prosrc`). M45-M48 definers:

| Function | EXECUTE | Verdict |
|---|---|---|
| control.release_actor_ok(uuid) | owner only | OK: session_user=framework_app, tenant GUC, live non-viewer membership |
| control.append_requirements(text,bigint,bytea) | framework_app | OK for scope/CAS; accepts non-canonical JSON (see findings) |
| control.author_acceptance(text,text,bytea) | framework_app | OK: author from actor, idempotent per author; accepts non-canonical JSON |
| control.stage_candidate(text,text,bytea) | framework_app | binds rows; risk/verification are body claims (see findings) |
| control.worker_grants_safe() | framework_worker | denylist guard; see findings |
| runtime.register_sandbox / freeze_sandbox | framework_app | OK: session_user + membership |
| control.worker_environment, tool_begin/finish/abandon | framework_worker | lease-bound |
| control.tool_reconcile | framework_app | session_user + membership |
| control.tool_lease, tool_key, tool_mode_rank, guard_* triggers | owner only | internal |

Non-definer `audit.reject_mutation()` has no pinned search_path (trigger only; no
runtime role can create objects, so not exploitable).

## Findings

### F1 — MEDIUM — M6R approval/activation SQL stays open to `framework_app`; a non-restorable body can be self-approved and activated

- Where: `db/migrations/00011_v3_release_and_outbox_contracts.sql:22-27,35-38`
  (`approval_insert` reads `HighRisk`/`RequesterID` from the jsonb view; column
  INSERT on `release_approvals`; table INSERT + UPDATE(artifact_hash,generation)
  on `active_releases`), untouched by `00048` (which only adds TRUNCATE guards).
  Live matrix: `framework_app` = `release_approvals` S,colI; `active_releases` S,I,colU.
- Reproduction (rolled back, `TestZZReviewM12DatabasePort`, section 3): stage the
  server-built HIGH candidate's bytes with a trailing duplicate
  `,"HighRisk":false,"Risk":{"Level":"LOW","PolicyVersion":"release-risk-v1"}`
  (see F2) as the requester alice -> `stage_candidate` returns true and the index
  row says `LOW/false`; alice then `INSERT INTO control.release_approvals` (policy
  passes: jsonb `HighRisk` is false) and `INSERT INTO control.active_releases ... 1`
  -> both succeed. `Store.Active` for the app then fails ("bundle 2.0.0 is not
  canonical"), and `promotion.CheckActiveCompatibility` (`cmd/release-check`, the
  operator deployment gate that restores every tenant's active release) would fail
  for the whole platform. Separately, for the canonical HIGH candidate the
  acceptance author bob's approval is accepted and ONE approval activates it,
  although the bundle records `Separation` HIGH = exclude requester and acceptance
  author (the shipped integration test does exactly this as "independent approval",
  `tests/integration/m12_candidate_test.go:567-579`).
- Impact: only via `framework_app` SQL (no HTTP route approves/activates in M12),
  i.e. an API-process compromise or a future buggy caller. ADR038 says nothing in
  M12 approves/activates, yet the DB still authorizes both with the weaker M6R rule;
  "Restore recomputation at approval is the backstop" (ADR038 §6) is not enforced
  by the database.
- Fix: revoke `framework_app` INSERT on `release_approvals` and INSERT/UPDATE on
  `active_releases` until M13 ports exist; M13 ports must read the derived
  `release_candidates` row (not the body) and require a Go Restore first.

### F2 — LOW — the ports accept non-canonical JSON; checks/index read the jsonb view, Go reads different bytes

- Where: `00048_m12_candidate_builder.sql:78,96,121` (`convert_from(...)::jsonb`, then
  `->>` checks); index row copied from that jsonb view (`:164-167`).
- Reproduction (rolled back, section 2; all `stage_candidate` calls returned true):
  | body variant | index row | jsonb view | lenient `json.Unmarshal` | `promotion.Restore` |
  |---|---|---|---|---|
  | leading duplicate `HighRisk:false` | HIGH/true | true | true | not canonical |
  | trailing `"highrisk":false` | HIGH/true | true | **false** | not canonical |
  | trailing `"requesterid":"<bob>"` | requester alice | alice | **bob** | not canonical |
  | trailing duplicate HighRisk+Risk LOW | **LOW/false** | false | false | not canonical |
  | `BaseGeneration` written `0e0` | HIGH/true | ok | unmarshal error | schema error |
  | leading whitespace | HIGH/true | ok | ok | not canonical |
  `append_requirements` / `author_acceptance` likewise stored duplicate-key bodies
  that `intent.ParseRequirementSet`/`ParseAcceptanceSpec` reject (INVALID_JSON).
- Impact: jsonb keeps the last duplicate and is case-sensitive; Go's lenient decoder
  is case-insensitive. No production Go reader decodes leniently (every reader
  restores with canonical re-marshal equality), so through the honest API this is
  not exploitable; it is what makes F1's chain work and it falsifies the
  migration comment that every index column is "copied ... from the verified body"
  (`00048:34-35`): the database never verifies the body.
- Fix: document the index as unverified; have DB consumers (M6R policy, future M13
  ports) rely on Go Restore, or add a cheap canonical guard (e.g. reject when
  `p_body` contains bytes outside the Go encoder's output alphabet/whitespace).

### F3 — LOW — idempotent replay is not idempotent once a binding moves

- Reproduction (section 3): after the candidate's base moved (activation), an
  exact replay by the same requester raises 55000 "candidate base is no longer
  current" (API 409) instead of returning false (`00048:152-161` run before the
  `ON CONFLICT`). Harmless (a stale candidate must be rebuilt) but ADR038
  Amendment 8 describes replays as `release.candidate_replayed`.


### F4 — LOW — what `stage_candidate` can and cannot bind (coordinator lead a: CONFIRMED); the base diff is never recomputed in production

- The port binds (all verified by probes): tenant = tenant GUC; app = `p_app`;
  `RequesterID` = actor; content hash; `Version` 2.0.0; `HighRisk` == (Level in
  HIGH/CRITICAL); `Verification.Outcome` == "PASS" (a claim); `Risk.PolicyVersion`
  length 1..80; `Source.{Revision,Hash}` = stored revision row; `Requirements.{Version,Hash}`
  = stored row; `Acceptance.{ID,Hash,RequirementsHash,AuthorID}` = stored row bound to
  that set; author != actor and != source author; `Sandbox.{DatasetID,EvidenceHash,
  Evidence,FrozenBy}` = FROZEN evidence row whose `ir_hash` = `IR.Hash`,
  `metadata_revision` = `Source.Revision`, data mode SYNTHETIC_SEPARATE;
  `BaseRelease/BaseGeneration` = current `active_releases` row (or none).
- It cannot/does not bind: `IR.JSON`, `Source.JSON`, `Requirements.JSON`,
  `Acceptance.JSON` to their hashes; `Diff`, `Storage`, `Evolution`, `Dependencies`
  (policy/tool/schema hashes, incl. `BaseSchemaHash` vs the base bundle),
  `RuntimeContract`, `Risk.{Level,Facts,Reasons,Separation}` correctness and the
  verification report. Reproduction (rolled back, section 8): canonical bodies with
  Risk LOW+HighRisk false, replaced `Diff.Hash`, `BaseSchemaHash`, `PolicyBundleHash`,
  emptied report, `Requirements.JSON`/`IR.JSON` swapped with hashes kept, unknown risk
  policy version, emptied separation — every one returned true from
  `stage_candidate` (index `LOW/false` for the first); `promotion.Restore` rejected
  every one. This matches ADR038 §6's stated residual.
- Gap: `restoreV2` checks the diff only structurally (`internal/promotion/bundle2.go:474-476,529-531`);
  only `Candidate.VerifyAgainstBase` (`bundle2.go:644`) recomputes diff/storage from the
  base program, and nothing outside tests calls it (`grep VerifyAgainstBase`). The
  database could bind `Diff` BaseIRHash / `Dependencies.BaseSchemaHash` to the base
  bundle row it already locates, but does not. ADR038 Amendment 2 ("fully
  recomputed (diff, storage class, ...)") overstates what `Restore` does.
- Fix: M13 approval/activation must call `VerifyAgainstBase` against the active
  base; optionally bind base IR/schema hashes in the port.

### F5 — LOW — `control.worker_grants_safe()` is a fixed denylist without REFERENCES/TRIGGER

- Reproduction (`guard.sql`, migrator transaction rolled back; each grant in its own
  subtransaction; guard body evaluated with `session_user` replaced by
  `'framework_worker'`): 264 grants trip the guard, including every table/column
  SELECT/INSERT/UPDATE/DELETE/TRUNCATE on the three M12 tables, sandboxes,
  sandbox_evidence and release_bundles. NOT detected: REFERENCES, TRIGGER and column
  REFERENCES on every listed table (incl. M12's), and any privilege on unlisted tables
  such as `control.release_approvals`, `control.active_releases`,
  `runtime.preview_records`, `runtime.preview_datasets`, `audit.entries`,
  `control.workflow_jobs` (371 undetected grant forms). Workflow readiness
  (`internal/runtime/workflow/readiness.go:80-108`) covers part of the rest.
  Impact low today (the worker has no CREATE/TEMP, so REFERENCES/TRIGGER are hard to
  use). Fix: derive the guard from an allowlist of what the worker may hold.

### F6 — MEDIUM (static, not dynamically reproduced) — coordinator lead b: worker UPDATE(state_id,status,...) plus a worker-asserted approval flag

- `00020_m9_workflow_runtime.sql:38` grants `framework_worker`
  UPDATE(state_id,status,version,attempt,entered_at,due_at,termination,failure) on
  `runtime.workflow_instances`; the `workflow_worker` policy restricts it to the exact
  leased job with a live policy decision; there is no trigger or CHECK validating
  transitions against the plan (live catalog: no triggers on the table).
- After 45-47, `control.tool_begin` accepts a step only if `state_id = step` and status
  READY/RETRY_WAIT and the step is a pinned activity — but both are worker-writable,
  and the approval evidence check runs only `IF needs_approval`
  (`00046_m11_sandbox.sql:148`), an argument the worker supplies
  (`internal/runtime/workflow/tool_step.go:230`, `def.Approval == "REQUIRED"`). A
  compromised worker holding a lease can set its instance to a REQUIRED tool step,
  call `tool_begin(..., false)` and obtain a ledger attempt without any approval
  signal, contrary to ADR036 §7 ("sends only if the database finds an API-written
  approval signal"). Network effects are performed by the worker anyway, so the
  breach is of the database evidence/authority claim. Fix: derive "approval
  required" in SQL from the pinned plan state / catalog hash, not from a parameter.

### Residual — operator (migrator) can bypass immutability deliberately (documented, not a finding)

Migrator transaction rolled back (`operator.sql`): UPDATE/DELETE on requirement_sets,
acceptance_specs, release_bundles, release_approvals -> "immutable artifact";
TRUNCATE release_candidates, `TRUNCATE control.metadata_heads CASCADE` (cascades to
28 tables) and `TRUNCATE control.release_bundles CASCADE` -> refused;
`SET session_replication_role=replica` -> permission denied. As owner it CAN
`ALTER TABLE ... DISABLE TRIGGER` and then DELETE spec rows, `CREATE RULE ... DO
INSTEAD NOTHING` on release_candidates, create an inheriting child of
release_bundles, and INSERT bundles directly. The migration comment promises
protection against "an operator mistake", which holds.

## Runtime-role immutability matrix (executed, rolled back)

`runtime_roles.sql` per role: framework_app, framework_worker,
framework_dispatcher -> UPDATE requirement_sets, DELETE release_candidates, INSERT
release_bundles/release_candidates, TRUNCATE release_bundles/sandbox_evidence all
"permission denied"; CREATE TEMP TABLE denied (no TEMP); CREATE TABLE in control
denied; CREATE RULE / ALTER TABLE DISABLE TRIGGER -> "must be owner";
`stage_candidate` -> 42501 'tenant release builder required' (app, no actor) or
"permission denied for function" (worker, dispatcher); migrator -> 42501 (session_user
check); `release_actor_ok` not executable by any runtime role.
`worker_grants_safe()` as the worker = true.

## Coordinator lead c — CONFIRMED

`framework_app` holds INSERT(tenant_id,app_id,artifact_hash) on
`control.release_approvals` and table INSERT + UPDATE(artifact_hash,generation) on
`control.active_releases` (`relacl`/`attacl`); `pg_default_acl` is empty, so no default
privilege re-grants anything; nothing re-grants INSERT on `release_bundles` (table
ACL `framework_app=r`, no column ACL). Exercised end to end in F1.

## Verified (attacks that held)

From `TestZZReviewM12DatabasePort` (one framework_app REPEATABLE READ transaction,
savepoint per attack, whole transaction rolled back; 0.69 s):
- Control: the server-built HIGH candidate stages; a same-actor replay returns
  false; exactly one index row (no partial write).
- Cross-app: acceptance of app2 (same author) -> 55000 "stored requirement set
  required"; frozen sandbox of app2 -> 55000; source hash of app2 -> 55000;
  `p_app`=app2 with the app1 body -> 22023.
- Cross-tenant: bob (owner of tenant B, admin in A) bound to B sees 0 rows of A's
  requirement sets/specs/sandboxes/evidence/metadata in the same transaction;
  staging A's body -> 22023; body re-labelled to tenant B -> 55000; authoring
  against A's requirement hash -> 55000; appending for A's app id -> 23503 (FK,
  detail names only tenant B).
- GUCs: tenant GUC spoofed to a non-member tenant or unset -> 0 visible rows and
  42501; `app.actor_id` swapped to bob without bob's issuer/subject -> 42501; viewer -> 42501.
- Acceptance: alice resubmitting bob's exact spec gets a NEW id authored by alice
  (cannot claim bob's); staging with alice's copy -> 42501 (author = requester);
  bob's replay returns his original id.
- Requirement CAS: expected 0, 2, 5 while head=1 -> 55000; no gap possible
  (`version = expected+1` only when `expected = max(version)`); concurrent
  appenders collide on the primary key (23505/55P03, both 409) — static.
- Inputs: invalid UTF-8, `\u0000`, 200k-deep nesting (7 ms), 8 MiB+1, hash
  mismatch, empty body -> 22023 'invalid candidate'; JSON array -> 22023;
  requirement bodies of 1 and 262145 bytes -> 22023. Error texts are constants.

## Contract honesty (ADR038 §5-§6, Amendments 4 and 7)

| Claim | Verdict | Evidence |
|---|---|---|
| §5 RequesterID = authenticated actor | VERIFIED | bob staging alice's body -> 22023; trailing-key tricks cannot change the jsonb RequesterID check |
| §5 acceptance author != requester | VERIFIED | alice's copy of the spec -> 42501 |
| §5 acceptance author != source author | VERIFIED | bob wrote source and spec, alice requests -> 42501 at the port |
| §5 "requester ... holding `candidates.build`" (heading: DB-enforced) | FALSIFIED as a DB claim | `release_actor_ok` checks only active non-viewer membership (`00048:66-69`); `candidates.build` is API policy only |
| §5 decider rule | superseded by Amendment 4 (not compared) | `00048` never reads `decided_by` |
| §6 owner/search_path/execute only framework_app | VERIFIED | definer inventory; worker/dispatcher "permission denied"; migrator (inherits EXECUTE) -> 42501 |
| §6 checks tenant scope, actor, hash, 2.0.0 | VERIFIED | GUC/tenant probes, edge probes |
| §6 "app id equals the body's IR app" | FALSIFIED (wording) | the port compares top-level `AppID` with `p_app`; the IR's app id is checked only by Go Restore |
| §6 rows exist with matching hashes and DB authors; base current or absent | VERIFIED | sections 4-5 and "base claimed" probes; stale base -> 55000 |
| §6 compromised API can stage self-consistent forged body; Restore is backstop | VERIFIED residual, backstop incomplete | F4 (all forgeries accepted, Restore rejects; diff not recomputed outside tests); F1 (DB approval path does not require Restore) |
| A4 R1/R2, freezer bound and recorded, no waiver | VERIFIED | probes above; index carries `sandbox_frozen_by` |
| A4 "database-stamped identities" | VERIFIED with caveat | stamped from `iam.authenticated_actor()` = GUC triple (actor id + issuer + subject); a compromised API process can impersonate any user whose subject it knows (GUCs are not credentials) |
| A7 binds evidence text/freezer, requirement row hash, acceptance row, active base | VERIFIED | doctored-binding probes (55000 each) |
| A7 bundle + index row written together | VERIFIED | one function; index count 1 after stage+replay; any failure aborts both |
| A7 append_requirements CAS; author_acceptance idempotent per author/content | VERIFIED | CAS probes; same-author replay returns same id; other author gets a new id |
| A7 direct INSERT on release_bundles revoked at table and column level | VERIFIED | `relacl` `framework_app=r`, no column ACL, INSERT -> permission denied, no default ACL |
| A7 TRUNCATE guards cover new tables, bundles, approvals | VERIFIED | migrator TRUNCATE (incl. CASCADE) refused |
| A7 worker_grants_safe includes the new tables | VERIFIED (S/I/U/D/T and column S/I/U), gaps F5 | guard.sql |
| A8 replay records `release.candidate_replayed` | PARTIAL | F3: a replay after the base moved is 409, not a replay |

## Not covered

- Real concurrency (two committed appenders, activation racing a stager): reasoned
  statically (PK serialization; stale-base staging is reported STALE on read), not run
  because it needs committed fixtures.
- F6 not reproduced dynamically (needs a dispatcher/worker lease fixture).
- A custom-role (non-viewer without `candidates.build`) direct port call: static only.
- Applying migrations 45-48 to an empty database (no database creation allowed).
- HTTP-level behaviour and the Go verifier/risk logic beyond what Restore printed.

M12 DB review STATUS: PASS WITH FINDINGS
