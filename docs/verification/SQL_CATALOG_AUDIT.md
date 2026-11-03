# SQL catalog audit (migrations 00001-00092)

Independent, report-only audit. Branch `audit/sql-catalog`, pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9`.
No product code, test, ADR or other document was changed.

## Progress

- [x] Catalog of the m12db stack (PostgreSQL 18.6, `current_database()` = `framework`, goose version 92, 72 tables,
  151 functions), read-only session (`default_transaction_read_only=on`), one query script through `dbrunm12db`.
  The stack was rebuilt by another agent between my first look (61 tables) and the main dump (72 tables,
  `pg_postmaster_start_time` 06:25:19). Every figure below comes from the main dump, taken in one session.
- [x] A private disposable PostgreSQL 16 cluster (unix socket in `.local/zz_pg16`, git-ignored, no shared stack) was built from
  the same 70 migration files (one deviation: `pg_database.datlocale` is PostgreSQL 17+, replaced by `daticulocale` in the
  body of `runtime.search_parser_hash`). Its catalog equals the m12db catalog for functions (151/151 incl. ACL, owner,
  `proconfig`), policies (223/223), triggers (103/103), table/column privileges, column ACLs and namespace/database ACLs.
  The only differences are PostgreSQL 18 additions (`m` MAINTAIN in ACL strings, `NOT NULL` rows in `pg_constraint`, the goose
  table) and the one patched function body. It was used for the executed probes (section "Executed probes").
- [x] Text scan of the 70 migration files.

## Verdict

PASS WITH FINDINGS for the scope (the schema produced by 00001-00092). No HIGH and no MEDIUM finding. Four LOW findings
(SC-01 .. SC-04), all hardening or consistency. The tenant-isolation core (forced RLS, tenant-bound policies, definer ownership
and EXECUTE, composite tenant keys) held under every catalog query and every executed probe.

## Check table

Result counts are from the m12db dump unless stated. Queries are summarised in appendix A.

| # | Check | Query / scan | Result |
|---|---|---|---|
| 1a | SECURITY DEFINER inventory | Q1: `pg_proc` joined to `pg_namespace` (not `pg_%`, not `information_schema`) | 151 functions, 104 SECURITY DEFINER, 47 invoker, all in `audit`/`control`/`iam`/`runtime`. PASS |
| 1b | definer owner | Q1 `pg_get_userbyid(proowner)` | 104/104 owned by `framework_schema_owner` (NOLOGIN, not superuser, not BYPASSRLS per `pg_roles`). PASS |
| 1c | definer `proconfig` search_path | Q1 `proconfig` | 104/104 set `search_path`; 100 are `pg_catalog, pg_temp`; 4 are `pg_catalog` only (SC-01); 0 with `public`, `$user` or empty. PASS WITH FINDING (SC-01) |
| 1d | EXECUTE not to PUBLIC | `has_function_privilege('public', oid, 'EXECUTE')` over all 151 | 0 of 151. Role sets: 38 owner/migrator only, 37 app, 20 worker, 4 dispatcher, 4 app+worker, 1 app+worker+dispatcher (`control.event_delivery_policy`, constants only). PASS |
| 1e | dynamic SQL in functions | scan of all 151 `prosrc` for `EXECUTE`, `format(`, `%s`, `%L` | one function: `runtime.install_tenant_rls(regclass)` (invoker, executable by no runtime role): 14 `%I`, 0 `%s`, 0 `%L`. 0 of 151 bodies contain `%s`. PASS |
| 1f | EXECUTE matches the in-body role gate | for the 70 functions executable by app/worker/dispatcher, follow the call graph to `session_user = 'framework_*'` or the worker-principal gate | 65 gated for exactly the granted role; 5 intentionally ungated read helpers (`event_delivery_policy`, `worker_grants_safe`, `worker_principal`, `iam.authenticated_actor`, `iam.tenant_matches_scope`). 0 mismatches. PASS |
| 1g | tenant parameter bound to the transaction scope | for the 34 app/worker/dispatcher definers with a `uuid` tenant parameter, search the body and transitive callees for `iam.current_tenant_id()`, `tenant_matches_scope`, worker principal | 33 bound; `control.finish_event` is dispatcher-only (cross-tenant by design, fenced by generation and lease token). PASS (static reachability, not a proof of every branch) |
| 2a | RLS enabled and forced | `relrowsecurity`, `relforcerowsecurity`, `relkind in (r,p)`, four schemas | 72 tables; 71 enabled and forced; exception `iam.roles` (global 4-row reference table: no RLS, no policy, no privilege for app/worker/dispatcher; the Go probe `postgres.go:66` asserts app has no SELECT). All 72 owned by `framework_schema_owner`; 0 unlogged; 0 partitions. PASS (documented exception) |
| 2b | policies per command | `pg_policy` | 223 policies: 198 ALL, 22 SELECT, 3 INSERT; 0 policies for PUBLIC. For app, worker and dispatcher every table-level privilege has a permissive policy naming the role that covers the command (0 dead grants); of 24 column-level (table, role, privilege) combinations 23 are covered, the exception is `framework_audit_writer` INSERT on `audit.entries` (SC-04). PASS WITH NOTE |
| 2c | `USING (true)` / `WITH CHECK (true)` | `pg_policy` literal `true` | 83 policies: 70 owner only (`schema_owner_access`/`context_owner`), 8 migrator only, 4 owner+migrator, 1 runtime role: `control.job_routes route_dispatch` SELECT for `framework_dispatcher` (columns tenant_id, id, actor_id, release_hash; role model: "routing/job metadata across tenants"). PASS (documented) |
| 2d | tenant binding of non-operator policies | the 12 distinct expressions of the 67 `framework_app` policies; worker policies | every expression uses `iam.current_tenant_id()` with `iam.tenant_matches_scope()` or `iam.authenticated_actor()`; worker policies use `iam.worker_identity_scope()` / job id / worker decision. A scan for permissive app/worker policies without a binding token found only the dispatcher's `route_dispatch`. PASS |
| 2e | `current_setting` missing_ok and NULL | every `current_setting(` in `pg_policy` | all `app.*` calls pass `missing_ok=true`; `iam.current_tenant_id()` returns NULL unless the setting matches a UUID regex, so `tenant_id = NULL` is not true; `tenant_matches_scope(NULL)` is false. Only `current_setting('transaction_isolation')` (built-in) has no missing_ok. PASS |
| 2f | BYPASSRLS / superuser roles | `pg_roles` | only `postgres` (bootstrap superuser). 0 of the 7 `framework_*` roles has superuser, BYPASSRLS, CREATEDB, CREATEROLE or REPLICATION. PASS |
| 2g | views | `pg_class.reloptions` | `runtime.read_records`, `read_preview_records`, `read_shares`: `security_invoker=true`, owner schema_owner. PASS |
| 2h | role membership | `pg_auth_members` | one edge: `framework_schema_owner` <- `framework_migrator` (admin f, inherit t, set t). No runtime role is a member of any role. PASS |
| 2i | Go runtime-safety probe replayed | the `checkRuntime` SQL of `internal/platform/postgres/postgres.go:50-70` under `SET SESSION AUTHORIZATION framework_app / framework_worker / framework_dispatcher` | `privileged` = false for all three. PASS |
| 3a | PUBLIC grants | `has_table_privilege('public', ...)`, `relacl`, `has_sequence_privilege` | 0 relations with any PUBLIC privilege; 0 PUBLIC grantees in `relacl`; 0 grant options; 0 non-owner MAINTAIN (PostgreSQL 18). Schema `public`: PUBLIC has USAGE, not CREATE. Database: no PUBLIC CONNECT or TEMP. PASS |
| 3b | `framework_app` | table and column privileges over 75 relations | SELECT on 58 relations; table-level writes only on `control.job_routes` (I,S), `control.metadata_heads` (I,S), `runtime.record_links` (D,I,S,U), `runtime.record_sets` (I,S), `runtime.records` (D,S), `runtime.workflow_instances` (I,S); column-level INSERT/UPDATE on 15 tables (139 column privileges). 0 TRUNCATE, 0 REFERENCES, 0 TRIGGER, 0 write on `iam`, 0 UPDATE/DELETE on `audit`, no SELECT on `iam.roles`. PASS |
| 3c | `framework_worker` | same, and `control.worker_guard_granted()` | SELECT on 16 relations, column-level INSERT/UPDATE on 6 tables (44 column privileges), 0 table-level write. `SET SESSION AUTHORIZATION framework_worker; SELECT control.worker_grants_safe()` = true. PASS |
| 3d | `framework_dispatcher` | same | SELECT on `control.job_routes` only; USAGE on schema `control`; EXECUTE on `claim_event`, `finish_event`, `claim_workflow`, `event_audience`, `event_delivery_policy`. PASS |
| 3e | other roles | same | `framework_audit_writer`: INSERT on 10 columns of `audit.entries`, USAGE on `audit`/`iam`, no policy (SC-04). `framework_maintenance`: nothing. `framework_migrator`: owner privileges through membership (all 7 privileges on 75/75 relations) and owner of `public.goose_db_version` and its sequence. PASS WITH NOTE |
| 3f | default privileges | `pg_default_acl` | 0 rows (so every function needed an explicit REVOKE; 0 of 151 is PUBLIC-executable, see 1d). PASS |
| 3g | sequences | `pg_class relkind='S'`, `pg_sequence` | none in the four schemas (all keys are UUID or composite); only `public.goose_db_version_id_seq`, owner migrator, no other grantee. PASS |
| 3h | schema / database privileges | `has_schema_privilege`, `has_database_privilege`, `nspacl`, `datacl`, `pg_db_role_setting` | no runtime role has CREATE on any of the five schemas or on the database; none has TEMP (database ACL: `framework_app=c`, `framework_worker=c`, `framework_dispatcher=c`; owner `postgres=CTc`); `pg_db_role_setting` 0 rows. PASS |
| 4 | append-only tables | `pg_trigger` BEFORE UPDATE/DELETE/TRUNCATE calling `immutable_artifact`, `reject_mutation`, `reject_metadata_mutation`, `retain_tool_evidence`; `has_table_privilege`/`has_column_privilege` | privileges: no runtime role holds UPDATE/DELETE/TRUNCATE on any table where a trigger blocks it. Blocking triggers on 44 tables, all `tgenabled='O'`. TRUNCATE is not trigger-guarded on 10 evidence tables (SC-02). PASS WITH FINDING (SC-02) |
| 5a | tenant in keys | `pg_constraint` p/u, `pg_index` unique | every tenant table's primary key contains `tenant_id`, except `audit.entries (id)` (UUID id; tenant nullable for pre-auth denials), the backend-keyed context tables `control.worker_context`, `evolution_context`, `unique_write_context`, and `control.worker_principals` (`PRIMARY KEY (role_id)`, `UNIQUE (role_name)`: one DB role is one tenant). Of 89 unique indexes only those six lack `tenant_id`. PASS |
| 5b | composite FKs | 82 FKs parsed, positional map of `tenant_id` | 77 FKs carry `tenant_id` in child and parent lists, mapped to each other; the 5 exceptions reference global tables (`iam.tenants(id)`, `iam.users(id)`, `iam.roles(name)`). 7 `ON DELETE CASCADE`, all derived rows of `runtime.records`. Composite FKs with nullable parts (`command_receipts`, `outbox_intents`, `workflow_bindings`; `records.owner_team_id`) are tied by CHECKs (both NULL or both set), so MATCH SIMPLE cannot hide a half reference. PASS |
| 5c | CHECK vocabularies | 260 CHECKs; text columns named state/status/kind/outcome/mode/class/level/reason/role | 35 constrained, 5 unconstrained (SC-03). One NOT VALID check: `control.release_rejections.release_rejections_note_check` (00088:24-25); enforced for new rows, rows are immutable. PASS WITH FINDING (SC-03) |
| 6 | triggers / event triggers | `pg_trigger` (not internal), `pg_event_trigger` | 103 triggers on tables, all `tgenabled='O'`, all trigger functions owned by `framework_schema_owner` (30 SECURITY DEFINER, 73 invoker; all have `search_path` except `audit.reject_mutation`, which only raises). 0 event triggers, 0 rules, 0 inheritance, 0 publications, 0 FDW, 0 user operators/casts/aggregates/domains. PASS (no DDL guard: the operator is the only DDL role, by design) |
| 7 | extensions | `pg_extension` | `plpgsql` 1.0 in `pg_catalog` only; no `CREATE EXTENSION` in any migration. PASS |
| 8 | migration text | scans of the 70 files, appendix B | no `GRANT ... TO PUBLIC`, `GRANT ALL`, `ON ALL ... IN SCHEMA` grant, `WITH GRANT OPTION`, `ALTER DEFAULT PRIVILEGES`, `DISABLE`/`NO FORCE ROW LEVEL SECURITY`, `SET ROLE`; `BYPASSRLS`/`SUPERUSER` appear only in role-safety checks; the one `ON ALL TABLES` is `REVOKE ALL ... FROM PUBLIC` (00002:45). 235 function definitions in Up sections: 6 without `SET search_path` (five SECURITY DEFINER in 00022, all replaced by 00023, and `audit.reject_mutation`); 54 lack `pg_temp`, of which four remain in the final catalog (SC-01). Every SECURITY DEFINER definition is followed by a `REVOKE ... FROM PUBLIC` for it in the same file (directly or in a `DO` loop). `%s` inside `format()`: 31 uses, all with `regprocedure`/`regclass` values or constant schema-qualified names, at migration time. PASS WITH FINDING (SC-01) |
| 9 | data types | `pg_attribute`, four schemas, 755 columns | 0 `real`/`double precision`/`money`/serial; 0 `timestamp without time zone` (54 `timestamptz`; the one naive timestamp is goose's own `public.goose_db_version.tstamp`); one `numeric` (`runtime.numeric_projections.value_number`); "money" is a field-type vocabulary value, not a column type. 30 text ids/hashes have neither a format CHECK nor an FK (SC-03). PASS WITH FINDING (SC-03) |

## Findings

### SC-01 LOW - four SECURITY DEFINER functions keep `search_path=pg_catalog` without `pg_temp`

- Objects: `control.claim_event(uuid)`, `control.finish_event(uuid,uuid,bigint,uuid,boolean)` (00055:37,64), `control.event_delivery_policy()`,
  `control.worker_authority()` (00056:7,34). Migration 00026 (loop at lines 38-43) set every definer of the four schemas to
  `pg_catalog, pg_temp`; these four were redefined later without it. The 100 other definers have it.
- Why it matters: without an explicit `pg_temp`, PostgreSQL searches the session's temporary schema first for relations and types.
  `claim_event` (`JOIN pg_roles r`) and `worker_authority` (`JOIN pg_roles principal`) are the two of the four that name the catalog view `pg_roles` unqualified (`prosrc ~ '\mpg_roles\M'` returns exactly them),
  so a temporary table named `pg_roles` would shadow it for the dispatcher/worker session that calls them (P5 shows the mechanism).
- Not exploitable on the delivered stack: no runtime role has TEMP on the database (`REVOKE ALL ON DATABASE framework FROM PUBLIC` in
  `infra/compose/postgres-init.sh`; `datacl`: `framework_app=c, framework_worker=c, framework_dispatcher=c`). It becomes exploitable only
  if TEMP is granted to PUBLIC or a runtime role. `internal/events/readiness.go:29,34` pins `search_path=pg_catalog` for
  `event_delivery_policy`/`event_retry_delay`, so a fix must change migration and probe together.
- Repro: `SELECT n.nspname||'.'||p.proname, p.proconfig FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE p.prosecdef AND NOT (p.proconfig::text ~ 'pg_temp');` -> 4 rows.
  `SELECT r, has_database_privilege(r,'framework','TEMP') FROM unnest(ARRAY['public','framework_app','framework_worker','framework_dispatcher']) r;` -> all false.

### SC-02 LOW - ten append-only evidence tables have UPDATE/DELETE guards but no TRUNCATE guard

- Objects: `control.event_replays`, `control.workflow_attempts`, `runtime.event_envelopes`, `runtime.event_inbox`, `runtime.outbox_intents`,
  `runtime.workflow_bindings`, `runtime.workflow_history`, `runtime.workflow_outbox`, `runtime.workflow_receipts`, `runtime.workflow_signals`
  (triggers of 00018, 00020, 00021 cover UPDATE/DELETE only). Every table added or fixed later guards TRUNCATE
  (00029, 00030, 00035, 00080, 00081, 00083, 00089: "never deleted or truncated, even by an operator mistake").
  `control.event_quarantine` (insert-only in practice, `control.quarantine_event`) has no immutability trigger at all.
- Impact: runtime roles hold no TRUNCATE (checked), so only the owner/migrator (operator) can erase these tables, for example the inbox that
  `AGENTS.md` says is never erased to retry. An operator mistake or a stolen migrator credential removes the evidence silently, where the newer tables refuse.
- Repro (catalog): `SELECT c.oid::regclass FROM pg_class c WHERE c.relnamespace IN ('runtime'::regnamespace,'control'::regnamespace) AND c.relkind='r' AND EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid=c.oid AND NOT t.tgisinternal AND t.tgfoid='control.immutable_artifact'::regproc AND (t.tgtype & 8)=8) AND NOT EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid=c.oid AND NOT t.tgisinternal AND t.tgfoid='control.immutable_artifact'::regproc AND (t.tgtype & 32)=32);` -> the 10 tables. Executed probe P1 (private cluster): the operator's plain `TRUNCATE` succeeds on 7 of them (`event_replays`, `workflow_attempts`, `event_inbox`, `workflow_history`, `workflow_outbox`, `workflow_receipts`, `workflow_signals`) and on `event_quarantine`; the other three (`event_envelopes`, `outbox_intents`, `workflow_bindings`) are stopped only by FK RESTRICT and, with `CASCADE`, by the guarded tables they drag in (`event_data_sources`, `tool_invocations`).

### SC-03 LOW - closed vocabularies and id formats are left to Go on evidence columns

- Text state columns with no CHECK: `runtime.workflow_history.status` (the sibling `runtime.workflow_instances.status` has a 12-value CHECK; the history column is
  INSERT-able by `framework_app` and `framework_worker` through column grants), `control.event_attempts.outcome`, `runtime.tool_attempts.mode`
  (`tool_invocations.mode` has the 5-value CHECK), `runtime.tool_invocations.outcome_class` (`tool_attempts.outcome_class` has one), `control.event_replays.writer_role`.
- Text identifier/hash columns with neither CHECK nor FK (30). Written directly by a runtime role through a column grant:
  `runtime.workflow_history(state_id, step_key, result_hash)`, `runtime.workflow_receipts(activity_ref, compensation_ref)`, `runtime.command_receipts(entity_id)`,
  `runtime.outbox_intents(entity_id)`, `runtime.record_links(field_id)`, `runtime.unique_projections(field_id, value_key)`. The rest are set by definers only
  (`control.unique_write_context`, `control.evolution_context`, `control.workflow_jobs.ir_hash`, `control.release_bundles.app_id`, `runtime.numeric_indexes.*`,
  `control.event_deliveries.aggregate_key`, `runtime.event_sequences.aggregate_key`).
- Impact: the database does not refuse a malformed state or id/hash written with a runtime credential; Go is the only format check. Tenant columns and keys are constrained, so this is not a tenant-isolation issue.
- Repro: `SELECT conrelid::regclass, conname, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='runtime.workflow_history'::regclass AND contype='c';` -> only `event` has a length CHECK.

### SC-04 LOW - a dead audit-writer grant, and a worker drift guard that does not see function EXECUTE

- `framework_audit_writer` holds INSERT on 10 columns of `audit.entries` (00010:60, 00012:5) and USAGE on `audit` and `iam`, but no RLS policy names it
  (policies on `audit.entries`: `authenticated_audit` app, `operator_audit` and `operator_audit_read` migrator, `schema_owner_access` owner, `worker_audit` worker).
  With forced RLS the grant is dead (default deny). `docs/v3/architecture/15_DATABASE_ROLE_MODEL.md` says "restricted append capability if separated"; the schema never completed the separation. No escalation.
- `control.worker_grants_safe()` (00087:244-257) derives and refuses unlisted TABLE, COLUMN and schema-CREATE privileges of the worker; it does not look at function EXECUTE,
  database TEMP/CREATE or sequence privileges. A mistaken `GRANT EXECUTE ON FUNCTION control.approve_release(text,text) TO framework_worker` is not detected by it (probe P2).
  The app-only definers carry a `session_user <> 'framework_app'` gate in the body, so such a grant would still not be usable (defence in depth holds), and the Go readiness probes assert function ACLs one by one.

## Executed probes (private disposable cluster only; no shared stack was written)

Cluster: PostgreSQL 16, unix socket in the worktree's git-ignored `.local/zz_pg16`, database `framework`, built from the 70 migration files by
applying each Up section as `framework_migrator` after the role bootstrap of `infra/compose` (same grants, same ACLs; catalog equal to m12db, see Progress).
Roles were switched with `SET SESSION AUTHORIZATION`; every GRANT used as an experiment was revoked afterwards.

| Probe | Input | Observed | Supports |
|---|---|---|---|
| P1 | as `framework_migrator` (operator), `BEGIN; TRUNCATE <t>; ROLLBACK;` for each table | accepted on `control.event_replays`, `control.workflow_attempts`, `runtime.event_inbox`, `runtime.workflow_history`, `runtime.workflow_outbox`, `runtime.workflow_receipts`, `runtime.workflow_signals` and the unguarded `control.event_quarantine`; `runtime.event_envelopes`, `runtime.outbox_intents`, `runtime.workflow_bindings` stopped only by FK RESTRICT, and `TRUNCATE ... CASCADE` on them reaches guarded tables (`event_data_sources`, `tool_invocations`) and fails with `immutable artifact`; controls `runtime.command_receipts`, `control.ai_invocations` fail with `immutable artifact`, `audit.entries` with `audit evidence is append-only` | SC-02 |
| P1b | catalog: runtime roles' TRUNCATE privilege on the ten unguarded tables; `framework_app` runs `TRUNCATE runtime.workflow_history` | 0 grants; `permission denied for table workflow_history` | SC-02 (operator only) |
| P2 | as `framework_worker`, `SELECT control.worker_grants_safe()` after each experiment | baseline `t`; after `GRANT SELECT ON control.release_candidates` `f` (guard works for tables); after `GRANT EXECUTE ON FUNCTION control.approve_release(text,text)` still `t` with `has_function_privilege` = `t`; after `GRANT TEMPORARY ON DATABASE framework` still `t`; restored `t` | SC-04 |
| P2b | worker session calls `control.approve_release('x','y')` with that EXECUTE grant | `ERROR: tenant release approver required` (in-body gate refuses) | SC-04 (defence in depth holds) |
| P3 | `framework_audit_writer` inserts a denial entry into `audit.entries`; control: `framework_app` inserts the same row shape | writer: `new row violates row-level security policy for table "entries"`; app: `INSERT 0 1` | SC-04 |
| P4 | tenants A and B, users u1 (member of A) and u2 (member of B), a team per tenant; `framework_app` reads `iam.teams`, `iam.tenants`, `iam.memberships`, `iam.users` | no `app.*` settings: 0/0/0/0 rows; u1 + tenant A: `teamA` only, 1 tenant, 1 membership; u1 claims tenant B: 0; `app.tenant_id` = `not-a-uuid`, empty, or upper-case UUID: 0; valid tenant with unknown actor: 0; u2 (member of B) claims tenant A: 0; app `INSERT`/`UPDATE`/`DELETE` on `iam.teams`/`iam.memberships`/`iam.tenants`: `permission denied` | checks 2d, 2e (held) |
| P5 | generic `SECURITY DEFINER` function over `pg_roles` with `search_path=pg_catalog` and a twin with `pg_catalog,pg_temp`; dispatcher role granted TEMP inside the private cluster creates `TEMP TABLE pg_roles` | before: both `false`; after the temp table: `pg_catalog` version returns `true` (shadowed), `pg_catalog,pg_temp` version returns `false`; after `REVOKE TEMPORARY`, `CREATE TEMP TABLE` fails with `permission denied for schema pg_temp_3` | SC-01 (mechanism; the real functions were not driven) |

## NOT RUN

- Behavioural proof of any finding on the shared m12db stack (read-only rule): the executed probes ran on the private cluster built from the same migration files (PostgreSQL 16, one patched function body).
- A populated-data RLS replay for all 72 tables (covered by the integration suite, not re-run here).
- A line-by-line review of every definer body for authorization logic (check 1g is static reachability of a tenant binding).
- `pg_parameter_acl` (PostgreSQL 15+ `GRANT SET ON PARAMETER`, relevant to `session_replication_role`, which would skip all `ENABLE`-mode triggers): queried on the private cluster only (0 rows); not queried on m12db.
- The finding repro queries (`SC-01` .. `SC-04`) were executed on the private cluster and returned exactly the rows stated (4 definers, 10 tables, the policy list). The same script was submitted to `dbrunm12db` after the main dump and was still waiting for the lock (another agent's integration run) when this report was committed; the check-table figures come from the main m12db dump.

## Appendix A - queries (summary)

```sql
-- Q1 functions
SELECT p.oid, n.nspname, p.proname, pg_get_function_identity_arguments(p.oid), pg_get_userbyid(p.proowner), p.prosecdef,
       p.proconfig, p.proacl, has_function_privilege('public', p.oid, 'EXECUTE'),
       has_function_privilege('framework_app', p.oid,'EXECUTE'), has_function_privilege('framework_worker', p.oid,'EXECUTE'),
       has_function_privilege('framework_dispatcher', p.oid,'EXECUTE'), p.prosrc
FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema';
-- Q2 relations, RLS, ACL
SELECT n.nspname, c.relname, c.relkind, c.relrowsecurity, c.relforcerowsecurity, pg_get_userbyid(c.relowner), c.relacl, c.reloptions
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind IN ('r','p','v','m','f','S');
-- Q3 effective privileges: has_table_privilege / has_sequence_privilege / has_column_privilege for
--    public, framework_app, framework_worker, framework_dispatcher, framework_migrator, framework_schema_owner,
--    framework_audit_writer, framework_maintenance
-- Q4 pg_policy; pg_get_constraintdef(oid) FROM pg_constraint; pg_get_triggerdef(oid) FROM pg_trigger WHERE NOT tgisinternal;
--    pg_get_indexdef; pg_attribute with format_type
-- Q5 pg_extension, pg_event_trigger, pg_default_acl, nspacl, datacl, pg_auth_members, pg_db_role_setting, pg_language,
--    pg_rewrite, pg_inherits, pg_publication, pg_foreign_data_wrapper, pg_operator, pg_cast, pg_aggregate
-- Q6 SET SESSION AUTHORIZATION framework_worker; SELECT control.worker_grants_safe(); RESET SESSION AUTHORIZATION;
--    and the checkRuntime SELECT of internal/platform/postgres/postgres.go under framework_app / _worker / _dispatcher
```

## Appendix B - migration text scans

```
grep -nEi "GRANT[^;]*\bTO\b[^;]*\bPUBLIC\b|ON ALL (TABLES|FUNCTIONS|SEQUENCES|ROUTINES) IN SCHEMA|ALTER DEFAULT PRIVILEGES|GRANT ALL|ALL PRIVILEGES" db/migrations/0*.sql -> 1 line (00002:45, a REVOKE)
grep -nEi "DISABLE ROW LEVEL|NO FORCE ROW LEVEL|BYPASSRLS|SUPERUSER|CREATEROLE|CREATEDB" db/migrations/0*.sql -> role-safety checks only (00004, 00022, 00023)
grep -nEi "CREATE ROLE|ALTER ROLE|CREATE EXTENSION|EVENT TRIGGER|ALTER SYSTEM|SET ROLE|WITH GRANT OPTION" db/migrations/0*.sql -> none (roles live in infra/compose)
function-definition scan: regex over CREATE [OR REPLACE] FUNCTION ... AS $ in the Up sections -> 235 definitions
```
