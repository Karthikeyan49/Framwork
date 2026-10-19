//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Independent probes retain the effective-target disposable fixture checks.
// Product code, already-applied migrations, service data and secrets are untouched.
func aggregateReviewAuthority(ctx context.Context, q postgres.Executor, f *iamFixture, p identity.Principal, app string) (tenant.Access, policy.Engine, data.Guard, data.ScopeResolver, error) {
	var a tenant.Access
	u, err := (identity.Store{}).Resolve(ctx, q, p)
	if err != nil {
		return a, nil, nil, nil, err
	}
	if err = (tenant.Store{}).BindActor(ctx, q, u); err != nil {
		return a, nil, nil, nil, err
	}
	a, err = (tenant.Store{}).Resolve(ctx, q, u, f.tenantA)
	if err != nil {
		return a, nil, nil, nil, err
	}
	engine, err := (policy.RoleStore{}).Load(ctx, q, a)
	if err != nil {
		return a, nil, nil, nil, err
	}
	teams, err := (tenant.Store{}).Teams(ctx, q, a)
	if err != nil {
		return a, nil, nil, nil, err
	}
	resource := func(r data.Resource, typ string) policy.Resource {
		return policy.Resource{TenantID: a.TenantID(), AppID: app, Type: typ, EntityID: r.EntityID, FieldID: r.FieldID, RecordID: r.RecordID, OwnerID: r.OwnerID}
	}
	guard := data.Guard(func(ctx context.Context, action string, r data.Resource) error {
		if !engine.Decide(ctx, policy.Subject{User: u, Access: a}, action, resource(r, strings.Split(action, ".")[1]), policy.Context{}).Allow {
			return data.ErrDenied
		}
		return nil
	})
	resolve := data.ScopeResolver(func(ctx context.Context, action string, r data.Resource) (policy.DataScope, error) {
		typ := "entity"
		if r.FieldID != "" {
			typ = "field"
		}
		return engine.(policy.ScopeEngine).DataScope(ctx, policy.Subject{User: u, Access: a}, action, resource(r, typ), policy.ScopeContext{Teams: teams})
	})
	return a, engine, guard, resolve, nil
}

func aggregateReviewID(t *testing.T, f *iamFixture, number int) string {
	t.Helper()
	var id string
	scalarSQL(t, f.operator.QueryRow(`SELECT md5($1::text)::uuid::text`, fmt.Sprint(number)).Scan(&id))
	return id
}

func aggregateReviewAdopt(t *testing.T, f *iamFixture, h http.Handler, token, app string, rows int) (data.Dataset, string) {
	t.Helper()
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	d := m9rDatasetCreate(t, h, root+"/datasets", token, "ADOPT", m9rDatasetWitness(t, h, root+"/versions/2", token))
	path := root + "/datasets/" + d.ID
	for i := 0; d.State != "OPEN" && i < 50; i++ {
		w := m5Call(h, path+"/verify", token, struct{}{})
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
			t.Fatal("independent aggregate adoption failed", w.Code)
		}
	}
	if d.State != "OPEN" || d.VerifiedRows != int64(rows) {
		t.Fatal("independent aggregate adoption coverage")
	}
	return d, path
}

func aggregateReviewEvolution(t *testing.T, f *iamFixture, h http.Handler, token, app, path string) data.Evolution {
	t.Helper()
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, f.tenantA, app).Scan(&raw))
	witness := shadowSource(t, h, token, f.tenantA, app, 3, raw, func(d *metadata.Document) { addCount(d, "integer", true) })
	intent := compiler.EvolutionIntent{Backfills: []compiler.FieldBackfill{{EntityID: "entity.contact", FieldID: "field.contact.count", When: "MISSING_OR_NULL", Value: json.RawMessage(`7`)}}}
	return shadowReady(t, h, token, path, shadowPrepare(t, h, token, path, 1, witness, intent))
}

