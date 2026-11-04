# M9R R5b independent adversarial review

Final reviewed source: `a5bba7b3db66af4773bb74e188c4b33f1104e0a6`.
Initial independently probed source: `d093e804159ac1929f0579aeb15c8e60eae71719`.
Independent reviewer: `/root/r5b_review`. Final review date: **15 October 2026 (IST)**.
Original logs retain their UTC chronology.

## Final bounded verdict

**R5b PASS for the reviewed exact-lease live worker SQL capability boundary.**
The original HIGH typed UUID finding and MEDIUM repeated-policy-evaluation
finding are corrected by additive migration40, with applied39 preserved. Final
source review and reviewer execution of seven existing regression roots PASS
at a5bba. Full R5/M9R remain PARTIAL. This verdict does not authorize production,
external effects, M10 or the remaining R5 product work.

Final verification is narrower than the original independently authored probe
phase. The root reported a platform content interruption and then explicitly
restricted final verification to ordinary existing repository regressions and
source review. No new independently authored final probes or manual database
experiments were created or executed. A new independent populated39-to40
migration rehearsal and renewed custom RC/RR/maximum-policy Go execution probes
are **NOT RUN** at a5bba. The original d093e80 probes, findings, failures and
38-to39 rehearsal below remain historical evidence; they are not relabeled as
final-source executions.

The main raw role/head/action/field/owner/team/share/condition revocation boundary
passed the independent populated controls below. Exact lease identity and denied
audit access remain available without business permission. Whole-plan activity
permission and suspension of CONDITION over retired generations are documented,
conservative restrictions, not unexpected findings.

## Final correction evidence at a5bba

