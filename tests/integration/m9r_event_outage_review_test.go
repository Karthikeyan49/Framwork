//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
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

func TestR7bReviewAlreadyBoundSnapshotBlocksUnsafeCutover(t *testing.T) {
	m8DatabaseAt(t, 55)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "already bound at cutover")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	tx := r7bReviewRawBind(t, f, worker, p, sql.LevelRepeatableRead)
	prepared, err := tx.Prepare(`SELECT count(*) FROM runtime.event_envelopes`)
	scalarSQL(t, err)
	defer prepared.Close()
	var visible int
	scalarSQL(t, prepared.QueryRow().Scan(&visible))
	if visible != 1 {
		t.Fatal("review pre-cutover prepared authority control")
	}
	before := r7bManifest(t, f.operator)
	provider, err := goose.NewProvider(goose.DialectPostgres, f.operator, migrations.Files, goose.WithDisableGlobalRegistry(true))
	scalarSQL(t, err)
	applied, err := provider.UpTo(context.Background(), 56)
	if err != nil || len(applied) != 1 {
		t.Fatal("review additive migration56 while old RR context is bound")
	}
	after := r7bManifest(t, f.operator)
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("review migration56 changed existing evidence", i)
		}
	}
	scalarSQL(t, prepared.QueryRow().Scan(&visible))
	if visible != 1 {
		t.Fatal("historical function-only replacement control changed")
	}
	var direct, fresh int
	var seesGuard bool
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&fresh))
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM control.worker_authority()`).Scan(&direct))
	scalarSQL(t, tx.QueryRow(`SELECT strpos(pg_get_functiondef('control.worker_authority()'::regprocedure),'read committed')>0`).Scan(&seesGuard))
	t.Logf("already-bound post-cutover authority: prepared=%d fresh_envelope=%d direct_authority=%d catalog_sees_guard=%t", visible, fresh, direct, seesGuard)
	if fresh != 1 || direct != 1 || !seesGuard {
		t.Fatal("historical cached-authority positive control changed")
	}
	// Successful installation must not overlap this known historical reader.
	_, err = provider.UpTo(context.Background(), r7bLastMigration)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55P03" {
		t.Fatal("live old reader did not refuse migration57 at NOWAIT barrier")
	}
	var applied57, policies int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM goose_db_version WHERE version_id=57 AND is_applied`).Scan(&applied57))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM pg_policy WHERE polname='event_read_committed'`).Scan(&policies))
	if applied57 != 0 || policies != 0 {
		t.Fatal("failed cutover left migration or partial policies")
	}
	failed := r7bManifest(t, f.operator)
	for i := range before {
		if before[i] != failed[i] {
			t.Fatal("failed barrier rewrote retained evidence", i)
		}
	}
	_, err = tx.Exec(`INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1,$2,$3,$4)`, p.TenantID, p.ConsumerID, p.EventID, p.BodyHash)
	if err == nil {
		t.Error("already-bound RR receipt survived authority cutover")
	} else if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("already-bound RR receipt failed outside authorization boundary")
	} else {
		t.Log("already-bound RR inbox insertion denied42501 after cutover")
	}
	tx.Rollback()
	applied, err = provider.UpTo(context.Background(), r7bLastMigration)
	if err != nil || len(applied) != 1 {
		t.Fatal("drained cutover did not install migration57")
	}
	after = r7bManifest(t, f.operator)
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("successful drained cutover rewrote evidence", i)
		}
	}
	for _, isolation := range []sql.IsolationLevel{sql.LevelRepeatableRead, sql.LevelSerializable} {
		denied, bound := r7bReviewRawTryBind(t, f, worker, p, isolation)
		denied.Rollback()
		if bound {
			t.Fatal("completed cutover admitted an old isolation")
		}
	}
	if m8Consumer(worker).Consume(context.Background(), p) != nil {
		t.Fatal("same immutable event not consumable after compatible cutover")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
}

func TestR7bReviewRelationPolicyRequiresDrainedReaders(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "cutover DDL requires drain")
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
		t.Fatal("review cutover reader positive control")
	}
	// Explicit fixture-only DDL diagnostic, not an applied product fix. An open
	// reader prevents policy replacement: an online barrier cannot be assumed.
	statement := `CREATE POLICY r7b_review_event_isolation ON runtime.event_envelopes AS RESTRICTIVE TO framework_worker USING(current_setting('transaction_isolation')='read committed')`
	operatorTx, err := f.operator.BeginTx(context.Background(), nil)
	scalarSQL(t, err)
	defer operatorTx.Rollback()
	_, err = operatorTx.Exec(`SET LOCAL lock_timeout='400ms'`)
	scalarSQL(t, err)
	_, err = operatorTx.Exec(statement)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55P03" {
		t.Fatal("review relation-policy lock assumption changed; inspect cutover contract")
	}
	operatorTx.Rollback()
	tx.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = f.operator.ExecContext(ctx, statement)
	cancel()
	scalarSQL(t, err)
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewPreCutoverExportedSnapshotCannotBindAfterUpgrade(t *testing.T) {
	m8DatabaseAt(t, 55)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "snapshot exported before upgrade")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	ticket, _, ready, _, err := jobs.EventTicket(context.Background(), worker, p.TenantID, p.EventID, p.BodyHash, p.ConsumerID, p.Generation, p.LeaseToken)
	if err != nil || !ready {
		t.Fatal("review pre-cutover ticket")
	}
	exporter, err := worker.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	scalarSQL(t, err)
	defer exporter.Rollback()
	var snapshot string
	err = exporter.QueryRow(`SELECT pg_export_snapshot()`).Scan(&snapshot)
	if err != nil || !regexp.MustCompile(`^[0-9A-Fa-f-]+$`).MatchString(snapshot) {
		t.Fatal("review pre-cutover snapshot export")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, f.operator, migrations.Files, goose.WithDisableGlobalRegistry(true))
	scalarSQL(t, err)
	applied, err := provider.UpTo(context.Background(), r7bLastMigration)
	if err != nil || len(applied) != 2 {
		t.Fatal("review migrations56 and57 after snapshot export")
	}
	importer, err := worker.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	scalarSQL(t, err)
	defer importer.Rollback()
	_, err = importer.Exec(`SET TRANSACTION SNAPSHOT '` + snapshot + `'`)
	scalarSQL(t, err)
	var bound bool
	err = importer.QueryRow(`SELECT control.bind_worker_job($1::uuid,$2::uuid,'event',$3,$4::uuid,$5)`, p.TenantID, p.EventID, p.Generation, p.LeaseToken, ticket.ReleaseHash()).Scan(&bound)
	scalarSQL(t, err)
	if bound {
		t.Error("pre-cutover exported snapshot bound event after upgrade")
	}
	importer.Rollback()
	exporter.Rollback()
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewRawEventIsolationWhitelist(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "closed event isolation")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	for _, isolation := range []sql.IsolationLevel{sql.LevelRepeatableRead, sql.LevelSerializable, sql.LevelReadUncommitted} {
		t.Run(isolation.String(), func(t *testing.T) {
			tx, bound := r7bReviewRawTryBind(t, f, worker, p, isolation)
			defer tx.Rollback()
			if bound {
				t.Fatal("event authority accepted isolation outside closed RC contract")
			}
			var visible int
			scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&visible))
			if visible != 0 {
				t.Fatal("denied non-RC bind retained event authority")
			}
		})
	}
	tx := r7bReviewRawBind(t, f, worker, p, sql.LevelReadCommitted)
	_, err := tx.Exec(`SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "25001" {
		t.Fatal("bound RC transaction could change to stale snapshot isolation")
	}
	tx.Rollback()
	if m8Consumer(worker).Consume(context.Background(), p) != nil {
		t.Fatal("closed isolation contract broke normal consumer")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
}