func TestIndependentR3dPinnedNamespaceMismatchFailsClosed(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	scratch := aggregateBulkSeed(t, f, h, base, token, app, 6)
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	probe := func(s data.Scope, denied bool) {
		t.Helper()
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			a, engine, guard, resolve, err := aggregateReviewAuthority(ctx, q, f, principal, app)
			if err != nil {
				return err
			}
			p, err := (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", aggregateQuery("count", ""), engine, guard, resolve)
			if denied {
				if !errors.Is(err, data.ErrDenied) || p.Aggregate != nil {
					return errors.New("forged dataset/generation witness admitted")
				}
				return nil
			}
			if err == nil && (p.Aggregate == nil || p.Aggregate.Count != 6) {
				return errors.New("positive scratch/binding witness lost records")
			}
			return err
		})
		scalarSQL(t, err)
	}
	missing, err := data.PinnedScope(scratch.Program(), 2, uuid(t), 1, 1, scratch.DataRevision(), scratch.DataHash())
	scalarSQL(t, err)
	probe(missing, true)
	d, _ := aggregateReviewAdopt(t, f, h, token, app, 6)
	for _, tc := range []struct {
		dataset string
		gen     int64
		deny    bool
	}{{d.ID, 1, false}, {d.ID, 2, true}, {uuid(t), 1, true}} {
		pin, err := data.PinnedScope(scratch.Program(), 2, tc.dataset, 1, tc.gen, scratch.DataRevision(), scratch.DataHash())
		scalarSQL(t, err)
		probe(pin, tc.deny)
	}
	probe(scratch, false) // A scratch Scope must discover its adopted mapping.
}