The reviewer executed exactly this ordinary repository command, without an
overlay, after the root released exclusive CPU/database ownership:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RSQL|^TestM9RWorkerCapability|^TestM9REventConsumer|^TestM9RExactCapability' -v ./tests/integration > /tmp/r5b-review/final-ordinary-regressions.txt 2>&1
```

**PASS, 33.457s; seven existing test roots.** These tests are implementer-authored
regressions executed and source-reviewed by this reviewer, not new independently
authored final attack fixtures. Their existing populated fixtures use
`m8Database(t)` and verified owned local disposable targets.

| Original finding / retained boundary | Final executed evidence |
| --- | --- |
| Uppercase UUID/reference constants | Lowercase core-ID positive and uppercase core-ID/reference negatives PASS. Migration40's constant regex admits only lowercase a-f, matching `tenant.ValidID`; Go validation is unchanged. |
| Repeated expensive policy computation | 96 seeded receipts, 7 scoped grants / 63 nodes per capability: compensation query PASS at **1933.184 ms**, under unchanged 2000 ms. EXPLAIN assertions find executed policy-function nodes with Actual Loops at most 1. Multiple RLS InitPlans can exist; this is not a claim of one total function call for every possible statement. |
| Lease freshness after optimization | Existing delayed UPDATE uses 0.7-second sleep after a lease shortened to 0.5 seconds and passes the expired-write denial assertion. Per-row `worker_identity_scope` remains in USING and WITH CHECK. Separate business SECURITY DEFINER functions still call live `worker_tenant_scope` directly. |
| Live business permission and denial evidence | Workflow/action/entity/row/field DENY, inactive head, empty scopes, owner/team/share changes, current typed conditions, source masking, denied state/effect writes and available exact denial evidence PASS. Allowed-audit write rejection was independently executed at d093e80; its retained policy is source-reviewed at a5bba, not directly exercised by these final seven roots. |
| Canonical identity / retained generations | Canonical OWNER/SHARE after cutover, retired-generation CONDITION denial and reviewed ALL recovery PASS; no immutable workflow rewrite. |
| Events and helper privileges | Live event capability/receipt revocation and trusted event consumption PASS. Private source-helper accidental EXECUTE grant remains invoker-confined and fails readiness; restoration PASS. |
| Exact capability | Wrong tenant, job, release, generation, token and kind, masked source and transaction-lifetime negatives PASS. |

The 1933 ms query has approximately **67 ms headroom** on this run. Root measured
1470 ms in its earlier targeted run; these are distinct observations. Expensive
role/IR evaluation no longer scales per scanned receipt in the executed plan,
but identity/lease checks and anti-join row work still cost time. This is a
bounded synthetic 96-receipt SQL workload with a six-state plan and seeded
receipts. It does not certify full 96-step workflow acceptance, broad capacity,
maximum policies on all query shapes, dense graphs or a stable production SLO.
The original out-of-envelope 1000-receipt timeout is retained below and was not
rerun at the final source. No timeout, pool, policy or performance budget was
raised.

ADR033 permits stricter Go execution budgets. The final SQL workload does not
prove every admitted maximum-scope definition executes end to end in Go. Source
review finds that the read Query path resets its scope node counter for the
entity predicate and each projected field; seven nine-node scopes plus the OR
wrapper reach 64 nodes in a predicate. Other combined scope trees, including
write paths, can exceed their execution budgets. No new final Go maximum-policy
probe ran, so this review claims neither a reproduced Go budget failure nor
maximum-policy workflow lifecycle acceptance. This limit is separate from the
corrected malformed UUID constant decision.

The root additionally reports final `make migrate migration-status` (1-40),
`make check test build` PASS at a5bba: full integration **558.071s**, 127 frozen
documents, Go race/static/format, Node 5, frontend 29, eight binary builds and
frontend build; `make test-e2e` **5 PASS, 10.3s**. These are attributed root
results, not duplicate reviewer executions. No ordinary 100k API read or
32-client write gate was repeated here; migration40 changes worker policies,
not those established API paths.

The requested corrections satisfy the two original review findings within this
explicit boundary. Full R5 still needs its documented product/administration,
approval-role, partial/masking and identity work; production/process authority
and M10 remain outside this verdict. Final-source test processes completed,
owned disposable cleanup ran, and CPU/database ownership was released after
this run. The only reviewer repository edit is this report.

## Historical independent findings and executions at d093e80

The findings and measurements below describe the original checkpoint and remain
unchanged historical FAIL/PASS evidence. Both findings are corrected as recorded
above; no current unresolved HIGH/MEDIUM is asserted for this reviewed slice.

## HIGH: invalid uppercase UUID constant opens the raw source port

`TestIndependentR5BComparatorCoreIdentityCase` publishes two actual configured
role versions through the public role API. Lowercase `field.contact.id eq <id>`
is a populated SQL and trusted-Go positive control. Changing only that constant
to uppercase produces `allowed=true`, one row from
`runtime.worker_source_record()`, and an actual `Worker.Step` rejection:
`invalid data request`. The workflow version stays 1; the probe executes no
business transition.

Migration39 `control.policy_compare` permits A-F in UUID/reference constants and
casts the core ID to UUID. The existing Go `data.fieldValue` uses
`tenant.ValidID`, which admits lowercase UUIDs only. The persisted definition is
accepted through public publication; no superuser definition rewrite or malformed
artifact was needed. A raw credential therefore retains its exact leased record
when the trusted typed executor rejects the configured condition. This violates
the claimed malformed-condition fail-closed boundary. No cross-tenant, unrelated
record, undeclared-field or expired-lease escape was demonstrated.

Required correction: preserve the existing typed constant contract in the SQL
port for both core UUIDs and reference values. Retain populated valid and invalid
constant controls; do not weaken Go validation to make the negative pass.

## MEDIUM: worker row policies repeat the full evaluator

Migration20's `workflow_worker` policies call
`iam.worker_tenant_scope(tenant_id)` for qualifying rows. Migration39 makes that
volatile function invoke the full decision: role body, plan/input/IR JSON parsing,
record/canonical access, team traversal and each declared field scope. It is
repeated by history/receipt scans, including compensation's anti-join.

Independent, sequential measurements on the existing small workflow and a
configured role, with unchanged SQL statement timeout:

| Executed workload | Result |
| --- | --- |
| History 1 row | PASS; 10.176 ms |
| History 100 rows | PASS; 102.627 ms |
| History 1000 rows, bounded reader shape | PASS; 1383.432 ms |
| Compensation anti-join/order/LIMIT, 1000 seeded receipts | FAIL; 2009.031 ms, SQLSTATE 57014 |
| Compensation query, 96 seeded receipts, publicly admitted 7 scoped grants / 63 nodes per data capability | FAIL; 2003.473 ms, SQLSTATE 57014; raw decision positive control succeeds |

The 1000-receipt case exceeds the honest DAG receipt envelope and is an adversarial
dense workload. The 96-receipt case stays within the numerical 100-state envelope
and the policy admission budgets, but uses the existing six-state plan with
operator-seeded receipts. It is a **bounded synthetic SQL workload**, not full
96-step workflow lifecycle acceptance. The repeated-authorization regression and
actual 2-second timeout are established; broader capacity is not.

Two attempted 99-state dense fixtures failed at HTTP workflow start (503), before
worker measurement, once with repeated conditions and once with unconditional
activity transitions. Their failures are retained. They do not establish a
migration39 worker regression or a successful 99-state workload.

Required correction: evaluate expensive current policy once per statement while
retaining exact identity, tenant/job and live lease fences for relevant rows.
Recheck revocation and comparator controls after the policy change. Do not raise
SQL timeout or numerical budgets to relabel this failure.

## Independent controls that passed

- Actual configured workflow execute/activity/entity/row/field DENY and inactive
  head block populated source, binding, instance and history reads. Raw state
  writes affect zero rows; effect and allowed-audit inserts reject with 42501.
  Exact denied-audit insertion remains available in the rolled-back probe.
- OWNER change, parent-team membership activation/revocation, SHARE insert/delete,
  and matching current typed CONDITION followed by a live record change give
  populated allow/deny controls without changing the original job.
- Private helper invocation rejects raw EXECUTE. An owned-database accidental
  EXECUTE grant to the source helper fails readiness and gains no revoked source
  rows; revoking that grant restores readiness.
- READ COMMITTED observes a committed inactive head in an already-bound
  transaction. REPEATABLE READ retains its prior role snapshot in that bounded
  transaction. A fresh transaction with the same lease denies in both cases.
  This is the documented snapshot boundary, not instantaneous revocation.
- Incomplete event envelopes and receipt writes deny after events.consume DENY or
  an inactive head. Event capabilities expose no workflow record data. An allowed
  consumer commits one receipt and local effect; completed receipt reconciliation
  after revocation returns without repeating the effect.
- A populated migration38-to39 rehearsal reproduces the pre39 raw field-DENY
  bypass, applies39 once, verifies repeated UpTo39 is a no-op and retains the
  SHA256 of original IR/plan/input bytes. The same existing lease then denies.
  A reviewed ALL role version restores access and resumes the original instance
  to state.activity without changing immutable bytes.
- Numeric literals 7, 7.0, 7e0 and 7e-0 all normalize through the existing public
  decoder and pass SQL plus rollback-only trusted authorization. Fractional 7.5
  denies. The initial integer-token hypothesis was **discarded**: the retained
  canonical parser makes these integral spellings equivalent.

## Commands actually executed

All commands ran from `/workspace/Framwork`. Commands use the repository's pinned
Go wrapper and local environment loader; no secret values are printed. Logs live
in `/tmp/r5b-review`. Every test uses `m8Database(t)`, which checks explicit local
development/test driver bindings, effective database/role and connected
identity before creating/mutating an owned disposable database. No implementation
or migration file was edited by the reviewer.

Initial parity/per-row run — **FAIL, 20.588s**:

```sh
.local/toolchains/go/bin/gofmt -w /tmp/r5b-review/probes_test.go
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5b-review/overlay.json -run '^TestIndependentR5B(ComparatorCoreIdentityCase|ComparatorIntegerTokens|PopulatedHistoryCost|PopulatedCompensationReceiptCost)$' -v ./tests/integration > /tmp/r5b-review/initial-parity-performance.txt 2>&1
```

Uppercase UUID failure and 1000-receipt timeout are the executed findings above.
The initial integer fixture incorrectly expected canonical 7.0 to deny; actual
Step succeeded and invalidated that lease, causing the later stale-lease fixture
failures. This fixture error is retained and corrected with rollback-only trusted
authorization and the actual canonical numeric contract.

Corrected numeric and independent revocation/helper/isolation/event controls —
**PASS, 18.227s**:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5b-review/overlay.json -run '^TestIndependentR5B(ComparatorIntegerTokens|ConfiguredRevocationAndHelpers|TransactionRevocationBoundary|EventReceiptsAndCompletedReconciliation)$' -v ./tests/integration > /tmp/r5b-review/controls-current.txt 2>&1
```

