# M9R R5a independent adversarial review

Reviewed source: `caa9d84064dfc9d6879ce60ba9952d283703aab3`.
Independent reviewer: `/root/m0_review`. Review date: 2026-10-14 UTC.

## Verdict and authority boundary

**R5a PASS for its explicitly bounded trusted-Go capability authority slice.
Full R5 remains PARTIAL.** A confirmed HIGH gap remains for enforcement of current
role bodies against compromised worker credentials. This report does not accept
that threat model, production deployment, full R5, M10, or the entire remediation
plan. Exact-job SQL confinement remains distinct from current capability-policy
enforcement.

The reviewed implementation adds immutable tenant role versions, operation-key
publication reconciliation, head CAS, membership role-label CAS, closed capability
definitions, entity/field SQL DataScope, per-transaction live loading, owner
concurrency protection, and durable workflow denial evidence. Existing bootstrap
roles remain broad by explicit adapter. Reserved capabilities unsupported by the
retained SQL executors are rejected before publication.

No implementation or applied migration files were edited by this reviewer.
Migration 38 was preserved. Independent overlays lived in `/tmp/r5a-review`.
All database attacks used `m8Database(t)`, which verifies local development/test
bindings, effective driver database/role and connected database/role before
modification, creates an owned disposable database, and drops it at cleanup.
The owner negative control deliberately replaced one function only in that
owned database. No shared Redis restart, production database change, credential
inspection, service reconfiguration or customer data mutation was performed.

## Confirmed remaining HIGH: raw worker role-body policy enforcement

`TestIndependentR5RawWorkerRoleBodyBoundary` exercised three actual role changes:

1. DENY `data.field.read` for the workflow's declared `field.contact.status`.
2. TEAM scopes with no matching team membership, so the authorized SQL query
   excludes the source row.
3. An inactive role head.

For each change, trusted `Worker.Step` denied the operation, preserved one denial
with the exact definition hash and instance ID, and performed no state transition.
A direct transaction using the assigned worker database credential nevertheless
successfully called `control.bind_worker_job` with its exact live lease and then
read one pinned record, including `field.contact.status`, through
`runtime.worker_source_record()`. Generic `runtime.records` SELECT remained
permission-denied. No cross-tenant, unrelated-record, undeclared-field, or expired
lease escape was demonstrated.

Evidence: migration 22 `control.worker_authority()` at lines 75–91 checks the
principal, live tenant/user/membership, exact transaction/job and lease. It does
not consume role heads or definition grants. Migration 37
`runtime.worker_source_record()` at lines 75–87 applies the immutable job's
physical record and declared-input field mask, plus canonical identity/tombstone
facts. Current role-body scopes and field DENY decisions are applied by the Go
adapter, rather than that raw SQL port.

This is HIGH against a claim of current policy revocation for a compromised
worker process/credential. It is a documented open full-R5 requirement, not a new
cross-tenant escape or evidence that trusted `Worker.Step` failed. R5a's
trusted-Go-only claim must remain explicit. Before full R5 acceptance, enforce
current applicable role authority at the protected port or an equivalently
trusted database boundary, and preserve populated raw-credential negative tests
for inactive roles, field denial, owner/team/share/condition scopes, existing
leases, and new lease issuance. Checking only a role head's active flag would not
close field/scope revocation.

Event raw-credential role-body bypass was not independently tested. Do not infer
closure from the trusted event-consumer regression.

## Findings corrected and independently verified