func TestIndependentR3dSnapshotCutoverPreservesThenRefreshesCanonicalAuthority(t *testing.T) {
	m8Database(t)
	f, h, scratch, owner, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	aggregateBulkSeed(t, f, h, scratch, owner, app, 6)
	m9rIndexReady(t, h, scratch+"/indexes/"+aggregateBalance, owner)
	// Force the retained generation's exact raw fallback for the record whose
	// current-generation amount changes after activation.
	aggregateReviewProjectionMutation(t, f, `UPDATE runtime.numeric_projections SET aggregate_valid_v1=NULL,value_number=999 WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('1')::uuid`, f.tenantA, app, aggregateBalance)
	d, path := aggregateReviewAdopt(t, f, h, owner, app, 6)
	old := path + "/bindings/1/entities/entity.contact"
	ready := aggregateReviewEvolution(t, f, h, owner, app, path)
	h = r5Handler(t, f, audit.Postgres{})
	roles := "/v1/tenants/" + f.tenantA
	r5Publish(t, h, roles+"/roles", owner, 0, r5Definition("finance_manager", app, policy.DataScope{Kind: "OWNER"}), true, uuid(t))
	r5Assign(t, h, roles, owner, f.viewer, "finance_manager", 200)
	viewer := f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}})
	principal, err := f.verifier.Verify(viewer)
	scalarSQL(t, err)
	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(resume) }) }
	defer unblock()
	result := make(chan error, 1)
	go func() {
		result <- postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			a, engine, guard, resolve, err := aggregateReviewAuthority(ctx, q, f, principal, app)
			if err != nil {
				return err
			}
			s, err := (data.DatasetStore{}).Scope(ctx, q, a, app, d.ID, 1)
			if err != nil {
				return err
			}
			for pass := 0; pass < 2; pass++ {
				p, err := (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", aggregateQuery("sum", aggregateBalance), engine, guard, resolve)
				if err != nil {
					return err
				}
				if p.Aggregate == nil || p.Aggregate.Count != 3 || p.Aggregate.Sum != "12" {
					return errors.New("one certified snapshot mixed cutover authority or values")
				}
				if pass == 0 {
					close(paused)
					select {
					case <-resume:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			return nil
		})
	}()
	select {
	case <-paused:
	case err := <-result:
		scalarSQL(t, err)
		t.Fatal("snapshot did not pause")
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot did not establish the original view")
	}
	shadowStep(t, h, owner, path, ready, "activate")
	current := path + "/bindings/2/entities/entity.contact"
	one, six := aggregateReviewID(t, f, 1), aggregateReviewID(t, f, 6)
	r5cChange(t, h, current+"/records/"+one+"/access", owner, data.AccessCommand{Operation: "owner", UserID: f.viewer, ExpectedVersion: 1}, 200)
	if w := m5Call(h, current+"/commands", owner, data.Command{Operation: "patch", ID: one, ExpectedVersion: 2, Values: map[string]any{aggregateBalance: map[string]any{"amount": "101", "currency": "USD"}}}); w.Code != 200 {
		t.Fatal("current-only amount patch", w.Code)
	}
	if w := m5Call(h, current+"/commands", owner, data.Command{Operation: "delete", ID: six, ExpectedVersion: 1}); w.Code != 200 {
		t.Fatal("current logical tombstone", w.Code)
	}
	unblock()
	scalarSQL(t, <-result)
	m9rIndexReady(t, h, current+"/indexes/"+aggregateBalance, owner)
	for _, tc := range []struct {
		base, sum string
	}{{scratch, "7"}, {old, "7"}, {current, "107"}} {
		got := aggregateResult(t, h, tc.base, viewer, aggregateQuery("sum", aggregateBalance))
		if got.Count != 3 || got.Sum != tc.sum {
			t.Fatal("next snapshot lost canonical authority or historical values")
		}
	}
}

func TestIndependentR3dCanonicalShareTeamRevocationAndMissingIdentity(t *testing.T) {
	m8Database(t)
	f, h, scratch, owner, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	scope := aggregateBulkSeed(t, f, h, scratch, owner, app, 6)
	m9rIndexReady(t, h, scratch+"/indexes/"+aggregateBalance, owner)
	_, path := aggregateReviewAdopt(t, f, h, owner, app, 6)
	ready := aggregateReviewEvolution(t, f, h, owner, app, path)
	shadowStep(t, h, owner, path, ready, "activate")
	current, old := path+"/bindings/2/entities/entity.contact", path+"/bindings/1/entities/entity.contact"
	m9rIndexReady(t, h, current+"/indexes/"+aggregateBalance, owner)
	h = r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	version := r5Publish(t, h, root+"/roles", owner, 0, r5Definition("finance_manager", app, policy.DataScope{Kind: "SHARE"}), true, uuid(t)).Version
	r5Assign(t, h, root, owner, f.viewer, "finance_manager", 200)
	viewer := f.token(t, f.viewerSubject, nil)
	check := func(token string, count int, sum string) {
		t.Helper()
		for _, base := range []string{scratch, old, current} {
			got := aggregateResult(t, h, base, token, aggregateQuery("sum", aggregateBalance))
			if got.Count != count || got.Sum != sum {
				t.Fatal("canonical share/team/identity authority was replaced by physical facts")
			}
		}
	}
	check(viewer, 0, "0")
	one := aggregateReviewID(t, f, 1)
	no := false
	r5cChange(t, h, current+"/records/"+one+"/access", owner, data.AccessCommand{Operation: "share", UserID: f.viewer, CanWrite: &no, ExpectedVersion: 1}, 200)
	check(viewer, 1, "1")
	r5cChange(t, h, current+"/records/"+one+"/access", owner, data.AccessCommand{Operation: "unshare", UserID: f.viewer, ExpectedVersion: 2}, 200)
	// A stale-generation physical share must not revive canonical access.
	_, err := f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) VALUES($1,$2,$3,$4,'entity.contact',md5('2')::uuid,$5)`, f.tenantA, app, scope.DataRevision(), scope.DataHash(), f.viewer)
	scalarSQL(t, err)
	check(viewer, 0, "0")
	team := uuid(t)
	r5cSaveTeam(t, h, root, owner, tenant.Team{ID: team, Name: "Independent aggregate team"}, 0, 200)
	r5cMember(t, h, root, owner, team, f.viewer, true, nil, 200)
	r5cChange(t, h, current+"/records/"+one+"/access", owner, data.AccessCommand{Operation: "owner", UserID: f.alice, TeamID: team, ExpectedVersion: 3}, 200)
	r5Publish(t, h, root+"/roles", owner, version, r5Definition("finance_manager", app, policy.DataScope{Kind: "TEAM"}), true, uuid(t))
	check(viewer, 1, "1")
	yes := true
	r5cMember(t, h, root, owner, team, f.viewer, false, &yes, 200)
	check(viewer, 0, "0")
	// Explicit trusted-operator corruption proves that the optimized canonical
	// branch never falls back to the physically retained row on missing identity.
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`ALTER TABLE runtime.record_identity DISABLE TRIGGER retain_record_identity`)
	scalarSQL(t, err)
	_, err = tx.Exec(`DELETE FROM runtime.record_identity WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, one)
	scalarSQL(t, err)
	_, err = tx.Exec(`ALTER TABLE runtime.record_identity ENABLE TRIGGER retain_record_identity`)
	scalarSQL(t, err)
	scalarSQL(t, tx.Commit())
	check(owner, 5, "20")
}