Dense 99-state attempts — **FAIL before measurement, 6.294s and 6.490s**:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5b-review/overlay.json -run '^TestIndependentR5BHonestSizeDenseWorkflowCost$' -v ./tests/integration > /tmp/r5b-review/dense-workflow-current.txt 2>&1
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5b-review/overlay.json -run '^TestIndependentR5BHonestSizeDenseWorkflowCost$' -v ./tests/integration > /tmp/r5b-review/dense-workflow-corrected.txt 2>&1
```

The second scratch fixture removes repeated transition conditions; source remains
frozen. Both fail at HTTP start 503, so neither is a worker-query benchmark.

Populated migration rehearsal — **PASS, 4.127s**:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5b-review/migration-overlay.json -run '^TestIndependentR5BMigration38To39KeepsWorkflow$' -v ./tests/integration > /tmp/r5b-review/migration-current.txt 2>&1
```

Its test-only overlay replaces both `p.Up(ctx)` calls in a copied
`tests/integration/m8_delivery_test.go` with `p.UpTo(ctx, 38)`; the original helper,
target verification and local owned-database cleanup are preserved. The probe then
uses the actual embedded migrations to apply39 to populated38 state. No Down or
migration rewrite is used.

Maximum admitted policy / 96 seeded receipts — **FAIL, 4.718s**:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5b-review/overlay.json -run '^TestIndependentR5BHonestSizeMaximumPolicyCost$' -v ./tests/integration > /tmp/r5b-review/maximum-policy-current.txt 2>&1
```

This reproduces the unchanged statement timeout after a positive SQL decision.
No performance gate, pool budget, lease timeout or source constant was raised.

## Provenance, recovery and remaining limits

The root reports `make migrate migration-status` and `make check test build` PASS
at d093e80, including full integration 587.550s, 127 frozen documents, Node 5,
frontend 29 and builds. These are attributed implementer results, not independent
executions. This reviewer did not duplicate that full suite, browser E2E,
established 100k read/32-client write workloads, remote CI, OS credential
isolation, production rollout or customer acceptance.

No reviewer API/worker/dispatcher service was started. All independent test
commands completed and their fixtures used owned disposable cleanup. Unowned
pre-existing processes were left alone. CPU/database ownership was released
before root implementation resumed. Only this report is a reviewer repository
edit; scratch code and logs remain outside the checkout.

Recovery is reviewed forward migration and compatible binary restart while
preserving original records, leases/jobs, immutable IR/input/plan bytes, receipt,
audit and role histories. Never restore broad worker grants or erase receipt/
inbox evidence. Additive40 and final ordinary regression verification are now
complete as recorded above; the original independently authored probe phase is
preserved below. New independent final migration/probe work remains NOT RUN.

| Witness | SHA256 |
| --- | --- |
| migration39, preserved through a5bba | `a6e86aaf3fe2170dd865b964749dcd12e1a5d3c9ec222561eed07309862e5950` |
| migration40 at a5bba | `252b9b5ae0bab48944ec88ae5e6c515085fd93cbfd7eb0d7405aa0b9e2341537` |
| Existing worker-policy regression source at a5bba | `445d71d65dd8650797185807d14a2c546e916ed5d8c8577372c428ddc3ff1d76` |
| ADR033 at a5bba | `7bad69916862570eb72d3a1c6d11c6028cc2f7051075775d117f291217bd3208` |
| Final ordinary regression log | `72fde67b54414b33903ba6830cecb28c14c660b02ae8c22395b6f3e5fd1dfe97` |
| workflow/execution.go at d093e80 | `0df71c75ff5034265d311493f5e1a59fa3cb05013f186cc4f9cc5aca829fd1c5` |
| policy/readiness.go at d093e80 | `c84322e75b000f5b28a429ffa6e550e64bab12d3d74f90a37b126b53f50b39fa` |
| Historical d093e80 full probe source | `83c1c555a8b9283027841e39712ac5431fd20166195312f8b2d20ce7f4907f3a` |
| Initial executed source | `8c21aa617c0c8e016e74b99c1de62313d4ae8c51fc857244d6437d4a3014e3ed` |
| Corrected controls source | `39e0737dcbb089552c0ad7c7c99e0655f295588818eb84837598fae3b7147a18` |
| First dense attempt source | `30a3c44d38ab6c53b973a3347cc66ea3273fcf00fc1eadeb64b454177f1b20a2` |
| Corrected dense/migration source | `d33d15ca7979041fe94709ee69d04369a0cfb603091e0e1ca959130c7f6dd976` |
| overlay.json | `90fd6a470ef7b979c7bfd8732af17c0b35d2ef8e58e3dfd7b1c4292eabc2ff59` |
| migration-overlay.json | `268d41dd441250d49441b71ce57f90101a9f66f8efc3fe21f83bbef05a583eae` |
| Copied baseline38 helper | `2582ff7432637617c6212a3e0d0f94af7d11a017e614e5626c6267b42433131a` |

## Historical independently authored scratch probe source

Map `tests/integration/independent_r5b_review_test.go` to this source in the Go
overlay. For the migration-only command additionally map
`tests/integration/m8_delivery_test.go` to its copied UpTo38 helper as described
above. This source was used for the historical d093e80 phase, not the final
ordinary command. The dense fixture failures and initial incorrect integer
expectation remain identified above; this snapshot includes their corrected
variants.

```go
//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
)

func independentR5BRole(t *testing.T, m *m9Fixture) (http.Handler, string, policy.Definition, policy.RoleVersion) {
	t.Helper()
	h := r5Handler(t, m.f, audit.Postgres{})
	root := "/v1/tenants/" + m.f.tenantA
	d := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "ALL"})
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.execute", "workflows.activity"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	v := r5Publish(t, h, root+"/roles", m.token, 0, d, true, uuid(t))
	r5Assign(t, h, root, m.token, m.f.bob, "owner", 200)
	r5Assign(t, h, root, m.approver, m.f.alice, d.Name, 200)
	m.worker.Store = access.WorkerWorkflowStore(policy.Runtime{Worker: true}, audit.Postgres{}, events.Postgres{}, func(ctx context.Context, q postgres.Executor, a tenant.Access) (identity.User, error) {
		var job string
		if e := q.QueryRowContext(ctx, `SELECT current_setting('app.job_id',true)`).Scan(&job); e != nil {
			return identity.User{}, e
		}
		return (identity.Store{}).ResolveWorker(ctx, q, job)
	})
	return h, root, d, v
}

