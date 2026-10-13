//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These tests exercise raw credentials, not only the trusted jobs.WithJob path.
func TestM9RWorkerCredentialCannotSelfIssueWorkflowLease(t *testing.T) {
	m := m9Setup(t, nil)
	m.start(t, "credential-boundary")
	var id string
	err := m.db.QueryRow(`SELECT id::text FROM control.claim_workflow($1::uuid)`, uuid(t)).Scan(&id)
	if err == nil {
		t.Fatal("worker credential issued its own cross-tenant routing lease")
	}
	if err == sql.ErrNoRows {
		t.Fatal("claim returned no job; expected privilege denial against a populated runnable fixture")
	}
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatal("expected SQL privilege denial, not an unrelated failure")
	}
}

func m9RBroker(t *testing.T, db *sql.DB) *events.RedisTransport {
	t.Helper()
	ctx := context.Background()
	var principal string
	if err := db.QueryRow(`SELECT session_user`).Scan(&principal); err != nil {
		t.Fatal(err)
	}
	operator := m8Redis(t, "EVENT_OPERATOR_REDIS_URL")
	user := "event_" + principal
	password := strings.ReplaceAll(uuid(t)+uuid(t), "-", "")
	hash := sha256.Sum256([]byte(password))
	stream := events.StreamPrefix + principal
	if err := operator.ACLSetUser(ctx, user, "on", "#"+hex.EncodeToString(hash[:]), "~"+stream, "-@all", "+ping", "+xgroup|create", "+xreadgroup", "+xautoclaim", "+xack").Err(); err != nil {
		t.Fatal("owned receiver provisioning")
	}
	t.Cleanup(func() {
		_ = operator.Del(context.Background(), stream).Err()
		_ = operator.ACLDelUser(context.Background(), user).Err()
	})
	u, err := url.Parse(os.Getenv("EVENT_RECEIVER_REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(user, password)
	broker, err := events.OpenRedis(u.String(), principal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { broker.Close() })
	return broker
}

func bindM9R(tx *sql.Tx, l jobs.WorkflowLease) (bool, error) {
	var bound bool
	err := tx.QueryRow(`SELECT control.bind_worker_job($1::uuid,$2::uuid,'workflow',$3,$4::uuid,$5)`, l.Ticket.TenantID(), l.Ticket.JobID(), l.Generation, l.Token, l.Ticket.ReleaseHash()).Scan(&bound)
	return bound, err
}

func TestM9RExactCapabilityMaskedSourceAndTransactionLifetime(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "source-confinement")
	l, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal(err)
	}
	for _, wrong := range []string{"tenant", "job", "release", "generation", "token", "kind"} {
		t.Run(wrong, func(t *testing.T) {
			tx, err := m.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			tenantID, job, hash, token, kind, gen := l.Ticket.TenantID(), l.Ticket.JobID(), l.Ticket.ReleaseHash(), l.Token, "workflow", l.Generation
			switch wrong {
			case "tenant":
				tenantID = m.f.tenantB
			case "job":
				job = uuid(t)
			case "release":
				hash = strings.Repeat("f", 64)
			case "generation":
				gen++
			case "token":
				token = uuid(t)
			case "kind":
				kind = "event"
			}
			var bound bool
			if err = tx.QueryRow(`SELECT control.bind_worker_job($1::uuid,$2::uuid,$3,$4,$5::uuid,$6)`, tenantID, job, kind, gen, token, hash).Scan(&bound); err != nil || bound {
				t.Fatal("invalid capability accepted", wrong, err)
			}
			var n int
			if err = tx.QueryRow(`SELECT count(*) FROM runtime.worker_source_record()`).Scan(&n); err != nil || n != 0 {
				t.Fatal("failed bind retained authority", err)
			}
		})
	}
	conn, err := m.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if ok, err := bindM9R(tx, l); err != nil || !ok {
		t.Fatal("legitimate capability denied", err)
	}
	var record string
	var raw, declared []byte
	if err = tx.QueryRow(`SELECT id::text,custom_data FROM runtime.worker_source_record()`).Scan(&record, &raw); err != nil || record != m.record.ID {
		t.Fatal("source port", err)
	}
	if err = m.f.operator.QueryRow(`SELECT inputs FROM runtime.workflow_bindings WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID).Scan(&declared); err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	var snapshot struct {
		Fields []string `json:"fields"`
	}
	if json.Unmarshal(raw, &values) != nil || json.Unmarshal(declared, &snapshot) != nil || len(values) == 0 {
		t.Fatal("masked source fixture")
	}
	for key := range values {
		allowed := false
		for _, f := range snapshot.Fields {
			allowed = allowed || f == key
		}
		if !allowed {
			t.Fatal("undeclared source field disclosed", key)
		}
	}
	if _, ok := values["field.contact.name"]; ok {
		t.Fatal("name not declared by workflow condition must be masked")
	}
	// Session variables surviving a pooled connection cannot restore a capability.
	if _, err = tx.Exec(`SELECT set_config('app.tenant_id',$1,false),set_config('app.job_id',$2,false)`, m.f.tenantA, x.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = conn.QueryRowContext(context.Background(), `SELECT count(*) FROM runtime.worker_source_record()`).Scan(&n); err != nil || n != 0 {
		t.Fatal("capability survived transaction", err)
	}
	tx, err = conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if ok, err := bindM9R(tx, l); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := bindM9R(tx, l); err != nil || ok {
		t.Fatal("transaction rebound", err)
	}
	if err = tx.QueryRow(`SELECT count(*) FROM runtime.worker_source_record()`).Scan(&n); err != nil || n != 0 {
		t.Fatal("failed rebind kept authority", err)
	}
}

func m9RSecondPrincipal(t *testing.T, m *m9Fixture) *sql.DB {
	t.Helper()
	u, err := url.Parse(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal("binding")
	}
	query := u.Query()
	query.Del("user")
	query.Del("password")
	query.Del("dbname")
	query.Del("database")
	u.RawQuery = query.Encode()
	adminURL := *u
	adminURL.User = url.UserPassword("postgres", os.Getenv("POSTGRES_PASSWORD"))
	admin, err := postgres.Open(adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	var actual, role string
	if err = admin.QueryRow(`SELECT current_database(),session_user`).Scan(&actual, &role); err != nil || actual != strings.TrimPrefix(u.Path, "/") || role != "postgres" {
		admin.Close()
		t.Fatal("owned database target mismatch")
	}
	principal := "m9r_worker_" + strings.ReplaceAll(uuid(t), "-", "")
	password := strings.ReplaceAll(uuid(t)+uuid(t), "-", "")
	if _, err = admin.Exec(`CREATE ROLE ` + pgx.Identifier{principal}.Sanitize() + ` LOGIN INHERIT NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION PASSWORD '` + password + `'`); err != nil {
		admin.Close()
		t.Fatal("test principal provisioning")
	}
	t.Cleanup(func() {
		_, _ = m.f.operator.Exec(`DELETE FROM control.worker_principals WHERE role_name=$1`, principal)
		if _, err := admin.Exec(`DROP ROLE ` + pgx.Identifier{principal}.Sanitize()); err != nil {
			t.Error("test principal cleanup")
		}
		admin.Close()
	})
	if _, err = admin.Exec(`GRANT framework_worker TO ` + pgx.Identifier{principal}.Sanitize()); err != nil {
		t.Fatal("test privilege group")
	}
	if _, err = m.f.operator.Exec(`SELECT control.assign_worker($1,$2::uuid,true)`, principal, m.f.tenantB); err != nil {
		t.Fatal("test assignment", err)
	}
	u.User = url.UserPassword(principal, password)
	binding, err := pgx.ParseConfig(u.String())
	if err != nil || binding.User != principal || binding.Database != actual {
		t.Fatal("worker target mismatch")
	}
	db, err := postgres.Open(u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.QueryRow(`SELECT current_database(),session_user`).Scan(&actual, &role); err != nil || actual != binding.Database || role != principal {
		t.Fatal("connected worker mismatch")
	}
	return db
}

func TestM9RPopulatedTenantsAndAuthenticatedAudience(t *testing.T) {
	m := m9Setup(t, nil)
	first := m.start(t, "tenant-a")
	bob := m.f.token(t, m.f.bobSubject, nil)
	if r := m3Call(m.handler, "POST", "/v1/tenants/"+m.f.tenantB+"/metadata/"+m.app+"/versions", bob, 0, m.raw); r.Code != 201 {
		t.Fatal("tenant B metadata", r.Code)
	}
	entity := strings.Replace(m.entity, m.f.tenantA, m.f.tenantB, 1)
	rec := m5Create(t, m.handler, entity, bob, "Tenant B protected source")
	base := strings.Replace(m.base, m.f.tenantA, m.f.tenantB, 1)
	second := m9Decode(t, m5Call(m.handler, base+"/start", bob, map[string]string{"record_id": rec.ID, "idempotency_key": "tenant-b"}))
	dbB := m9RSecondPrincipal(t, m)
	for range 2 {
		if _, found, err := jobs.ClaimWorkflow(context.Background(), m.worker.dispatcher, uuid(t)); err != nil || !found {
			t.Fatal("dispatcher issue", err)
		}
	}
	lA, found, err := jobs.AssignedWorkflow(context.Background(), m.db)
	if err != nil || !found || lA.Ticket.JobID() != first.ID {
		t.Fatal("audience A assignment", err)
	}
	lB, found, err := jobs.AssignedWorkflow(context.Background(), dbB)
	if err != nil || !found || lB.Ticket.JobID() != second.ID {
		t.Fatal("audience B assignment", err)
	}
	for _, pair := range []struct {
		db           *sql.DB
		own, foreign jobs.WorkflowLease
		record       string
	}{{m.db, lA, lB, m.record.ID}, {dbB, lB, lA, rec.ID}} {
		tx, err := pair.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`SET LOCAL ROLE framework_worker`); err != nil {
			tx.Rollback()
			t.Fatal("constrained role group", err)
		}
		if ok, err := bindM9R(tx, pair.foreign); err != nil || ok {
			tx.Rollback()
			t.Fatal("foreign audience admitted", err)
		}
		tx.Rollback()
		tx, err = pair.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`SET LOCAL ROLE framework_worker`); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if ok, err := bindM9R(tx, pair.own); err != nil || !ok {
			tx.Rollback()
			t.Fatal("own audience denied", err)
		}
		var source string
		if err = tx.QueryRow(`SELECT id::text FROM runtime.worker_source_record()`).Scan(&source); err != nil || source != pair.record {
			tx.Rollback()
			t.Fatal("source mismatch", err)
		}
		tx.Rollback()
	}
	if err = workflow.Ready(context.Background(), dbB, "framework_worker"); err != nil {
		t.Fatal("secondary principal readiness", err)
	}
	if err = (workflow.Worker{DB: dbB, Store: m.worker.Store}).Step(context.Background(), lB); err != nil {
		t.Fatal("secondary principal legitimate execution", err)
	}
	if _, err = m.f.operator.Exec(`UPDATE control.worker_principals SET active=false WHERE tenant_id=$1`, m.f.tenantA); err != nil {
		t.Fatal(err)
	}
	if _, found, err = jobs.AssignedWorkflow(context.Background(), m.db); err == nil || found {
		t.Fatal("revoked assignment remained usable")
	}
}

func TestM9RExpiredCapabilityAndPrivateContextDenial(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "lease-expiry")
	l, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal(err)
	}
	for _, sql := range []string{`SELECT * FROM control.worker_context`, `DELETE FROM control.worker_context`, `SELECT * FROM control.worker_principals`, `SELECT control.assign_worker('framework_worker',NULL,true)`} {
		_, err := m.db.Exec(sql)
		requireM9RPrivilegeDenial(t, err)
	}
	if _, err = m.f.operator.Exec(`UPDATE control.workflow_jobs SET lease_until=clock_timestamp()+interval '10 milliseconds' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	tx, err := m.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if ok, err := bindM9R(tx, l); err != nil || ok {
		t.Fatal("expired capability admitted", err)
	}
}

func TestM9ROfflineOffersDoNotExhaustExecutionBudget(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "offline-offers")
	for range 25 {
		l, found, err := jobs.ClaimWorkflow(context.Background(), m.worker.dispatcher, uuid(t))
		if err != nil || !found {
			t.Fatal("offline issue", err)
		}
		if l.Ticket.JobID() != x.ID {
			t.Fatal("wrong offered job")
		}
		if _, err = m.f.operator.Exec(`UPDATE control.workflow_jobs SET due_at=clock_timestamp()-interval '1 second',lease_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); err != nil {
			t.Fatal(err)
		}
	}
	var state string
	var failures int64
	if err := m.f.operator.QueryRow(`SELECT state,lease_failures FROM control.workflow_jobs WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID).Scan(&state, &failures); err != nil || state != "RUNNABLE" || failures != 0 {
		t.Fatal("offline issuance exhausted execution budget", state, failures, err)
	}
	l, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal("live pickup", err)
	}
	if _, found, err = jobs.AssignedWorkflow(context.Background(), m.db); err != nil || found {
		t.Fatal("offer picked up twice", err)
	}
	if err = m.worker.Step(context.Background(), l); err != nil {
		t.Fatal("returned worker failed execution", err)
	}
}