func TestIndependentR3dOrdinaryCallbackPanicRollsBackAndReusesConnection(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	s := aggregateBulkSeed(t, f, h, base, token, app, 6)
	f.db.SetMaxOpenConns(1)
	f.db.SetMaxIdleConns(1)
	var initialJIT, initialMode string
	var beforePID int
	scalarSQL(t, f.db.QueryRow(`SELECT pg_backend_pid(),current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&beforePID, &initialJIT, &initialMode))
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	id := uuid(t)
	var caught any
	var callbackErr error
	func() {
		defer func() { caught = recover() }()
		callbackErr = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			a, engine, guard, resolve, err := aggregateReviewAuthority(ctx, q, f, principal, app)
			if err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `SELECT set_config('jit','off',true),set_config('plan_cache_mode','force_custom_plan',true)`); err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data) VALUES($1,$2,2,$3,'entity.contact',$4,'{"field.contact.name":"Uncommitted callback fixture","field.contact.status":"active"}'::jsonb)`, f.tenantA, app, s.Hash(), id); err != nil {
				return err
			}
			original := guard
			guard = func(ctx context.Context, action string, r data.Resource) error {
				if action == "data.aggregate.read" {
					panic("independent ordinary callback panic")
				}
				return original(ctx, action, r)
			}
			_, err = (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", aggregateQuery("count", ""), engine, guard, resolve)
			return err
		})
	}()
	if caught != "independent ordinary callback panic" {
		scalarSQL(t, callbackErr)
		t.Fatal("ordinary callback panic was replaced or suppressed")
	}
	var afterPID int
	var jit, mode string
	scalarSQL(t, f.db.QueryRow(`SELECT pg_backend_pid(),current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&afterPID, &jit, &mode))
	if afterPID != beforePID || jit != initialJIT || mode != initialMode {
		t.Fatal("ordinary callback rollback did not reset and reuse the connection")
	}
	var survived int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, id).Scan(&survived))
	if survived != 0 || aggregateResult(t, h, base, token, aggregateQuery("count", "")).Count != 6 {
		t.Fatal("ordinary callback panic committed an earlier transaction write")
	}
}

func TestIndependentR3dEarlyErrorsAndInvalidInputsRestoreSettings(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	s := aggregateBulkSeed(t, f, h, base, token, app, 6)
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	probes := []struct {
		name string
		edit func(*data.Query)
		deny bool
	}{{"invalid shape", func(q *data.Query) { q.Sort = &data.Sort{Field: aggregateUnits} }, false}, {"invalid filter", func(q *data.Query) {
		q.Filter = &data.Filter{Op: "eq", Field: aggregateUnits, Value: json.RawMessage(`false`)}
	}, false}, {"denied aggregate", func(*data.Query) {}, true}}
	for _, probe := range probes {
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			a, engine, guard, resolve, err := aggregateReviewAuthority(ctx, q, f, principal, app)
			if err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `SELECT set_config('jit','on',true),set_config('plan_cache_mode','force_generic_plan',true)`); err != nil {
				return err
			}
			query := aggregateQuery("sum", aggregateUnits)
			probe.edit(&query)
			if probe.deny {
				original := guard
				guard = func(ctx context.Context, action string, r data.Resource) error {
					if action == "data.aggregate.read" {
						return data.ErrDenied
					}
					return original(ctx, action, r)
				}
			}
			p, err := (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", query, engine, guard, resolve)
			if err == nil || p.Aggregate != nil {
				return errors.New("early error exposed a result")
			}
			var jit, mode string
			if err := q.QueryRowContext(ctx, `SELECT current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&jit, &mode); err != nil {
				return err
			}
			if jit != "on" || mode != "force_generic_plan" {
				return errors.New("early error changed expression settings")
			}
			return nil
		})
		scalarSQL(t, err)
	}
}

func aggregateReviewProjectionMutation(t *testing.T, f *iamFixture, statement string, args ...any) {
	t.Helper()
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`ALTER TABLE runtime.numeric_projections DISABLE TRIGGER certify_aggregate_projection_v1`)
	scalarSQL(t, err)
	_, err = tx.Exec(statement, args...)
	scalarSQL(t, err)
	_, err = tx.Exec(`ALTER TABLE runtime.numeric_projections ENABLE TRIGGER certify_aggregate_projection_v1`)
	scalarSQL(t, err)
	scalarSQL(t, tx.Commit())
}