func independentR5BBind(t *testing.T, m *m9Fixture, l jobs.WorkflowLease, isolation sql.IsolationLevel) *sql.Tx {
	t.Helper()
	tx, e := m.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: isolation})
	if e != nil {
		t.Fatal("raw transaction", e)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if ok, e := bindM9R(tx, l); e != nil || !ok {
		t.Fatal("raw exact lease", e)
	}
	var actor, issuer, subject string
	if e = tx.QueryRow(`SELECT actor::text,issuer,subject FROM iam.worker_identity($1::uuid)`, l.Ticket.JobID()).Scan(&actor, &issuer, &subject); e != nil || actor != m.f.alice {
		t.Fatal("raw exact identity", e)
	}
	if _, e = tx.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer',$2,true),set_config('app.auth_subject',$3,true)`, actor, issuer, subject); e != nil {
		t.Fatal("identity context", e)
	}
	return tx
}

func independentR5BDecision(t *testing.T, m *m9Fixture, l jobs.WorkflowLease) (bool, int) {
	t.Helper()
	tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
	defer tx.Rollback()
	var allowed bool
	var n int
	if e := tx.QueryRow(`SELECT allowed FROM control.worker_policy_decision()`).Scan(&allowed); e != nil {
		t.Fatal("SQL decision", e)
	}
	if e := tx.QueryRow(`SELECT count(*) FROM runtime.worker_source_record()`).Scan(&n); e != nil {
		t.Fatal("source port", e)
	}
	return allowed, n
}

func independentR5BTrusted(t *testing.T, m *m9Fixture, l jobs.WorkflowLease) error {
	t.Helper()
	rollback := errors.New("independent authorized read rollback")
	e := jobs.WithJob(context.Background(), m.db, l.Ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var pin workflow.Pin
		var inputs []byte
		var snapshot workflow.Snapshot
		e := q.QueryRowContext(ctx, `SELECT app_id,metadata_revision,ir_hash,ir_body,workflow_id,workflow_version,plan_hash,record_id::text,request_id,inputs FROM runtime.workflow_bindings WHERE tenant_id=$1 AND id=$2`, a.TenantID(), l.Ticket.JobID()).Scan(&pin.AppID, &pin.Revision, &pin.IRHash, &pin.IR, &pin.WorkflowID, &pin.WorkflowVersion, &pin.PlanHash, &pin.RecordID, &pin.RequestID, &inputs)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(inputs, &snapshot); e != nil {
			return e
		}
		_, e = m.worker.Store.Authorize(ctx, q, a, "workflows.execute", pin, snapshot.Fields)
		if e != nil {
			return e
		}
		return rollback
	})
	if errors.Is(e, rollback) {
		return nil
	}
	return e
}

