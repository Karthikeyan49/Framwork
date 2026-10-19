# M9R R2c independent review

**Verdict: PASS for the reviewed preview schema-evolution foundation at `4958df91fe7e634e1c3281ceaa8deec1ea4d6116`.** No unresolved BLOCKER, HIGH or MEDIUM finding was identified in this scope. This verdict does not certify production release authority, custom business permissions, external effects, operating-system process isolation, or the remaining R3c/R3d/R5–R7 gates.

## Scope and independence

Reviewed ADR026/ADR031, migrations35–37, generation/identity views and SQL ports, shadow preparation/copy/link/verification/activation, API/policy/audit composition, workflow data pins, exact worker source/share ports, receipts and event lineage. The implementation was frozen during execution; the implementer ran no concurrent workload. All probes used verified-target disposable databases and controlled fixture records. Applied migrations35–37 were not edited. No shared service or Redis restart occurred. This report is the reviewer's sole repository edit.

The reviewer independently authored fault, authorization, worker, capacity and mapped-query overlays. Repository shadow regressions were also executed independently. An ordinary build was used for capacity/performance; fault/security regressions used `-race`. Temporary paths are under `/tmp/r2-review`; complete source witnesses are embedded below so environment restoration cannot erase the reproduction.

## Commands actually executed

All commands ran from `/workspace/Framwork` at the stable commit above. Each test invocation used `-count=1`. Workloads ran sequentially.

| Exact command | Result |
|---|---|
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r2-review/overlay.json -run '^TestIndependentR2\|^TestM9RShadow' -v ./tests/integration > /tmp/r2-review/correctness-4958df9.txt 2>&1` | PASS,52.927s: three independently authored roots and all six repository shadow roots. |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -count=1 -timeout=20m -overlay /tmp/r2-review/overlay.json -run '^TestIndependentR2HundredThousand' -v ./tests/integration > /tmp/r2-review/capacity-4958df9.txt 2>&1` | PASS,545.375s: actual adoption and schema migration of100,000 records. |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -count=1 -timeout=5m -overlay /tmp/r2-review/overlay.json -run '^TestIndependentR2MappedGeneration' -v ./tests/integration > /tmp/r2-review/mapped-query-4958df9.txt 2>&1` | PASS,29.073s: separate mapped/canonical generation2 query/index workload. |
| `make test-performance > /tmp/r2-review/baseline-read-4958df9.txt 2>&1` | PASS,44.894s: unchanged ordinary numeric/read gate. |
| `make test-write-performance > /tmp/r2-review/baseline-write-4958df9.txt 2>&1` | PASS,61.517s: unchanged32-client/60-second write gate. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r2-review/overlay.json -run '^TestIndependentR2Canonical' -v ./tests/integration > /tmp/r2-review/final-guards-4958df9.txt 2>&1` | PASS,5.576s: canonical team FK, worker grant drift and tombstone enforcement. |

No independent execution failed at this stable checkpoint. Earlier concerns below were source-review findings, not pre-fix executed attacks. The implementer's initial unmigrated-development-schema regression failures and old generic-worker-share fixture failure are retained in its implementation report; this reviewer did not execute those failures. The implementer reported complete `make check test build` PASS with integration331.945s, frontend29 tests, Node5 tests and all builds. Those are attributed results, not reviewer full-suite runs.

Independent full repository suite, browser, remote CI, main merge, production deployment and recovery from a real production backup: **NOT RUN** by this reviewer. Browser ownership was explicitly returned to the implementer after these workloads stopped.

## Fault and authorization evidence

The independently authored `TestIndependentR2LostCommitBoundaries` dropped actual PostgreSQL COMMIT success replies after the database emitted them, separately for prepare, copy checkpoint and activation. Each public request returned503; authenticated GET recovered the exact committed operation/checkpoint. Prepare and activation replay did not duplicate bindings or operations. A stale copy checkpoint returned409 without repeating its effects. The final operation/binding counts were one/two.

`TestIndependentR2ManagementDenialIsAtomic` supplied signed token role/tenant claims that disagreed with live membership. Viewer and foreign-tenant actors received403; rejected prepare left the dataset OPEN with no operation, and rejected advance left checkpoint1/copied_rows0. The actual owner request provided the positive control. No token or credential values were printed.

`TestIndependentR2WorkerPinnedGenerationUsesCurrentShares` started a workflow on generation1, activated generation2 and exercised its exact database capability. The source port still returned the pinned physical hash. Its narrow share port returned the legitimate current share. Revoking the share through the current generation denied the old pinned worker even though the old physical share remained. No workflow business effect occurred.

The final guard probe established: a foreign-tenant canonical team assignment fails23503; a safe worker is admitted; adding a column-level generic identity grant causes admission failure and raw RLS returns zero rows; revoking it recovers normal capability binding. An old pinned source returned one legitimate record, then zero after a current logical tombstone. The zero-business-effects assertion passed.

Independent execution of the six repository roots also covered populated exact transforms, cyclic/self-compatible reference reconstruction, immutable headers/versions, source retention, causal sequencing/source sidecars, old-reader snapshots, sealed old writes, current tombstones, ACL-epoch re-verification, pre-cutover abort, both workflow generations, controlled concurrent distinct deletes, missing-operation SQL rejection, unexpected raw target detection, witness immutability, share revocation, conversion and audit rollback.

## Source-review findings and closure