func TestM9RRepeatableReadCannotRestoreReplacedLease(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "stale-snapshot")
	l, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal(err)
	}
	tx, err := m.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow(`SELECT count(*) FROM runtime.workflow_bindings`).Scan(&n); err != nil || n != 0 {
		t.Fatal("unbound snapshot", err)
	}
	if _, err = m.f.operator.Exec(`UPDATE control.workflow_jobs SET due_at=clock_timestamp()-interval '1 second',lease_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); err != nil {
		t.Fatal(err)
	}
	newLease, found, err := jobs.ClaimWorkflow(context.Background(), m.worker.dispatcher, uuid(t))
	if err != nil || !found || newLease.Generation <= l.Generation {
		t.Fatal("rollover", err)
	}
	bound, err := bindM9R(tx, l)
	var abort *pgconn.PgError
	if bound || !errors.As(err, &abort) || abort.Code != "40001" {
		t.Fatal("stale MVCC lease did not abort at locked fence", err)
	}
}

func TestM9RWorkerForgedSessionCannotReadOrMutateRecords(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "forged-session-boundary")
	unrelated := m5Create(t, m.handler, m.entity, m.token, "Unrelated protected record")
	var issuer, subject string
	if err := m.f.operator.QueryRow(`SELECT issuer,subject FROM iam.users WHERE id=$1`, m.f.alice).Scan(&issuer, &subject); err != nil {
		t.Fatal("fixture identity unavailable")
	}
	for _, operation := range []string{"read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			tx, err := m.db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal("worker transaction unavailable")
			}
			defer tx.Rollback()
			_, err = tx.Exec(`SELECT set_config('app.tenant_id',$1,true),set_config('app.job_id',$2,true),set_config('app.actor_id',$3,true),set_config('app.auth_issuer',$4,true),set_config('app.auth_subject',$5,true)`, m.f.tenantA, x.ID, m.f.alice, issuer, subject)
			if err != nil {
				t.Fatal("fixture scope setup unavailable")
			}
			if operation == "read" {
				var n int
				err = tx.QueryRow(`SELECT count(*) FROM runtime.records WHERE id=$1`, unrelated.ID).Scan(&n)
				if err == nil && n != 0 {
					t.Fatal("forged session read unrelated record")
				}
				if err != nil {
					requireM9RPrivilegeDenial(t, err)
				}
				return
			}
			statement := `UPDATE runtime.records SET row_version=row_version+1 WHERE id=$1`
			if operation == "delete" {
				statement = `DELETE FROM runtime.records WHERE id=$1`
			}
			result, err := tx.Exec(statement, unrelated.ID)
			if err == nil {
				n, err := result.RowsAffected()
				if err != nil || n != 0 {
					t.Fatal("forged session mutated unrelated record")
				}
			} else {
				requireM9RPrivilegeDenial(t, err)
			}
		})
	}
}

func requireM9RPrivilegeDenial(t *testing.T, err error) {
	t.Helper()
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatal("unrelated SQL failure cannot establish authorization denial")
	}
}