func independentR5BExpect(t *testing.T, m *m9Fixture, l jobs.WorkflowLease, want bool) {
	t.Helper()
	tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
	defer tx.Rollback()
	var got bool
	if e := tx.QueryRow(`SELECT allowed FROM control.worker_policy_decision()`).Scan(&got); e != nil || got != want {
		t.Fatal("decision allow/deny", e, got, want)
	}
	for _, table := range []string{"runtime.workflow_instances", "runtime.workflow_bindings", "runtime.workflow_history"} {
		var n int
		if e := tx.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || (want && n != 1) || (!want && n != 0) {
			t.Fatal("populated business port", table, e, n, want)
		}
	}
	var n int
	if e := tx.QueryRow(`SELECT count(*) FROM runtime.worker_source_record()`).Scan(&n); e != nil || (want && n != 1) || (!want && n != 0) {
		t.Fatal("source boundary", e, n, want)
	}
	if want {
		return
	}
	result, e := tx.Exec(`UPDATE runtime.workflow_instances SET version=version+1 WHERE id=$1::uuid`, l.Ticket.JobID())
	if e != nil {
		t.Fatal("denied state write", e)
	}
	if n, e := result.RowsAffected(); e != nil || n != 0 {
		t.Fatal("denied state mutation", e, n)
	}
	if _, e = tx.Exec(`SAVEPOINT effects`); e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(`INSERT INTO runtime.m9_test_effects(tenant_id,instance_id,step_key) VALUES($1,$2,'independent')`, m.f.tenantA, l.Ticket.JobID())
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != "42501" {
		t.Fatal("denied raw effect did not reject with privilege error")
	}
	if _, e = tx.Exec(`ROLLBACK TO SAVEPOINT effects`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(`INSERT INTO audit.entries(actor_id,tenant_id,action,resource,allowed,decision_id,policy_version,request_id,trace_id,state) VALUES($1,$2,'workflows.execute','workflow',false,'independent','sql-denied','probe','','{}')`, m.f.alice, m.f.tenantA); e != nil {
		t.Fatal("bounded denial audit", e)
	}
	if _, e = tx.Exec(`SAVEPOINT audit_allow`); e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(`INSERT INTO audit.entries(actor_id,tenant_id,action,resource,allowed,decision_id,policy_version,request_id,trace_id,state) VALUES($1,$2,'workflows.execute','workflow',true,'independent','sql-denied','probe','','{}')`, m.f.alice, m.f.tenantA)
	if !errors.As(e, &pe) || pe.Code != "42501" {
		t.Fatal("revoked allowed audit was accepted")
	}
	if _, e = tx.Exec(`ROLLBACK TO SAVEPOINT audit_allow`); e != nil {
		t.Fatal(e)
	}
}

func TestIndependentR5BComparatorCoreIdentityCase(t *testing.T) {
	m := m9Setup(t, nil)
	m.start(t, "independent-core-case")
	h, root, d, v := independentR5BRole(t, m)
	l, ok, e := m.worker.Claim(context.Background())
	if e != nil || !ok {
		t.Fatal("lease", e)
	}
	for _, tc := range []struct {
		name, value string
		want        bool
	}{{"lowercase-control", m.record.ID, true}, {"uppercase-invalid", strings.ToUpper(m.record.ID), false}} {
		t.Run(tc.name, func(t *testing.T) {
			scope := policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.id", Op: "eq", Value: json.RawMessage(fmt.Sprintf("%q", tc.value))}}
			d.Grants[1].Scope = &scope
			v = r5Publish(t, h, root+"/roles", m.approver, v.Version, d, true, uuid(t))
			allowed, n := independentR5BDecision(t, m, l)
			if tc.want {
				if !allowed || n != 1 {
					t.Fatal("populated positive control", allowed, n)
				}
				if e := independentR5BTrusted(t, m, l); e != nil {
					t.Fatal("trusted lowercase control", e)
				}
				return
			}
			err := m.worker.Step(context.Background(), l)
			var version int64
			if e := m.f.operator.QueryRow(`SELECT version FROM runtime.workflow_instances WHERE id=$1`, l.Ticket.JobID()).Scan(&version); e != nil || version != 1 {
				t.Fatal("typed control mutated", e, version)
			}
			t.Logf("raw SQL allowed=%v source_rows=%d; trusted Worker.Step error=%v; instance_version=%d", allowed, n, err, version)
			if err == nil {
				t.Fatal("trusted typed rejection control did not reject")
			}
			if allowed || n != 0 {
				t.Error("raw SQL accepted uppercase UUID constant rejected by trusted typed path")
			}
		})
	}
}

func TestIndependentR5BComparatorIntegerTokens(t *testing.T) {
	m := m9Setup(t, nil)
	shadowSource(t, m.handler, m.token, m.f.tenantA, m.app, 2, m.raw, func(d *metadata.Document) {
		addCount(d, "integer", false)
		for i := range d.Definitions {
			if w := d.Definitions[i].Workflow; w != nil {
				old := w.States[0].Transitions[0].Condition
				w.States[0].Transitions[0].Condition = &metadata.Expression{Op: "and", Args: []metadata.Expression{*old, {Op: "gte", Args: []metadata.Expression{{Op: "field", FieldRef: "field.contact.count"}, {Op: "literal", Value: json.RawMessage(`0`)}}}}}
			}
		}
	})
	prefix := fmt.Sprintf("/v1/tenants/%s/preview/%s/versions/2", m.f.tenantA, m.app)
	values := m5Values("Integer parity")
	values["field.contact.count"] = json.Number("7")
	w := m5Call(m.handler, prefix+"/entities/entity.contact/commands", m.token, data.Command{Operation: "create", Values: values})
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &m.record) != nil {
		t.Fatal("integer record", w.Code)
	}
	m.base = prefix + "/workflows/workflow.contact.review"
	m.start(t, "independent-integer")
	h, root, d, v := independentR5BRole(t, m)
	l, ok, e := m.worker.Claim(context.Background())
	if e != nil || !ok {
		t.Fatal("lease", e)
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{{"7", true}, {"7.0", true}, {"7e0", true}, {"7e-0", true}, {"7.5", false}} {
		t.Run(tc.value, func(t *testing.T) {
			scope := policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.count", Op: "eq", Value: json.RawMessage(tc.value)}}
			d.Grants[1].Scope = &scope
			v = r5Publish(t, h, root+"/roles", m.approver, v.Version, d, true, uuid(t))
			allowed, n := independentR5BDecision(t, m, l)
			if tc.want {
				if !allowed || n != 1 {
					t.Fatal("integer positive control", allowed, n)
				}
				if e := independentR5BTrusted(t, m, l); e != nil {
					t.Fatal("trusted integer control", e)
				}
				return
			}
			err := independentR5BTrusted(t, m, l)
			t.Logf("constant=%s raw SQL allowed=%v source_rows=%d trusted error=%v", tc.value, allowed, n, err)
			if err == nil {
				t.Fatal("trusted fractional integer control allowed")
			}
			if allowed || n != 0 {
				t.Error("SQL admitted a non-integral integer condition")
			}
		})
	}
}

func TestIndependentR5BPopulatedHistoryCost(t *testing.T) {
	m := m9Setup(t, nil)
	m.start(t, "independent-history-cost")
	_, _, _, _ = independentR5BRole(t, m)
	l, ok, e := m.worker.Claim(context.Background())
	if e != nil || !ok {
		t.Fatal("lease", e)
	}
	for _, rows := range []int{1, 100, 1000} {
		if rows > 1 {
			_, e = m.f.operator.Exec(`INSERT INTO runtime.workflow_history(tenant_id,instance_id,sequence,state_id,status,event,actor_id) SELECT $1,$2,g,'state.start','READY','probe',$3 FROM generate_series($4::integer,$5::integer) g`, m.f.tenantA, l.Ticket.JobID(), m.f.alice, func() int {
				if rows == 100 {
					return 2
				}
				return 101
			}(), rows)
			if e != nil {
				t.Fatal("populated history", e)
			}
		}
		tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
		if _, e = tx.Exec(`SET LOCAL statement_timeout='2000ms'`); e != nil {
			t.Fatal(e)
		}
		started := time.Now()
		var n int
		e = tx.QueryRow(`SELECT count(*) FROM (SELECT sequence FROM runtime.workflow_history WHERE tenant_id=$1 AND instance_id=$2 ORDER BY sequence LIMIT 1000) h`, m.f.tenantA, l.Ticket.JobID()).Scan(&n)
		elapsed := time.Since(started)
		t.Logf("history_rows=%d returned=%d elapsed=%s error=%v", rows, n, elapsed, e)
		if e != nil || n != rows {
			t.Errorf("configured worker history cannot read populated bounded history: rows=%d duration=%s error=%v", rows, elapsed, e)
		}
		_ = tx.Rollback()
	}
}

func TestIndependentR5BPopulatedCompensationReceiptCost(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "independent-receipt-cost")
	_, _, _, _ = independentR5BRole(t, m)
	if _, e := m.f.operator.Exec(`INSERT INTO runtime.workflow_receipts(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) SELECT $1,$2,'probe-'||g,'local.noop',repeat('a',64),convert_to('{}','UTF8'),encode(sha256(convert_to('{}','UTF8')),'hex'),g,'local.noop' FROM generate_series(1,1000) g`, m.f.tenantA, x.ID); e != nil {
		t.Fatal("populated receipts", e)
	}
	l, ok, e := m.worker.Claim(context.Background())
	if e != nil || !ok {
		t.Fatal("lease", e)
	}
	tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
	defer tx.Rollback()
	if _, e = tx.Exec(`SET LOCAL statement_timeout='2000ms'`); e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	var step string
	e = tx.QueryRow(`SELECT r.step_key FROM runtime.workflow_receipts r WHERE r.tenant_id=$1 AND r.instance_id=$2 AND r.compensation_ref<>'' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts c WHERE c.tenant_id=r.tenant_id AND c.instance_id=r.instance_id AND c.step_key=r.step_key||':compensate') ORDER BY r.sequence DESC,r.step_key DESC LIMIT 1`, m.f.tenantA, x.ID).Scan(&step)
	elapsed := time.Since(started)
	t.Logf("receipt_rows=1000 compensation_candidate=%s elapsed=%s error=%v", step, elapsed, e)
	if e != nil || step != "probe-1000" {
		t.Errorf("configured worker compensation receipt query failed at 1000 receipts: duration=%s error=%v", elapsed, e)
	}
}