| Initial concern | Correction and executed closure |
|---|---|
| HIGH reliability: shared dataset gate upgraded during unrelated DELETEs. | OPEN mirrors no longer update the dataset epoch. Frozen maintenance updates invalidate verification. `TestM9RShadowConcurrentDistinctDeletesDoNotUpgradeDatasetGate` passed3.44s with controlled concurrent transactions. |
| MEDIUM SQL fail-closed gap: missing operation could pass NULL guards in ABORT. | Explicit missing-operation guards were added. Direct SQL invalid-ABORT regression passed; the dataset remained FROZEN. HTTP rollback was not used to conceal the SQL result. |
| MEDIUM verification gap: hidden unmapped target rows could escape view-based counts. | Final physical counts and identity completeness use raw records. An extra raw destination inserted after copy prevented verification and left its checkpoint unchanged. |
| Canonical team key lacked tenant-inclusive FK. | FK added; independent actual foreign-team UPDATE failed23503. |
| Final namespace scans could violate the normal2-second statement budget. | The actual100k rehearsal below completed through final verification without raising budgets. This is a measured envelope, not a proof for arbitrary sizes or edge distributions. |

## Actual migration capacity observation

The fixture verified the connected reference PostgreSQL18.6 container and existing limits: four CPUs,8GiB, swap disabled, image `sha256:29754c7520f4b9654cf8ff98e19ac73b88e255c3a53f6236ebe9d52249bb6df3`. It seeded a100,000-record hot namespace plus nine other tenants with10,000 records each. Hot records averaged886 bytes. There were no populated reference or share edges in this capacity fixture; functional edge cases were exercised separately.

| Observation | Result |
|---|---:|
| Actual adoption |1000 public API verification batches;36.414s including setup between recorded phase timers|
| Actual COPY/LINK/VERIFY and activation/read checks |3000 advance batches;499.343s|
| Longest measured evolution API batch |422.669ms|
| Copy/verification counts |100,000 each; ordered manifests equal|
| Retained raw records/logical identities |200,000 physical records;100,000 identities|
| Other tenants after migration |90,000 records unchanged|
| Original and target reads |Original ID/version/name retained; target typed backfill admitted|

All normal2-second SQL and3-second transaction deadlines remained. The test-runner20-minute timeout was only an outer rehearsal bound, not an increased application/SQL budget. There was no timeout or visible API failure. Complete verification recomputed every transformed row/header/link expectation; the displayed original-record read was an additional endpoint control, not a substitute for that verification.

This does not establish capacity for dense reference/share graphs, all32-field layouts, maximum-size records, simultaneous migrations, heavily selective policy scopes or larger datasets. Writes remain frozen during evolution; roughly8.3 minutes for this evolution is a real availability cost that must remain visible in operational planning. Final scans still use ordinary deadlines and may block larger migrations; no universal scale guarantee is claimed.

## Post-cutover query and preserved gates

A separate fixture performed a real one-record cutover, then used trusted operator setup to seed another99,999 post-cutover generation2 records and canonical identities. This synthetic growth is query/index evidence, **not** a second100k migration proof. It used the same constraints, mapping, canonical views and public API. After fixture statistics, its public money index copied and verified100,000 records in203 checkpoints/16.802s. The fixture refreshed projection statistics after index completion.

| Workload | p50 | p95 | p99 | Throughput/errors |
|---|---:|---:|---:|---|
| Canonical/mapped generation2,10 clients/100 HTTP samples |191.722ms|270.905ms|310.172ms|49.77/s;0 errors|
| Existing unmapped numeric/read gate,10 clients/100 HTTP samples |71.936ms|130.939ms|156.082ms|125.76/s;0 errors|
| Existing distinct-record write gate,32 clients/60.142s |164.219ms|209.973ms|238.472ms|190.95/s;11,484 successes;0 errors|

Mapped pages asserted exact IDs, money amount/currency, required integer backfill, ordering and tie/cursor continuation. Actual generic execution used `numeric_projection_asc`,101 rows/one loop, and canonical identity PK lookups of one row across101 loops in each of two stages. This tested actual mapped authorization/view joins rather than assuming the baseline namespace represented them.

The unchanged baseline SQL p95 observations were key0.662ms, numeric105.574ms and selective85.244ms; its governed index took13.665s. The write gate verified receipt/business/audit/outbox counts and observed pool wait0. HTTP thresholds750ms for reads and500ms for writes, pool10, client/sample counts and the reference tier were not increased. These are bounded synthetic observations, not generalized production SLO certification.

## Remaining boundaries

Every unequal schema, including optional additions, uses a retained shadow generation; only identical storage hashes share their anchor. In-place optional-field reader projection is intentionally not claimed. Old bindings remain typed read-only snapshots with current logical access/tombstones. Post-cutover abort is refused; reverse evolution or backup/reconciliation needs a separate plan. Shadows, source generations, witnesses, receipts and event evidence remain retained.

Dataset evolution is an authorized preview operation, not production promotion or independent acceptance authority. Native roles/scopes remain the known R5 limitation. External tools, data-migration scheduling/quarantine UI, automatic index inheritance, typed ERP Domain Pack persistence, operating-system hostile-process isolation and R3c/R3d/R5–R7 remain separate work. Worker safety assumes its process holds only its assigned credentials, preserving the R1 threat model.

Reviewer workloads and owned listeners stopped before the implementer's browser checks began. Read-only process observation after handoff saw the implementer's Go/API processes; the reviewer did not stop them. No implementation or migration edit was made, and no outstanding finding from this review remains.

## Persistent probe-source witnesses