func TestIndependentR3dMixedWitnessesNeverShrinkCountOrReadHiddenInputs(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	aggregateBulkSeed(t, f, h, base, token, app, 6)
	m9rIndexReady(t, h, base+"/indexes/"+aggregateUnits, token)
	aggregateReviewProjectionMutation(t, f, `UPDATE runtime.numeric_projections SET aggregate_valid_v1=NULL,value_number=999 WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('2')::uuid`, f.tenantA, app, aggregateUnits)
	if got := aggregateResult(t, h, base, token, aggregateQuery("sum", aggregateUnits)); got.Count != 6 || got.Sum != "21" {
		t.Fatal("legacy NULL witness trusted its projected value")
	}
	_, err := f.operator.Exec(`UPDATE runtime.numeric_projections SET aggregate_valid_v1=true,value_number=-1 WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('1')::uuid`, f.tenantA, app, aggregateUnits)
	scalarSQL(t, err)
	h = r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	r5Publish(t, h, root+"/roles", token, 0, r5Definition("finance_manager", app, policy.DataScope{Kind: "OWNER"}), true, uuid(t))
	r5Assign(t, h, root, token, f.viewer, "finance_manager", 200)
	viewer := f.token(t, f.viewerSubject, nil)
	if got := aggregateResult(t, h, base, viewer, aggregateQuery("sum", aggregateUnits)); got.Count != 3 || got.Sum != "12" {
		t.Fatal("invalid input outside entity scope affected result")
	}
	if w := m5Call(h, base+"/query", token, aggregateQuery("sum", aggregateUnits)); w.Code != 503 || strings.Contains(w.Body.String(), `"aggregate"`) {
		t.Fatal("false witness produced a partial result", w.Code)
	}
	_, err = f.operator.Exec(`UPDATE runtime.numeric_projections SET value_number=1 WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('1')::uuid`, f.tenantA, app, aggregateUnits)
	scalarSQL(t, err)
	aggregateReviewProjectionMutation(t, f, `UPDATE runtime.numeric_projections SET source_version=2,aggregate_valid_v1=true WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('4')::uuid`, f.tenantA, app, aggregateUnits)
	if w := m5Call(h, base+"/query", viewer, aggregateQuery("sum", aggregateUnits)); w.Code != 503 {
		t.Fatal("stale certified projection was summed", w.Code)
	}
	_, err = f.operator.Exec(`UPDATE runtime.numeric_projections SET source_version=1,value_number=4 WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('4')::uuid`, f.tenantA, app, aggregateUnits)
	scalarSQL(t, err)
	_, err = f.operator.Exec(`DELETE FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('6')::uuid`, f.tenantA, app, aggregateUnits)
	scalarSQL(t, err)
	if w := m5Call(h, base+"/query", viewer, aggregateQuery("sum", aggregateUnits)); w.Code != 503 {
		t.Fatal("missing projection silently shrank matching count", w.Code)
	}
	if got := aggregateResult(t, h, base, viewer, aggregateQuery("count", "")); got.Count != 3 {
		t.Fatal("count depended on unrelated broken numeric projections")
	}
}

