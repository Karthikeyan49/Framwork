//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
)

func r7bDenied(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("expected populated SQL privilege denial")
	}
}

func TestR7bSQLBackoffAndTerminalBudget(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "backoff boundary")
	db, worker := m8Roles(t)
	d := events.Dispatcher{DB: db}
	var prior events.Pointer
	var minimumDelay float64
	for attempt := 1; attempt <= events.MaxDeliveryAttempts; attempt++ {
		p := m8Claim(t, d)
		if p.Generation <= prior.Generation || attempt > 1 && (p.EventID != prior.EventID || p.BodyHash != prior.BodyHash) {
			t.Fatal("retry lost original identity or fence")
		}
		expected := math.Min(60, .5*math.Pow(2, float64(attempt-1)))
		var lease, delay float64
		if err := f.operator.QueryRow(`SELECT extract(epoch FROM d.lease_until-a.claimed_at),extract(epoch FROM d.next_at-d.lease_until) FROM control.event_deliveries d JOIN control.event_attempts a USING(tenant_id,id,generation) WHERE d.tenant_id=$1 AND d.id=$2`, p.TenantID, p.EventID).Scan(&lease, &delay); err != nil || lease < 4.9 || lease > 5.1 || math.Abs(delay-expected) > .01 {
			t.Fatal("publication lease/backoff bounds")
		}
		bad := p
		bad.Generation++
		if d.Finish(context.Background(), bad, true) == nil {
			t.Fatal("stale/forged generation confirmed")
		}
		if err := d.Finish(context.Background(), p, false); err != nil {
			t.Fatal("known broker failure transition")
		}
		var state, outcome string
		var attempts int
		if err := f.operator.QueryRow(`SELECT d.state,d.attempts,a.outcome,extract(epoch FROM d.next_at-a.finished_at) FROM control.event_deliveries d JOIN control.event_attempts a USING(tenant_id,id,generation) WHERE d.tenant_id=$1 AND d.id=$2`, p.TenantID, p.EventID).Scan(&state, &attempts, &outcome, &delay); err != nil || attempts != attempt || outcome != "PUBLISH_FAILED" || math.Abs(delay-expected) > .01 {
			t.Fatal("known failure evidence/backoff")
		}
		if state != map[bool]string{true: "DEAD", false: "PENDING"}[attempt == events.MaxDeliveryAttempts] {
			t.Fatal("incorrect terminal attempt boundary")
		}
		if _, found, err := d.Claim(context.Background()); err != nil || found {
			t.Fatal("retry ignored due time or terminal state")
		}
		if attempt < events.MaxDeliveryAttempts {
			minimumDelay += expected
			// Explicit accelerated boundary fixture, not the real outage proof.
			m8Expire(t, f.operator, p)
		}
		prior = p
	}
	if minimumDelay != 783.5 {
		t.Fatal("known failure schedule no longer tolerates ten minutes")
	}
	for _, role := range []*sql.DB{f.db, worker} {
		r7bDenied(t, role, `SELECT control.claim_event($1::uuid)`, uuid(t))
	}
	for _, role := range []*sql.DB{f.db, worker, db} {
		r7bDenied(t, role, `SELECT control.event_retry_delay(1)`)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
	var count int
	if err := f.operator.QueryRow(`SELECT count(*) FROM control.event_attempts WHERE tenant_id=$1 AND id=$2`, prior.TenantID, prior.EventID).Scan(&count); err != nil || count != events.MaxDeliveryAttempts {
		t.Fatal("attempt history lost")
	}
}

