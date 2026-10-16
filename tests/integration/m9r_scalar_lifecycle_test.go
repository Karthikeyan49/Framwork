//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestM9RScalarIndexCombinedQuotasAndRegistrationRace(t *testing.T) {
	m8Database(t)
	t.Run("entity and concurrent last slot", func(t *testing.T) {
		f, h, base, token, app := scalarFixture(t, 7)
		scalarCreate(t, h, base, token, "Quota")
		for _, field := range []string{"field.contact.balance", "field.contact.joined", "field.contact.memo0", "field.contact.memo1", "field.contact.memo2", "field.contact.memo3", "field.contact.memo4"} {
			m9rIndex(t, h, base+"/indexes/"+field, token, data.NumericIndex{}, "register")
		}
		start := make(chan struct{})
		results := make(chan int, 2)
		var wg sync.WaitGroup
		for _, id := range []string{"field.contact.memo5", "field.contact.memo6"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results <- m5Call(h, base+"/indexes/"+id+"/register", token, struct{}{}).Code
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		success := 0
		for code := range results {
			if code == 200 {
				success++
			} else if code != 400 && code != 409 {
				t.Fatal("quota race unexpected result", code)
			}
		}
		if success != 1 {
			t.Fatal("quota race did not admit exactly one registration", success)
		}
		var count int
		scalarSQL(t, f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2)+(SELECT count(*) FROM runtime.numeric_indexes WHERE tenant_id=$1 AND app_id=$2)`, f.tenantA, app).Scan(&count))
		if count != 8 {
			t.Fatal("combined entity quota exceeded")
		}
		if w := m5Call(h, base+"/indexes/field.contact.name/register", token, struct{}{}); w.Code != 400 {
			t.Fatal("combined scalar quota not enforced", w.Code)
		}
		// Existing registrations reconcile even when the quota is full.
		m9rIndex(t, h, base+"/indexes/field.contact.balance", token, data.NumericIndex{}, "register")
	})
	t.Run("namespace and numeric entrypoint", func(t *testing.T) {
		f, h, base, token, app := scalarFixture(t, 1, 5)
		scalarCreate(t, h, base, token, "Namespace")
		prefix := strings.Split(base, "/entities/")[0]
		for entity := range 4 {
			for field := range 8 {
				path := fmt.Sprintf("%s/entities/entity.quota%d/indexes/field.quota%d.value%d", prefix, entity, entity, field)
				m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
			}
		}
		for _, field := range []string{"field.contact.balance", "field.contact.memo0"} {
			if w := m5Call(h, base+"/indexes/"+field+"/register", token, struct{}{}); w.Code != 400 {
				t.Fatal("shared namespace quota bypass", w.Code)
			}
		}
		var count int
		scalarSQL(t, f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2)+(SELECT count(*) FROM runtime.numeric_indexes WHERE tenant_id=$1 AND app_id=$2)`, f.tenantA, app).Scan(&count))
		if count != 32 {
			t.Fatal("combined namespace quota mismatch")
		}
	})
}

type scalarOpaque struct {
	postgres.Executor
	scalarCalls int
}

func (q *scalarOpaque) QueryRowContext(ctx context.Context, statement string, args ...any) *sql.Row {
	if strings.Contains(statement, "runtime.scalar_") {
		q.scalarCalls++
	}
	return q.Executor.QueryRowContext(ctx, statement, args...)
}
func (q *scalarOpaque) QueryContext(ctx context.Context, statement string, args ...any) (*sql.Rows, error) {
	if strings.Contains(statement, "runtime.scalar_") {
		q.scalarCalls++
	}
	return q.Executor.QueryContext(ctx, statement, args...)
}

func TestM9RScalarIndexSnapshotOverflowAndOpaqueFallback(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := scalarFixture(t, 1)
	scalarCreate(t, h, base, token, "First")
	scalarCreate(t, h, base, token, "Second")
	m9rIndexReady(t, h, base+"/indexes/field.contact.memo0", token)
	scope := scalarScope(t, h, base, token)
	principal, _ := f.verifier.Verify(token)
	query := m5Query()
	query.Projection = append(query.Projection, "field.contact.memo0")
	query.Sort = &data.Sort{Field: "field.contact.memo0"}
	query.Limit = 100
	allow := func(context.Context, string, data.Resource) error { return nil }
	all := func(context.Context, string, data.Resource) (policy.DataScope, error) {
		return policy.DataScope{Kind: "ALL"}, nil
	}
	scalarSQL(t, postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		access, err := m3Scope(f, ctx, q, principal, f.tenantA)
		if err != nil {
			return err
		}
		opaque := &scalarOpaque{Executor: q}
		page, err := (data.Store{}).Query(ctx, opaque, scope, access, "entity.contact", query, allow, all)
		if err != nil {
			return err
		}
		if len(page.Records) != 2 || opaque.scalarCalls != 0 {
			return errors.New("opaque executor inherited indexed admission")
		}
		return nil
	}))
	// Stop execution at join authorization, after the overflow probe has run.
	query.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id"}}
	probed, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			access, err := m3Scope(f, ctx, q, principal, f.tenantA)
			if err != nil {
				return err
			}
			rowChecks := 0
			guard := func(ctx context.Context, action string, _ data.Resource) error {
				if action == "data.row.read" {
					rowChecks++
					if rowChecks == 2 {
						close(probed)
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
				return nil
			}
			page, err := (data.Store{}).Query(ctx, q, scope, access, "entity.contact", query, guard, all)
			if err != nil {
				return err
			}
			if len(page.Records) != 2 {
				return errors.New("new overflow row leaked into older certified snapshot")
			}
			return nil
		})
	}()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	select {
	case <-probed:
	case err := <-result:
		scalarSQL(t, err)
		t.Fatal("snapshot probe did not reach join")
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot probe timed out")
	}
	scalarCreate(t, h, base, token, strings.Repeat("x", 4096))
	unblock()
	scalarSQL(t, <-result)
	page := m5Page(t, m5Call(h, base+"/query", token, query))
	if len(page.Records) != 3 {
		t.Fatal("new snapshot failed to include overflow fallback row")
	}
}