func TestIndependentR5BHonestSizeDenseWorkflowCost(t *testing.T) {
	m := m9Setup(t, func(w *metadata.Workflow) {
		condition := w.States[0].Transitions[0].Condition
		activity := w.States[1].Activity
		w.TimeoutMS = 604800000
		w.States = []metadata.WorkflowState{{ID: "state.start", Type: "start", Transitions: []metadata.Transition{{To: "state.step001", Condition: condition}, {To: "state.failed"}}}}
		for i := 1; i <= 96; i++ {
			next := fmt.Sprintf("state.step%03d", i+1)
			if i == 96 {
				next = "state.complete"
			}
			w.States = append(w.States, metadata.WorkflowState{ID: fmt.Sprintf("state.step%03d", i), Type: "activity", Activity: activity, Transitions: []metadata.Transition{{To: next}}})
		}
		w.States = append(w.States, metadata.WorkflowState{ID: "state.complete", Type: "complete", Transitions: []metadata.Transition{}}, metadata.WorkflowState{ID: "state.failed", Type: "failed", Transitions: []metadata.Transition{}})
	})
	x := m.start(t, "independent-honest-dense")
	_, _, _, _ = independentR5BRole(t, m)
	if _, e := m.f.operator.Exec(`INSERT INTO runtime.workflow_receipts(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) SELECT $1,$2,'state.step'||lpad(g::text,3,'0'),'local.noop',repeat('a',64),convert_to('{}','UTF8'),encode(sha256(convert_to('{}','UTF8')),'hex'),g,'local.noop' FROM generate_series(1,96) g`, m.f.tenantA, x.ID); e != nil {
		t.Fatal("honest-size receipts", e)
	}
	var irBytes, planBytes int
	if e := m.f.operator.QueryRow(`SELECT octet_length(ir_body),octet_length(plan_body) FROM runtime.workflow_bindings WHERE id=$1`, x.ID).Scan(&irBytes, &planBytes); e != nil {
		t.Fatal(e)
	}
	l, ok, e := m.worker.Claim(context.Background())
	if e != nil || !ok {
		t.Fatal("lease", e)
	}
	tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
	defer tx.Rollback()
	if _, e = tx.Exec(`SET LOCAL statement_timeout='2000ms'`); e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	var step string
	e = tx.QueryRow(`SELECT r.step_key FROM runtime.workflow_receipts r WHERE r.tenant_id=$1 AND r.instance_id=$2 AND r.compensation_ref<>'' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts c WHERE c.tenant_id=r.tenant_id AND c.instance_id=r.instance_id AND c.step_key=r.step_key||':compensate') ORDER BY r.sequence DESC,r.step_key DESC LIMIT 1`, m.f.tenantA, x.ID).Scan(&step)
	elapsed := time.Since(started)
	t.Logf("valid_plan_states=99 IR_bytes=%d plan_bytes=%d receipt_rows=96 compensation_candidate=%s elapsed=%s error=%v", irBytes, planBytes, step, elapsed, e)
	if e != nil || step != "state.step096" {
		t.Errorf("compensation query fails inside valid 100-state workflow envelope: duration=%s error=%v", elapsed, e)
	}
}