func TestR7bReviewWorkflowKeepsCertifiedRepeatableRead(t *testing.T) {
	m := m9Setup(t, nil)
	m.start(t, "workflow-isolation-review")
	lease, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal("review live workflow lease")
	}
	valid, err := jobs.TicketAuthority(context.Background(), m.db, lease.Ticket)
	if err != nil || !valid {
		t.Fatal("review workflow ticket authority")
	}
	called := false
	err = jobs.WithJob(context.Background(), m.db, lease.Ticket, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
		called = true
		var isolation string
		if err := q.QueryRowContext(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
			return err
		}
		if isolation != "repeatable read" || !postgres.RuntimeSnapshot(q) {
			t.Error("workflow lost certified repeatable-read isolation")
		}
		return nil
	})
	if err != nil || !called {
		t.Fatal("review workflow callback authority")
	}
}

func TestR7bReviewSemanticProbeDeniesChangedInvalidInputs(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "invalid retry input semantics")
	db, worker := m8Roles(t)
	roles := map[string]*sql.DB{"framework_app": f.db, "framework_worker": worker, "framework_dispatcher": db}
	var original string
	scalarSQL(t, f.operator.QueryRow(`SELECT pg_get_functiondef('control.event_retry_delay(integer)'::regprocedure)`).Scan(&original))
	t.Cleanup(func() {
		if _, err := f.operator.Exec(original); err != nil {
			t.Error("review helper semantic restoration")
		}
	})
	for _, input := range []string{"0", "-1", "21", "2147483647", "-2147483648", "7"} {
		t.Run(input, func(t *testing.T) {
			_, err := f.operator.Exec(`CREATE OR REPLACE FUNCTION control.event_retry_delay(n integer) RETURNS interval LANGUAGE sql IMMUTABLE STRICT SET search_path=pg_catalog AS $$ SELECT CASE WHEN n=` + input + ` THEN interval '1 microsecond' WHEN n BETWEEN 1 AND 20 THEN interval '1 millisecond'*least(60000::double precision,500*power(2::double precision,n-1)) ELSE NULL::interval END $$`)
			scalarSQL(t, err)
			for name, connection := range roles {
				if events.Ready(context.Background(), connection, name) == nil {
					t.Fatal("semantic probe accepted changed retry input", name)
				}
				r7bDenied(t, connection, `SELECT control.event_retry_delay(1)`)
			}
			scalarSQL(t, func() error { _, err := f.operator.Exec(original); return err }())
			for name, connection := range roles {
				if events.Ready(context.Background(), connection, name) != nil {
					t.Fatal("restored semantic helper did not recover readiness", name)
				}
			}
		})
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewUnsafeHelperRejectedBeforeOwnerInvocation(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "unsafe readiness helper")
	db, worker := m8Roles(t)
	_, err := f.operator.Exec(`CREATE TABLE control.r7b_review_invocations(mark integer NOT NULL); ALTER TABLE control.r7b_review_invocations OWNER TO framework_schema_owner; REVOKE ALL ON control.r7b_review_invocations FROM PUBLIC,framework_app,framework_worker,framework_dispatcher`)
	scalarSQL(t, err)
	var original string
	scalarSQL(t, f.operator.QueryRow(`SELECT pg_get_functiondef('control.event_retry_delay(integer)'::regprocedure)`).Scan(&original))
	t.Cleanup(func() {
		if _, err := f.operator.Exec(original); err != nil {
			t.Error("review unsafe helper restoration")
		}
	})
	// Explicit operator deployment drift: same private grants, but volatility is
	// unsafe. A runtime caller must be rejected before SECURITY DEFINER invokes it.
	_, err = f.operator.Exec(`CREATE OR REPLACE FUNCTION control.event_retry_delay(n integer) RETURNS interval LANGUAGE plpgsql VOLATILE STRICT SET search_path=pg_catalog AS $$ BEGIN INSERT INTO control.r7b_review_invocations VALUES(n); RETURN CASE WHEN n BETWEEN 1 AND 20 THEN interval '1 millisecond'*least(60000::double precision,500*power(2::double precision,n-1)) ELSE NULL::interval END; END $$`)
	scalarSQL(t, err)
	for name, connection := range map[string]*sql.DB{"framework_app": f.db, "framework_worker": worker, "framework_dispatcher": db} {
		if events.Ready(context.Background(), connection, name) == nil {
			t.Error("unsafe helper reported ready", name)
		}
	}
	var calls int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM control.r7b_review_invocations`).Scan(&calls))
	if calls != 0 {
		t.Fatalf("readiness invoked structurally unsafe helper with owner privileges before denial; writes=%d", calls)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewReadinessRejectsIsolationPolicyDrift(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "readiness isolation policy drift")
	db, worker := m8Roles(t)
	roles := map[string]*sql.DB{"framework_app": f.db, "framework_worker": worker, "framework_dispatcher": db}
	check := func(want bool) {
		t.Helper()
		for name, connection := range roles {
			if (events.Ready(context.Background(), connection, name) == nil) != want {
				t.Fatal("unexpected event isolation-policy readiness", name, want)
			}
		}
	}
	check(true)
	for _, fault := range []struct{ name, table, definition string }{
		{"missing_envelope", "event_envelopes", ""},
		{"missing_inbox", "event_inbox", ""},
		{"permissive", "event_envelopes", "AS PERMISSIVE TO framework_worker USING(current_setting('transaction_isolation')='read committed') WITH CHECK(current_setting('transaction_isolation')='read committed')"},
		{"wrong_role", "event_envelopes", "AS RESTRICTIVE TO framework_app USING(current_setting('transaction_isolation')='read committed') WITH CHECK(current_setting('transaction_isolation')='read committed')"},
		{"extra_role", "event_envelopes", "AS RESTRICTIVE TO framework_worker,framework_app USING(current_setting('transaction_isolation')='read committed') WITH CHECK(current_setting('transaction_isolation')='read committed')"},
		{"read_true", "event_inbox", "AS RESTRICTIVE TO framework_worker USING(true) WITH CHECK(current_setting('transaction_isolation')='read committed')"},
		{"write_true", "event_inbox", "AS RESTRICTIVE TO framework_worker USING(current_setting('transaction_isolation')='read committed') WITH CHECK(true)"},
		{"select_only", "event_inbox", "AS RESTRICTIVE FOR SELECT TO framework_worker USING(current_setting('transaction_isolation')='read committed')"},
	} {
		t.Run(fault.name, func(t *testing.T) {
			t.Cleanup(func() {
				_, err := f.operator.Exec(`DROP POLICY IF EXISTS event_read_committed ON runtime.` + fault.table + `; CREATE POLICY event_read_committed ON runtime.` + fault.table + ` AS RESTRICTIVE TO framework_worker USING(current_setting('transaction_isolation')='read committed') WITH CHECK(current_setting('transaction_isolation')='read committed')`)
				if err != nil {
					t.Error("review isolation-policy restoration")
				}
			})
			_, err := f.operator.Exec(`DROP POLICY event_read_committed ON runtime.` + fault.table)
			scalarSQL(t, err)
			if fault.definition != "" {
				_, err = f.operator.Exec(`CREATE POLICY event_read_committed ON runtime.` + fault.table + ` ` + fault.definition)
				scalarSQL(t, err)
			}
			check(false)
		})
		check(true)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
}

func TestR7bReviewRestrictivePolicyDeniesStaleFunctionAuthority(t *testing.T) {
	m8Database(t)
	f, h, base, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	m5Create(t, h, base, owner, "direct relation guard")
	db, worker := m8Roles(t)
	dispatch := events.Dispatcher{DB: db}
	p := m8Claim(t, dispatch)
	if dispatch.Finish(context.Background(), p, true) != nil {
		t.Fatal("review confirmed publication")
	}
	var original string
	scalarSQL(t, f.operator.QueryRow(`SELECT pg_get_functiondef('control.worker_authority()'::regprocedure)`).Scan(&original))
	guard := `current_setting('transaction_isolation')='read committed' AND `
	if strings.Count(original, guard) != 1 {
		t.Fatal("review function-guard diagnostic needs explicit inspection")
	}
	t.Cleanup(func() {
		if _, err := f.operator.Exec(original); err != nil {
			t.Error("review authority function restoration")
		}
	})
	// Operator-only fixture simulates the old function behavior; the product's
	// direct restrictive RLS must still deny envelope and inbox access.
	_, err := f.operator.Exec(strings.Replace(original, guard, "", 1))
	scalarSQL(t, err)
	tx := r7bReviewRawBind(t, f, worker, p, sql.LevelRepeatableRead)
	var authority, visible int
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM control.worker_authority()`).Scan(&authority))
	scalarSQL(t, tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&visible))
	if authority != 1 || visible != 0 {
		t.Fatal("direct restrictive policy did not override stale function authority")
	}
	_, err = tx.Exec(`INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1,$2,$3,$4)`, p.TenantID, p.ConsumerID, p.EventID, p.BodyHash)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("direct restrictive inbox policy did not reject stale function authority")
	}
	tx.Rollback()
	scalarSQL(t, func() error { _, err := f.operator.Exec(original); return err }())
	if m8Consumer(worker).Consume(context.Background(), p) != nil {
		t.Fatal("guard restoration broke normal consumer")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
}