The embedded sources depend on the repository's fixture helpers at the reviewed commit. They are independent review harnesses, not production capabilities. `probes_test.go` accumulated the final FK/drift/tombstone root after the first combined correctness run; the latter final root was executed separately as recorded above. Performance sources are build-tagged and were excluded from race correctness runs.

| Original temporary source | SHA256 of final source |
|---|---|
| `/tmp/r2-review/probes_test.go` | `f46b085e2610d9e997e942b00725bfb4c902cf02ab6391336dbdcc2305b2020f` |
| `/tmp/r2-review/capacity_test.go` | `149a9d4cccd7be22c06ea9344c12aa58402f009fee0465f33082ea1a2db62460` |
| `/tmp/r2-review/mapped_test.go` | `fa6a0d7a27a048ed1969349aca64844c5a900786188c9b964d75dd414e2e52ba` |
| `/tmp/r2-review/overlay.json` | `5d6d5266f378f9f40288221c1454869f755eb58c4d1eacb8b73935ec8a631421` |

### overlay.json

```json
{"Replace":{"/workspace/Framwork/tests/integration/independent_r2_test.go":"/tmp/r2-review/probes_test.go","/workspace/Framwork/tests/integration/independent_r2_capacity_test.go":"/tmp/r2-review/capacity_test.go","/workspace/Framwork/tests/integration/independent_r2_mapped_test.go":"/tmp/r2-review/mapped_test.go"}}
```

### probes_test.go

```go
//go:build integration
package integration
import (
 "context"
 "encoding/json"
 "errors"
 "github.com/jackc/pgx/v5/pgconn"
 "github.com/Karthikeyan49/Framwork/internal/events"
 "github.com/Karthikeyan49/Framwork/internal/identity"
 "github.com/Karthikeyan49/Framwork/internal/platform/jobs"
 "github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
 "github.com/Karthikeyan49/Framwork/internal/tenant"
 "io"
 "log/slog"
 "os"
 "testing"
 "github.com/Karthikeyan49/Framwork/internal/audit"
 "github.com/Karthikeyan49/Framwork/internal/compiler"
 "github.com/Karthikeyan49/Framwork/internal/metadata"
 "github.com/Karthikeyan49/Framwork/internal/platform/access"
 "github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
 "github.com/Karthikeyan49/Framwork/internal/platform/postgres"
 "github.com/Karthikeyan49/Framwork/internal/policy"
 "github.com/Karthikeyan49/Framwork/internal/runtime/data"
)
func TestIndependentR2LostCommitBoundaries(t *testing.T) {
 m8Database(t)
 f,h,base,token,app:=m5Fixture(t)
 m5Create(t,h,base,token,"Retained source")
 root:="/v1/tenants/"+f.tenantA+"/preview/"+app
 _,path:=shadowAdopt(t,h,token,root)
 raw,err:=os.ReadFile("../../contracts/examples/contacts-draft-v1.json");if err!=nil{t.Fatal(err)}
 witness:=shadowSource(t,h,token,f.tenantA,app,2,raw,func(d *metadata.Document){addCount(d,"integer",false)})
 op:=uuid(t);body:=map[string]any{"operation_id":op,"expected_binding":1,"intent":compiler.EvolutionIntent{}}
 for k,v:=range witness{body[k]=v}
 var progress data.Evolution
 for _,stage:=range []string{"prepare","advance","activate"} {
  request:=path+"/evolutions";var payload any=body
  if stage=="advance" {request+="/"+op+"/advance";payload=map[string]int64{"expected_checkpoint":progress.Checkpoint}}
  if stage=="activate" {progress=shadowReady(t,h,token,path,progress);request+="/"+op+"/activate";payload=map[string]int64{"expected_checkpoint":progress.Checkpoint}}
  proxy,observed:=m8DropCommitResponse(t,os.Getenv("DATABASE_URL"));db,err:=postgres.Open(proxy);if err!=nil{t.Fatal(err)}
  if err=postgres.WithRuntime(context.Background(),db,func(context.Context,postgres.Executor)error{return nil});err!=nil{t.Fatal("prime",err)}
  api:=access.API{DB:db,Auth:f.verifier,Policy:policy.Native{},Audit:audit.Postgres{},Registry:m3Registry(t),PreviewEnabled:true}
  fault:=httpserver.Handler(httpserver.Options{Logger:slog.New(slog.NewJSONHandler(io.Discard,nil)),Register:api.Register,AllowPOST:api.AcceptsPOST})
  w:=m5Call(fault,request,token,payload);_ = db.Close()
  if w.Code!=503||!observed.Load(){t.Fatal("lost commit not observed",stage,w.Code,observed.Load())}
  w=m3Call(h,"GET",path+"/evolutions/"+op,token,0,nil)
  if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&progress)!=nil{t.Fatal("GET reconcile",stage,w.Code)}
  if stage=="prepare" {if progress.State!="COPYING"||progress.Checkpoint!=1{t.Fatal("prepare checkpoint",progress)}}
  if stage=="advance" {if progress.State!="LINKING"||progress.CopiedRows!=1||progress.Checkpoint!=2{t.Fatal("committed copy not reconciled",progress)}}
  if stage=="activate" {if progress.State!="ACTIVATED"{t.Fatal("committed cutover not reconciled",progress)}}
  before:=progress
  replay:=m5Call(h,request,token,payload)
  expected:=200;if stage=="advance"{expected=409}
  if replay.Code!=expected{t.Fatal("exact replay",stage,replay.Code)}
  w=m3Call(h,"GET",path+"/evolutions/"+op,token,0,nil)
  if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&progress)!=nil||progress!=before{t.Fatal("replay changed durable checkpoint",stage)}
 }
 var operations,bindings int
 if err=f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.dataset_evolutions WHERE tenant_id=$1 AND app_id=$2),(SELECT count(*) FROM runtime.dataset_bindings WHERE tenant_id=$1 AND app_id=$2)`,f.tenantA,app).Scan(&operations,&bindings);err!=nil||operations!=1||bindings!=2{t.Fatal("duplicate commit facts",operations,bindings,err)}
}