func TestIndependentR5BHonestSizeMaximumPolicyCost(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "independent-honest-policy")
	h, root, d, v := independentR5BRole(t, m)
	children := []policy.DataScope{}
	for i := 0; i < 8; i++ {
		children = append(children, policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"active"`)}})
	}
	scope := policy.DataScope{Kind: "OR", Children: children}
	d.Grants[1].Scope = &scope
	for i := 0; i < 6; i++ {
		d.Grants = append(d.Grants, d.Grants[1])
	}
	v = r5Publish(t, h, root+"/roles", m.approver, v.Version, d, true, uuid(t))
	_ = v
	if _, e := m.f.operator.Exec(`INSERT INTO runtime.workflow_receipts(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) SELECT $1,$2,'probe-'||g,'local.noop',repeat('a',64),convert_to('{}','UTF8'),encode(sha256(convert_to('{}','UTF8')),'hex'),g,'local.noop' FROM generate_series(1,96) g`, m.f.tenantA, x.ID); e != nil {
		t.Fatal("96 receipts", e)
	}
	l, ok, e := m.worker.Claim(context.Background())
	if e != nil || !ok {
		t.Fatal(e)
	}
	tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
	defer tx.Rollback()
	if _, e = tx.Exec(`SET LOCAL statement_timeout='2000ms'`); e != nil {
		t.Fatal(e)
	}
	var allowed bool
	if e = tx.QueryRow(`SELECT allowed FROM control.worker_policy_decision()`).Scan(&allowed); e != nil || !allowed {
		t.Fatal("maximum valid policy positive", e, allowed)
	}
	started := time.Now()
	var step string
	e = tx.QueryRow(`SELECT r.step_key FROM runtime.workflow_receipts r WHERE r.tenant_id=$1 AND r.instance_id=$2 AND r.compensation_ref<>'' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts c WHERE c.tenant_id=r.tenant_id AND c.instance_id=r.instance_id AND c.step_key=r.step_key||':compensate') ORDER BY r.sequence DESC,r.step_key DESC LIMIT 1`, m.f.tenantA, x.ID).Scan(&step)
	elapsed := time.Since(started)
	t.Logf("admitted_scoped_grants=7 admitted_nodes_per_capability=63 receipt_rows=96 candidate=%s elapsed=%s error=%v", step, elapsed, e)
	if e != nil || step != "probe-96" {
		t.Errorf("compensation query failed inside admitted policy/receipt envelope: duration=%s error=%v", elapsed, e)
	}
}

func independentR5BFixtureSQL(t *testing.T, m *m9Fixture, query string, args ...any) {
	t.Helper()
	tx, e := m.f.operator.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, m.f.aliceSubject, m.f.alice, m.f.tenantA); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(query, args...); e != nil {
		t.Fatal("owned fixture change", e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
}

func TestIndependentR5BConfiguredRevocationAndHelpers(t *testing.T) {
	m := m9Setup(t, nil)
	m.start(t, "independent-revocation")
	h, root, d, v := independentR5BRole(t, m)
	l, ok, e := m.worker.Claim(context.Background())
	if e != nil || !ok {
		t.Fatal("lease", e)
	}
	independentR5BExpect(t, m, l, true)
	publish := func(s policy.DataScope, active bool) {
		d.Grants[1].Scope = &s
		v = r5Publish(t, h, root+"/roles", m.approver, v.Version, d, active, uuid(t))
	}
	for _, action := range []string{"workflows.execute", "workflows.activity", "data.entity.read", "data.row.read", "data.field.read"} {
		t.Run(action, func(t *testing.T) {
			bad := d
			bad.Grants = append(append([]policy.Grant{}, d.Grants...), policy.Grant{Effect: "DENY", Actions: []string{action}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
			v = r5Publish(t, h, root+"/roles", m.approver, v.Version, bad, true, uuid(t))
			independentR5BExpect(t, m, l, false)
			v = r5Publish(t, h, root+"/roles", m.approver, v.Version, d, true, uuid(t))
			independentR5BExpect(t, m, l, true)
		})
	}
	publish(policy.DataScope{Kind: "ALL"}, false)
	independentR5BExpect(t, m, l, false)
	publish(policy.DataScope{Kind: "OWNER"}, true)
	independentR5BExpect(t, m, l, true)
	independentR5BFixtureSQL(t, m, `UPDATE runtime.records SET owner_user_id=$1 WHERE tenant_id=$2 AND id=$3`, m.f.bob, m.f.tenantA, m.record.ID)
	independentR5BExpect(t, m, l, false)
	parent, child := uuid(t), uuid(t)
	independentR5BFixtureSQL(t, m, `INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'independent parent')`, m.f.tenantA, parent)
	independentR5BFixtureSQL(t, m, `INSERT INTO iam.teams(tenant_id,id,parent_id,name) VALUES($1,$2,$3,'independent child')`, m.f.tenantA, child, parent)
	independentR5BFixtureSQL(t, m, `UPDATE runtime.records SET owner_team_id=$1 WHERE tenant_id=$2 AND id=$3`, child, m.f.tenantA, m.record.ID)
	publish(policy.DataScope{Kind: "TEAM"}, true)
	independentR5BExpect(t, m, l, false)
	independentR5BFixtureSQL(t, m, `INSERT INTO iam.team_memberships(tenant_id,team_id,user_id) VALUES($1,$2,$3)`, m.f.tenantA, parent, m.f.alice)
	independentR5BExpect(t, m, l, true)
	independentR5BFixtureSQL(t, m, `UPDATE iam.team_memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, m.f.tenantA, m.f.alice)
	independentR5BExpect(t, m, l, false)
	publish(policy.DataScope{Kind: "SHARE"}, true)
	independentR5BExpect(t, m, l, false)
	independentR5BFixtureSQL(t, m, `INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$1 FROM runtime.records WHERE tenant_id=$2 AND id=$3`, m.f.alice, m.f.tenantA, m.record.ID)
	independentR5BExpect(t, m, l, true)
	independentR5BFixtureSQL(t, m, `DELETE FROM runtime.record_shares WHERE tenant_id=$1 AND id=$2 AND user_id=$3`, m.f.tenantA, m.record.ID, m.f.alice)
	independentR5BExpect(t, m, l, false)
	publish(policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"active"`)}}, true)
	independentR5BExpect(t, m, l, true)
	independentR5BFixtureSQL(t, m, `UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.status}','"inactive"') WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, m.record.ID)
	independentR5BExpect(t, m, l, false)
	publish(policy.DataScope{Kind: "NONE"}, true)
	for _, call := range []string{`SELECT runtime.worker_lease_source_record()`, `SELECT control.policy_compare('{"type":"string"}','"a"','"a"','eq')`, `SELECT control.policy_scope_matches('{"kind":"ALL"}','{}',NULL,NULL,false,'{}',0)`} {
		tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
		_, e := tx.Exec(call)
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "42501" {
			t.Fatal("private helper callable by raw worker")
		}
		_ = tx.Rollback()
	}
	independentR5BFixtureSQL(t, m, `GRANT EXECUTE ON FUNCTION runtime.worker_lease_source_record() TO framework_worker`)
	if e = (policy.RoleStore{}).Ready(context.Background(), m.db, true); e == nil {
		t.Fatal("helper drift passed readiness")
	}
	tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
	var n int
	e = tx.QueryRow(`SELECT count(*) FROM runtime.worker_lease_source_record()`).Scan(&n)
	if e == nil && n != 0 {
		t.Fatal("invoker helper leaked revoked source after grant")
	}
	_ = tx.Rollback()
	independentR5BFixtureSQL(t, m, `REVOKE EXECUTE ON FUNCTION runtime.worker_lease_source_record() FROM framework_worker`)
	if e = (policy.RoleStore{}).Ready(context.Background(), m.db, true); e != nil {
		t.Fatal("restored readiness", e)
	}
	if e = m.f.operator.QueryRow(`SELECT count(*) FROM runtime.m9_test_effects`).Scan(&n); e != nil || n != 0 {
		t.Fatal("raw probes committed effects", e, n)
	}
}

func TestIndependentR5BTransactionRevocationBoundary(t *testing.T) {
	for _, level := range []sql.IsolationLevel{sql.LevelReadCommitted, sql.LevelRepeatableRead} {
		t.Run(level.String(), func(t *testing.T) {
			m := m9Setup(t, nil)
			m.start(t, "independent-transaction")
			h, root, d, v := independentR5BRole(t, m)
			l, ok, e := m.worker.Claim(context.Background())
			if e != nil || !ok {
				t.Fatal(e)
			}
			tx := independentR5BBind(t, m, l, level)
			var allowed bool
			if e = tx.QueryRow(`SELECT allowed FROM control.worker_policy_decision()`).Scan(&allowed); e != nil || !allowed {
				t.Fatal("transaction positive", e)
			}
			_ = r5Publish(t, h, root+"/roles", m.approver, v.Version, d, false, uuid(t))
			if e = tx.QueryRow(`SELECT allowed FROM control.worker_policy_decision()`).Scan(&allowed); e != nil || allowed != (level == sql.LevelRepeatableRead) {
				t.Fatal("documented snapshot boundary", e, allowed)
			}
			t.Logf("existing %s transaction allowed after role-head commit=%v", level, allowed)
			_ = tx.Rollback()
			independentR5BExpect(t, m, l, false)
		})
	}
}