| Finding | Initial evidence | Current result |
| --- | --- | --- |
| Stored role engine could be reused across same-name tenant memberships | Old `1ae1f27` definition/store overlay: A HTTP allowed, B HTTP denied, then A-loaded engine allowed trusted B access; expected FAIL | Own current test PASS. Repository populated tenant and actor controls PASS. `RoleStore.Load` lines 113 and `LoadWorker` line 133 bind authority; pure compile output cannot authorize. |
| RR last-owner write skew with a lock-only tenant fence | Owned-database negative control retained authentication/membership checks but replaced tenant UPDATE with FOR UPDATE. Both stale owner self-demotions committed and left zero owners. | Repository actual-write fence PASS: exactly one writer succeeds, one known abort, one owner remains. Migration 38's real tenant row UPDATE forces stale RR managers to abort. |
| Accepted row/aggregate/non-data scopes were not enforced | Earlier source-review finding; no pre-fix execution was claimed | Own admission tests reject OWNER scopes on row, aggregate, workflow, event, preview, role-assignment and tenant-access gates. Eight scoped grants admit; nine reject. Entity/field scopes remain SQL predicates. |
| Missing durable worker denial provenance | Earlier source-only finding; implementer pre-fix failure is attributed to its report | Independent actual workflow/action, field, NONE scope, SQL-empty scope and inactive-role regression PASS. Failed business work rolls back; fresh exact-ticket transaction writes only bounded denial evidence. |
| Audit failure falsely inferred lease expiry | Earlier source-only finding | Current `Worker.Step` lines 177–185 returns the fresh audit transaction's error directly. Source inspected; no separate canceled-denial-audit fault was executed in this review. |
| Global IAM role-name privilege drift missed during transactions | Earlier source-only finding | Actual API and worker column SELECT grant drift rejected before callbacks; protected worker guard also returned false. Corrected migration 38 and transaction admission checked. |
| Configurable role grants could admit work rejected by retained SQL role checks | Earlier source-only finding | Public API rejects viewer workflow/event grants and custom IAM/evolution/index management grants. No claim that these adapters now support custom role delegation. |
| SQL-empty denial omitted its effective scope witness | Earlier source-only finding | Effective-scope hash is captured at `workflows.go` lines 183–185; actual SQL-empty denial regression retains definition provenance. This is a data-scope rejection, not proof that the separate action gate denied. |

Tenant access no longer implicitly grants before DENY. Definition scope results
copy condition bytes, child scopes and trusted team slices. Hash mismatches,
case aliases, duplicate/trailing JSON, bootstrap injection, unknown actions,
unsupported scopes and merged budgets are exercised by the fresh policy tests.

## Commands actually executed

All commands ran from `/workspace/Framwork`. Redirection retained local logs;
exit statuses below describe the entire command, including failures.

### Expected pre-fix reproduction — FAIL

```sh
git show 1ae1f27:internal/policy/definition.go > /tmp/r5a-review/definition-1ae1f27.go
git show 1ae1f27:internal/policy/store.go > /tmp/r5a-review/store-1ae1f27.go
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5a-review/baseline-overlay.json -run '^TestIndependentR5StoredEngineTenantBinding$' -v ./tests/integration > /tmp/r5a-review/baseline-engine.txt 2>&1
```

FAIL, 2.310s: `tenant A stored role authorized tenant B via same-name membership`.
Only the two old policy source files were overlaid; current composition and SQL
were retained. This is public Go-port misuse evidence, not an HTTP bypass. Actual
HTTP controls correctly allowed A and denied B.

### Focused adversarial/integration run — initial FAIL, corrected PASS

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5a-review/overlay.json -run '^TestIndependentR5(StoredEngineTenantBinding|ClosedScopeAdmission|OwnerFenceNegativeControl|RawWorkerRoleBodyBoundary)$|^TestM9R(CapabilitiesPublicAPISQLScopesAndRevocation|EventConsumerUsesLiveTenantCapabilities|CapabilityWitnessCASAuditRollbackAndUnknownCommit|ConcurrentOwnerDemotionsCannotRemoveLastOwner|WorkerCapabilityDefinitionConfinementAndLiveRevocation|RoleNameGrantDriftRejectsAPIAndWorkerTransactions|StoredRoleEngineBindsTenantAndActor|ReservedCapabilitiesRejectUnexecutableGrants)$' -v ./tests/integration > /tmp/r5a-review/focused-current.txt 2>&1
```

Initial FAIL, 23.973s. All eight repository roots and three other independent
roots passed. The raw-worker fixture denied `name`, although the workflow reads
`status`; trusted execution legitimately advanced, and subsequent attempts used
the stale lease. This reviewer fixture error was corrected without implementation
changes. Preserve this failure; it is not an implementation defect.

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r5a-review/overlay.json -run '^TestIndependentR5RawWorkerRoleBodyBoundary$' -v ./tests/integration > /tmp/r5a-review/raw-worker-current.txt 2>&1
```

PASS, 4.905s. Correctly denied the declared status field; established the remaining
raw SQL policy boundary in all three variants.

The full focused command above was rerun with the corrected fixture and output
redirected to `/tmp/r5a-review/focused-corrected.txt`: **PASS, 30.639s**, four
independent roots plus eight repository roots. Its actual assertions include:
populated OWNER/TEAM-descendant/SHARE/condition/AND/OR visibility; query filter
and aggregate controls; cross-tenant and forged-token denial; public field deny;
actual publication COMMIT-success-response loss; immutable exact replay after a
new head; stale/conflicting CAS; atomic audit-failure rollback; last-owner
concurrency; confined worker definitions and live revocation; durable denial
hash/instance binding; API/worker privilege drift; tenant/actor binding; and
reserved-grant rejection.

