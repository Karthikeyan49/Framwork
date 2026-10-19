//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestR7bReviewRetryHelperSemanticDriftDoesNotCertifyTiming(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "semantic retry drift")
	db, worker := m8Roles(t)
	roles := map[string]*sql.DB{"framework_app": f.db, "framework_dispatcher": db, "framework_worker": worker}
	for name, connection := range roles {
		if events.Ready(context.Background(), connection, name) != nil {
			t.Fatal("review positive event readiness control", name)
		}
	}
	var original string
	scalarSQL(t, f.operator.QueryRow(`SELECT pg_get_functiondef('control.event_retry_delay(integer)'::regprocedure)`).Scan(&original))
	t.Cleanup(func() {
		if _, err := f.operator.Exec(original); err != nil {
			t.Error("review helper restoration")
		}
	})
	// Operator deployment drift, not a runtime credential exploit: retain the
	// exact owner/signature/strictness/volatility/search path and private grants.
	_, err := f.operator.Exec(`CREATE OR REPLACE FUNCTION control.event_retry_delay(n integer) RETURNS interval LANGUAGE sql IMMUTABLE STRICT SET search_path=pg_catalog AS 'SELECT interval ''0 seconds'''`)
	scalarSQL(t, err)
	for name, connection := range roles {
		if events.Ready(context.Background(), connection, name) == nil {
			t.Errorf("zero-delay helper falsely certifies bounded delivery policy for %s", name)
		}
	}
	// Demonstrate actual behavior against the populated dispatcher port, even
	// though production should stop at failed readiness after a corrective fix.
	dispatch := events.Dispatcher{DB: db}
	start := time.Now()
	var last events.Pointer
	for range 20 {
		last = m8Claim(t, dispatch)
		if dispatch.Finish(context.Background(), last, false) != nil {
			t.Fatal("review known broker failure")
		}
	}
	var state string
	var attempts int
	scalarSQL(t, f.operator.QueryRow(`SELECT state,attempts FROM control.event_deliveries WHERE tenant_id=$1 AND id=$2`, last.TenantID, last.EventID).Scan(&state, &attempts))
	if state != "DEAD" || attempts != 20 {
		t.Fatal("review drift did not exercise the terminal budget")
	}
	t.Logf("operator-drift negative control: state=DEAD attempts=20 elapsed=%s", time.Since(start).Round(time.Millisecond))
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func r7bReviewRawBind(t *testing.T, f *iamFixture, worker *sql.DB, p events.Pointer, isolation sql.IsolationLevel) *sql.Tx {
	tx, bound := r7bReviewRawTryBind(t, f, worker, p, isolation)
	if !bound {
		t.Fatal("review exact raw event binding")
	}
	return tx
}

func r7bReviewRawTryBind(t *testing.T, f *iamFixture, worker *sql.DB, p events.Pointer, isolation sql.IsolationLevel) (*sql.Tx, bool) {
	t.Helper()
	ticket, completed, ready, dead, err := jobs.EventTicket(context.Background(), worker, p.TenantID, p.EventID, p.BodyHash, p.ConsumerID, p.Generation, p.LeaseToken)
	if err != nil || completed || !ready || dead {
		t.Fatal("review raw event routing control")
	}
	tx, err := worker.BeginTx(context.Background(), &sql.TxOptions{Isolation: isolation})
	if err != nil {
		t.Fatal("review raw transaction")
	}
	t.Cleanup(func() { tx.Rollback() })
	var bound bool
	if err = tx.QueryRow(`SELECT control.bind_worker_job($1::uuid,$2::uuid,'event',$3,$4::uuid,$5)`, p.TenantID, p.EventID, p.Generation, p.LeaseToken, ticket.ReleaseHash()).Scan(&bound); err != nil {
		t.Fatal("review exact raw event binding")
	}
	if !bound {
		return tx, false
	}
	if _, err = tx.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$2,true)`, f.alice, f.aliceSubject); err != nil {
		t.Fatal("review exact actor binding")
	}
	return tx, true
}

func TestR7bReviewRawEventExpiresInsideBoundTransaction(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "raw receipt expiry")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	// Explicitly accelerated boundary fixture; this is not the real600s proof.
	_, err := f.operator.Exec(`UPDATE control.event_deliveries SET next_at=clock_timestamp()+interval '300 milliseconds' WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EventID)
	scalarSQL(t, err)
	tx := r7bReviewRawBind(t, f, worker, p, sql.LevelReadCommitted)
	var visible int
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&visible))
	if visible != 1 {
		t.Fatal("review raw envelope positive control")
	}
	time.Sleep(350 * time.Millisecond)
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&visible))
	if visible != 0 {
		t.Fatal("expired receipt still reveals event inside an already-bound raw transaction")
	}
	_, err = tx.Exec(`INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1,$2,$3,$4)`, p.TenantID, p.ConsumerID, p.EventID, p.BodyHash)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("expired raw receipt write not denied")
	}
	tx.Rollback()
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewRawEventDoesNotPinRevokedMembershipPastPublishLease(t *testing.T) {
	for _, target := range []int64{54, 0} {
		name := "current"
		if target == 54 {
			name = "schema54"
		}
		t.Run(name, func(t *testing.T) { r7bReviewRevokedSnapshot(t, target) })
	}
}