func TestIndependentR2WorkerPinnedGenerationUsesCurrentShares(t *testing.T) {
 m:=m9Setup(t,nil)
 root:="/v1/tenants/"+m.f.tenantA+"/preview/"+m.app
 d,path:=shadowAdopt(t,m.handler,m.token,root)
 if _,err:=m.f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id,can_write) SELECT $1,$2,anchor_revision,anchor_hash,'entity.contact',$4,$5,true FROM runtime.dataset_generations WHERE tenant_id=$1 AND dataset_id=$3 AND generation=1`,m.f.tenantA,m.app,d.ID,m.record.ID,m.f.alice);err!=nil{t.Fatal(err)}
 m.base=path+"/bindings/1/workflows/workflow.contact.review"
 x:=m.start(t,"old-generation-current-share")
 target:=shadowSource(t,m.handler,m.token,m.f.tenantA,m.app,2,m.raw,func(d *metadata.Document){addCount(d,"integer",false)})
 ready:=shadowReady(t,m.handler,m.token,path,shadowPrepare(t,m.handler,m.token,path,1,target,compiler.EvolutionIntent{}))
 shadowStep(t,m.handler,m.token,path,ready,"activate")
 if _,err:=m.f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id,can_write) SELECT $1,$2,anchor_revision,anchor_hash,'entity.contact',$4,$5,true FROM runtime.dataset_generations WHERE tenant_id=$1 AND dataset_id=$3 AND generation=2`,m.f.tenantA,m.app,d.ID,m.record.ID,m.f.alice);err!=nil{t.Fatal(err)}
 lease,found,err:=m.worker.Claim(context.Background());if err!=nil||!found{t.Fatal(err)}
 if err=jobs.WithJob(context.Background(),m.worker.DB,lease.Ticket,func(ctx context.Context,q postgres.Executor,a tenant.Access)error{
   var shares int;var hash string
   if err:=q.QueryRowContext(ctx,"SELECT count(*) FROM runtime.worker_source_shares()").Scan(&shares);err!=nil{return err}
   if shares!=1{t.Fatal("legitimate exact current share missing",shares)}
   if err:=q.QueryRowContext(ctx,"SELECT ir_hash FROM runtime.worker_source_record()").Scan(&hash);err!=nil{return err}
   if hash!=x.Pin.DataHash{t.Fatal("old pin followed active generation")};return nil
 });err!=nil{t.Fatal(err)}
 if _,err=m.f.operator.Exec(`DELETE FROM runtime.record_shares WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=1 AND ir_hash=(SELECT anchor_hash FROM runtime.dataset_generations WHERE tenant_id=$1 AND dataset_id=$3 AND generation=2) AND id=$4 AND user_id=$5`,m.f.tenantA,m.app,d.ID,m.record.ID,m.f.alice);err!=nil{t.Fatal(err)}
 activity:=m.worker.Store.Activity
 m.worker.Store=access.WorkerWorkflowStore(scopedPolicy{mode:"share"},audit.Postgres{},events.Postgres{},func(ctx context.Context,q postgres.Executor,a tenant.Access)(identity.User,error){var job string;if err:=q.QueryRowContext(ctx,"SELECT current_setting('app.job_id',true)").Scan(&job);err!=nil{return identity.User{},err};return (identity.Store{}).ResolveWorker(ctx,q,job)})
 m.worker.Store.Activity=activity
 err=m.worker.Step(context.Background(),lease)
 if !errors.Is(err,workflow.ErrNotFound)&&!errors.Is(err,workflow.ErrDenied){t.Fatal("historical share granted stale worker access",err)}
 m.counts(t,x,0)
}
func TestIndependentR2ManagementDenialIsAtomic(t *testing.T) {
 m8Database(t)
 f,h,base,token,app:=m5Fixture(t);m5Create(t,h,base,token,"Protected source")
 root:="/v1/tenants/"+f.tenantA+"/preview/"+app;_,path:=shadowAdopt(t,h,token,root)
 raw,err:=os.ReadFile("../../contracts/examples/contacts-draft-v1.json");if err!=nil{t.Fatal(err)}
 witness:=shadowSource(t,h,token,f.tenantA,app,2,raw,func(d *metadata.Document){addCount(d,"integer",false)})
 body:=map[string]any{"operation_id":uuid(t),"expected_binding":1,"intent":compiler.EvolutionIntent{}};for k,v:=range witness{body[k]=v}
 for _,subject:=range []string{f.viewerSubject,f.bobSubject} {
  forged:=f.token(t,subject,map[string]any{"roles":[]string{"owner"},"tenant_id":f.tenantA})
  if w:=m5Call(h,path+"/evolutions",forged,body);w.Code!=403{t.Fatal("token-supplied privilege/tenant accepted",w.Code)}
 }
 var ops int;var state string
 if err=f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.dataset_evolutions WHERE tenant_id=$1 AND app_id=$2),state FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2`,f.tenantA,app).Scan(&ops,&state);err!=nil||ops!=0||state!="OPEN"{t.Fatal("denied prepare mutated dataset",ops,state,err)}
 w:=m5Call(h,path+"/evolutions",token,body);var x data.Evolution
 if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&x)!=nil{t.Fatal("positive manager",w.Code)}
 for _,subject:=range []string{f.viewerSubject,f.bobSubject} {
  if w=m5Call(h,path+"/evolutions/"+x.ID+"/advance",f.token(t,subject,nil),map[string]int64{"expected_checkpoint":x.Checkpoint});w.Code!=403{t.Fatal("unauthorized advance",w.Code)}
 }
 var cp,rows int64
 if err=f.operator.QueryRow(`SELECT checkpoint,copied_rows FROM runtime.dataset_evolutions WHERE tenant_id=$1 AND operation_id=$2`,f.tenantA,x.ID).Scan(&cp,&rows);err!=nil||cp!=1||rows!=0{t.Fatal("denied advance copied/checkpointed",cp,rows,err)}
 shadowStep(t,h,token,path,x,"abort")
}

func TestIndependentR2CanonicalFKWorkerDriftAndTombstone(t *testing.T) {
 m:=m9Setup(t,nil);root:="/v1/tenants/"+m.f.tenantA+"/preview/"+m.app;d,path:=shadowAdopt(t,m.handler,m.token,root)
 m.base=path+"/bindings/1/workflows/workflow.contact.review";x:=m.start(t,"tombstone-current-old-source")
 target:=shadowSource(t,m.handler,m.token,m.f.tenantA,m.app,2,m.raw,func(d *metadata.Document){addCount(d,"integer",false)})
 ready:=shadowReady(t,m.handler,m.token,path,shadowPrepare(t,m.handler,m.token,path,1,target,compiler.EvolutionIntent{}));shadowStep(t,m.handler,m.token,path,ready,"activate")
 team:=uuid(t);if _,err:=m.f.operator.Exec("INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'Owned foreign team')",m.f.tenantB,team);err!=nil{t.Fatal(err)}
 _,err:=m.f.operator.Exec("UPDATE runtime.record_identity SET owner_team_id=$1 WHERE tenant_id=$2 AND dataset_id=$3 AND id=$4",team,m.f.tenantA,d.ID,m.record.ID)
 var pg *pgconn.PgError;if !errors.As(err,&pg)||pg.Code!="23503"{t.Fatal("canonical foreign-team FK did not reject",err)}
 if err=jobs.CheckRole(context.Background(),m.worker.DB,"framework_worker");err!=nil{t.Fatal("worker positive role control",err)}
 if _,err=m.f.operator.Exec("GRANT SELECT(owner_user_id) ON runtime.record_identity TO framework_worker");err!=nil{t.Fatal(err)}
 admissionErr:=jobs.CheckRole(context.Background(),m.worker.DB,"framework_worker")
 rows,rawErr:=m.worker.DB.Query("SELECT owner_user_id FROM runtime.record_identity");visible:=false
 if rawErr==nil{visible=rows.Next();rawErr=rows.Err();_ = rows.Close()}
 _,revokeErr:=m.f.operator.Exec("REVOKE SELECT(owner_user_id) ON runtime.record_identity FROM framework_worker");if revokeErr!=nil{t.Fatal(revokeErr)}
 if admissionErr==nil||rawErr!=nil||visible{t.Fatal("canonical grant drift not fenced",admissionErr,rawErr,visible)}
 lease,found,err:=m.worker.Claim(context.Background());if err!=nil||!found{t.Fatal(err)}
 if err=jobs.WithJob(context.Background(),m.worker.DB,lease.Ticket,func(ctx context.Context,q postgres.Executor,a tenant.Access)error{var count int;if err:=q.QueryRowContext(ctx,"SELECT count(*) FROM runtime.worker_source_record()").Scan(&count);err!=nil{return err};if count!=1{t.Fatal("legitimate old source missing")};return nil});err!=nil{t.Fatal(err)}
 if _,err=m.f.operator.Exec("UPDATE runtime.record_identity SET tombstone=true WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3",m.f.tenantA,d.ID,m.record.ID);err!=nil{t.Fatal(err)}
 if err=jobs.WithJob(context.Background(),m.worker.DB,lease.Ticket,func(ctx context.Context,q postgres.Executor,a tenant.Access)error{var count int;if err:=q.QueryRowContext(ctx,"SELECT count(*) FROM runtime.worker_source_record()").Scan(&count);err!=nil{return err};if count!=0{t.Fatal("old worker source ignored current tombstone")};return nil});err!=nil{t.Fatal(err)}
 m.counts(t,x,0)
}
```