### Fresh pure race checks — PASS

```sh
./scripts/go.sh test -race -count=1 ./internal/policy ./internal/tenant ./internal/runtime/workflow ./internal/platform/postgres ./internal/events ./tests/security > /tmp/r5a-review/unit-current.txt 2>&1
```

All six packages PASS, uncached `-count=1`. These checks supplement, rather than
replace, the actual database/HTTP tests.

### Ordinary isolated performance gates — PASS

```sh
make test-performance > /tmp/r5a-review/query-performance-current.txt 2>&1
make test-write-performance > /tmp/r5a-review/write-performance-current.txt 2>&1
```

Executed sequentially, after correctness tests, with no root workload. Both use
the actual R5 policy loader. Verified reference: PostgreSQL 18.6, 4 CPU, 8 GiB,
swap disabled, image
`sha256:29754c7520f4b9654cf8ff98e19ac73b88e255c3a53f6236ebe9d52249bb6df3`.
No HTTP/SQL/pool/latency budget was raised.

| Gate | Executed result |
| --- | --- |
| Indexed read: 100k hot rows, ten tenants, nine other 10k datasets, average 761 bytes; 10 clients/100 actual HTTP samples | PASS, 61.861s total; HTTP p50 82.953, p95 215.675, p99 263.940 ms; 100.24/s; zero errors. Unchanged 750 ms p95 gate. |
| SQL 100 samples per query | PASS; p95 key 1.513, numeric 153.994, selective 114.798 ms. |
| Distinct-record write: 32 clients, 60.157s | PASS, 62.242s total; 9,177 successes, zero errors; p50 203.494, p95 260.225, p99 327.716 ms; 152.55/s. Unchanged 500 ms p95 gate; pool wait count/duration zero; final facts/receipt checks pass. |

These are the governed fixtures, not production certification or a guarantee
for maximum-complexity custom policies, all query shapes, dense relationship
graphs, arbitrary hardware, full cold-cache behavior or every tenant size.
Prior performance failures remain in the earlier milestone reports; these
measurements do not relabel those observations.

### Actual workflow startup/audit prerequisite — PASS

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9WorkflowOnlyStartupRequiresAuditAndRunsWithoutEventBroker$' -v ./tests/integration > /tmp/r5a-review/startup-current.txt 2>&1
```

PASS, 7.830s. An owned worker starts unsuccessfully when its mandatory audit
column privilege is revoked; after restoration actual configured dispatcher and
worker execute the workflow and shut down. No event broker dependency is needed.

### NOT RUN by this reviewer

Full repository `make check test build`, browser E2E, remote CI, production
migration/deployment, human business acceptance, OS/process-secret isolation,
raw event role-body enforcement, full custom-workflow approval references,
masking/partial-update semantics and record/team/share administration.

Implementer reports full suite PASS at `1ae1f27` (586.315s) after an earlier
WorkerLifecycle timing failure, plus final focused checks/build and browser PASS.
Those results belong to its report; they are not independent executions here.

## Remaining acceptance limits

Full R5 must close the raw worker policy boundary above. It also still needs
sanctioned record/team/share administration, custom workflow approval-role
references, masking/partial updates and the corresponding UI/identity work.
Existing assignment CAS compares a role label, not a membership revision/ABA
witness; lost assignment responses require membership reconciliation. Tenant
owners and bootstrap roles remain explicitly broad. An immutable engine's
actor/tenant binding does not make its definition fresh indefinitely: callers
must continue loading it per transaction.

The new role-management API is tested through real handlers/database roles, but
no complete administration frontend or production acceptance is claimed. No M10
or external effects were implemented. Full-R2/R3c/R3d/R6/R7 gates are not closed
by this review. The bounded R5a PASS must not erase the confirmed full-R5 HIGH.

## Reproducibility witnesses

Scratch paths are ephemeral. The independent probe source is embedded below so
it remains reviewable after environment replacement. Recreate overlay mappings
from `tests/integration/independent_r5a_review_test.go` to that scratch file; for
the old engine negative also map only `internal/policy/definition.go` and
`internal/policy/store.go` to the `git show 1ae1f27` outputs. The final probe file
removes an unexecuted preparatory viewer helper; all four executed probes are
unchanged. The exact larger source used for the corrected combined run is
retained locally as `probes-executed.go.txt`.

| Witness | SHA256 |
| --- | --- |
| Final four-case `probes_test.go` | `ed91d922cfb371ac15ad59e34250fb57027c7022eb1471d2a9e9ce80bac24d3a` |
| Corrected execution source `probes-executed.go.txt` | `35a21123aaf02bd241d7dbbeff55f885c07310c9fbc1d96869ea0afe5d07d37d` |
| Initial fixture source | `d1bc0e3df9d8580992e7ec414f83e7878563116b9c568c7afc90121c1af16b16` |
| Old definition.go | `6799d3237c852ce622f29b2192252662f4a13adc3ec504fd078a5ef6ec0516b5` |
| Old store.go | `2fb85d1cc5d71e98035aaab85f91c812d98f5a08ab82b0b1c7d30796461ffe1e` |
| Current overlay.json | `bd6ebb1b6b7d6e810cc9105b25ee576b57a214d39c08671a8e1d6ef5d4e6c443` |
| Baseline overlay.json | `0c521126976942b438a6fe978d236f49bc0ba0aed10932ce8f2bf4cd61e383c1` |

At handoff no reviewer test, Go, API, dispatcher, worker or Vite processes remain.
Only this independent report is a reviewer repository edit. CPU/database ownership
was released before subsequent implementation activity.

### Independent probe source

```go
//go:build integration