func r7bReviewRevokedSnapshot(t *testing.T, target int64) {
	m8DatabaseAt(t, target)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "raw snapshot revocation")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	tx, bound := r7bReviewRawTryBind(t, f, worker, p, sql.LevelRepeatableRead)
	if !bound {
		if target == 54 {
			t.Fatal("historical repeatable-read positive control unexpectedly denied")
		}
		tx.Rollback()
		if m8Consumer(worker).Consume(context.Background(), p) != nil {
			t.Fatal("denying raw repeatable-read also broke current event consumer")
		}
		m8AssertCounts(t, f.operator, f.tenantA, 1)
		t.Log("current raw repeatable-read denied before authority; trusted consumer remains functional")
		return
	}
	var visible int
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&visible))
	if visible != 1 {
		t.Fatal("review raw snapshot positive control")
	}
	_, err := f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice)
	scalarSQL(t, err)
	// Exceed the old5s receipt boundary without changing the new15m deadline.
	time.Sleep(5200 * time.Millisecond)
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&visible))
	t.Logf("schema_target=%d revoked raw RR after5.2s visible=%d", target, visible)
	if visible != 0 {
		t.Errorf("raw repeatable-read snapshot retained revoked membership after old publication/receipt boundary; visible=%d", visible)
	}
	_, err = tx.Exec(`INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1,$2,$3,$4)`, p.TenantID, p.ConsumerID, p.EventID, p.BodyHash)
	var pg *pgconn.PgError
	if err == nil {
		t.Error("revoked raw repeatable-read transaction still accepted an inbox receipt")
	} else if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("review revoked receipt failed outside authorization boundary")
	}
	tx.Rollback()
	if err = m8Consumer(worker).Consume(context.Background(), p); err == nil {
		t.Fatal("fresh transaction retained revoked membership")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewExportedSnapshotCannotRefreshRevokedEventAuthority(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "raw imported snapshot")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	ticket, _, ready, _, err := jobs.EventTicket(context.Background(), worker, p.TenantID, p.EventID, p.BodyHash, p.ConsumerID, p.Generation, p.LeaseToken)
	if err != nil || !ready {
		t.Fatal("review import routing control")
	}
	exporter, err := worker.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal("review snapshot exporter")
	}
	defer exporter.Rollback()
	var snapshot string
	if err = exporter.QueryRow(`SELECT pg_export_snapshot()`).Scan(&snapshot); err != nil || !regexp.MustCompile(`^[0-9A-Fa-f-]+$`).MatchString(snapshot) {
		t.Fatal("review snapshot export not available")
	}
	_, err = f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice)
	scalarSQL(t, err)
	time.Sleep(5200 * time.Millisecond)
	importer, err := worker.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal("review fresh snapshot importer")
	}
	defer importer.Rollback()
	// PostgreSQL SET TRANSACTION does not accept a prepared parameter. The
	// server-issued snapshot identifier is strictly checked above before quoting.
	_, err = importer.Exec(`SET TRANSACTION SNAPSHOT '` + snapshot + `'`)
	scalarSQL(t, err)
	var bound bool
	if err = importer.QueryRow(`SELECT control.bind_worker_job($1::uuid,$2::uuid,'event',$3,$4::uuid,$5)`, p.TenantID, p.EventID, p.Generation, p.LeaseToken, ticket.ReleaseHash()).Scan(&bound); err != nil {
		t.Fatal("review imported-snapshot bind query")
	}
	if bound {
		_, err = importer.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$2,true)`, f.alice, f.aliceSubject)
		scalarSQL(t, err)
		var visible int
		var transactionAge float64
		scalarSQL(t, importer.QueryRow(`SELECT count(*),extract(epoch FROM clock_timestamp()-transaction_timestamp()) FROM runtime.event_envelopes`).Scan(&visible, &transactionAge))
		t.Logf("old exported snapshot imported into fresh transaction: bound=true visible=%d new_transaction_age_s=%.3f", visible, transactionAge)
		t.Error("old imported snapshot refreshed revoked event authority in a fresh transaction")
		_, err = importer.Exec(`INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1,$2,$3,$4)`, p.TenantID, p.ConsumerID, p.EventID, p.BodyHash)
		if err == nil {
			t.Error("revoked imported snapshot still accepted an inbox receipt")
		}
	}
	importer.Rollback()
	exporter.Rollback()
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewReadCommittedSeesMembershipRevocation(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "read committed live revocation")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	tx := r7bReviewRawBind(t, f, worker, p, sql.LevelReadCommitted)
	var visible int
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&visible))
	if visible != 1 {
		t.Fatal("review live read committed positive control")
	}
	_, err := f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice)
	scalarSQL(t, err)
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&visible))
	if visible != 0 {
		t.Fatal("read committed retained revoked event authority")
	}
	_, err = tx.Exec(`INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1,$2,$3,$4)`, p.TenantID, p.ConsumerID, p.EventID, p.BodyHash)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("read committed accepted revoked receipt")
	}
	tx.Rollback()
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewEventCallbackUsesUncertifiedReadCommitted(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "event isolation boundary")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	c := m8Consumer(worker)
	original := c.Handler
	called := false
	c.Handler = func(ctx context.Context, q postgres.Executor, a tenant.Access, e events.Envelope) error {
		called = true
		var isolation string
		if err := q.QueryRowContext(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
			return err
		}
		if isolation != "read committed" || postgres.RuntimeSnapshot(q) {
			t.Errorf("event callback uses isolation=%s certified_snapshot=%t", isolation, postgres.RuntimeSnapshot(q))
		}
		return original(ctx, q, a, e)
	}
	if c.Consume(context.Background(), p) != nil || !called {
		t.Fatal("review event callback positive control")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		var isolation string
		if err := q.QueryRowContext(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
			return err
		}
		if isolation != "repeatable read" || !postgres.RuntimeSnapshot(q) {
			t.Error("ordinary API runtime lost its certified repeatable-read snapshot")
		}
		return nil
	}); err != nil {
		t.Fatal("review ordinary runtime isolation control")
	}
}
