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
| T2 (i) 00091 replaces a function | in progress | NOT RUN yet |
| T2 (ii) duplicate 00091 | pending | NOT RUN yet |
| T2 (iii) file below the applied maximum | pending | NOT RUN yet |
| T2 (iv) holes 58-79 | pending | NOT RUN yet |
| T2 (v) lineage guard | partial | 57, 89 and 90 checked in T1 (no misfire); rest NOT RUN yet |
| T2 (vi) number/count assumptions | pending | NOT RUN yet |
| Verdict on the numbering rule | pending | |

Environment: `ensure-docker` (all three stacks ready), `reset.sh` of the M13 stack from this worktree (68 migrations, head 90). Every
database command ran under `/home/user/bin/dbrun13 nice -n 15`.

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