package integration

import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/Karthikeyan49/Framwork/internal/audit"
 "github.com/Karthikeyan49/Framwork/internal/events"
 "github.com/Karthikeyan49/Framwork/internal/platform/access"
 "github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
 "github.com/Karthikeyan49/Framwork/internal/identity"
 "github.com/Karthikeyan49/Framwork/internal/platform/postgres"
 "github.com/Karthikeyan49/Framwork/internal/policy"
 "github.com/Karthikeyan49/Framwork/internal/tenant"
)

// The normal HTTP composition is a control: it reloads B and denies. This
// exercises the public stored-engine contract rather than claiming HTTP bypass.
func TestIndependentR5StoredEngineTenantBinding(t *testing.T) {
 m8Database(t)
 f := newIAM(t)
 h := r5Handler(t, f, audit.Postgres{})
 selector := policy.Selector{AppID:"*",EntityID:"*",FieldID:"*"}
 a := policy.Definition{SchemaVersion:policy.DefinitionVersion,Name:"finance_manager",Grants:[]policy.Grant{{Effect:"ALLOW",Actions:[]string{"memberships.read"},Resource:selector}}}
 b := policy.Definition{SchemaVersion:policy.DefinitionVersion,Name:"finance_manager",Grants:[]policy.Grant{}}
 alice,bob,viewer := f.token(t,f.aliceSubject,nil), f.token(t,f.bobSubject,nil), f.token(t,f.viewerSubject,nil)
 r5Publish(t,h,"/v1/tenants/"+f.tenantA+"/roles",alice,0,a,true,uuid(t))
 r5Publish(t,h,"/v1/tenants/"+f.tenantB+"/roles",bob,0,b,true,uuid(t))
 r5Assign(t,h,"/v1/tenants/"+f.tenantA,alice,f.viewer,"finance_manager",200)
 if _,e:=f.operator.Exec(`INSERT INTO iam.memberships(tenant_id,user_id,role,active) VALUES($1,$2,'finance_manager',true)`,f.tenantB,f.viewer);e!=nil {t.Fatal("private same-name membership",e)}
 if w:=m3Call(h,"GET","/v1/tenants/"+f.tenantA+"/members",viewer,0,nil);w.Code!=200 {t.Fatal("A allow control",w.Code)}
 if w:=m3Call(h,"GET","/v1/tenants/"+f.tenantB+"/members",viewer,0,nil);w.Code!=403 {t.Fatal("B deny control",w.Code)}
 principal,e:=f.verifier.Verify(viewer);if e!=nil {t.Fatal("principal")}
 var engine policy.Engine
 if e=postgres.WithRuntime(context.Background(),f.db,func(ctx context.Context,q postgres.Executor)error {
  access,e:=m3Scope(f,ctx,q,principal,f.tenantA);if e!=nil{return e}
  engine,e=(policy.RoleStore{}).Load(ctx,q,access);return e
 });e!=nil {t.Fatal("A load",e)}
 if e=postgres.WithRuntime(context.Background(),f.db,func(ctx context.Context,q postgres.Executor)error {
  access,e:=m3Scope(f,ctx,q,principal,f.tenantB);if e!=nil{return e}
  user,e:=(identity.Store{}).Resolve(ctx,q,principal);if e!=nil{return e}
  decision:=engine.Decide(ctx,policy.Subject{User:user,Access:access},"memberships.read",policy.Resource{TenantID:f.tenantB,Type:"membership"},policy.Context{})
  if decision.Allow {t.Fatal("tenant A stored role authorized tenant B via same-name membership")}
  return nil
 });e!=nil {t.Fatal("B request",e)}
}