func TestIndependentR5BMigration38To39KeepsWorkflow(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "independent-migration-existing")
	h, root, d, v := independentR5BRole(t, m)
	l, ok, e := m.worker.Claim(context.Background())
	if e != nil || !ok {
		t.Fatal(e)
	}
	witness := func() string {
		var s string
		e := m.f.operator.QueryRow(`SELECT encode(sha256(ir_body||plan_body||inputs),'hex') FROM runtime.workflow_bindings WHERE id=$1`, x.ID).Scan(&s)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	before := witness()
	denied := d
	denied.Grants = append(append([]policy.Grant{}, d.Grants...), policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "field.contact.status"}})
	v = r5Publish(t, h, root+"/roles", m.approver, v.Version, denied, true, uuid(t))
	tx := independentR5BBind(t, m, l, sql.LevelReadCommitted)
	var n int
	if e = tx.QueryRow(`SELECT count(*) FROM runtime.worker_source_record()`).Scan(&n); e != nil || n != 1 {
		t.Fatal("pre39 raw gap positive witness", e, n)
	}
	_ = tx.Rollback()
	p, e := goose.NewProvider(goose.DialectPostgres, m.f.operator, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if e != nil {
		t.Fatal(e)
	}
	applied, e := p.UpTo(context.Background(), 39)
	if e != nil || len(applied) != 1 {
		t.Fatal("populated migration39", e, len(applied))
	}
	if again, e := p.UpTo(context.Background(), 39); e != nil || len(again) != 0 {
		t.Fatal("migration repeatability", e)
	}
	if witness() != before {
		t.Fatal("migration rewrote immutable workflow witness")
	}
	independentR5BExpect(t, m, l, false)
	v = r5Publish(t, h, root+"/roles", m.approver, v.Version, d, true, uuid(t))
	_ = v
	independentR5BExpect(t, m, l, true)
	if e = m.worker.Step(context.Background(), l); e != nil {
		t.Fatal("existing workflow did not resume", e)
	}
	var state string
	if e = m.f.operator.QueryRow(`SELECT state_id FROM runtime.workflow_instances WHERE id=$1`, x.ID).Scan(&state); e != nil || state != "state.activity" {
		t.Fatal("workflow continuation", e, state)
	}
	if witness() != before {
		t.Fatal("resume altered immutable witness")
	}
	t.Log("pre39 field-DENY raw bypass reproduced; migration39 denies same lease; reviewed role recovery resumes existing pin")
}

func TestIndependentR5BEventReceiptsAndCompletedReconciliation(t *testing.T) {
	m8Database(t)
	f, h, base, alice, app := m5Fixture(t)
	m5Create(t, h, base, alice, "independent event")
	rh := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	d := r5Definition("finance_manager", app, policy.DataScope{Kind: "NONE"})
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"events.consume"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}})
	v := r5Publish(t, rh, root+"/roles", alice, 0, d, true, uuid(t))
	r5Assign(t, rh, root, alice, f.viewer, "owner", 200)
	owner := f.token(t, f.viewerSubject, nil)
	r5Assign(t, rh, root, owner, f.alice, d.Name, 200)
	dispatcher, db := m8Roles(t)
	dispatch := events.Dispatcher{DB: dispatcher}
	pointer := m8Claim(t, dispatch)
	if e := dispatch.Finish(context.Background(), pointer, true); e != nil {
		t.Fatal(e)
	}
	probe := func(want bool) {
		ticket, completed, ready, dead, e := jobs.EventTicket(context.Background(), db, pointer.TenantID, pointer.EventID, pointer.BodyHash, pointer.ConsumerID, pointer.Generation, pointer.LeaseToken)
		if e != nil || completed || !ready || dead {
			t.Fatal("exact event routing", e)
		}
		tx, e := db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		var ok bool
		if e = tx.QueryRow(`SELECT control.bind_worker_job($1,$2,'event',$3,$4,$5)`, pointer.TenantID, pointer.EventID, pointer.Generation, pointer.LeaseToken, ticket.ReleaseHash()).Scan(&ok); e != nil || !ok {
			t.Fatal("event bind", e)
		}
		if _, e = tx.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$2,true)`, f.alice, f.aliceSubject); e != nil {
			t.Fatal(e)
		}
		var n int
		if e = tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&n); e != nil || (want && n != 1) || (!want && n != 0) {
			t.Fatal("event body RLS", e, n, want)
		}
		if e = tx.QueryRow(`SELECT count(*) FROM runtime.worker_source_record()`).Scan(&n); e != nil || n != 0 {
			t.Fatal("event obtained record data", e, n)
		}
		_, e = tx.Exec(`INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1,$2,$3,$4)`, pointer.TenantID, pointer.ConsumerID, pointer.EventID, pointer.BodyHash)
		if want {
			if e != nil {
				t.Fatal("allowed receipt", e)
			}
		} else {
			var pe *pgconn.PgError
			if !errors.As(e, &pe) || pe.Code != "42501" {
				t.Fatal("revoked receipt not rejected")
			}
		}
	}
	probe(true)
	bad := d
	bad.Grants = append(append([]policy.Grant{}, d.Grants...), policy.Grant{Effect: "DENY", Actions: []string{"events.consume"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}})
	v = r5Publish(t, rh, root+"/roles", owner, v.Version, bad, true, uuid(t))
	probe(false)
	v = r5Publish(t, rh, root+"/roles", owner, v.Version, d, false, uuid(t))
	probe(false)
	v = r5Publish(t, rh, root+"/roles", owner, v.Version, d, true, uuid(t))
	consumer := events.Consumer{DB: db, Policy: policy.Runtime{Worker: true}, Audit: audit.Postgres{}, Handler: func(ctx context.Context, q postgres.Executor, a tenant.Access, e events.Envelope) error {
		_, err := q.ExecContext(ctx, `INSERT INTO runtime.m8_test_effects(tenant_id,event_id) VALUES($1,$2)`, a.TenantID(), e.ID)
		return err
	}}
	if e := consumer.Consume(context.Background(), pointer); e != nil {
		t.Fatal("authorized committed event", e)
	}
	_ = r5Publish(t, rh, root+"/roles", owner, v.Version, d, false, uuid(t))
	if e := consumer.Consume(context.Background(), pointer); e != nil {
		t.Fatal("completed receipt reconciliation after revoke", e)
	}
	var effects, receipts int
	if e := f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.m8_test_effects),(SELECT count(*) FROM runtime.event_inbox)`).Scan(&effects, &receipts); e != nil || effects != 1 || receipts != 1 {
		t.Fatal("receipt/effect duplicated", e, effects, receipts)
	}
	t.Log("revoked incomplete event denied; completed receipt reconciles without repeating effect")
}

var _ = os.Getenv

```