func TestM9RScalarIndexRegistrationFencesOldWriterAndReadyDrift(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := scalarFixture(t, 1)
	r := scalarCreate(t, h, base, token, "Fenced")
	principal, _ := f.verifier.Verify(token)
	tx, err := f.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	scalarSQL(t, err)
	defer tx.Rollback()
	_, err = m3Scope(f, context.Background(), tx, principal, f.tenantA)
	scalarSQL(t, err)
	m9rIndex(t, h, base+"/indexes/field.contact.memo0", token, data.NumericIndex{}, "register")
	_, err = tx.Exec(`UPDATE runtime.records SET row_version=row_version+1 WHERE tenant_id=$1 AND id=$2`, f.tenantA, r.ID)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "40001" {
		t.Fatal("older RR writer was not fenced")
	}
	_ = tx.Rollback()
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	_, err = f.operator.Exec(`ALTER TABLE runtime.records DISABLE TRIGGER record_scalar_projection`)
	scalarSQL(t, err)
	t.Cleanup(func() { _, _ = f.operator.Exec(`ALTER TABLE runtime.records ENABLE TRIGGER record_scalar_projection`) })
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("disabled scalar trigger remained ready")
	}
	_, err = f.operator.Exec(`ALTER TABLE runtime.records ENABLE TRIGGER record_scalar_projection`)
	scalarSQL(t, err)
	_, err = f.operator.Exec(`GRANT EXECUTE ON FUNCTION runtime.scalar_value(jsonb,text,boolean) TO framework_app`)
	scalarSQL(t, err)
	t.Cleanup(func() {
		_, _ = f.operator.Exec(`REVOKE EXECUTE ON FUNCTION runtime.scalar_value(jsonb,text,boolean) FROM framework_app`)
	})
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("new scalar routine privilege drift remained ready")
	}
	_, err = f.operator.Exec(`REVOKE EXECUTE ON FUNCTION runtime.scalar_value(jsonb,text,boolean) FROM framework_app`)
	scalarSQL(t, err)
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}

func TestM9RScalarIndexAdoptionAndLiveRecordScope(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarCreate(t, h, base, token, "Owned")
	scalarSeed(t, f, app, 20)
	// Switch live product role authority after capturing the unindexed owner result.
	h = r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	r5Assign(t, h, root, token, f.viewer, "owner", 200)
	admin := f.token(t, f.viewerSubject, nil)
	d := r5Definition("scalar_reader", app, policy.DataScope{Kind: "OWNER"})
	r5Publish(t, h, root+"/roles", admin, 0, d, true, uuid(t))
	r5Assign(t, h, root, admin, f.alice, d.Name, 200)
	before := scalarPages(t, h, base, token, "field.contact.memo0", false, nil)
	if len(before) != 11 {
		t.Fatal("populated owner scope oracle")
	}
	for _, r := range before {
		if r.OwnerID != f.alice {
			t.Fatal("unindexed scope exposed another owner")
		}
	}
	idx := m9rIndexReady(t, h, base+"/indexes/field.contact.memo0", admin)
	after := scalarPages(t, h, base, token, "field.contact.memo0", false, nil)
	if len(after) != len(before) {
		t.Fatal("indexed owner scope cardinality changed")
	}
	for i, r := range after {
		if r.ID != before[i].ID || r.OwnerID != f.alice {
			t.Fatal("indexed owner scope changed rows")
		}
	}
	// Hidden sort/filter inputs must still deny after a physical index exists.
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.memo0"}})
	r5Publish(t, h, root+"/roles", admin, 1, d, true, uuid(t))
	query := m5Query()
	query.Sort = &data.Sort{Field: "field.contact.memo0"}
	if w := m5Call(h, base+"/query", token, query); w.Code != 403 {
		t.Fatal("hidden indexed sort inferred values", w.Code)
	}
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], admin)
	dsroot := root + "/preview/" + app + "/datasets"
	ds := m9rDatasetCreate(t, h, dsroot, admin, "ADOPT", witness)
	for ds.State != "OPEN" {
		w := m5Call(h, dsroot+"/"+ds.ID+"/verify", admin, struct{}{})
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ds) != nil {
			t.Fatal("scalar adoption", w.Code)
		}
	}
	bound := fmt.Sprintf("%s/%s/bindings/1/entities/entity.contact", dsroot, ds.ID)
	actual := m9rIndexReady(t, h, bound+"/indexes/field.contact.memo0", admin)
	if actual != idx {
		t.Fatal("adoption changed scalar physical identity")
	}
	if records := scalarPages(t, h, bound, admin, "field.contact.memo0", false, nil); len(records) != 21 {
		t.Fatal("adopted scalar data missing")
	}
}