### capacity_test.go

```go
//go:build integration && performance
package integration
import (
 "context"
 "encoding/json"
 "github.com/Karthikeyan49/Framwork/internal/tenant"
 "os"
 "testing"
 "time"
 "github.com/Karthikeyan49/Framwork/internal/compiler"
 "github.com/Karthikeyan49/Framwork/internal/metadata"
 "github.com/Karthikeyan49/Framwork/internal/runtime/data"
)
func TestIndependentR2HundredThousandMigrationRehearsal(t *testing.T) {
 requireM9RReferencePostgres(t)
 m8Database(t)
 f,h,base,token,app:=m5Fixture(t)
 original:=m5Create(t,h,base,token,"Original retained")
 witness:=m9rDatasetWitness(t,h,"/v1/tenants/"+f.tenantA+"/preview/"+app+"/versions/1",token)
 _,err:=f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,1,$3,'entity.contact',gen_random_uuid(),jsonb_build_object('field.contact.name','Migrating '||i||' '||repeat('x',450),'field.contact.status','active','field.contact.balance',jsonb_build_object('amount',(i/7)::text,'currency','USD')),$4,$4,$4 FROM generate_series(1,99999)i`,f.tenantA,app,witness["ir_hash"],f.alice)
 if err!=nil{t.Fatal("owned seed",err)}
 
 others:=[]string{f.tenantB};tx,err:=f.operator.Begin();if err!=nil{t.Fatal(err)}
 for range 8 { id:=uuid(t);if err=(tenant.Store{}).Provision(context.Background(),tx,id,"Owned capacity tenant",f.bob,"owner",true);err!=nil{_ = tx.Rollback();t.Fatal(err)};others=append(others,id) }
 if err=tx.Commit();err!=nil{t.Fatal(err)}
 for _,id:=range others {
  if _,err=f.operator.Exec("INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,1,$3)",id,app,witness["ir_hash"]);err!=nil{t.Fatal(err)}
  if _,err=f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id) SELECT $1,$2,1,$3,'entity.contact',gen_random_uuid(),jsonb_build_object('field.contact.name','Other owned tenant','field.contact.status','active','field.contact.balance',jsonb_build_object('amount','1','currency','USD')),$4,$4,$4 FROM generate_series(1,10000)`,id,app,witness["ir_hash"],f.bob);err!=nil{t.Fatal(err)}
 }
 if _,err=f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.record_shares`);err!=nil{t.Fatal(err)}

 var width int;var version string
 if err=f.operator.QueryRow(`SELECT current_setting('server_version'),avg(pg_column_size(r))::int FROM runtime.records r WHERE tenant_id=$1 AND app_id=$2`,f.tenantA,app).Scan(&version,&width);err!=nil{t.Fatal(err)}
 t.Logf("capacity_fixture PG=%s rows=100000 tenant_count=10 other_rows_each=10000 avg_record_bytes=%d; no populated reference/share edges; ordinary API budgets retained",version,width)
 root:="/v1/tenants/"+f.tenantA+"/preview/"+app
 d:=m9rDatasetCreate(t,h,root+"/datasets",token,"ADOPT",witness)
 path:=root+"/datasets/"+d.ID
 adoptionStart:=time.Now();adoptionBatches:=0
 for d.State!="OPEN"&&adoptionBatches<1002 {
   w:=m5Call(h,path+"/verify",token,struct{}{});if w.Code!=200{t.Fatal("adoption capacity",adoptionBatches,w.Code)}
   if json.Unmarshal(w.Body.Bytes(),&d)!=nil{t.Fatal("adoption progress")};adoptionBatches++
   if adoptionBatches%100==0{t.Logf("adoption_progress batches=%d verified=%d elapsed_s=%.3f",adoptionBatches,d.VerifiedRows,time.Since(adoptionStart).Seconds())}
 }
 if d.State!="OPEN"||d.VerifiedRows!=100000{t.Fatal("adoption incomplete",d.State,d.VerifiedRows)}
 raw,err:=os.ReadFile("../../contracts/examples/contacts-draft-v1.json");if err!=nil{t.Fatal(err)}
 target:=shadowSource(t,h,token,f.tenantA,app,2,raw,func(d *metadata.Document){addCount(d,"integer",true)})
 intent:=compiler.EvolutionIntent{Backfills:[]compiler.FieldBackfill{{EntityID:"entity.contact",FieldID:"field.contact.count",When:"MISSING_OR_NULL",Value:json.RawMessage(`7`)}}}
 x:=shadowPrepare(t,h,token,path,1,target,intent)
 evolutionStart:=time.Now();batches:=0;maxBatch:=time.Duration(0)
 for x.State!="READY"&&batches<3105 {
   priorPhase:=x.State;start:=time.Now();w:=m5Call(h,path+"/evolutions/"+x.ID+"/advance",token,map[string]int64{"expected_checkpoint":x.Checkpoint});elapsed:=time.Since(start);if elapsed>maxBatch{maxBatch=elapsed}
   if w.Code!=200{t.Fatalf("migration capacity failure phase=%s checkpoint=%d copied=%d verified=%d status=%d elapsed_ms=%.3f",priorPhase,x.Checkpoint,x.CopiedRows,x.VerifiedRows,w.Code,float64(elapsed.Microseconds())/1000)}
   if json.Unmarshal(w.Body.Bytes(),&x)!=nil{t.Fatal("evolution progress")};batches++
   if x.State!=priorPhase||batches%100==0{t.Logf("evolution_progress batches=%d phase=%s checkpoint=%d copied=%d verified=%d elapsed_s=%.3f max_batch_ms=%.3f",batches,x.State,x.Checkpoint,x.CopiedRows,x.VerifiedRows,time.Since(evolutionStart).Seconds(),float64(maxBatch.Microseconds())/1000)}
 }
 if x.State!="READY"||x.CopiedRows!=100000||x.VerifiedRows!=100000||x.SourceManifest!=x.TargetManifest{t.Fatal("100k not verified",x)}
 x=shadowStep(t,h,token,path,x,"activate")
 for binding:=1;binding<=2;binding++ {
   url:=path+"/bindings/1/entities/entity.contact/query";projection:=[]string{"field.contact.name"}
   if binding==2{url=path+"/bindings/2/entities/entity.contact/query";projection=append(projection,"field.contact.count")}
   page:=m5Page(t,m5Call(h,url,token,data.Query{RecordID:original.ID,Projection:projection,Limit:1}))
   if len(page.Records)!=1||page.Records[0].Version!=1||page.Records[0].Values["field.contact.name"]!="Original retained"{t.Fatal("source/target read preservation",binding)}
 }
 var rawCount,identities,otherCount int
 if err=f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2),(SELECT count(*) FROM runtime.record_identity WHERE tenant_id=$1 AND app_id=$2)`,f.tenantA,app).Scan(&rawCount,&identities);err!=nil||rawCount!=200000||identities!=100000{t.Fatal("retained physical/logical counts",rawCount,identities,err)}
 if err=f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id<>$1 AND app_id=$2`,f.tenantA,app).Scan(&otherCount);err!=nil||otherCount!=90000{t.Fatal("unrelated tenants changed",otherCount,err)}
 t.Logf("capacity_complete adoption_batches=%d adoption_s=%.3f migration_batches=%d migration_s=%.3f max_batch_ms=%.3f raw_records=%d identities=%d",adoptionBatches,time.Since(adoptionStart).Seconds()-time.Since(evolutionStart).Seconds(),batches,time.Since(evolutionStart).Seconds(),float64(maxBatch.Microseconds())/1000,rawCount,identities)
 _=base
}
```

### mapped_test.go

```go
//go:build integration && performance
package integration
import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "log/slog"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "sync"
 "testing"
 "time"
 "github.com/Karthikeyan49/Framwork/internal/audit"
 "github.com/Karthikeyan49/Framwork/internal/compiler"
 "github.com/Karthikeyan49/Framwork/internal/metadata"
 "github.com/Karthikeyan49/Framwork/internal/platform/access"
 "github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
 "github.com/Karthikeyan49/Framwork/internal/platform/postgres"
 "github.com/Karthikeyan49/Framwork/internal/policy"
 "github.com/Karthikeyan49/Framwork/internal/runtime/data"
)
func TestIndependentR2MappedGenerationNumericQueryGate(t *testing.T) {
 requireM9RReferencePostgres(t);m8Database(t)
 f,h,base,token,app:=m5Fixture(t);m5Create(t,h,base,token,"Retained original")
 root:="/v1/tenants/"+f.tenantA+"/preview/"+app;d,path:=shadowAdopt(t,h,token,root)
 raw,err:=os.ReadFile("../../contracts/examples/contacts-draft-v1.json");if err!=nil{t.Fatal(err)}
 target:=shadowSource(t,h,token,f.tenantA,app,2,raw,func(d *metadata.Document){addCount(d,"integer",true)})
 intent:=compiler.EvolutionIntent{Backfills:[]compiler.FieldBackfill{{EntityID:"entity.contact",FieldID:"field.contact.count",When:"MISSING_OR_NULL",Value:json.RawMessage(`7`)}}}
 ready:=shadowReady(t,h,token,path,shadowPrepare(t,h,token,path,1,target,intent));shadowStep(t,h,token,path,ready,"activate")
 // Synthetic growth AFTER a one-record real cutover, not100k migration evidence.
 _,err=f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,g.anchor_revision,g.anchor_hash,'entity.contact',gen_random_uuid(),jsonb_build_object('field.contact.name','Mapped '||lpad(i::text,8,'0')||repeat('x',450),'field.contact.status','active','field.contact.count',7,'field.contact.balance',CASE WHEN i%997=0 THEN 'null'::jsonb ELSE jsonb_build_object('amount',(i/7)::text,'currency','USD') END),$4,$4,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END FROM runtime.dataset_generations g CROSS JOIN generate_series(1,99999)i WHERE g.tenant_id=$1 AND g.dataset_id=$3 AND g.generation=2`,f.tenantA,app,d.ID,f.alice,f.viewer)
 if err!=nil{t.Fatal("owned synthetic canonical growth",err)}
 if _,err=f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.record_identity;ANALYZE runtime.dataset_generations;ANALYZE runtime.datasets;ANALYZE runtime.identity_shares`);err!=nil{t.Fatal(err)}
 bound:=path+"/bindings/2/entities/entity.contact"
 start:=time.Now();index:=m9rIndexReady(t,h,bound+"/indexes/field.contact.balance",token)
 if index.ScannedRows!=100000||index.VerifiedRows!=100000{t.Fatal("mapped index coverage",index)}
 t.Logf("mapped_synthetic_generation=2 rows=100000 canonical_identities=100000 index_checkpoints=%d index_s=%.3f",index.Checkpoint,time.Since(start).Seconds())
 if _,err=f.operator.Exec(`ANALYZE runtime.numeric_indexes;ANALYZE runtime.numeric_projections`);err!=nil{t.Fatal(err)}
 query:=data.Query{Projection:[]string{"field.contact.name","field.contact.balance","field.contact.count"},Sort:&data.Sort{Field:"field.contact.balance"},Limit:100}
 type expectedRow struct{id,amount string};expected:=[]expectedRow{}
 rows,err:=f.operator.Query(`SELECT r.id::text,r.custom_data->'field.contact.balance'->>'amount' FROM runtime.records r JOIN runtime.dataset_generations g ON g.tenant_id=r.tenant_id AND g.app_id=r.app_id AND g.anchor_revision=r.metadata_revision AND g.anchor_hash=r.ir_hash WHERE g.tenant_id=$1 AND g.dataset_id=$2 AND g.generation=2 ORDER BY (r.custom_data->'field.contact.balance'->>'amount')::numeric ASC NULLS LAST,r.id LIMIT 200`,f.tenantA,d.ID)
 if err!=nil{t.Fatal(err)}
 for rows.Next(){var row expectedRow;if err=rows.Scan(&row.id,&row.amount);err!=nil{t.Fatal(err)};expected=append(expected,row)}
 if err=rows.Err();err!=nil{t.Fatal(err)};_ = rows.Close();if len(expected)!=200{t.Fatal("expected page control")}
 admission,err:=postgres.NewAdmission(10,128);if err!=nil{t.Fatal(err)}
 api:=access.API{DB:f.db,Auth:f.verifier,Policy:policy.Native{},Audit:audit.Postgres{},Registry:m3Registry(t),PreviewEnabled:true,Admission:admission}
 handler:=httpserver.Handler(httpserver.Options{Logger:slog.New(slog.NewJSONHandler(io.Discard,nil)),Register:api.Register,AllowPOST:api.AcceptsPOST})
 server:=httptest.NewServer(handler);defer server.Close();client:=&http.Client{Timeout:4*time.Second}
 call:=func(q data.Query,offset int)(float64,data.Page,error){
   body,_:=json.Marshal(q);request,e:=http.NewRequest("POST",server.URL+bound+"/query",bytes.NewReader(body));if e!=nil{return 0,data.Page{},e};request.Header.Set("Authorization","Bearer "+token);request.Header.Set("Content-Type","application/json")
   start:=time.Now();response,e:=client.Do(request);if e!=nil{return 0,data.Page{},errors.New("owned HTTP transport failure")};defer response.Body.Close();var page data.Page
   if response.StatusCode!=200||json.NewDecoder(io.LimitReader(response.Body,1<<20)).Decode(&page)!=nil||len(page.Records)!=100||page.Next==""{return 0,page,fmt.Errorf("mapped page status/count %d/%d",response.StatusCode,len(page.Records))}
   for i,row:=range page.Records{balance,ok:=row.Values["field.contact.balance"].(map[string]any);if !ok||row.ID!=expected[offset+i].id||balance["amount"]!=expected[offset+i].amount||balance["currency"]!="USD"||fmt.Sprint(row.Values["field.contact.count"])!="7"{return 0,page,errors.New("mapped page content/order/currency mismatch")}}
   return float64(time.Since(start).Microseconds())/1000,page,nil
 }
 for range 3{if _,_,err=call(query,0);err!=nil{t.Fatal(err)}}
 times:=make(chan float64,100);failures:=make(chan error,100);var wg sync.WaitGroup;begin:=time.Now()
 for range 10{wg.Add(1);go func(){defer wg.Done();for range 10{elapsed,_,e:=call(query,0);if e!=nil{failures<-e}else{times<-elapsed}}}()};wg.Wait();close(times);close(failures)
 for e:=range failures{t.Error(e)};samples:=[]float64{};for elapsed:=range times{samples=append(samples,elapsed)}
 if len(samples)!=100{t.Fatal("mapped100sample gate incomplete",len(samples))};if reportLatency(t,"mapped_generation2_numeric_http_10clients",samples,time.Since(begin))>750{t.Fatal("mapped API p95 exceeds unchanged750ms gate")}
 _,first,err:=call(query,0);if err!=nil{t.Fatal(err)};query.After=first.Next;if _,_,err=call(query,100);err!=nil{t.Fatal("mapped tie/cursor continuation",err)};query.After=""
 principal,_:=f.verifier.Verify(token)
 err=postgres.WithRuntime(context.Background(),f.db,func(ctx context.Context,q postgres.Executor)error{
   a,e:=m3Scope(f,ctx,q,principal,f.tenantA);if e!=nil{return e};scope,e:=(data.DatasetStore{}).Scope(ctx,q,a,app,d.ID,2);if e!=nil{return e}
   observer:=&pagePlanObserver{Executor:q};_,e=(data.Store{}).Query(ctx,observer,scope,a,"entity.contact",query,func(context.Context,string,data.Resource)error{return nil},func(context.Context,string,data.Resource)(policy.DataScope,error){return policy.DataScope{Kind:"ALL"},nil});if e!=nil{return e}
   if _,e=q.ExecContext(ctx,"SET LOCAL plan_cache_mode=force_generic_plan");e!=nil{return e};var plan []byte
   if e=q.QueryRowContext(ctx,"EXPLAIN(ANALYZE,BUFFERS,VERBOSE,FORMAT JSON) "+observer.query,observer.args...).Scan(&plan);e!=nil{return e}
   var roots []map[string]any;if json.Unmarshal(plan,&roots)!=nil{return errors.New("mapped plan parse")};indexed:=false;identities:=false
   var visit func(any);visit=func(node any){switch x:=node.(type){case map[string]any:
    if x["Relation Name"]=="numeric_projections"{t.Logf("mapped_numeric_plan index=%v rows=%v loops=%v",x["Index Name"],x["Actual Rows"],x["Actual Loops"]);indexed=strings.HasPrefix(fmt.Sprint(x["Index Name"]),"numeric_projection_")&&x["Actual Rows"]==float64(101)}
    if x["Relation Name"]=="record_identity"{identities=true;t.Logf("mapped_identity_plan index=%v rows=%v loops=%v",x["Index Name"],x["Actual Rows"],x["Actual Loops"])}
    for _,v:=range x{visit(v)}
   case []any:for _,v:=range x{visit(v)}}};visit(roots[0]);if !indexed||!identities{return errors.New("actual mapped numeric/canonical plan absent")};return nil
 });if err!=nil{t.Fatal("mapped actual generic plan",err)}
}
```