func TestIndependentR5ClosedScopeAdmission(t *testing.T) {
 selector:=policy.Selector{AppID:"*",EntityID:"*",FieldID:"*"}
 for _,action:=range []string{"data.row.read","data.aggregate.read","workflows.execute","events.consume","preview.read","roles.assign","tenant.access"} {
  t.Run(action,func(t *testing.T){
   d:=policy.Definition{SchemaVersion:policy.DefinitionVersion,Name:"finance_manager",Grants:[]policy.Grant{{Effect:"ALLOW",Actions:[]string{action},Resource:selector,Scope:&policy.DataScope{Kind:"OWNER"}}}}
   raw,_:=json.Marshal(d);if _,e:=policy.CompileDefinition(raw,"");e==nil {t.Fatal("unused scope accepted")}
  })
 }
 for _,n:=range []int{8,9} {
  d:=policy.Definition{SchemaVersion:policy.DefinitionVersion,Name:"finance_manager",Grants:[]policy.Grant{}}
  for i:=0;i<n;i++ {d.Grants=append(d.Grants,policy.Grant{Effect:"ALLOW",Actions:[]string{"data.entity.read"},Resource:selector,Scope:&policy.DataScope{Kind:"OWNER"}})}
  raw,_:=json.Marshal(d);_,e:=policy.CompileDefinition(raw,"");if (n==8)!=(e==nil) {t.Fatal("merged scope budget",n,e)}
 }
}

var _ tenant.Access

// Only an owned disposable database is weakened, to demonstrate that a pure
// tenant row lock is not an RR write-skew fence. No repository migration changes.
func TestIndependentR5OwnerFenceNegativeControl(t *testing.T) {
 m8Database(t)
 f,_,_,alice,app:=m5Fixture(t)
 if e:=(tenant.Store{}).Provision(context.Background(),f.operator,f.tenantA,"Roles",f.bob,"owner",true);e!=nil {t.Fatal("owner setup",e)}
 h:=r5Handler(t,f,audit.Postgres{})
 r5Publish(t,h,"/v1/tenants/"+f.tenantA+"/roles",alice,0,r5Definition("finance_manager",app,policy.DataScope{Kind:"OWNER"}),true,uuid(t))
 if _,e:=f.operator.Exec(`CREATE OR REPLACE FUNCTION control.role_manager(t uuid) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR NOT iam.tenant_matches_scope(t) THEN RAISE EXCEPTION 'active tenant owner required' USING ERRCODE='42501'; END IF;
 PERFORM id FROM iam.tenants WHERE id=t FOR UPDATE;
 PERFORM 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role='owner' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'active tenant owner required' USING ERRCODE='42501'; END IF;
 END $$`);e!=nil {t.Fatal("private negative control",e)}
 ready:=make(chan struct{},2);start:=make(chan struct{});results:=make(chan error,2)
 for _,pair:=range []struct{token,user string}{{alice,f.alice},{f.token(t,f.bobSubject,nil),f.bob}} {
  p,e:=f.verifier.Verify(pair.token);if e!=nil {t.Fatal("principal")}
  go func(user string,p identity.Principal){
   results<-postgres.WithRuntime(context.Background(),f.db,func(ctx context.Context,q postgres.Executor)error{
    a,e:=m3Scope(f,ctx,q,p,f.tenantA);ready<-struct{}{};<-start;if e!=nil{return e}
    return (policy.RoleStore{}).Assign(ctx,q,a,user,"finance_manager","owner")
   })
  }(pair.user,p)
 }
 <-ready;<-ready;close(start)
 first,second:=<-results,<-results
 if first!=nil || second!=nil {t.Fatal("negative control did not execute both stale writers",first,second)}
 var owners int
 if e:=f.operator.QueryRow(`SELECT count(*) FROM iam.memberships WHERE tenant_id=$1 AND role='owner' AND active`,f.tenantA).Scan(&owners);e!=nil||owners!=0 {t.Fatal("negative control did not reproduce lost owner",e,owners)}
 t.Log("lock-only negative control: both stale owner demotions committed; zero owners (disposable DB only)")
}