func TestIndependentR3dReadinessRejectsWitnessMaintenanceAndPrivilegeDrift(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	aggregateBulkSeed(t, f, h, base, token, app, 6)
	m9rIndexReady(t, h, base+"/indexes/"+aggregateUnits, token)
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	for _, probe := range []struct{ corrupt, restore string }{
		{`ALTER TABLE runtime.numeric_projections DISABLE TRIGGER certify_aggregate_projection_v1`, `ALTER TABLE runtime.numeric_projections ENABLE TRIGGER certify_aggregate_projection_v1`},
		{`GRANT EXECUTE ON FUNCTION runtime.certify_aggregate_projection_v1() TO PUBLIC`, `REVOKE EXECUTE ON FUNCTION runtime.certify_aggregate_projection_v1() FROM PUBLIC`},
		{`GRANT UPDATE(aggregate_valid_v1) ON runtime.numeric_projections TO framework_app`, `REVOKE UPDATE(aggregate_valid_v1) ON runtime.numeric_projections FROM framework_app`},
	} {
		_, err := f.operator.Exec(probe.corrupt)
		scalarSQL(t, err)
		err = (data.Store{}).Ready(context.Background(), f.db)
		_, restoreErr := f.operator.Exec(probe.restore)
		scalarSQL(t, restoreErr)
		if !errors.Is(err, data.ErrUnavailable) {
			t.Fatal("witness readiness accepted changed maintenance or privileges")
		}
		scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	}
	for _, item := range []struct{ key, role string }{{"WORKER_DATABASE_URL", "framework_worker"}, {"DISPATCHER_DATABASE_URL", "framework_dispatcher"}} {
		db, err := postgres.Open(os.Getenv(item.key))
		scalarSQL(t, err)
		var actual, expected, role string
		scalarSQL(t, f.db.QueryRow(`SELECT current_database()`).Scan(&expected))
		scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user`).Scan(&actual, &role))
		if actual != expected || role != item.role {
			db.Close()
			t.Fatal("independent raw role target mismatch")
		}
		_, err = db.Exec(`SELECT aggregate_valid_v1 FROM runtime.numeric_projections`)
		nativePGCode(t, err, "42501")
		_, err = db.Exec(`SELECT runtime.certify_aggregate_projection_v1()`)
		nativePGCode(t, err, "42501")
		scalarSQL(t, db.Close())
	}
}

type aggregateReviewBlockingPolicy struct {
	policy.Native
	entered chan struct{}
	release <-chan struct{}
}

func (p aggregateReviewBlockingPolicy) Decide(ctx context.Context, s policy.Subject, action string, r policy.Resource, c policy.Context) policy.Decision {
	if action == "tenant.access" {
		p.entered <- struct{}{}
		select {
		case <-p.release:
		case <-ctx.Done():
		}
	}
	return p.Native.Decide(ctx, s, action, r, c)
}

func TestIndependentR3dAdmissionFallbackAndExplicitGateAreActuallyUsed(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	scalarReviewTarget(t, f)
	token := f.token(t, f.aliceSubject, nil)
	for _, tc := range []struct{ pool, explicit, want int }{{10, 0, 4}, {0, 0, 4}, {2, 0, 2}, {10, 1, 1}, {10, 5, 5}} {
		f.db.SetMaxOpenConns(tc.pool)
		entered, release := make(chan struct{}, 8), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		api := access.API{DB: f.db, Auth: f.verifier, Policy: aggregateReviewBlockingPolicy{entered: entered, release: release}, Audit: audit.Postgres{}}
		if tc.explicit != 0 {
			api.Admission, _ = postgres.NewAdmission(tc.explicit, 128)
		}
		mux := http.NewServeMux()
		api.Register(mux)
		before := f.db.Stats().WaitCount
		results := make(chan int, 8)
		for range 8 {
			go func() { results <- m3Call(mux, "GET", "/v1/tenants/"+f.tenantA+"/access", token, 0, nil).Code }()
		}
		failed := false
		for range tc.want {
			select {
			case <-entered:
			case <-time.After(time.Second):
				failed = true
			}
		}
		select {
		case <-entered:
			failed = true
		case <-time.After(30 * time.Millisecond):
		}
		if f.db.Stats().WaitCount != before {
			failed = true // A too-large fallback gate would wait in a small pool.
		}
		unblock()
		for range 8 {
			select {
			case status := <-results:
				if status != 200 {
					failed = true
				}
			case <-time.After(4 * time.Second):
				failed = true
			}
		}
		if failed {
			t.Fatal("actual admission fallback or injected limit changed", tc.pool, tc.explicit, tc.want)
		}
	}
}

func TestIndependentR3dCertificationRejectsCorruptLegacySource(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t, true)
	scalarReviewTarget(t, f)
	aggregateBulkSeed(t, f, h, base, token, app, 6)
	for _, field := range []string{aggregateUnits, aggregateBalance} {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	for _, tc := range []struct {
		field string
		value json.RawMessage
	}{{aggregateUnits, json.RawMessage(`1.0`)}, {aggregateUnits, json.RawMessage(`null`)}, {aggregateUnits, json.RawMessage(`"1"`)}, {aggregateUnits, json.RawMessage(`9007199254740992`)}, {aggregateBalance, json.RawMessage(`{"amount":"1","currency":"EUR"}`)}, {aggregateBalance, json.RawMessage(`{"amount":"1","currency":"USD","extra":true}`)}} {
		tx, err := f.operator.Begin()
		scalarSQL(t, err)
		_, err = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA)
		scalarSQL(t, err)
		_, err = tx.Exec(`ALTER TABLE runtime.records DISABLE TRIGGER record_numeric_projection`)
		scalarSQL(t, err)
		_, err = tx.Exec(`UPDATE runtime.records SET custom_data=jsonb_set(custom_data,ARRAY[$3::text],$4::jsonb),row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND id=md5('1')::uuid`, f.tenantA, app, tc.field, string(tc.value))
		scalarSQL(t, err)
		// The new certification trigger stays active and must ignore forged true.
		_, err = tx.Exec(`UPDATE runtime.numeric_projections p SET source_version=r.row_version,value_number=1,aggregate_valid_v1=true FROM runtime.records r WHERE p.tenant_id=$1 AND p.app_id=$2 AND p.field_id=$3 AND p.id=md5('1')::uuid AND r.tenant_id=p.tenant_id AND r.app_id=p.app_id AND r.metadata_revision=p.metadata_revision AND r.ir_hash=p.ir_hash AND r.entity_id=p.entity_id AND r.id=p.id`, f.tenantA, app, tc.field)
		scalarSQL(t, err)
		_, err = tx.Exec(`ALTER TABLE runtime.records ENABLE TRIGGER record_numeric_projection`)
		scalarSQL(t, err)
		scalarSQL(t, tx.Commit())
		var certified bool
		scalarSQL(t, f.operator.QueryRow(`SELECT aggregate_valid_v1 FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('1')::uuid`, f.tenantA, app, tc.field).Scan(&certified))
		if certified {
			t.Fatal("malformed legacy source obtained a true numeric witness")
		}
		for _, legacy := range []bool{false, true} {
			if legacy {
				aggregateReviewProjectionMutation(t, f, `UPDATE runtime.numeric_projections SET aggregate_valid_v1=NULL WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('1')::uuid`, f.tenantA, app, tc.field)
			}
			if w := m5Call(h, base+"/query", token, aggregateQuery("sum", tc.field)); w.Code != 503 || strings.Contains(w.Body.String(), `"aggregate"`) {
				t.Fatal("corrupt certified/legacy input produced a total", w.Code)
			}
		}
		if got := aggregateResult(t, h, base, token, aggregateQuery("count", "")); got.Count != 6 {
			t.Fatal("unrelated numeric corruption changed entity cardinality")
		}
	}
}

type aggregateReviewTimeoutTracer struct {
	fired, failed atomic.Bool
}

func (p *aggregateReviewTimeoutTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "WITH aggregate_input AS") && p.fired.CompareAndSwap(false, true) {
		// Driver entry precedes sending the actual aggregate. Narrow the owned
		// fixture transaction budget to force a real PostgreSQL cancellation.
		if _, err := conn.Exec(ctx, `SELECT set_config('statement_timeout','1',true)`); err != nil {
			p.failed.Store(true)
		}
	}
	return ctx
}
func (*aggregateReviewTimeoutTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestIndependentR3dBudgetRestorationAndActualStatementTimeout(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	s := aggregateBulkSeed(t, f, h, base, token, app, data.MaxSQLAggregateRows+1)
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		a, engine, guard, resolve, err := aggregateReviewAuthority(ctx, q, f, principal, app)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `SELECT set_config('jit','on',true),set_config('plan_cache_mode','force_generic_plan',true)`); err != nil {
			return err
		}
		p, err := (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", aggregateQuery("count", ""), engine, guard, resolve)
		if !errors.Is(err, data.ErrBudget) || p.Aggregate != nil {
			return errors.New("over-cap count returned partial output")
		}
		var jit, mode string
		if err := q.QueryRowContext(ctx, `SELECT current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&jit, &mode); err != nil {
			return err
		}
		if jit != "on" || mode != "force_generic_plan" {
			return errors.New("budget error failed to restore local modes")
		}
		return nil
	})
	scalarSQL(t, err)
	tracer := &aggregateReviewTimeoutTracer{}
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	scalarSQL(t, err)
	cfg.Tracer = tracer
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var actual, expected, role, initialJIT, initialMode, initialTimeout string
	scalarSQL(t, f.db.QueryRow(`SELECT current_database()`).Scan(&expected))
	scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user,current_setting('jit'),current_setting('plan_cache_mode'),current_setting('statement_timeout')`).Scan(&actual, &role, &initialJIT, &initialMode, &initialTimeout))
	if actual != expected || role != "framework_app" {
		t.Fatal("independent timeout target mismatch")
	}
	var page data.Page
	err = postgres.WithRuntime(context.Background(), db, func(ctx context.Context, q postgres.Executor) error {
		a, engine, guard, resolve, err := aggregateReviewAuthority(ctx, q, f, principal, app)
		if err != nil {
			return err
		}
		page, err = (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", aggregateQuery("count", ""), engine, guard, resolve)
		return err
	})
	nativePGCode(t, err, "57014")
	if !tracer.fired.Load() || tracer.failed.Load() || page.Aggregate != nil {
		t.Fatal("actual statement-timeout fixture did not suppress output")
	}
	var afterJIT, afterMode, afterTimeout string
	scalarSQL(t, db.QueryRow(`SELECT current_setting('jit'),current_setting('plan_cache_mode'),current_setting('statement_timeout')`).Scan(&afterJIT, &afterMode, &afterTimeout))
	if afterJIT != initialJIT || afterMode != initialMode || afterTimeout != initialTimeout {
		t.Fatal("statement timeout leaked transaction-local settings into the pool")
	}
	// Reuse the over-cap namespace to prove that live nested scope and a rare
	// core-ID filter select inputs before the aggregate cap. The physical source
	// boundary must retain JSON used only by a policy CONDITION.
	h = r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	scoped := policy.DataScope{Kind: "AND", Children: []policy.DataScope{
		{Kind: "OWNER"},
		{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"inactive"`)}},
	}}
	r5Publish(t, h, root+"/roles", token, 0, r5Definition("finance_manager", app, scoped), true, uuid(t))
	r5Assign(t, h, root, token, f.viewer, "finance_manager", 200)
	viewer := f.token(t, f.viewerSubject, nil)
	wantCount, wantSum := 0, int64(0)
	for i := 14; i <= data.MaxSQLAggregateRows+1; i += 14 {
		wantCount++
		wantSum += int64(i)
	}
	if got := aggregateResult(t, h, base, viewer, aggregateQuery("sum", aggregateUnits)); got.Count != wantCount || got.Sum != fmt.Sprint(wantSum) {
		t.Fatal("over-cap namespace truncated before nested live scope")
	}
	for _, ordinal := range []int{99988, 99989} {
		query := aggregateQuery("sum", aggregateUnits)
		id, err := json.Marshal(aggregateReviewID(t, f, ordinal))
		scalarSQL(t, err)
		query.Filter = &data.Filter{Op: "eq", Field: "field.contact.id", Value: id}
		want := data.AggregateResult{Sum: "0"}
		if ordinal%14 == 0 {
			want = data.AggregateResult{Count: 1, Sum: fmt.Sprint(ordinal)}
		}
		if got := aggregateResult(t, h, base, viewer, query); got != want {
			t.Fatal("rare core-ID filter bypassed live scope or followed the input cap")
		}
	}
}

