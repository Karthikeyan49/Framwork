# M11 independent adversarial review (Sandbox V2)

## Progress

- [x] Read briefs, AGENTS.md, ADR036/ADR037, migration 00046 and its predecessors
      (00028, 00035, 00036, 00037, 00045), data/access/policy/tools/workflow code.
- [x] Probe run 1 (MAIN stack via `/home/user/bin/dbrun`): frozen mutation paths,
      freeze/tool races, evidence recomputation, SQL-port authority. Findings R01–R06.
- [x] Probe run 2: creation guarantees, ceiling derivation/DB check, EffectiveMode
      exhaustive table, REROUTE credential (R07), 10k budget, audit completeness.
- [x] Existing M11 tests re-run; contract-honesty table for ADR037.

Status: COMPLETE — PASS WITH FINDINGS (0 HIGH, 3 MEDIUM, 4 LOW).

## Scope and commit

Reviewed commit `fb52ad3` ("docs(m12): record full integration result and decider
wording"), branch `review/m11-final`. Scope per brief 03: migration 00046,
`internal/runtime/data/{dataset.go,access.go,store.go}`,
`internal/platform/access/{sandboxes.go,access.go}`, `internal/policy`,
`internal/tools/mode.go`, `tests/integration/m11_sandbox_test.go` and the M11 tests
inside `tests/integration/m10_tools_test.go`. Not re-reported (known): ADR037
`sandbox.manage` delegation wording; gateway credential/host separation findings.

## Method

Static reading of every SQL guard and port touched by 00046 against the earlier
dataset/evolution/tool-ledger ports, then executed probes in a temporary
`tests/integration/zz_review_m11_test.go` (deleted before commit; repro snippets
kept below). DB-layer probes run as `framework_app` (asserted via
`session_user`), with the app GUCs a compromised API process could set, inside
transactions that are always rolled back.

## Commands

| # | Command | Result | Duration |
|---|---------|--------|----------|
| 1 | `dbrun node scripts/with-env.mjs sh -c 'psql "$MIGRATION_DATABASE_URL" -f priv.sql'` (catalog read: `framework_app` table/column privileges) | PASS (read-only) | <5 s |
| 2 | same, `trg.sql` (non-internal triggers on sandbox-relevant tables) | PASS (read-only) | <5 s |
| 3 | `./scripts/go.sh vet -tags=integration ./tests/integration/` | PASS | ~60 s |
| 4 | `dbrun nice -n 10 node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -count=1 -timeout 12m -run 'TestZZReviewM11' -v ./tests/integration` | FAIL by design (2 probes assert R01/R02; the third logs R03–R06) | 23 s |
| 5 | `nice -n 10 ./scripts/go.sh test -count=1 -run 'TestZZReview' -v ./internal/tools/` | FAIL by design (repro of R07; table shows only the catalog-rejected WRITE/READ_ONLY case) | <1 s |
| 6 | `dbrun ... go.sh test -tags=integration -count=1 -run 'TestZZReviewM11CreationCeilingAndEnvironment' -v ./tests/integration` (twice, second with two added probes) | PASS (logs) | 8 s, 6 s |
| 7 | `dbrun ... go.sh test -tags=integration -count=1 -run 'TestZZReviewM11EvidenceBudget\|^TestM11' -v ./tests/integration` | PASS: `TestM11SandboxCeilingGovernsEffectiveToolMode` (17.5 s), `TestM11ForgedWorkerModeAndFrozenSandboxAreRefusedByTheDatabase` (2.3 s), `TestM11SandboxIsolationPolicyFreezeAndEvidence` (1.6 s), budget probe (4.1 s) | 25 s |
| 8 | `dbrun ... go.sh test -tags=integration -count=1 -run 'TestZZReviewM11Audit' -v ./tests/integration` | PASS (logs) | <1 s |
| 9 | three read-only `psql` audit summaries on `audit.entries` | PASS (read-only; fixture cleanup removes test tenants' entries, so audit was re-checked inside probe 8) | <5 s each |

The MAIN stack is at migration 49 (M13 approval tables). 00049 replaces only
`control.worker_grants_safe` among the functions in scope; every sandbox
function probed is the 00046 version.

## Findings

### M11-R01 — MEDIUM — the tool ledger of a frozen sandbox keeps changing; evidence diverges

`control.tool_finish`, `control.tool_abandon` and `control.tool_reconcile`
(db/migrations/00045_m10_tool_ledger.sql:192-249) have no sandbox-state check;
00046 replaced only `tool_begin` (00046:125-176). `freeze_sandbox` hashes
`state` and `attempts` of every invocation (00046:98). A freeze that commits while
an attempt is between transactions (a) and (c) of ADR036 #5 therefore produces
evidence that no longer matches the ledger moments later, and the workflow keeps
running on the "permanently read-only" sandbox.

Executed (probe `TestZZReviewM11FreezeDuringToolSend`): sandbox ceiling
TEST_ACCOUNT, `read` tool; an `m10Exec.setAfter` hook calls
`POST .../sandboxes/{id}/freeze` after the provider responded and before
`tool_finish`.

```
freeze code=200 final status=SUCCEEDED state=state.complete
evidence ledger count=1 hash=0db4cdd2…3a3
live ledger count=1 hash=76a39d52…884 state=SUCCEEDED attempts=1
recomputed-as-SENDING hash=0db4cdd2…3a3 equal-to-evidence=true
attempts finished after freeze=1 history rows after freeze=2
```

The evidence was correct at freeze time (independent Go recomputation with the
row forced to `SENDING`/1 reproduces it exactly) and is stale afterwards: the
outcome, the receipt and two workflow history entries were written after
`frozen_at`. The same holds, by reading, for `tool_abandon` (SENDING→UNKNOWN)
and an owner/admin `reconcile` signal (`tool_reconcile`, UNKNOWN→APPLIED /
NOT_APPLIED). Impact: ADR037 Consequences "A frozen sandbox is reproducible
evidence but also permanently read-only" is false; M12 binds the evidence text
(00048:146-151), which then describes a ledger state that no longer exists.
Recommended fix: refuse freeze while any invocation of the dataset is
`SENDING`/`UNKNOWN` (or any of its workflows is non-terminal), and/or make
`tool_finish`/`tool_abandon`/`tool_reconcile` refuse a frozen sandbox.

### M11-R02 — MEDIUM — `tool_begin` races `freeze_sandbox`: an attempt (and its send) lands after the freeze

`tool_begin` reads `runtime.sandboxes` without a lock (00046:140) while
`freeze_sandbox` takes `FOR UPDATE` on the sandbox and dataset rows only
(00046:90-92); neither locks the other's rows, and the ledger aggregate (00046:98)
cannot see an uncommitted `tool_invocations` insert.

Executed (probe `TestZZReviewM11ToolBeginRacesFreeze`): a worker lease
transaction calls `control.tool_begin(... 'TEST_ACCOUNT','SEND',false)` and
stays open; meanwhile `POST .../freeze` is issued; then the worker commits.

```
tool_begin tx err=<nil> freeze code=200 freezeReturnedBeforeCommit=true
evidence ledger count=0 ; live ledger count=1 live hash equal=false
```

The freeze committed first (it did not block), the attempt committed after it,
and in the real worker the HTTP send follows that commit: a tool send on a frozen
sandbox that the evidence does not contain. ADR037 #3/#5 ("rejects ... any
attempt on a frozen sandbox", "tool sends are refused") hold only without
concurrency. Recommended fix: `SELECT ... FROM runtime.sandboxes ... FOR SHARE`
in `tool_begin` (serializes with the freeze's `FOR UPDATE` and re-reads
`FROZEN`), or have `freeze_sandbox` lock the dataset's workflow instances.

### M11-R03 — MEDIUM — workflow signals and steps still mutate a frozen sandbox's workflows

Executed (probe `TestZZReviewM11FrozenMutationPaths`): after freeze,
`POST .../instances/{id}/signals {"operation":"cancel"}` on the sandbox workflow
returned `200` and moved the instance `READY v1 → COMPENSATING v2`. Workflow
start is refused (`409 workflow_conflict`, `check_workflow_data_pin`
00037:33), but signals (cancel/approve/reject/reconcile) and worker steps
(compensation, receipts, see R01) are not gated on the sandbox state. With R01
(reconcile changes the hashed ledger) this is a second mutation path on a
"permanently read-only" sandbox. Recommended fix: refuse signals/step advances
whose `workflow_bindings.dataset_id` is a FROZEN sandbox, or require quiescence
before freeze (R01).

### M11-R04 — LOW — the SQL layer still lets the app credential rewrite a frozen sandbox's dataset row, binding and link projection

Executed as `framework_app` with app GUCs, each in a rolled-back transaction with
`SET CONSTRAINTS ALL IMMEDIATE`:

```
DB  record_links insert          -> ACCEPTED rows=1
DB  record_links delete          -> ACCEPTED rows=0
DB  datasets manifest/verified   -> ACCEPTED rows=1   (manifest_hash='aaa…', verified_rows+7)
DB  rebind binding+pointer       -> ACCEPTED rows=1   (new dataset_bindings v2 + active_binding 1→2)
DB  datasets reopen              -> refused: frozen sandbox datasets stay frozen (55000)
DB  records insert               -> refused: dataset binding is stale or sealed (55000)
```

`runtime.record_links` has no fence trigger and `framework_app` holds
INSERT/UPDATE/DELETE on it; `check_dataset_update` (00035:97-112) and
`guard_sandbox_dataset_update` (00046:59-64) only stop leaving FROZEN, so the
app's column grants (`active_binding, epoch, manifest_hash, verified_*`,
00028:25) and `INSERT` on `dataset_bindings` still apply to a frozen sandbox. The
API refuses all of these (`409` for rebind/verify, see Verified), so this needs a
compromised API credential; but ADR037 Consequences claims isolation is enforced
"by PostgreSQL constraints and ports, not caller flags" and #5 says "evolution is
impossible"; a frozen sandbox can be re-pointed to another binding at the SQL
layer. Recommended fix: in `guard_sandbox_dataset_update` reject every change to a
dataset row whose sandbox is FROZEN, refuse binding inserts for it, and fence
`record_links` like `records`.

### M11-R05 — LOW — `register_sandbox`/`freeze_sandbox` admit any non-viewer and accept forged witness hashes

`register_sandbox` and `freeze_sandbox` require only `m.role<>'viewer'`
(00046:73-74, 86-87), unlike `evolution_manager`/`check_numeric_manager`
(`role IN('owner','admin')`, 00036:21, 00037:43), while ADR037 #5 reads
"`runtime.freeze_sandbox` (owner/admin, `sandbox.manage`)". The catalog and
policy hashes are SQL arguments checked only for shape (00046:89).

Executed: a freshly provisioned bootstrap `member` (no `sandbox.manage`), as
`framework_app` with its GUCs, ran
`SELECT runtime.freeze_sandbox(t,app,id,repeat('a',64),repeat('b',64))` on an
OPEN sandbox:
`member freeze_sandbox -> ACCEPTED frozen_by_member=true forged hashes in body=true`.
The API layer holds (`sandbox.manage` is owner/admin, custom roles cannot hold
it; the freeze body is `struct{}` with `DisallowUnknownFields`, so a request can
never supply a hash). What a compromised app credential can forge: the
`tool_catalog_hash` and `policy_hash` fields and the freezer identity among any
non-viewer member; it cannot forge the record/ledger manifests, binding, IR,
schema, anchor, ceiling or data mode (computed inside the function). ADR037's
"computed by the server, not request fields" is therefore true of the API, not
of the SQL port, and M12 binds `frozen_by` and the body as given
(00048:146-151). Recommended fix: owner/admin check in both ports; move the
catalog hash into a server-owned table (or document the residual).

### M11-R06 — LOW — evidence hash ordering depends on the database collation

`freeze_sandbox` orders the manifests with `ORDER BY r.entity_id,r.id` and
`ORDER BY v.instance_id,v.step_key` (00046:96-98) under the database default
collation, which on the MAIN stack is `en_US.utf8` (probe log
`database collation=en_US.utf8`). Linguistic collations do not order by bytes
and can change with a glibc/ICU upgrade, so "reproducible evidence" recomputed
elsewhere (bytewise, as M12 or an auditor would) or after an OS upgrade can
differ for multi-entity/multi-step sandboxes. The single-row recomputations in
this review matched only because ordering was trivial. Recommended fix:
`COLLATE "C"` in both `ORDER BY` clauses (a v2 evidence schema, since v1 bodies
are retained).

### M11-R07 — LOW — a REROUTE (or READ_ONLY) ceiling does not remove a TEST_ACCOUNT tool's credential

`EffectiveMode` narrows a declared TEST_ACCOUNT tool to REROUTE under a REROUTE
ceiling (internal/tools/mode.go:53-55), but `effectiveEndpoint` returns the same
sandbox endpoint including its `secret_ref` for REROUTE and READ_ONLY
(internal/tools/executor.go:223-246) and `execute` injects any non-empty
`secret_ref` (executor.go:207-214). The narrowed request is byte-identical to the
TEST_ACCOUNT request.

Executed (unit probe `TestZZReviewRerouteCeilingStillSendsTestAccountSecret`,
`./scripts/go.sh test -run TestZZReview ./internal/tools/`, PASS-as-repro):
`effective=REROUTE outcome=SUCCESS/ok secrets=[pay_sandbox] auth-present=true`
(the sandbox server received `Bearer <pay_sandbox value>`). The integration test
`TestM11SandboxCeilingGovernsEffectiveToolMode` asserts only that `prod_token` is
not resolved under REROUTE, never that `sandbox_token` is not. Impact: the
ceiling order BLOCK < … < REROUTE < TEST_ACCOUNT (ADR037 #3) promises that a
ceiling below TEST_ACCOUNT reduces capability; ADR036 #9 says only TEST_ACCOUNT
uses the sandbox-only secret. Only sandbox credentials are involved (production
separation holds), hence LOW. This is distinct from the already-fixed
production/sandbox host and credential separation findings; if the coordinator
counts it as part of them, drop it. Recommended fix: pass the effective mode to
endpoint selection and strip `secret_ref` for REROUTE/READ_ONLY (or define and
document REROUTE as "sandbox endpoint with its own credential" and drop the
claim that narrowing TEST_ACCOUNT→REROUTE reduces anything).

## Verified (attacks that held)

Frozen sandbox, through the API (probe `TestZZReviewM11FrozenMutationPaths`):
workflow start `409`; record share `409`; owner transfer `409`; record update
`409`; record delete `409`; rebind `409`; adoption verify `409`; numeric index
register `409`. Through SQL as `framework_app`: dataset reopen refused
(`frozen sandbox datasets stay frozen`); record insert refused (fence);
`INSERT INTO runtime.sandbox_evidence` and `UPDATE runtime.sandboxes` refused
(`permission denied`); second `freeze_sandbox` refused (`open sandbox required`);
`start_evolution` on the frozen dataset refused (`base binding changed`, state
check 00036:36). Evidence row and hash unchanged after all probes.

Evidence correctness: record manifest recomputed independently in Go from
`runtime.records` (`entity:id:version:sha256(custom_data::text)`, joined by
`\n`) equals the stored `records.hash` (count 1); ledger manifest recomputed as
of freeze equals the stored `tool_ledger.hash` (R01); stored `body_hash` equals
`sha256(body)`.

Creation (probe `TestZZReviewM11CreationCeilingAndEnvironment`): API create with
a forged `ir_hash` → `400`; with a wrong revision → `404`; as a bootstrap
`member` → `403`; freeze as `member` → `403`; freeze body carrying
`tool_catalog_hash` → `400` (unknown field); tenant-B owner reading tenant-A's
sandbox and evidence → `404`/`404`; evidence of an OPEN sandbox → `404`. SQL as
`framework_app` (rolled back): sandbox dataset over the populated legacy
namespace → `a sandbox dataset must be created empty and open`; sandbox
dataset with a binding but without `register_sandbox` → deferred
`a sandbox dataset requires its registered sandbox`; sandbox at an empty preview
dataset's anchor → unique violation on `(tenant,app,anchor)`;
`register_sandbox` on a preview dataset → `open sandbox dataset required`;
twice → PK violation; foreign tenant argument → `42501` (register and freeze);
viewer actor → `42501`; direct `INSERT INTO runtime.sandboxes` → permission
denied; `UPDATE ... SET environment='sandbox'` → permission denied. A fresh
caller-chosen empty anchor plus `register_sandbox` is accepted — the EMPTY-only
guarantee holds; the anchor derivation is not enforced (not needed for
isolation because anchors are unique).

Ceiling derivation (same probe): `control.worker_environment()` from the worker
role without a bound lease → 0 rows; with forged `app.job_id`/`app.tenant_id`
GUCs → 0 rows; under the exact lease it returns `sandbox/READ_ONLY`.
`tool_begin` under the READ_ONLY ceiling refuses TEST_ACCOUNT and REROUTE
(`tool mode exceeds the sandbox ceiling`, 42501) and accepts READ_ONLY,
SIMULATE, BLOCK (rolled back). The existing
`TestM11ForgedWorkerModeAndFrozenSandboxAreRefusedByTheDatabase` covers the BLOCK
ceiling. Note: the DB cannot know side effects, so SIMULATE for a fixture-less
tool or READ_ONLY for a WRITE tool are accepted by `tool_begin`; the gateway then
blocks them (executor.go:181, 238-241) — no I/O.

`EffectiveMode` exhaustive table (unit probe `TestZZReviewEffectiveModeExhaustive`,
3 side effects × 7 declared × 8 ceilings): never above the ceiling, never above
the declared mode, never SIMULATE without a declared fixture, never
TEST_ACCOUNT unless declared, unknown/LIVE → BLOCK. The only "violations" are a
hand-built WRITE definition declaring READ_ONLY (returns READ_ONLY), which the
catalog rejects (catalog.go:300) and the gateway refuses (executor.go:238-241).

Event paths (static): M8 consumers only audit/receipt (internal/events/delivery.go
:117-200, "Network/tool effects are prohibited here"); record-triggered
workflows start inside the originating command transaction with that command's
own dataset scope (internal/platform/access/workflow_triggers.go:29), so a
sandbox record event starts a workflow pinned to the same sandbox
(`check_workflow_data_pin` maps the anchor to exactly one dataset, 00037:26-36)
and a preview event cannot start inside a sandbox. Envelope `mode` stays
`preview` as documented.

Evidence budget (probe `TestZZReviewM11EvidenceBudget`, as `framework_app`,
rolled back): 10 000 records freeze in 33 ms; 10 001 records →
`sandbox evidence budget exceeded` (22023, API `400 invalid_sandbox`) — fail
closed, no truncation. The aggregate runs before the count check, so the cost of
a refused freeze grows with the sandbox size (owner/admin-triggered; no separate
finding), and a sandbox above 10 000 records can never be frozen.

Audit (probe `TestZZReviewM11Audit`): create → `sandbox.manage` allowed with
dataset id, IR hash and ceiling; invalid ceiling → `sandbox.manage.rejected`;
viewer create/freeze → `sandbox.manage` allowed=false (policy denial); read →
`sandbox.read`; freeze → `sandbox.frozen` with the evidence hash as
`artifact_hash`; double freeze → `sandbox.manage.rejected` with dataset id;
evidence read → `sandbox.read`. Minor gaps (no finding): the evidence read entry
does not carry the evidence hash, and policy-denied freezes do not carry the
dataset id (set only after the decision, sandboxes.go:98). Error mapping
(sandboxes.go:32-58) returns fixed codes/messages; raw PostgreSQL text never
reaches the response in any probe.

## ADR037 contract honesty

| Claim | Verdict | Evidence |
|---|---|---|
| #1 sandbox = EMPTY dataset, never adopting preview/legacy, registered in the same transaction, identity immutable, rows not deletable | VERIFIED | creation probes (populated legacy namespace, preview anchor, deferred registration, direct inserts, environment flip); existing test (operator UPDATE/DELETE of `runtime.sandboxes`) |
| #1 same runtime/DataScope/RLS/receipts/audit/outbox, no second path | VERIFIED (static + API) | `CreateSandbox` → `createDataset`; sandbox data uses the dataset binding routes |
| #2 only SYNTHETIC_SEPARATE; seeding via governed commands; no bulk loader | VERIFIED at API/CHECK | 00046:11; no loader route. At SQL the app credential can insert records (as for preview) |
| #3 ceiling order, `EffectiveMode`, derivation from the leased job | VERIFIED | exhaustive table; `worker_environment` spoof probes; DB rank check matrix under READ_ONLY |
| #3 `tool_begin` rejects any attempt on a frozen sandbox | FALSIFIED under concurrency | R02 |
| #4 the ceiling can only reduce; production reference never resolved | PARTLY FALSIFIED | R07 (narrowing keeps the test-account credential); production separation held in `TestM11SandboxCeilingGovernsEffectiveToolMode` |
| #5 atomic immutable evidence bound to the listed facts | VERIFIED | body fields present; independent recomputation of record and ledger manifests; UPDATE/DELETE/TRUNCATE refused (existing test + permission probes) |
| #5 record writes, workflow starts refused; FROZEN cannot be left | VERIFIED | API 409s; SQL fence; reopen refused |
| #5 tool sends refused | FALSIFIED under concurrency | R02 |
| #5 evolution impossible | VERIFIED at API/`start_evolution`; SQL rebind possible with app credential | R04 |
| #5 catalog/policy hashes computed by the server, not request fields | VERIFIED at API, FALSIFIED at the SQL port | R05 |
| #5 `freeze_sandbox` owner/admin | FALSIFIED at the SQL port (any non-viewer) | R05 |
| #5 evidence contains no PASS/risk/approval claim | VERIFIED | body keys (00046:101-105) |
| #6 `sandbox.Context`/bundle 1.0.0 bytes unchanged | NOT TESTED | — |
| #7 routes/actions; `sandbox.manage` owner/admin only; routes only with preview | VERIFIED | member/viewer/cross-tenant probes; policy.go:76-89, definition.go:127-128; access.go:64-75 |
| Consequence: isolation enforced by PostgreSQL constraints/ports | VERIFIED for creation/isolation; PARTLY FALSIFIED for frozen state | R04 |
| Consequence: frozen sandbox is reproducible evidence and permanently read-only | FALSIFIED | R01, R03, R06 |
| Consequence: M8 envelopes keep `preview`; no event subscriber can send | VERIFIED (static) | events/delivery.go:117; workflow_triggers.go:29 |
| Verification: tests cover forged worker mode, frozen refusal, isolation | VERIFIED (tests exist and PASS) | command 7 |
| Recovery: revoke `EXECUTE` on the sandbox ports | NOT TESTED | — |

## Repro snippets (probe files deleted)

R01 — inside `tests/integration` with the M10 fixture:

```go
m := m10Setup(t, m10Options{allowInternal: true})
box, x := m10Sandbox(t, m, "TEST_ACCOUNT", "read")
m.exec.setAfter(func(tools.Request, tools.Result) { // after send, before tool_finish
	once.Do(func() { m5Call(m.handler, root+"/sandboxes/"+box.ID+"/freeze", m.token, struct{}{}) })
})
// drive with m.worker.Once until SUCCEEDED, then recompute the ledger manifest:
// instance:step:tool_hash:mode:state:attempts:request_hash joined by "\n", sha256
// -> differs from evidence tool_ledger.hash; equals it with state forced to SENDING/1.
```

R02:

```go
lease, _, _ := m.worker.Claim(ctx) // instance at state.tool
jobs.WithJob(ctx, m.worker.DB, lease.Ticket, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
	q.ExecContext(ctx, `SELECT control.tool_begin('state.tool','tool.contact_lookup','1.0.0',$1,repeat('b',64),'TEST_ACCOUNT','SEND',false)`, toolHash)
	go func() { done <- m5Call(m.handler, root+"/sandboxes/"+box.ID+"/freeze", m.token, struct{}{}).Code }()
	<-time.After(1200 * time.Millisecond) // freeze returns 200 while this tx is open
	return nil                            // commit: ledger row absent from the evidence
})
```

R04/R05 — as `framework_app` (`session_user` asserted) after
`set_config('app.auth_issuer'|'app.auth_subject'|'app.actor_id'|'app.tenant_id')`,
then `ROLLBACK`:

```sql
INSERT INTO runtime.dataset_bindings(tenant_id,app_id,dataset_id,version,metadata_revision,ir_hash,ir_body,schema_hash,generation)
 SELECT tenant_id,app_id,dataset_id,version+1,metadata_revision,ir_hash,ir_body,schema_hash,generation
 FROM runtime.dataset_bindings WHERE tenant_id=$1 AND app_id=$2 AND dataset_id=$frozen AND version=1;
UPDATE runtime.datasets SET active_binding=active_binding+1,epoch=epoch+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$frozen;
SET CONSTRAINTS ALL IMMEDIATE;                                   -- accepted
INSERT INTO runtime.record_links(...) VALUES(... frozen anchor ...);  -- accepted
-- as a plain 'member' actor, on an OPEN sandbox:
SELECT runtime.freeze_sandbox($1,$2,$open,repeat('a',64),repeat('b',64)); -- accepted
```

R07 — `internal/tools` package test using the existing harness:

```go
def := lookupDefinition("pay.example.com", prod.port, "pay-sandbox.example.com", sb.port) // TEST_ACCOUNT, pay_sandbox
mode := EffectiveMode(def, ModeReroute)                                                   // REROUTE
h.run(def, mode, KindSend, testKey, map[string]any{"amount": "1.00"})
// h.secrets.refs() == [pay_sandbox]; sandbox server saw "Authorization: Bearer <pay_sandbox>"
```

## Not covered

- Isolation of sandbox records from preview queries beyond the existing
  `TestM11SandboxIsolationPolicyFreezeAndEvidence` (re-run, PASS); no new
  cross-dataset DataScope probe.
- Custom (non-bootstrap) role holders at the API (only bootstrap member/viewer
  and the policy code were checked); route-pattern fuzzing beyond the
  `/sandboxes/evidence` and `%2F` cases reasoned statically.
- Concurrent duplicate creates with the same workspace (static: UNIQUE
  `(tenant,app,workspace)` → 23505 → 409); concurrent double freeze (static:
  `FOR UPDATE` serializes, the second sees FROZEN).
- Freeze racing a record write (static: the writer's `FOR SHARE OF d` in
  `lockBinding`/fence vs the freeze's `FOR UPDATE` serialize; not executed).
- `tool_abandon`/`tool_reconcile` after freeze (covered by reading only, R01).
- Empty legacy namespace claimed by an app-credential sandbox (would seal the
  legacy route for that revision; app-credential only, not executed).
- ADR037 #6 bundle bytes, recovery by `EXECUTE` revocation, UI.

M11 review STATUS: PASS WITH FINDINGS