// This records the deliberately unresolved split: trusted Go enforces current
// capability bodies; the raw credential source port enforces exact job/pin only.
func TestIndependentR5RawWorkerRoleBodyBoundary(t *testing.T) {
 m:=m9Setup(t,nil)
 instance:=m.start(t,"raw-role-boundary")
 h:=r5Handler(t,m.f,audit.Postgres{})
 root:="/v1/tenants/"+m.f.tenantA
 allowed:=r5Definition("finance_manager",m.app,policy.DataScope{Kind:"ALL"})
 allowed.Grants=append(allowed.Grants,policy.Grant{Effect:"ALLOW",Actions:[]string{"workflows.execute","workflows.activity"},Resource:policy.Selector{AppID:m.app,EntityID:"entity.contact",FieldID:"*"}})
 v:=r5Publish(t,h,root+"/roles",m.token,0,allowed,true,uuid(t))
 r5Assign(t,h,root,m.token,m.f.bob,"owner",200)
 r5Assign(t,h,root,m.approver,m.f.alice,"finance_manager",200)
 lease,found,e:=m.worker.Claim(context.Background());if e!=nil||!found {t.Fatal("lease",e)}
 m.worker.Store=access.WorkerWorkflowStore(policy.Runtime{Worker:true},audit.Postgres{},events.Postgres{},func(ctx context.Context,q postgres.Executor,a tenant.Access)(identity.User,error){
  return (identity.Store{}).ResolveWorker(ctx,q,lease.Ticket.JobID())
 })
 fieldDeny:=r5Definition("finance_manager",m.app,policy.DataScope{Kind:"ALL"})
 fieldDeny.Grants=append(fieldDeny.Grants,allowed.Grants[len(allowed.Grants)-1],policy.Grant{Effect:"DENY",Actions:[]string{"data.field.read"},Resource:policy.Selector{AppID:m.app,EntityID:"entity.contact",FieldID:"field.contact.status"}})
 team:=r5Definition("finance_manager",m.app,policy.DataScope{Kind:"TEAM"})
 team.Grants=append(team.Grants,allowed.Grants[len(allowed.Grants)-1])
 for _,tc:=range []struct{name string;d policy.Definition;active bool}{{"field-denied",fieldDeny,true},{"team-scope-none",team,true},{"inactive",allowed,false}} {
  t.Run(tc.name,func(t *testing.T){
   v=r5Publish(t,h,root+"/roles",m.approver,v.Version,tc.d,tc.active,uuid(t))
   if e=m.worker.Step(context.Background(),lease);!errors.Is(e,workflow.ErrDenied) {t.Fatal("trusted worker control failed",e)}
   tx,e:=m.db.BeginTx(context.Background(),nil);if e!=nil {t.Fatal("raw credential transaction",e)};defer tx.Rollback()
   var bound bool
   if e=tx.QueryRow(`SELECT control.bind_worker_job($1::uuid,$2::uuid,'workflow',$3,$4::uuid,$5)`,m.f.tenantA,instance.ID,lease.Generation,lease.Token,lease.Ticket.ReleaseHash()).Scan(&bound);e!=nil||!bound {t.Fatal("raw exact-ticket binding unexpectedly unavailable",e,bound)}
   var rows int;var exposes bool
   if e=tx.QueryRow(`SELECT count(*),coalesce(bool_and(custom_data ? 'field.contact.status'),false) FROM runtime.worker_source_record()`).Scan(&rows,&exposes);e!=nil||rows!=1||!exposes {t.Fatal("raw bounded source witness changed",e,rows,exposes)}
   var generic int
   if e=tx.QueryRow(`SELECT count(*) FROM runtime.records`).Scan(&generic);e==nil {t.Fatal("generic worker access unexpectedly available")}
   _=tx.Rollback()
   var denials int
   if e=m.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND NOT allowed AND policy_version=$2 AND state->>'workflow_instance_id'=$3`,m.f.tenantA,"tenant-role-v1:"+v.Hash,instance.ID).Scan(&denials);e!=nil||denials!=1 {t.Fatal("trusted denial evidence",e,denials)}
   t.Log("trusted Step denied with durable exact-version audit; raw credential retained one pinned record and declared field; generic table denied")
  })
 }
}

```