func TestIndependentR3dNumericBuildOverlapsWritesWithoutNewLockInversion(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	s := aggregateBulkSeed(t, f, h, base, token, app, 6)
	path := base + "/indexes/" + aggregateUnits
	index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	built := make(chan error, 1)
	go func() {
		built <- postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			a, err := m3Scope(f, ctx, q, principal, f.tenantA)
			if err != nil {
				return err
			}
			x, err := (data.Store{}).BuildNumericIndex(ctx, q, s, a, "entity.contact", aggregateUnits, index.Checkpoint)
			if err != nil {
				return err
			}
			if x.State != "VERIFYING" || x.ScannedRows != 6 {
				return errors.New("numeric copy did not hold the expected source prefix")
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-ready:
	case err := <-built:
		scalarSQL(t, err)
		t.Fatal("numeric copy did not hold source locks")
	case <-time.After(2 * time.Second):
		t.Fatal("numeric copy setup exceeded its deadline")
	}
	one, six := aggregateReviewID(t, f, 1), aggregateReviewID(t, f, 6)
	results := make(chan int, 2)
	go func() {
		results <- m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: one, ExpectedVersion: 1, Values: map[string]any{aggregateUnits: json.Number("101")}}).Code
	}()
	go func() {
		results <- m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: six, ExpectedVersion: 1}).Code
	}()
	overlap := false
	deadline := time.Now().Add(350 * time.Millisecond)
	for time.Now().Before(deadline) {
		var blocked int
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usename='framework_app' AND cardinality(pg_blocking_pids(pid))>0`).Scan(&blocked))
		if blocked >= 2 {
			overlap = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	unblock()
	scalarSQL(t, <-built)
	for range 2 {
		if status := <-results; status != 200 {
			t.Fatal("overlapping indexed write failed", status)
		}
	}
	if !overlap {
		t.Fatal("concurrent copy/write fixture did not observe real lock overlap")
	}
	m9rIndexReady(t, h, path, token)
	got := aggregateResult(t, h, base, token, aggregateQuery("sum", aggregateUnits))
	if got.Count != 5 || got.Sum != "115" {
		t.Fatal("concurrent copy resurrected deletion or lost a newer source value")
	}
	var invalid int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records r LEFT JOIN runtime.numeric_projections p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.field_id=$3 AND p.id=r.id WHERE r.tenant_id=$1 AND r.app_id=$2 AND (p.id IS NULL OR p.source_version<>r.row_version OR p.aggregate_valid_v1 IS DISTINCT FROM true OR p.value_number<>(r.custom_data->>$3)::numeric)`, f.tenantA, app, aggregateUnits).Scan(&invalid))
	if invalid != 0 {
		t.Fatal("concurrent numeric witness/value/version invariant broke")
	}
}
