# Upgrade and migration-ordering rehearsal (independent)

Scope: `integration/m9r-m14` at `be6ec7c`, branch `audit/upgrade-rehearsal`. The verifier wrote none of the code or migrations under
test and changed none of them. Every hypothetical migration lives in a scratch copy of the tree that is never committed. All databases
are disposable databases created on the isolated M13 Compose stack (127.0.0.1:5433, project `framework-m13`, container
`framework-m13-postgres-1`, PostgreSQL 18.6), or that stack's own database after `reset.sh`. Every probe checked `current_database()`
and `session_user` of each binding before writing. Probe tests were temporary `tests/integration/zz_review_*_test.go` files that are
not committed; their text is summarised here.

## Progress

| Item | Status | Result |
|---|---|---|
| T1 (a)-(d) populated 57 -> 90 (data written by the remediation line's own code) | done | **PASS** |
| T1 (a)-(d) populated 89 -> 90 (00090 over live unique claims) | done | **PASS** |
| T1 schema: fresh 1..90 vs populated 57 -> 90 vs populated 89 -> 90 | done | **PASS** (byte-identical dumps) |
| T2 (i) 00091 replaces a function: fresh vs upgraded | done | **PASS** (identical schema) |
| T2 (ii) two 00091 files; renumbering the later one | done | **PASS** as stated; rule gap found: **D1 (MEDIUM)** |
| T2 (iii) file below the applied maximum | done | **PASS** (refused, nothing applied); out-of-order shown to diverge |
| T2 (iv) holes 58-79 | done | **PASS** |
| T2 (v) lineage guard | done | **PASS** (no misfire at empty/57/89/90/91; fires on a real old-numbered database) |
| T2 (vi) number/count assumptions | done | **PASS** (nothing breaks with 00091; list below) |
| Verdict on the numbering rule | done | rules 1, 3 confirmed with corrections; rule 2 incomplete (D1); rule 4 NOT RUN |
| Proposed static test | done | `db/migrations/numbering_test.go`, separate commit "proposed test" |
| Re-run R1 on `43d417e` (00091): release data 89 -> 91 | done | **PASS** |
| Re-run R2 on `43d417e`: populated 57 -> 91, lineage at 90/91 | done | **PASS** |
| Re-run R3 on `43d417e`: `numbering_test.go`, `cmd/migrate` messages, hard-coded numbers | done | **PASS** (D2 fixed on the head) |
| Re-run R4 on `507c12c` (00092): release data 89 -> 92 and 91 -> 92, populated 57 -> 92, lineage 90/91/92 | done | **PASS** |
| 00092 (a): candidates in every state at 91 -> 92, one fence row each, `writes` kept, no other table changed, dumps equal | done | **PASS** |
| 00092 (b): fresh install 1..92 with the real binary, status, lineage at 92, numbering test | done | **PASS** |
| 00092 (c): `SET LOCAL ROLE` backfill with the real migrator login; role of a following hypothetical 00093 | done | **PASS** |

Environment: `ensure-docker` (all three stacks ready), `reset.sh` of the M13 stack from this worktree (68 migrations, head 90). Every
database command ran under `/home/user/bin/dbrun13 nice -n 15`. The host restarted once during Task 2 (after item vi had run); the stack
was rebuilt with `reset.sh` and the scratch evidence survived.

Defects found (details and repros in the sections below):

| ID | Severity | Summary |
|---|---|---|
| D1 | MEDIUM | Rule 2 ("the later line renumbers its own unmerged migration") says nothing about databases that already applied that migration under its old number. Such a database is then either blocked (`migrate up` exit 1) or, if the migration is idempotent, silently skips the earlier line's migration while `migrate status` reports everything applied; nothing detects it |
| D2 | LOW | `cmd/migrate` replaces goose's reason with a generic text: a duplicate number gives `migration provider initialization failed`, a file below the applied maximum gives `migration failed (no connection details logged); review migration and database state`. The plan's quoted goose message is never shown to the operator |
| D3 | LOW | The plan says 58-79 stay unused "as in every database built so far"; the old milestone database `MAIN` holds version 60 (the milestone line's old number for today's 00089) |
| D4 | LOW | The plan cites `tests/integration/m_migration_lineage_test.go` as "executed against the real `migrate` command"; the test calls `postgres.CheckMigrationLineage` in process on a database whose row 80 it deletes. This rehearsal ran the real binary on a real old-numbered database (PASS, item v) |

## Task 1 - populated upgrade rehearsal

### Method, and why the 57 data comes from the remediation line's code

A first attempt populated a database at 57 with the integrated binary's own HTTP handlers, as the remediation fixtures do. It stopped
at the first dataset: `POST .../datasets` answered **503 `service_unavailable`**, because the integrated dataset reader also reads the
sandbox state that only exists from `00081` on. The integrated binary needs the integrated schema (migrate first, start the binary
second); it cannot stand in for the remediation binary. Rather than hand-writing rows that only imitate the remediation runtime, the
57 database was written **by the remediation line itself**:

1. `git diff b00da36 be6ec7c -- db/migrations` adds only `00080`-`00090`; the blob hashes of `00001`-`00057` are identical on both
   commits (`git ls-tree` of the 57 files hashed: `3cfd069b...` on both).
2. `git archive b00da36` (the remediation head that is the merge's first parent) was extracted into the session scratch directory
   (not a worktree, nothing checked out, never committed). Two test-helper edits were made in that scratch copy only: the disposable
   database and the IAM rows are kept when `ZZ_KEEP=1`, and the event-effects fixture table is created in a schema `m8_fixture` instead
   of `runtime.m8_test_effects`, so the database models a production remediation database (no test relation with a worker grant inside
   the four guarded schemas; see "Observations").
3. A scratch test in that tree (`TestZZReviewPopulateRemediation57`, run with the worktree's `with-env.mjs` and `go -C <scratch>`)
   migrated a disposable database with the remediation tree's goose files (`00001`-`00057`, `max(version_id)=57` asserted) and wrote,
   through the remediation line's own HTTP handlers, worker, dispatcher and consumer, plus the operator-SQL seeds of its own fixtures:
   * tenant A (owner alice, viewer, admin bob) and tenant B; the disposable worker principal assigned to tenant A;
   * app U: revision 1 and 2 at `1.0.0`, 60 records seeded by operator SQL (the `uniqueFixture` insert), a dataset adopted through
     its verification batches, revision 3 at **`1.4.0`** with `field.contact.memo0` unique, bound as binding 2; the unique constraint
     registered and built to **READY** (60 scanned, 60 verified), then API creates (`pre-upgrade-1` 200, the same value 409, a null 200);
     a scalar text index, a numeric index and a search index on that binding, each built to READY;
   * app W: a `1.3.0` source whose workflow is **DSL `2.1.0`** with the approval roles `finance_manager, owner`; the tenant role
     `finance_manager` published and assigned to bob; an instance started and driven by the worker to `WAITING_APPROVAL` (version 6);
   * events: one delivery claimed, published and consumed (COMPLETED, inbox row, effect, consume audit), one claimed and published but
     not consumed (SENT), one PENDING, plus the M8 fixture's `LEGACY_PAUSED` intent written at migration 17;
   * the audit rows all of this produced (272).

   Result: 62 tables, **43 with rows, 745 rows** (`audit.entries` 272, `runtime.records` 63, `runtime.unique_claims` 62, the scalar,
   numeric and search projections 62 each, `control.metadata_versions` 4 = `1.0.0` x2, `1.3.0`, `1.4.0`, workflow history/outbox 6 each,
   role heads/versions 9 each, event deliveries 4, envelopes 3, inbox 1, ...).
4. The integrated-tree probe (`TestZZReviewUpgradeRemediation57`) attached to that database (every binding re-pointed and verified:
   database name and session role of app, migrator, worker and dispatcher), rebuilt the fixture handles from the recorded UUIDs (a new
   signing key; identities resolve by issuer and subject), took the "before" evidence, ran `goose.NewProvider(..., migrations.Files)`
   `.UpTo(ctx, 90)` and `Up` again (0 applied), took the "after" evidence, dumped the schema, then exercised the current code.

The 89 rehearsal (`TestZZReviewUpgradePopulated89`) used `m8DatabaseAt(t, 89)` and the integrated binary itself for the same data
(at 89 the integrated binary has its schema), plus one `1.5.0` app with a unique field registered **before** `00090`.

The evidence per table: `count(*)` and `sha256` over the sorted `ROW(<columns as they were before>)::text` of every ordinary and
partitioned table outside the system schemas, read as the superuser of the disposable database (so forced RLS hides nothing). The
column list of a pre-existing table is frozen from the "before" snapshot, so an added column could not mask a changed value (none was
added). `public.goose_db_version` is excluded from the equality (it gains the new versions by design).

### (a) the upgrade succeeds with the data present - PASS

| Start | Applied | Repeat `Up` | Before the upgrade |
|---|---|---|---|
| 57 (remediation data) | `[80 81 82 83 84 85 86 87 88 89 90]` | 0 applied | `metadata_versions` 1.0.0=2, 1.3.0=1, 1.4.0=1; CHECK `IN ('1.0.0','1.1.0','1.2.0','1.3.0','1.4.0')` |
| 89 (integrated data) | `[90]` | 0 applied | 1.0.0=4, 1.3.0=1, 1.4.0=1, 1.5.0=1; CHECK already lists 1.0.0-1.5.0 |

`00080` drops and re-adds `metadata_versions_schema_version_check`; PostgreSQL validates the new CHECK against the stored `1.3.0` and
`1.4.0` rows and the migration commits. Negative control: the milestone line's original statement (`git show
60da6f2:db/migrations/00045_m10_tool_ledger.sql`, lines 5-7, list `'1.0.0','1.1.0','1.2.0','1.3.0'`) run inside `BEGIN ... ROLLBACK` on
the upgraded remediation database (which holds two `1.4.0` documents) fails with `ERROR: check constraint
"metadata_versions_schema_version_check" of relation "metadata_versions" is violated by some row`; after the rollback the merged
constraint is unchanged. So the original numbering's `00045` would have blocked this upgrade; the merged `00080` does not.

### (b) every pre-existing row is unchanged - PASS

| Start | Tables compared | Changed (count or hash) | RLS/FORCE weakened | Columns added to old tables | New tables |
|---|---|---|---|---|---|
| 57 | 61 (+ goose table) | **0** | 0 | 0 | 13, all `FORCE ROW LEVEL SECURITY`, 0 rows: `control.{acceptance_specs, ai_invocations, ai_proposals, ai_usage, release_candidates, release_fences, release_history, release_rejections, requirement_sets}`, `runtime.{sandbox_evidence, sandboxes, tool_attempts, tool_invocations}` |
| 89 | 74 (+ goose table) | **0** | 0 | 0 | none |

The per-table counts and hashes are in the probe's scratch output (`from57_before.json`, `from57_after.json`, `from89_*.json`).

### (c) CHECK, worker guard, forced RLS, SECURITY DEFINER grants - PASS

* CHECK after the upgrade (both starts): `CHECK ((schema_version = ANY (ARRAY['1.0.0', '1.1.0', '1.2.0', '1.3.0', '1.4.0', '1.5.0'])))`.
* `SELECT control.worker_grants_safe()` on the worker binding (`session_user` = `framework_worker`, `current_database()` = the
  disposable database): **true** at 90 after both upgrades.
* Forced RLS: no table that had RLS or FORCE before lost it; every new table has FORCE. The only table of `control`/`runtime`/`iam`/
  `audit` without FORCE is `iam.roles`, before and after (pre-existing, not changed by the upgrade).
* SECURITY DEFINER functions (all non-system schemas): 73 before / 103 after at 57; 103 / 103 at 89. **No ACL change on any of the
  73 pre-existing ones** (explicit grantee list and effective `has_function_privilege('framework_worker', ..., 'EXECUTE')` compared).
  **No SECURITY DEFINER function, old or new, grants EXECUTE to PUBLIC.** The new functions the worker can execute are exactly the
  milestone ports `control.tool_begin(...)`, `control.tool_finish(...)`, `control.tool_abandon(text)` (M10) and
  `control.worker_environment()` (M11); they are identical on a fresh install (see the dump comparison) and belong to the milestone
  design, not to the upgrade path. A stricter reading of the brief ("no new worker EXECUTE grant") would be false for these four.

### (d) the current code works on the upgraded data - PASS

On the database populated by the remediation line at 57 and upgraded (the same results on the 89 database):

| Check | Result |
|---|---|
| `compiler.Restore` of every pinned IR (`runtime.dataset_bindings.ir_body`, `runtime.workflow_bindings.ir_body`) with the integrated `SupportedIRVersions()` | all restore: `1.0.0/1.0.0/1.0.0`, `1.3.0/1.3.0/1.3.0` (the pinned workflow), `1.4.0/1.4.0/1.4.0` (IR/compiler/source schema) |
| the stored `1.4.0` document (revision 3) parsed by the integrated registry and compiled by the integrated compiler with `PlatformCatalog()` | **byte-identical** to the IR the remediation compiler pinned at 57 (`d2ce0a25...cdb19`) |
| unique constraint registered under 1.4.0 before the upgrade | still READY generation 1; `post-upgrade-1` 200 then 409; `pre-upgrade-1` (claimed at 57) 409; `unique-1` (seeded row) 409 |
| unique registration after the upgrade | a new 1.4.0 app and a new **1.5.0** app: register, build to READY (30 scanned), create 200, duplicate 409, seeded value 409 |
| 89 only: the 1.5.0 constraint authored before `00090` | at 89 `register` -> **HTTP 400 `invalid_index`** and every write 409 (the B-05 defect reproduced); after `00090` the same constraint registers, builds READY (20 scanned), create 200, duplicate 409 |
| pinned DSL `2.1.0` workflow with the tenant role | bob (holder of `finance_manager`) approves with a fresh token; the pin (IR and plan hash) is unchanged; the worker drives it to **SUCCEEDED** at version 8; history = outbox = version, one effect |
| events | the SENT delivery from before the upgrade is consumed; the PENDING one (and one created after the upgrade) are claimed, published and consumed: `COMPLETED=4, LEGACY_PAUSED=1`, envelopes 4, inbox 4, effects 4 |
| `data.Store{}.Ready` on the app role, `postgres.CheckMigrationLineage` at 57/89 before and at 90 after | ready; no lineage error |

### Schema: fresh install versus upgraded - PASS

`docker exec framework-m13-postgres-1 pg_dump -U postgres -d <db> --schema-only --exclude-schema=m8_fixture --exclude-schema=m9_fixture`
of (1) a fresh disposable database migrated `UpTo(90)` (`portsDatabaseAt(t, 90)`), (2) the remediation-populated database after 57 -> 90,
(3) the 89-populated database after 89 -> 90. Normalisation removed only the `\restrict`/`\unrestrict` lines, which PostgreSQL 18's
`pg_dump` writes with a random key per run. Result: the three dumps are **byte-identical** (625825 bytes each; after also dropping
comments, `SET` lines and blank lines, 5730 lines each, sha256 `f8a161ba...5cef3021`); they contain 359 `GRANT`, 231 `OWNER TO`, 225
`CREATE POLICY`, 72 `FORCE ROW LEVEL SECURITY` and 150 `CREATE FUNCTION` statements, so ownership, ACLs, policies and function bodies
are part of the comparison. The fixture schemas were excluded from all three because they are test scaffolding outside the product.

### Observations (no defect)

* A database built by the **remediation line's test suite** (`m8DatabaseAt`) carries `runtime.m8_test_effects`, a test table with a worker
  `SELECT, INSERT` grant inside a guarded schema. After `00087` the derived guard would return false on such a database (that is its
  purpose); production databases never hold that table. The integrated tree already moved the table to `m8_fixture`. Not exercised.
* Starting the integrated binary on a schema older than its own fails closed on the affected routes (503) instead of refusing to start;
  the deployment order is "migrate, then start". Recorded as context, not graded.

## Task 2 - numbering rule rehearsal

### Method

* Scratch migration sets (session scratch directory, never committed), each a copy of today's 68 files plus hypothetical files:
  `91a` (+ `00091_zz_line_a_unique_target_guard.sql`: `runtime.register_unique_constraint` replaced from its latest definition,
  `00090`, with one added guard `unique target required`; every older RAISE message kept), `dup` (+ line A's `00091` and a line-B
  `00091_zz_line_b_marker.sql` that creates `control.zz_line_b_marker()`), `renum` (line A `00091`, line B renumbered `00092`), `75`
  (+ `00075_zz_remediation_from_00052.sql`: the same function as the remediation line would write it from the definition it knows,
  `00052`, i.e. the single gate `m.schema_version='1.4.0'`, with one reworded message), and `old` (the milestone line's original files,
  `git archive 60da6f2 db/migrations`: 00001-00053 and 00060).
* Real `migrate` binaries: `go build ./cmd/migrate` in a scratch copy of `be6ec7c` (`git archive`), once per set (the embedded files
  differ). They ran against disposable databases with only `MIGRATION_DATABASE_URL` overridden, exactly like the operator command.
* In-process goose (the same v3.28.0 the binary uses) over `os.DirFS(<set>)` for the exact goose messages and for out-of-order runs.
* Schema comparison: `pg_dump --schema-only` through `docker exec framework-m13-postgres-1`, only the `\restrict` lines removed.
* Probe: `tests/integration/zz_review_ordering_test.go` (temporary), run with
  `dbrun13 nice -n 15 node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -count=1 -v -run '^TestZZReviewT2' ./tests/integration`.

### (i) a new 00091 that replaces a function from its latest definition - PASS

| Database | How | Result |
|---|---|---|
| fresh | goose `Up` over set `91a` | applied `1..57, 80..91` |
| upgraded | goose `UpTo(90)` over today's files, then the real `migrate up` of the `91a` tree | `applied 1 migration(s)`; `migrate status`: 69 applied, 0 pending |

The two schema dumps are **identical** (626173 bytes); the live function carries the new guard in both. Replacing a function from
its latest definition with the next number behaves exactly like `00090` did in Task 1.

### (ii) two migrations that both claim 00091 - PASS, with a rule gap (D1)

* goose refuses the set when the provider is opened, before touching the database:
  `found duplicate migration version 91: existing:00091_zz_line_a_unique_target_guard.sql current:00091_zz_line_b_marker.sql`
  (goose prints the two names on separate lines).
* The real `migrate` built from that tree: `up` and `status` both exit 1 with only `migration provider initialization failed`
  (D2); the database stays at 90.
* Line B renumbered to `00092`: a fresh install applies `..., 90, 91, 92`; a database at 90 gets `applied 2 migration(s)`; a database
  at 91 that already had line A gets `applied 1 migration(s)` in the order `[91 92]`. All three schema dumps are **identical**.

**D1 (MEDIUM, a documented contract that does not hold as written).** The rule renumbers the later line's migration but says nothing
about databases that already applied it under its old number (that line's development, CI or rehearsal databases - `MAIN` is the
same situation at a larger scale). Executed with the real binaries (`TestZZReviewT2iiRenumberedAfterApply`):

| Database built with line B as `00091`, then the merged tree (A=`00091`, B=`00092`) | `migrate up` | `migrate status` | Line A's change | Schema vs fresh install of the merged tree |
|---|---|---|---|---|
| B not idempotent (`CREATE FUNCTION`) | exit 1, generic message | 69 applied (counts B's 91 as A's), 1 pending (`00092`) | never applied | differs |
| B idempotent (`CREATE OR REPLACE FUNCTION`) | exit 0, `applied 1 migration(s)` | 70 applied, 0 pending | **silently skipped** | differs |

goose compares version numbers only, and `postgres.CheckMigrationLineage` recognises only the 2026 milestone renumbering (the tool
ledger without version 80). Repro (scratch, a few minutes): copy `db/migrations` into `A/` and `B/`; add line B's file as
`B/00091_x.sql` and both lines' files to `A/` as `00091_a.sql` and `00092_x.sql`; build `migrate` once with `A/` embedded; migrate a
disposable database with `B/` (goose `Up`); run the `A/` binary's `up` and `status`; compare `pg_get_functiondef` of line A's object.
Remedy (wording below): such databases are rebuilt, never migrated; a renumbering that may meet a persistent database adds a lineage
check keyed to an object only that migration creates, as `CheckMigrationLineage` does for 00080.

### (iii) a file numbered below the applied maximum - PASS (refused); out-of-order shown to diverge

On a database at 90 with set `75`:

| Call | Result |
|---|---|
| goose `Up`, `UpTo(75)`, `UpByOne` | each: `detected 1 missing (out-of-order) migration lower than database version (90): version 75` |
| goose `Status` | `00075_zz_remediation_from_00052.sql pending`, `00090_integration_unique_schema_1_5.sql applied` |
| real `migrate up` | exit 1, `migration failed (no connection details logged); review migration and database state` (D2) |
| real `migrate status` | exit 0, 68 applied, 1 pending: `00075_zz_remediation_from_00052.sql pending` |
| afterwards | version rows unchanged (last 90, no 75); `runtime.register_unique_constraint` unchanged (gate `IN('1.4.0','1.5.0')`) |

The same tree elsewhere:

| Database | Order | Live gate of `register_unique_constraint` |
|---|---|---|
| fresh install | 69 files, `00075` applied at position 57 (after 57, before 80); `00090` replaces it later | `IN('1.4.0','1.5.0')` (00090) |
| upgraded from 90 with `goose.WithAllowOutofOrder(true)` | `00075` applied after 90: `[89 90 75]` | **`='1.4.0'` only (00075): the B-05 defect is back** |

The two schemas differ in exactly two lines of that function (the version predicate and the reworded message). `00090`'s own
post-check query, run on the out-of-order database, now finds `runtime.register_unique_constraint`, but `00090` never runs again, so
nothing refuses that state. The static scans in `internal/metadata/sql_version_gate_test.go` (`TestSQLFunctionsNameWholeSchemaVersionFamilies`,
`TestMetadataVersionsCheckListsEveryKnownVersion`) **pass** on the tree with `00075`, because they take "the latest definition" in file
order: they certify the fresh-install state only, which is one more reason to keep out-of-order off.

### (iv) holes 58-79 - PASS

| Database | Real `migrate status` | Real `migrate up` |
|---|---|---|
| empty | exit 0: 0 applied, 68 pending (no entry for 58-79) | `applied 68 migration(s)`; then status 68 applied, 0 pending |
| at 57 (goose `UpTo(57)`) | 57 applied, 11 pending (`00080`-`00090`) | `applied 11 migration(s)`; version rows `[57 80 81 ... 90]` |

Task 1 also applied 80-90 over the hole with data present (goose `UpTo(90)` from 57).

### (v) the lineage guard - PASS

No misfire with the real binary (the check runs before `up` and `status`): empty database (`status` and `up`), 57 (`status`, `up`),
89 (`status` 1 pending, `up` applied 1), 90 (`status`), 91 (`status` of the `91a` tree: 69 applied; `status` and `up` of today's tree:
68 applied, `applied 0 migration(s)`), plus `CheckMigrationLineage` in process at 57, 89 and 90 in Task 1.

It fires on a real old-numbered database (goose `Up` over set `old`: 54 migrations, last `[52 53 60]`): `migrate status` and
`migrate up` both exit 1 with `database was built with the milestone line's pre-integration migration numbers (00045-00053, 00060);
rebuild it from the current migrations, do not migrate it`; the version rows are unchanged; `CheckMigrationLineage` returns
`ErrForeignMigrationNumbering`. Without the guard, goose `Status` (read only) would take 53 of today's files as applied by number and
leave 15 pending (`00054`-`00057`, `00080`-`00090`): the remediation migrations 45-53 would be skipped.

Observation: today's binary on a database at 91 reports `68 applied, 0 pending` and `applied 0 migration(s)`; goose does not report
database versions that have no file, so an older binary cannot tell that the database is ahead of it.

### (vi) what assumes the last number or counts migrations - PASS (nothing breaks with 00091)

| Location | Assumption | With 00091 |
|---|---|---|
| `tests/integration/m9r_event_policy_test.go:236` | `const r7bLastMigration int64 = 57` (end of R7b, not the trunk head) | unaffected |
| `tests/integration/m9r_event_policy_test.go:281,284`; `m9r_event_outage_review_test.go:356-357, 380, 406-407, 493-494` | `UpTo(56/57)` with exact counts 3/0, 1, 1, 2 | bounded, unaffected |
| `tests/integration/identity_policy_test.go:612-624` (`TestM1FreshDatabaseMigrations`) | counted `Up` equals the number of embedded files | derived, PASS |
| `tests/integration/m13_integrity_test.go:131-135, 167-168` | starts at 83 (`max(version_id)=83`); `Up` applies >= 2, first 84 | PASS |
| `tests/integration/m13_ports_test.go:1048, 1074-1077` | `portsDatabaseAt(t, 83)`; `Up`, then a repeat applies 0 | PASS |
| `tests/integration/m8_delivery_test.go:130, 139-146`; `foundation_test.go:90-95`; `m6r_migration_test.go:78, 140, 165-168` | `UpTo(17/8/9)` or `Up` + repeat applies 0 | PASS |
| `internal/platform/postgres/lineage.go:28` | version 80 marks the renumbered line | correct for 91 |
| `internal/metadata/sql_version_gate_test.go:66-80, 163`; `tests/integration/m13_integrity_test.go:205-217, 301-361` | "latest definition" = highest file name | valid only while databases apply files in file order (rule 3) |
| `tests/integration/m13_integrity_test.go:281-296` (`m13DroppedMessages`) | a replacement keeps every older RAISE message or lists it | applies to the real 00091 (`control.rollback_release`, latest definition `00088`) |
| `cmd/migrate/main.go:54` | prints the applied count | informational |
| docs | `LINE_INTEGRATION_PLAN.md:198` "67 migrations apply" and similar are historical records | no check reads them; `Makefile`, `.github`, `scripts`, `infra` count nothing (grep) |

Executed in a scratch copy of `be6ec7c` with the `91a` files (69 embedded), through `with-env.mjs` and `go -C`:
`TestM1FreshDatabaseMigrations`, `TestFoundationDependenciesAndMigration`, `TestM13IntegrationUpgradeFrom83KeepsM12DataAndStillPromotes`,
`TestM13IntegrationReplacedFunctionsKeepTheirGuards`, `TestM13PortsCloseTheM6RWriteGap`,
`TestM6RLegacyLateCommitMigrationReadEquivalenceAndWriteSeal`, `TestR7bForwardMigrationPreservesIssuedStateAndEvidence`,
`TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema` (1.4.0 and 1.5.0) and
`TestMigrationLineageRefusesTheMilestoneLinesOldNumbering`: **all PASS** (13.7 s); unit tests `TestSQLFunctionsNameWholeSchemaVersionFamilies`,
`TestMetadataVersionsCheckListsEveryKnownVersion`, `TestFamilyGapsSeesTheOriginalDefect`: PASS. (`TestFoundationDependenciesAndMigration`
migrates the stack's own `framework` database, which therefore briefly held the hypothetical 00091; the stack was rebuilt with
`reset.sh` afterwards.)

## Verdict on the proposed numbering rule

Sentence by sentence ("Migration numbers from here on" in `docs/migration/LINE_INTEGRATION_PLAN.md`):

| Sentence | Verdict | Evidence |
|---|---|---|
| `cmd/migrate` opens goose without out-of-order support | **confirmed** | `cmd/migrate/main.go:45`; (iii) |
| goose refuses any file numbered below the highest applied version ("missing (out-of-order) migrations lower than database version") | **confirmed, incomplete** | exact text `detected 1 missing (out-of-order) migration lower than database version (90): version 75`. Through `migrate up` the operator sees only the generic failure (D2); `migrate status` lists the file as pending with exit 0; a fresh install applies the same file |
| a database built from this branch is at 90 | **confirmed** | 68 migrations, max 90 |
| a migration numbered 58-79 would be refused on every upgrade | **confirmed** for every database at 80 or above (all built from this branch) | (iii); a database still at 57 was not tested |
| out-of-order on: fresh (58 before 80) and upgraded (58 after 90) differ wherever two migrations touch one function | **confirmed** | (iii): the B-05 gate returns on the upgraded database only |
| 1. one increasing sequence; next is 00091; highest plus one at merge time; 58-79 stay unused | **confirmed**, two corrections | (i), (iv), proposed test. "as in every database built so far" is false for `MAIN` (version 60, D3). Missing: a replacement must start from the latest definition **on the integration branch**; a remediation fix written from `00052` reverts `00090` whatever its number, because the last definition applied wins ((iii) shows the mechanism) |
| 2. final when pushed; the later line renumbers its own unmerged migration; nobody renumbers a merged one | **confirmed for the trees, incomplete for databases** | (ii) PASS; D1 |
| 3. out-of-order stays off; forward rehearsals use `UpTo(n)`, never a counted `Up` | **first half confirmed and necessary; second half too strict and not what the tree does** | (iii); (vi): counted `Up` with a derived count (`TestM1FreshDatabaseMigrations`) or a lower bound (`TestM13IntegrationUpgradeFrom83...`) is robust; the hazard is a hard-coded count of "all remaining" migrations |
| 4. ADR reservations; documents have no order constraint | **NOT RUN** | no ADR tooling was exercised |

### Recommended wording

> ### Migration numbers from here on
>
> 1. **One increasing sequence.** Migration files are `00001`-`00057` and then `00080` upward without gaps; `00058`-`00079` are
>    never used (no database built from the remediation or integrated trees holds them; the old milestone database `MAIN` holds
>    version 60 under its pre-integration numbering and `migrate` refuses it). A new migration takes the highest number on
>    `integration/m9r-m14` plus one at the moment its line merges: after the release-fence migration `00091`
>    (`control.rollback_release`) the next is `00092`.
> 2. **A pushed number is final.** Nobody renumbers a migration that is on `integration/m9r-m14`. If two lines add the same number,
>    the line that merges later renumbers its own migration, with the tests and documents that name it, before merging. Every database
>    that applied that migration under its old number (that line's development, CI and rehearsal databases) is rebuilt from the merged
>    tree, never migrated: goose compares numbers only and would either stop or silently skip the other line's migration. If such a
>    database must be kept, the renumbering adds a lineage check keyed to an object only that migration creates, as
>    `postgres.CheckMigrationLineage` does for `00080`.
> 3. **Replace from the trunk.** A migration that replaces a function, policy, CHECK or grant starts from its latest definition on
>    `integration/m9r-m14` (not on its own line's branch) and keeps every guard of it, or lists the dropped message in
>    `m13DroppedMessages` with the reason.
> 4. **Out-of-order stays off.** A file numbered below a database's highest applied version is refused on that database (`migrate up`
>    fails, `migrate status` lists it as pending) while a fresh install would apply it earlier, and the two would end in different
>    schemas. Tests bound forward rehearsals with `UpTo(n)` or derive expected counts from the embedded files; none hard-codes how many
>    migrations `Up` applies.
> 5. ADR numbers keep their reservations (064-099 remediation, 100-119 milestone); documents have no order constraint.
>
> `db/migrations/numbering_test.go` checks rule 1 without a database.

### Proposed test (separate commit "proposed test")

`db/migrations/numbering_test.go` (package `migrations`, run by `make test-unit` through `./db/...`) reads the embedded files and fails
on a name that is not `NNNNN_lower_snake_case.sql`, on a number used twice, and on any sequence other than `00001`-`00057` followed by
`00080` upward without gaps. It is deterministic (no database, no clock). Results: PASS on today's tree, on the `91a` set and on the
renumbered `91a + 92` set; FAIL with a precise message on the duplicate `00091` pair, on `00075`, on a gap (`00093` after `00090`) and
on a file named `00091_ZZ-Bad.sql`. It cannot see D1 (a database-side problem) and does not stop a replacement written from a stale
definition (rule 3 above stays a review duty, helped by `TestM13IntegrationReplacedFunctionsKeepTheirGuards` and the version-gate scan).

## Not covered

* Milestone-line data at 80-89 (tool ledger rows, sandboxes, candidates, approvals, release history, AI proposals): the 89 rehearsal
  populated remediation data and one 1.5.0 document only.
* Remediation data paths not populated at 57: teams, record shares and links, field-patch receipts, aggregate witnesses, OIDC sign-in
  state, choice and datetime indexes (one text, one numeric and one search index were built).
* The development database `MAIN` (not touched); the old-numbered database of item (v) was empty.
* The milestone line's original `00045` as a whole migration (only its CHECK statement ran, inside a rolled-back transaction).
* Upgrades with live workers, open transactions or load during 80-90 (all pools were idle), volume (745 rows), and timing of the
  CHECK validations at production size.
* A database still at 57 receiving a file numbered 58-79 (expected to apply in file order; not run).
* Rule 4 (ADR reservations); the real migration `00091` (not available on this head); `-race`; repeated runs (each probe ran once;
  the first 57 attempt with the integrated binary stopped at the 503 described in Task 1 and is not counted): no flake statistics.

## Re-run on the integration head `43d417e` (contains 00091)

Pin: `43d417e` ("test(metadata): the version-gate scan reads every spelling...", the newest commit of the local
`integration/m9r-m14` when this round started; it contains `bfa47c4` = migration `00091_m13_rollback_release_fence.sql`, `b05494a` =
the new `cmd/migrate` messages and `db/migrations/numbering_test.go`, which is byte-identical to the test proposed in `188e9d6`). The
head was read from the shared object store by hash (`git archive 43d417e` into scratch); the `/home/user/integ` worktree was only
listed (`git -C /home/user/integ log --oneline -12`). The host rebooted once more during this round; the M13 stack was rebuilt with
`reset.sh` from this worktree before every database step. All database commands ran under `dbrun13 nice -n 10`.

### R1 - release data at 89 upgraded through 00090 and 00091 - PASS

Population (scratch export of `be6ec7c`, the binary that ran at 89/90, test helpers patched in scratch only so the database and the
IAM rows are kept): `m13Setup` at 89, then through the real HTTP routes and ports: R1 (the HIGH template) approved and activated, R2
approved and activated, a pointer-safe **rollback** to R1 (history `ACTIVATE, ACTIVATE, ROLLBACK`, pointer R1 generation 3), staged
candidates R3 (two approvals), R4 (**rejected**, `OTHER`), R5 (one approval) and R6 (a CRITICAL candidate staged through the M12 port on
the current pointer, quorum two, one approval by dave). Counts before the upgrade: history 3, approvals 6, rejections 1, candidates 6,
fences 3.

Upgrade and checks in a scratch export of `43d417e` (`TestZZReviewReleaseUpgrade`, `goose UpTo(91)` with the head's embedded files):

| Check | Result |
|---|---|
| applied | `[90 91]` |
| pre-existing rows | 73 tables compared (count and content hash), **0 changed** |
| `control.rollback_release` | owner `framework_schema_owner`, ACL `{framework_schema_owner=X/framework_schema_owner,framework_app=X/framework_schema_owner}`, SECURITY DEFINER, `search_path=pg_catalog, pg_temp`, identity arguments, result and volatility **identical before and after**; only the body changed: a line diff of `pg_get_functiondef` shows **no removed line** and four added lines (three comment lines and `INSERT INTO control.release_fences AS f(...) VALUES(t,p_app,p_target) ON CONFLICT(...) DO UPDATE SET writes=f.writes+1;`) |
| EXECUTE on `rollback_release` | `framework_app` yes; `framework_worker`, `framework_dispatcher` and PUBLIC **no** |
| `approve_release`, `activate_release`, `reject_release` | unchanged (owner, ACL and body hash) |
| all SECURITY DEFINER functions | 103 -> 103, no ACL drift; `control.worker_grants_safe()` as `framework_worker` true; lineage check nil |
| head code on the upgraded data | second approval of R6 through `approve_release` returns **2** (the pre-upgrade approval is counted); R3, approved before the upgrade, activates (generation 4); rollback to R2 (activated before the upgrade) returns 200 generation 5 and **writes the fence** (`release_fences.writes` for R2: 1 -> 2); R1 rejected after it ran, then a rollback onto it -> 409; approving the pre-upgrade-rejected R4 -> 409; history `ACTIVATE ACTIVATE ROLLBACK ACTIVATE ROLLBACK` passes the fixture's chain check |
| schema | `pg_dump --schema-only` (fixture schemas excluded, only `\restrict` lines removed) of the upgraded database and of a fresh `UpTo(91)` install: **identical** (626180 bytes, sha256 prefix `9c9f43261790af6f`) |

The migration's header says it replaces the function "from its only definition (00088)"; `rollback_release` is defined in `00084` and
`00088`, so "latest" is meant. The body comparison above confirms the substance ("byte for byte except for that statement"). Wording
only; a merged migration is not edited.

### R2 - populated 57 to the head, lineage at 90 and 91 - PASS

The remediation-tree population (`b00da36`, as in Task 1) was re-run at 57 and the database upgraded with the head's files: applied
`[80 ... 90 91]`; 61 pre-existing tables, 0 changed; CHECK 1.0.0-1.5.0; worker guard true; no ACL drift on the 73 old SECURITY DEFINER
functions and no PUBLIC EXECUTE; pinned IR restores; the stored 1.4.0 source recompiles byte-identically to the pinned IR; the DSL
2.1.0 approval workflow finishes SUCCEEDED; events consumed (`COMPLETED=4, LEGACY_PAUSED=1`); unique registration under 1.4.0 and 1.5.0
works. The upgraded schema dump is **identical** to the fresh `UpTo(91)` dump (same sha256 prefix `9c9f43261790af6f`).

Lineage with the head's real `migrate` binary (`go build ./cmd/migrate` of the head export):

| Database | `status` | `up` |
|---|---|---|
| empty | exit 0, 69 pending | `applied 69 migration(s)` |
| at 90 (`CheckMigrationLineage` nil) | exit 0, 68 applied, `00091_m13_rollback_release_fence.sql pending` | `applied 1 migration(s)` |
| at 91 (`CheckMigrationLineage` nil) | exit 0, 69 applied, 0 pending | `applied 0 migration(s)` |
| old milestone numbering (60da6f2 files) | exit 1 `database was built with the milestone line's pre-integration migration numbers (00045-00053, 00060); rebuild it from the current migrations, do not migrate it` | the same, versions unchanged (`[53 60]`) |

### R3 - numbering test, `cmd/migrate` messages, hard-coded numbers - PASS

* `go test ./db/migrations ./cmd/migrate` in the head export: `TestMigrationNumbersFormOneIncreasingSequence` PASS (69 files) and
  `TestUpFailureNamesTheNumberingRuleAndTheFailedMigrationButNothingFromTheDriver` PASS.
* The real binary built from the head plus one hypothetical file each, on a database at 91:

| Variant | Message (exit 1 unless stated) |
|---|---|
| a second `00091` | `migration provider initialization failed: found duplicate migration version 91: existing:00091_m13_rollback_release_fence.sql current:00091_zz_line_b_marker.sql` (both `status` and `up`) |
| `00075` below 91 | `up`: `migration refused: detected 1 missing (out-of-order) migration lower than database version (91): version 75; a migration numbered below the highest applied version is never applied (docs/migration/LINE_INTEGRATION_PLAN.md, migration numbers)`; `status`: exit 0, `00075_zz_remediation_from_00052.sql pending` |
| a `00092` that raises | `migration 92 failed (no connection details logged); review migration and database state`; the database stays at 91 |

  **D2 is fixed on the head.** Residual (LOW, operability): `migrate status` still reports a below-maximum file as plain `pending` with
  exit 0, although `up` will always refuse it.
* Hard-coded migration numbers in the head (grep of all Go files, `Makefile`, `.github`, `scripts`, `infra`; probes excluded): only
  fixed intermediate points, none equal to the head and no total count except one derived from the embedded files -
  `tests/integration/m6r_migration_test.go:78,140` (`UpTo(8)`, `UpTo(9)`), `m8_delivery_test.go:130` (`UpTo(17)`),
  `m9r_event_policy_test.go:239,281,284` (54, `r7bLastMigration`=57), `m9r_event_outage_review_test.go:334,356-357,380,386,406-407,469,493-494`
  (55, 56, 57), `m13_integrity_test.go:131,135,168` (83, 84), `m13_ports_test.go:1048` (83), `m_migration_lineage_test.go:23` and
  `internal/platform/postgres/lineage.go:28` (80, the renumbering marker), `identity_policy_test.go:612-624` (count derived from the
  files). `00091` appears only in comments (`m13r_rollback_fence_test.go:7`). `Makefile:24,26` and `.github/workflows/ci.yml:25` run
  `migrate up`/`status` without numbers.
* The rule text on the head (`docs/migration/LINE_INTEGRATION_PLAN.md:49-64`) is unchanged and now stale: "A database built from this
  branch is at 90" (now 91) and "The next number is `00091`" (taken). See the wording note below.

### Wording note after the re-run (changes to the recommendation above)

The recommendation stands with three edits, all caused by numbers going stale:

1. Rule 1 should not name the next number. `00091` is taken and `00092` is in progress, so "after `00091` the next is `00092`" is
   already out of date. Proposed text: "A new migration takes the highest number on `integration/m9r-m14` plus one when its line
   merges; `db/migrations/numbering_test.go` fails on a gap, a duplicate or a number in 58-79."
2. The facts paragraph should say "a database built from this branch is at the newest migration file" instead of "at 90", and can
   now quote what the operator sees: `migrate up` answers `migration refused: detected N missing (out-of-order) migration(s) lower than
   database version (M): version V; ...` (D2 is fixed by `b05494a`).
3. Rule 3 ("replace from the trunk") can cite `00091` as its model: `control.rollback_release` was replaced from its latest definition
   (`00088`) with only the fence statement added, owner and ACL asserted unchanged in the migration's own post-check.

### Consolidated recommended wording (supersedes the earlier text)

> ### Migration numbers from here on
>
> Facts: `cmd/migrate` opens goose without out-of-order support. A database built from this branch is at its newest migration file.
> goose refuses a file numbered below a database's highest applied version (`migrate up` answers `migration refused: detected N
> missing (out-of-order) migration(s) lower than database version (M): version V; ...`), while a fresh install applies the same file in
> number order, so allowing it would leave fresh and upgraded databases with different schemas wherever two migrations touch one object.
>
> 1. **One increasing sequence.** Files are `00001`-`00057` and then `00080` upward without gaps; `00058`-`00079` are never used (no
>    database built from the remediation or integrated trees holds them; the old milestone database `MAIN` holds 60 under its
>    pre-integration numbering and `migrate` refuses it). A new migration takes the highest number on `integration/m9r-m14` plus one
>    when its line merges. `db/migrations/numbering_test.go` fails on a gap, a duplicate or a number in 58-79.
> 2. **A pushed number is final.** Nobody renumbers a migration that is on `integration/m9r-m14`. If two lines add the same number, the
>    line that merges later renumbers its own migration, with the tests and documents that name it, before merging. Every database that
>    applied that migration under its old number (that line's development, CI and rehearsal databases) is rebuilt from the merged tree,
>    never migrated: goose compares numbers only and would stop or silently skip the other line's migration. If such a database must be
>    kept, the renumbering adds a lineage check keyed to an object only that migration creates, as `postgres.CheckMigrationLineage`
>    does for `00080`.
> 3. **Replace from the trunk.** A migration that replaces a function, policy, CHECK or grant starts from its latest definition on
>    `integration/m9r-m14` (not on its own line's branch), keeps every guard of it (or lists a dropped message in `m13DroppedMessages`
>    with the reason) and asserts owner and ACL in a post-check, as `00090` and `00091` do.
> 4. **Out-of-order stays off.** Tests bound forward rehearsals with `UpTo(n)` or derive expected counts from the embedded files; none
>    hard-codes how many migrations `Up` applies.
> 5. ADR numbers keep their reservations (064-099 remediation, 100-119 milestone); documents have no order constraint.

## Re-run on the integration head `507c12c` (contains 00092)

Pin: `507c12c` (newest local commit of `integration/m9r-m14` when the coordinator named it; `00091` = `bfa47c4`, `00092` = `37d9b14`
`00092_m13_approve_release_fence.sql`), read by hash with `git archive` into scratch. All database work ran on the M13 stack under
`dbrun13 nice -n 19`; nothing touched the M14 stack.

Populations (kept with the scratch-only `ZZ_KEEP` patch, consumed and dropped by the upgrade probes): release data at **89** by the
`be6ec7c` binary and at **91** by the `43d417e` binary, each with candidates in every state - R1 activated and later the rollback
target, R2 activated and rolled back from, R3 approved twice, R4 rejected, R5 approved once, R6 CRITICAL (quorum two) approved once,
**R7 never touched**; history `ACTIVATE, ACTIVATE, ROLLBACK`; plus the remediation population at 57 (`b00da36`).

### (a) and R4-(1): release data upgraded to 92 - PASS

| Check | from 91 (applied `[92]`) | from 89 (applied `[90 91 92]`) |
|---|---|---|
| tables other than `control.release_fences` (count and content hash) | 72 compared, **0 changed** | 72 compared, **0 changed** |
| fence rows before -> after | 3 -> 7 | 3 -> 7 |
| pre-existing fence rows | all 3 kept their `writes` (R1 **2**, R2 1, R4 1: R1 was fenced by its activation and by the 00091 rollback) | all 3 kept their `writes` (R1 1, R2 1, R4 1: the rollback at 89 predates 00091) |
| rows added by the backfill | R3, R5, R6, R7, each `writes=1` | the same |
| candidates without exactly one fence row / fence rows without a candidate | 0 / 0 | 0 / 0 |
| what `framework_migrator` (the role of 00092's post-check) sees under forced RLS | 7 of 7 candidates, 3 of 3 fences: the post-check is not vacuous | the same |
| `control.approve_release` | owner `framework_schema_owner`, ACL `{framework_schema_owner=X/...,framework_app=X/...}`, SECURITY DEFINER, `search_path`, identity, result and volatility **identical**; EXECUTE app yes, worker/dispatcher/PUBLIC no; line diff: one line removed (the plain pointer `SELECT`), added the same `SELECT ... FOR SHARE`, the fence `INSERT ... ON CONFLICT ... DO NOTHING` and six comment lines | the same |
| `control.release_candidate_fence()` | new, owner `framework_schema_owner`, ACL `{framework_schema_owner=X/framework_schema_owner}` only, SECURITY DEFINER, `search_path=pg_catalog, pg_temp`; EXECUTE for app, worker, dispatcher and PUBLIC: **none**; trigger `CREATE TRIGGER release_candidate_fence AFTER INSERT ON control.release_candidates FOR EACH ROW EXECUTE FUNCTION control.release_candidate_fence()`, enabled `O` | the same |
| `rollback_release`, `activate_release`, `reject_release` | unchanged | `rollback_release` changed by 00091 only (as in R1), the others unchanged |
| worker guard, lineage, migrator pool afterwards | `worker_grants_safe()` true; lineage nil; `current_user`/`session_user` of the migrator pool after the upgrade `framework_migrator` | the same |

The head's code on the upgraded data (both starts): the CRITICAL R6 approved once before the upgrade gets its second approval through
the new `approve_release` -> **count 2** (the pre-upgrade approval is counted), fence `writes` unchanged (1 -> 1, `DO NOTHING`); the
never-touched R7 -> count 1, fence unchanged; R3 (approved before the upgrade) activates (fence 1 -> 2); rollback to R2 (fence 1 -> 2);
the now-stale R5 -> 409; **R8 staged after the upgrade gets its fence row from the trigger** (`writes=1`) and an approval counts 1;
R1 rejected after it ran, then a rollback onto it -> 409; the pre-upgrade-rejected R4 -> 409; history `ACTIVATE ACTIVATE ROLLBACK
ACTIVATE ROLLBACK`; still no candidate without exactly one fence row.

Schema: `pg_dump --schema-only` (only `\restrict` lines removed) of the 91 -> 92 database, the 89 -> 92 database, the 57 -> 92
database and a fresh `UpTo(92)` install are **identical**: 628066 bytes, sha256 prefix `c89270aa6f8a3947`, including the trigger, the
new function and every owner and ACL.

### R4-(2): populated 57 -> 92 - PASS

Applied `[80 ... 92]`; 61 pre-existing tables, 0 changed; CHECK 1.0.0-1.5.0; worker guard true; 73 old SECURITY DEFINER functions
without ACL drift (104 after); pinned IR restores; the 1.4.0 source recompiles byte-identically; the DSL 2.1.0 approval workflow
finishes SUCCEEDED; events `COMPLETED=4, LEGACY_PAUSED=1`; unique registration under 1.4.0 and 1.5.0; schema identical to fresh.

Fixture note (not a defect): the first 57 run used a population made about 40 minutes earlier. Consuming its SENT pointer then
answered `event publication is unconfirmed or paused`, which fits the R7b 15-minute receipt budget having expired. The dispatcher
re-claimed the same delivery and it was consumed (`COMPLETED=4`, inbox 4, effects 4). A fresh population upgraded at once consumes the
pre-upgrade pointer directly (`2 more consumed`) and passes; only that run is counted.

### (b) fresh install 1..92 end to end - PASS

Real `migrate` built from `507c12c`: on an empty database `up` -> `applied 70 migration(s)`, `status` -> 70 applied, 0 pending;
`CheckMigrationLineage` nil; the schema it built equals the `goose UpTo(92)` install. `go test ./db/migrations ./cmd/migrate`:
`TestMigrationNumbersFormOneIncreasingSequence` (70 files) and `TestUpFailureNamesTheNumberingRuleAndTheFailedMigrationButNothingFromTheDriver`
PASS. Lineage with the same binary: at 90 `status` lists `00091` and `00092` pending, `up` applies 2; at 91 one pending, `up` applies 1;
at 92 nothing pending, `up` applies 0; never a lineage error. The database built with the milestone line's old numbering is still
refused by `status` and `up` (exit 1, the `ErrForeignMigrationNumbering` text), versions unchanged.

### (c) `SET LOCAL ROLE` in the 00092 backfill - PASS

The backfill ran under the real migrator login (`framework_migrator`, the login of `MIGRATION_DATABASE_URL`) in every upgrade above
and in every fresh install; its rows were written (see (a)). A hypothetical `00093_zz_role_probe.sql` in a scratch copy (raises unless
`current_user` and `session_user` are both `framework_migrator`, then creates `public.zz_role_probe` with both values), built into the
binary and run by `migrate up`: from an empty database (`applied 71 migration(s)`), from 91 (`applied 2`) and from 92 (`applied 1`),
it succeeded each time, recorded `framework_migrator/framework_migrator`, and the probe table is owned by `framework_migrator`.
goose runs each migration in its own transaction, so `SET LOCAL` ends at commit in any case.

Observation: `RESET ROLE` inside the DO block returns to the session's login role, not to "the role before `SET LOCAL`". With
`cmd/migrate` those are the same. Someone applying the file by hand from a session that had already run `SET ROLE` would run the rest
of 00092 as their login role.