func TestR7bPublicationAndReceiptDeadlinesFenceIndependently(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "receipt boundary")
	db, worker := m8Roles(t)
	d := events.Dispatcher{DB: db}
	first := m8Claim(t, d)
	if err := d.Finish(context.Background(), first, true); err != nil {
		t.Fatal("confirmed publish")
	}
	var window float64
	if err := f.operator.QueryRow(`SELECT extract(epoch FROM d.next_at-a.published_at) FROM control.event_deliveries d JOIN control.event_attempts a USING(tenant_id,id,generation) WHERE d.tenant_id=$1 AND d.id=$2`, first.TenantID, first.EventID).Scan(&window); err != nil || math.Abs(window-900) > .01 {
		t.Fatal("receipt window does not match contract")
	}
	if _, err := f.operator.Exec(`UPDATE control.event_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, first.TenantID, first.EventID); err != nil {
		t.Fatal("explicit expired broker-lease fixture")
	}
	if d.Finish(context.Background(), first, true) == nil {
		t.Fatal("late duplicate publisher renewed receipt authority")
	}
	if _, found, err := d.Claim(context.Background()); err != nil || found {
		t.Fatal("broker lease expiry reissued a live SENT receipt")
	}
	if err := m8Consumer(worker).Consume(context.Background(), first); err != nil {
		t.Fatal("broker lease incorrectly expired the published receipt")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	m5Create(t, h, base, token, "expired receipt")
	old := m8Claim(t, d)
	if err := d.Finish(context.Background(), old, true); err != nil {
		t.Fatal("second confirmed publish")
	}
	m8Expire(t, f.operator, old) // Explicit accelerated receipt-expiry boundary.
	if err := m8Consumer(worker).Consume(context.Background(), old); !errors.Is(err, events.ErrNotReady) {
		t.Fatal("expired event capability accepted")
	}
	next := m8Claim(t, d)
	if next.EventID != old.EventID || next.BodyHash != old.BodyHash || next.Generation <= old.Generation || next.LeaseToken == old.LeaseToken {
		t.Fatal("receipt reissue did not fence exact original bytes")
	}
	if d.Finish(context.Background(), old, true) == nil || d.Finish(context.Background(), next, true) != nil {
		t.Fatal("old/current publish fence")
	}
	if err := m8Consumer(worker).Consume(context.Background(), old); !errors.Is(err, events.ErrNotReady) {
		t.Fatal("old pointer acquired new generation authority")
	}
	if err := m8Consumer(worker).Consume(context.Background(), next); err != nil {
		t.Fatal("reissued exact event failed")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 2)
}

func TestR7bReadinessRejectsPolicyDrift(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	db, worker := m8Roles(t)
	roles := map[string]*sql.DB{"framework_app": f.db, "framework_worker": worker, "framework_dispatcher": db}
	check := func(want bool) {
		t.Helper()
		for role, connection := range roles {
			if (events.Ready(context.Background(), connection, role) == nil) != want {
				t.Fatal("unexpected event readiness", role, want)
			}
		}
	}
	check(true)
	var original string
	if err := f.operator.QueryRow(`SELECT pg_get_functiondef('control.event_delivery_policy()'::regprocedure)`).Scan(&original); err != nil {
		t.Fatal("owned policy definition")
	}
	if _, err := f.operator.Exec(`CREATE OR REPLACE FUNCTION control.event_delivery_policy() RETURNS TABLE(version text,lease_ms integer,receipt_ms integer,max_attempts integer,retry_base_ms integer,retry_cap_ms integer) LANGUAGE sql IMMUTABLE SET search_path=pg_catalog AS 'SELECT ''wrong-policy''::text,5000,900000,20,500,60000'`); err != nil {
		t.Fatal("policy drift fixture")
	}
	check(false)
	if _, err := f.operator.Exec(original); err != nil {
		t.Fatal("owned policy restoration")
	}
	check(true)
	if _, err := f.operator.Exec(`REVOKE EXECUTE ON FUNCTION control.event_delivery_policy() FROM framework_app`); err != nil {
		t.Fatal("policy grant fixture")
	}
	if events.Ready(context.Background(), f.db, "framework_app") == nil {
		t.Fatal("missing policy grant ready")
	}
}

func TestR7bReadinessRequiresPrivateRetryDependency(t *testing.T) {
	for _, fault := range []string{"missing", "runtime_grant", "custom_worker_grant"} {
		t.Run(fault, func(t *testing.T) {
			m8Database(t)
			f, h, base, token, _ := m5Fixture(t)
			m5Create(t, h, base, token, "dependency fixture")
			db, worker := m8Roles(t)
			roles := map[string]*sql.DB{"framework_app": f.db, "framework_worker": worker, "framework_dispatcher": db}
			if events.Ready(context.Background(), db, "framework_dispatcher") != nil {
				t.Fatal("positive readiness control")
			}
			statement := `DROP FUNCTION control.event_retry_delay(integer)`
			if fault == "runtime_grant" {
				statement = `GRANT EXECUTE ON FUNCTION control.event_retry_delay(integer) TO framework_dispatcher`
			}
			if fault == "custom_worker_grant" {
				custom := m9RSecondPrincipal(t, &m9Fixture{f: f})
				var principal string
				if err := custom.QueryRow(`SELECT session_user`).Scan(&principal); err != nil || events.Ready(context.Background(), custom, "framework_worker") != nil {
					t.Fatal("custom worker positive control")
				}
				statement = `GRANT EXECUTE ON FUNCTION control.event_retry_delay(integer) TO ` + pgx.Identifier{principal}.Sanitize()
				t.Cleanup(func() {
					if _, err := f.operator.Exec(`REVOKE EXECUTE ON FUNCTION control.event_retry_delay(integer) FROM ` + pgx.Identifier{principal}.Sanitize()); err != nil {
						t.Error("owned helper grant cleanup")
					}
				})
				roles = map[string]*sql.DB{"framework_worker": custom}
			}
			if _, err := f.operator.Exec(statement); err != nil {
				t.Fatal("owned dependency drift fixture")
			}
			if fault == "missing" {
				if _, found, err := (events.Dispatcher{DB: db}).Claim(context.Background()); err == nil || found {
					t.Fatal("populated claim did not expose missing dependency")
				}
			}
			for role, connection := range roles {
				if events.Ready(context.Background(), connection, role) == nil {
					t.Fatal("dependency/grant drift reported ready", role)
				}
			}
		})
	}
}

func r7bManifest(t *testing.T, db *sql.DB) []string {
	t.Helper()
	result := []string{}
	for _, table := range []string{"control.event_deliveries", "control.event_attempts", "runtime.event_envelopes", "runtime.event_inbox", "control.event_replays", "runtime.records", "runtime.outbox_intents", "audit.entries"} {
		var hash string
		// Fixed trusted table list; JSON and immutable bytes stay inside the DB.
		if err := db.QueryRow(`SELECT encode(sha256(convert_to(coalesce(jsonb_agg(j ORDER BY j::text)::text,''),'UTF8')),'hex') FROM (SELECT to_jsonb(r) j FROM ` + table + ` r) s`).Scan(&hash); err != nil {
			t.Fatal("owned evidence manifest")
		}
		result = append(result, hash)
	}
	return result
}

func TestR7bForwardMigrationPreservesIssuedStateAndEvidence(t *testing.T) {
	m8DatabaseAt(t, 54)
	f, h, base, token, _ := m5Fixture(t)
	db, worker := m8Roles(t)
	d := events.Dispatcher{DB: db}
	if events.Ready(context.Background(), f.db, "framework_app") == nil {
		t.Fatal("new runtime accepted pre55 delivery policy")
	}
	m5Create(t, h, base, token, "complete before upgrade")
	p := m8Claim(t, d)
	if d.Finish(context.Background(), p, true) != nil || m8Consumer(worker).Consume(context.Background(), p) != nil {
		t.Fatal("completed legacy fixture")
	}
	m5Create(t, h, base, token, "sent before upgrade")
	p = m8Claim(t, d)
	if d.Finish(context.Background(), p, true) != nil {
		t.Fatal("sent legacy fixture")
	}
	m5Create(t, h, base, token, "dead before upgrade")
	for i := 1; i <= 5; i++ { // Intentionally exercise the actual pre55 five-attempt policy.
		p = m8Claim(t, d)
		if d.Finish(context.Background(), p, false) != nil {
			t.Fatal("legacy terminal fixture")
		}
		if i < 5 {
			m8Expire(t, f.operator, p)
		}
	}
	m5Create(t, h, base, token, "leased before upgrade")
	_ = m8Claim(t, d)
	m5Create(t, h, base, token, "pending before upgrade")
	var states int
	if err := f.operator.QueryRow(`SELECT count(DISTINCT state) FROM control.event_deliveries`).Scan(&states); err != nil || states != 6 {
		t.Fatal("migration fixture must populate all six states")
	}
	before := r7bManifest(t, f.operator)
	if err := worker.Close(); err != nil {
		t.Fatal("drain owned worker before migration cutover")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, f.operator, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal("migration provider")
	}
	if applied, err := provider.Up(context.Background()); err != nil || len(applied) != 3 {
		t.Fatal("additive migrations55 through57")
	}
	if applied, err := provider.Up(context.Background()); err != nil || len(applied) != 0 {
		t.Fatal("migrations55 through57 repeatability")
	}
	after := r7bManifest(t, f.operator)
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("migration rewrote issued state, bytes or evidence", i)
		}
	}
	_, upgradedWorker := m8Roles(t)
	for role, connection := range map[string]*sql.DB{"framework_app": f.db, "framework_worker": upgradedWorker, "framework_dispatcher": db} {
		if events.Ready(context.Background(), connection, role) != nil {
			t.Fatal("migrated event contract not ready", role)
		}
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
}
