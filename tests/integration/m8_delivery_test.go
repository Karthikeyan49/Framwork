//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every M8 test owns a disposable database; no customer rows or old event due
// times are changed to make dispatch tests convenient.
func m8Database(t *testing.T) {
	t.Helper()
	if environment := os.Getenv("APP_ENV"); environment != "development" && environment != "test" {
		t.Fatal("M8 integration fixtures require explicit development/test environment")
	}
	for _, key := range []string{"DATABASE_URL", "MIGRATION_DATABASE_URL", "WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		binding, e := pgx.ParseConfig(os.Getenv(key))
		if e != nil || (binding.Host != "127.0.0.1" && binding.Host != "localhost" && binding.Host != "::1") {
			t.Fatal("M8 disposable database fixtures require local driver bindings")
		}
		for _, fallback := range binding.Fallbacks {
			if fallback.Host != "127.0.0.1" && fallback.Host != "localhost" && fallback.Host != "::1" {
				t.Fatal("M8 database fallback must remain local")
			}
		}
	}
	ctx := context.Background()
	adminURL, e := url.Parse(os.Getenv("DATABASE_URL"))
	if e != nil {
		t.Fatal("admin URL")
	}
	adminURL.User = url.UserPassword("postgres", os.Getenv("POSTGRES_PASSWORD"))
	admin, e := postgres.Open(adminURL.String())
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	name := "m8_delivery_" + strings.ReplaceAll(uuid(t), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, e = admin.ExecContext(ctx, `CREATE DATABASE `+quoted+` OWNER framework_migrator`); e != nil {
		t.Fatal("disposable DB creation")
	}
	t.Cleanup(func() {
		db, e := postgres.Open(adminURL.String())
		if e != nil {
			t.Error(e)
			return
		}
		defer db.Close()
		if _, e = db.Exec(`DROP DATABASE ` + quoted + ` WITH (FORCE)`); e != nil {
			t.Error("disposable DB cleanup")
		}
	})
	if _, e = admin.ExecContext(ctx, `REVOKE ALL ON DATABASE `+quoted+` FROM PUBLIC`); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.ExecContext(ctx, `GRANT CONNECT ON DATABASE `+quoted+` TO framework_app,framework_worker,framework_dispatcher`); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"DATABASE_URL", "MIGRATION_DATABASE_URL", "WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		u, e := url.Parse(os.Getenv(key))
		if e != nil || u.Hostname() == "" {
			t.Fatal("dedicated binding missing")
		}
		u.Path = "/" + name
		t.Setenv(key, u.String())
	}
	m, e := postgres.Open(os.Getenv("MIGRATION_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	p, e := goose.NewProvider(goose.DialectPostgres, m, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.UpTo(ctx, 17); e != nil {
		t.Fatal("pre-M8 migrations", e)
	}
	legacyTenant, legacyID := uuid(t), uuid(t)
	if _, e = m.Exec(`INSERT INTO runtime.outbox_intents(tenant_id,id,actor_id,app_id,metadata_revision,ir_hash,entity_id,record_id,record_version,event_type) VALUES($1,$2,$3,'app.legacy',1,$4,'entity.legacy',$5,1,'record.created')`, legacyTenant, legacyID, uuid(t), strings.Repeat("a", 64), uuid(t)); e != nil {
		t.Fatal("legacy fixture")
	}
	if _, e = p.Up(ctx); e != nil {
		t.Fatal("M8 migration", e)
	}
	if applied, e := p.Up(ctx); e != nil || len(applied) != 0 {
		t.Fatal("migration repeatability")
	}
	var state string
	var n int
	if e = m.QueryRow(`SELECT state,(SELECT count(*) FROM runtime.event_envelopes) FROM control.event_deliveries WHERE tenant_id=$1 AND id=$2`, legacyTenant, legacyID).Scan(&state, &n); e != nil || state != "LEGACY_PAUSED" || n != 0 {
		t.Fatal("legacy intent was rewritten or broadcast")
	}
	if _, e = m.Exec(`CREATE TABLE runtime.m8_test_effects(tenant_id uuid NOT NULL,event_id uuid NOT NULL,PRIMARY KEY(tenant_id,event_id))`); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Exec(`SELECT runtime.install_tenant_rls('runtime.m8_test_effects')`); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Exec(`REVOKE ALL ON runtime.m8_test_effects FROM framework_app; CREATE POLICY test_worker ON runtime.m8_test_effects TO framework_worker USING(iam.worker_tenant_scope(tenant_id) AND event_id::text=current_setting('app.job_id',true)) WITH CHECK(iam.worker_tenant_scope(tenant_id) AND event_id::text=current_setting('app.job_id',true)); GRANT SELECT,INSERT ON runtime.m8_test_effects TO framework_worker`); e != nil {
		t.Fatal(e)
	}
}

func m8Roles(t *testing.T) (*sql.DB, *sql.DB) {
	t.Helper()
	d, e := postgres.Open(os.Getenv("DISPATCHER_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	w, e := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close(); w.Close() })
	return d, w
}
func m8Consumer(db *sql.DB) events.Consumer {
	return events.Consumer{DB: db, Policy: policy.Native{}, Audit: audit.Postgres{}, Handler: func(ctx context.Context, q postgres.Executor, a tenant.Access, e events.Envelope) error {
		_, err := q.ExecContext(ctx, `INSERT INTO runtime.m8_test_effects(tenant_id,event_id) VALUES($1::uuid,$2::uuid)`, a.TenantID(), e.ID)
		return err
	}}
}
func m8Claim(t *testing.T, d events.Dispatcher) events.Pointer {
	t.Helper()
	p, found, e := d.Claim(context.Background())
	if e != nil || !found {
		t.Fatal("expected event claim", e)
	}
	return p
}
func m8Counts(t *testing.T, db *sql.DB, tenant string) (int, int, int) {
	t.Helper()
	var inbox, effects, auditN int
	if e := db.QueryRow(`SELECT (SELECT count(*) FROM runtime.event_inbox WHERE tenant_id=$1),(SELECT count(*) FROM runtime.m8_test_effects WHERE tenant_id=$1),(SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='events.consume')`, tenant).Scan(&inbox, &effects, &auditN); e != nil {
		t.Fatal(e)
	}
	return inbox, effects, auditN
}
func m8AssertCounts(t *testing.T, db *sql.DB, tenant string, n int) {
	t.Helper()
	a, b, c := m8Counts(t, db, tenant)
	if a != n || b != n || c != n {
		t.Fatalf("partial/duplicate commit: inbox=%d effects=%d audit=%d want=%d", a, b, c, n)
	}
}
func m8Expire(t *testing.T, db *sql.DB, p events.Pointer) {
	t.Helper()
	if _, e := db.Exec(`UPDATE control.event_deliveries SET next_at=clock_timestamp()-interval '1 second',lease_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EventID); e != nil {
		t.Fatal(e)
	}
}

func TestM8ImmutableEnvelopeOrderAndDeleteSequence(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	r := m5Create(t, h, base, token, "snapshot")
	for _, c := range []data.Command{{Operation: "update", ID: r.ID, ExpectedVersion: 1, Values: m5Values("updated")}, {Operation: "delete", ID: r.ID, ExpectedVersion: 2}} {
		if got := m5Call(h, base+"/commands", token, c); got.Code != 200 {
			t.Fatal(got.Code)
		}
	}
	d, w := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	consumer := m8Consumer(w)
	for sequence := int64(1); sequence <= 3; sequence++ {
		p := m8Claim(t, dispatch)
		var body []byte
		if e := f.operator.QueryRow(`SELECT body FROM runtime.event_envelopes WHERE tenant_id=$1 AND id=$2`, f.tenantA, p.EventID).Scan(&body); e != nil {
			t.Fatal(e)
		}
		e, err := events.DecodeEnvelope(body, p)
		if err != nil || e.AggregateSequence != sequence || e.AppID != app || e.RequestID == "" || e.AggregateID != r.ID {
			t.Fatal("envelope source/sequence mismatch", err)
		}
		if sequence > 1 && e.Payload.RecordVersion != 2 {
			t.Fatal("delete version was changed to invent sequence")
		}
		if _, found, err := dispatch.Claim(context.Background()); err != nil || found {
			t.Fatal("successor bypassed unfinished predecessor")
		}
		if err = dispatch.Finish(context.Background(), p, true); err != nil {
			t.Fatal(err)
		}
		if err = consumer.Consume(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		m8AssertCounts(t, f.operator, f.tenantA, int(sequence))
		if _, err = f.operator.Exec(`UPDATE runtime.event_envelopes SET body=body WHERE tenant_id=$1 AND id=$2`, f.tenantA, p.EventID); err == nil {
			t.Fatal("immutable body changed")
		}
	}
	var n int
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.outbox_intents WHERE tenant_id=$1`, f.tenantA).Scan(&n); err != nil || n != 3 {
		t.Fatal("intent retention")
	}
}

func TestM8InboxAtomicFailureDeduplicationAndRevocation(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "atomic")
	d, w := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	p := m8Claim(t, dispatch)
	if err := dispatch.Finish(context.Background(), p, true); err != nil {
		t.Fatal(err)
	}
	consumer := m8Consumer(w)
	consumer.Policy = m8BadEvidencePolicy{}
	if consumer.Consume(context.Background(), p) == nil {
		t.Fatal("missing policy provenance accepted")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
	consumer.Policy = policy.Native{}
	goodHandler := consumer.Handler
	consumer.Handler = func(ctx context.Context, q postgres.Executor, a tenant.Access, e events.Envelope) error {
		if err := goodHandler(ctx, q, a, e); err != nil {
			return err
		}
		return errors.New("injected after local effect")
	}
	if consumer.Consume(context.Background(), p) == nil {
		t.Fatal("failed effect accepted")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
	consumer.Handler = goodHandler
	consumer.Audit = failedAudit{}
	if consumer.Consume(context.Background(), p) == nil {
		t.Fatal("failed audit accepted")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
	consumer.Audit = audit.Postgres{}
	if _, e := f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice); e != nil {
		t.Fatal(e)
	}
	if consumer.Consume(context.Background(), p) == nil {
		t.Fatal("revoked membership consumed")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
	if _, e := f.operator.Exec(`UPDATE iam.memberships SET active=true WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice); e != nil {
		t.Fatal(e)
	}
	if err := consumer.Consume(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Consume(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	bad := p
	bad.BodyHash = strings.Repeat("b", 64)
	if consumer.Consume(context.Background(), bad) == nil {
		t.Fatal("changed body/event ID accepted")
	}
	bad = p
	bad.TenantID = f.tenantB
	if consumer.Consume(context.Background(), bad) == nil {
		t.Fatal("cross-tenant routing accepted")
	}
	bad = p
	bad.LeaseToken = uuid(t)
	if consumer.Consume(context.Background(), bad) == nil {
		t.Fatal("forged lease accepted")
	}
	if _, e := f.operator.Exec(`DELETE FROM runtime.event_inbox WHERE tenant_id=$1`, f.tenantA); e == nil {
		t.Fatal("dedup history removed")
	}
	var replay bool
	if e := f.operator.QueryRow(`SELECT control.replay_event($1::uuid,$2::uuid,$3,'test.operator','completed replay preserves inbox')`, p.TenantID, p.EventID, p.Generation).Scan(&replay); e != nil || !replay {
		t.Fatal("completed replay review")
	}
	if _, found, e := dispatch.Claim(context.Background()); e != nil || found {
		t.Fatal("completed replay reexecuted effect")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
}

type m8BadEvidencePolicy struct{}

func (m8BadEvidencePolicy) Decide(context.Context, policy.Subject, string, policy.Resource, policy.Context) policy.Decision {
	return policy.Decision{Allow: true, Obligations: []string{"audit"}}
}

func TestM8ConcurrentClaimsConsumersAndWorkerIsolation(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	for i := 0; i < 4; i++ {
		m5Create(t, h, base, token, "concurrent")
	}
	d, w := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	start := make(chan struct{})
	pointers := make(chan events.Pointer, 4)
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p, found, e := dispatch.Claim(context.Background())
			if e != nil || !found {
				errs <- events.ErrDelivery
				return
			}
			pointers <- p
		}()
	}
	close(start)
	wg.Wait()
	close(pointers)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	unique := map[string]bool{}
	var chosen events.Pointer
	for p := range pointers {
		if unique[p.EventID] {
			t.Fatal("double fenced claim")
		}
		unique[p.EventID] = true
		if e := dispatch.Finish(context.Background(), p, true); e != nil {
			t.Fatal(e)
		}
		chosen = p
	}
	if len(unique) != 4 {
		t.Fatal("missing claims")
	}
	consumer := m8Consumer(w)
	wg = sync.WaitGroup{}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = consumer.Consume(context.Background(), chosen) }()
	}
	wg.Wait()
	if e := consumer.Consume(context.Background(), chosen); e != nil {
		t.Fatal(e)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	ticket, _, _, _, e := jobs.EventTicket(context.Background(), w, chosen.TenantID, chosen.EventID, chosen.BodyHash, chosen.ConsumerID, chosen.Generation, chosen.LeaseToken)
	if e != nil {
		t.Fatal(e)
	}
	if e = jobs.WithJob(context.Background(), w, ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var n int
		if e := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.event_envelopes`).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			t.Fatal("worker exposed another event envelope")
		}
		if _, e := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.tenantB); e != nil {
			return e
		}
		if e := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.event_envelopes`).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			t.Fatal("worker tenant switch exposed payload")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	for _, db := range []*sql.DB{d, f.db} {
		if _, e := db.Query(`SELECT body FROM runtime.event_envelopes`); e == nil {
			t.Fatal("non-worker accessed payload")
		}
	}
	if _, e = w.Exec(`SELECT control.claim_event($1::uuid)`, uuid(t)); e == nil {
		t.Fatal("worker dispatch privilege")
	}
	if _, e = d.Exec(`SELECT control.replay_event($1::uuid,$2::uuid,$3,'bad','bad')`, chosen.TenantID, chosen.EventID, chosen.Generation); e == nil {
		t.Fatal("dispatcher replay privilege")
	}
}

func TestM8LeaseFencingBoundedRetriesDeadLetterAndReplay(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "retry")
	d, w := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	first := m8Claim(t, dispatch)
	m8Expire(t, f.operator, first)
	p := m8Claim(t, dispatch)
	if p.Generation <= first.Generation {
		t.Fatal("fence did not advance")
	}
	if dispatch.Finish(context.Background(), first, true) == nil {
		t.Fatal("stale dispatcher confirmed")
	}
	for attempt := 2; attempt <= 5; attempt++ {
		if e := dispatch.Finish(context.Background(), p, false); e != nil {
			t.Fatal(e)
		}
		if attempt < 5 {
			m8Expire(t, f.operator, p)
			p = m8Claim(t, dispatch)
		}
	}
	var state string
	if e := f.operator.QueryRow(`SELECT state FROM control.event_deliveries WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EventID).Scan(&state); e != nil || state != "DEAD" {
		t.Fatal("retry budget not terminal")
	}
	var ok bool
	if e := f.operator.QueryRow(`SELECT control.replay_event($1::uuid,$2::uuid,$3,'test.operator','reviewed transient outage')`, p.TenantID, p.EventID, p.Generation-1).Scan(&ok); e != nil || ok {
		t.Fatal("stale replay accepted")
	}
	if e := f.operator.QueryRow(`SELECT control.replay_event($1::uuid,$2::uuid,$3,'test.operator','reviewed transient outage')`, p.TenantID, p.EventID, p.Generation).Scan(&ok); e != nil || !ok {
		t.Fatal("reviewed replay failed")
	}
	resumed := m8Claim(t, dispatch)
	if resumed.Generation <= p.Generation || resumed.BodyHash != p.BodyHash || resumed.EventID != p.EventID {
		t.Fatal("replay changed identity/body or reused fence")
	}
	if e := dispatch.Finish(context.Background(), resumed, true); e != nil {
		t.Fatal(e)
	}
	if e := m8Consumer(w).Consume(context.Background(), resumed); e != nil {
		t.Fatal(e)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	// Reaching DEAD from an expired SENT lease returns no new routing row. That
	// empty result must commit the terminal update, rather than roll it back.
	m5Create(t, h, base, token, "receipt timeout")
	for attempt := 0; attempt < 5; attempt++ {
		p = m8Claim(t, dispatch)
		if e := dispatch.Finish(context.Background(), p, true); e != nil {
			t.Fatal(e)
		}
		m8Expire(t, f.operator, p)
	}
	if _, found, e := dispatch.Claim(context.Background()); e != nil || found {
		t.Fatal("expired budget produced another claim")
	}
	if e := f.operator.QueryRow(`SELECT state FROM control.event_deliveries WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EventID).Scan(&state); e != nil || state != "DEAD" {
		t.Fatal("empty claim rolled back terminal state")
	}
	if e := m8Consumer(w).Consume(context.Background(), p); !errors.Is(e, events.ErrDead) {
		t.Fatal("durable receipt timeout not terminal")
	}
}

type m8AckTransport struct {
	ack     int
	failure bool
}

func (t *m8AckTransport) Publish(context.Context, events.Pointer) error { return nil }
func (t *m8AckTransport) Read(context.Context) (events.Message, error)  { return events.Message{}, nil }
func (t *m8AckTransport) Ack(context.Context, string) error {
	t.ack++
	if t.failure {
		return errors.New("lost broker acknowledgment")
	}
	return nil
}
func TestM8AckAfterCommitAndDurableQuarantine(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "ack")
	d, w := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	p := m8Claim(t, dispatch)
	if e := dispatch.Finish(context.Background(), p, true); e != nil {
		t.Fatal(e)
	}
	raw, _ := p.Bytes()
	consumer := m8Consumer(w)
	transport := &m8AckTransport{}
	consumer.Policy = nil
	if consumer.Handle(context.Background(), transport, events.Message{ID: "1-0", Body: raw}) == nil || transport.ack != 0 {
		t.Fatal("missing policy discarded a valid event")
	}
	consumer.Policy = policy.Native{}
	consumer.Audit = failedAudit{}
	if consumer.Handle(context.Background(), transport, events.Message{ID: "1-0", Body: raw}) == nil || transport.ack != 0 {
		t.Fatal("ack before confirmed transaction")
	}
	consumer.Audit = audit.Postgres{}
	transport.failure = true
	if consumer.Handle(context.Background(), transport, events.Message{ID: "1-0", Body: raw}) == nil || transport.ack != 1 {
		t.Fatal("lost acknowledgment hidden")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	transport.failure = false
	if e := consumer.Handle(context.Background(), transport, events.Message{ID: "1-0", Body: raw}); e != nil {
		t.Fatal(e)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	if e := consumer.Handle(context.Background(), transport, events.Message{ID: "2-0", Body: []byte(`{"sql":"secret must never persist"}`)}); e != nil {
		t.Fatal(e)
	}
	var n int
	if e := f.operator.QueryRow(`SELECT count(*) FROM control.event_quarantine WHERE reason='malformed'`).Scan(&n); e != nil || n != 1 {
		t.Fatal("malformed pointer not quarantined durably")
	}
}

func m8Redis(t *testing.T, env string) *redis.Client {
	t.Helper()
	u, err := url.Parse(os.Getenv(env))
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		t.Fatal("M8 broker recovery fixtures require a local Redis binding")
	}
	o, e := redis.ParseURL(os.Getenv(env))
	if e != nil {
		t.Fatal("Redis binding")
	}
	o.MaxRetries = -1
	o.ContextTimeoutEnabled = true
	c := redis.NewClient(o)
	t.Cleanup(func() { c.Close() })
	if c.Ping(context.Background()).Err() != nil {
		t.Fatal("Redis unavailable")
	}
	return c
}
func TestM8RedisACLStreamLossAndPendingRecovery(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "transport")
	d, w := m8Roles(t)
	sender, e := events.OpenRedis(os.Getenv("EVENT_SENDER_REDIS_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer sender.Close()
	receiver, e := events.OpenRedis(os.Getenv("EVENT_RECEIVER_REDIS_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer receiver.Close()
	operator := m8Redis(t, "EVENT_OPERATOR_REDIS_URL")
	ctx := context.Background()
	if operator.Del(ctx, events.Stream).Err() != nil {
		t.Fatal("owned preview stream reset")
	}
	t.Cleanup(func() { _ = operator.Del(context.Background(), events.Stream).Err() })
	dispatch := events.Dispatcher{DB: d, Transport: sender}
	p := m8Claim(t, dispatch)
	if e = sender.Publish(ctx, p); e != nil {
		t.Fatal(e)
	}
	if e = m8Consumer(w).Consume(ctx, p); !errors.Is(e, events.ErrNotReady) {
		t.Fatal("unconfirmed publish consumed")
	}
	if e = dispatch.Finish(ctx, p, true); e != nil {
		t.Fatal(e)
	}
	// Real local Redis restart loses its ephemeral stream. No PostgreSQL data/volume is deleted.
	command := exec.Command("docker", "compose", "--env-file", ".env", "-f", "infra/compose/compose.yaml", "restart", "redis")
	command.Dir = "../.."
	if out, e := command.CombinedOutput(); e != nil {
		t.Fatalf("local Redis restart: %s", out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for operator.Ping(ctx).Err() != nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n, e := operator.XLen(ctx, events.Stream).Result(); e != nil || n != 0 {
		t.Fatal("Redis stream unexpectedly survived reset")
	}
	m8Expire(t, f.operator, p)
	again := m8Claim(t, dispatch)
	if again.EventID != p.EventID || again.BodyHash != p.BodyHash {
		t.Fatal("broker loss changed event")
	}
	if e = sender.Publish(ctx, again); e != nil {
		t.Fatal(e)
	}
	if e = dispatch.Finish(ctx, again, true); e != nil {
		t.Fatal(e)
	}
	m, e := receiver.Read(ctx)
	if e != nil || m.ID == "" {
		t.Fatal("message unavailable")
	}
	peer, e := events.OpenRedis(os.Getenv("EVENT_RECEIVER_REDIS_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer peer.Close()
	time.Sleep(1100 * time.Millisecond)
	redelivery, e := peer.Read(ctx)
	if e != nil || redelivery.ID != m.ID {
		t.Fatal("dead consumer pending message not claimed")
	}
	if e = m8Consumer(w).Handle(ctx, peer, redelivery); e != nil {
		t.Fatal(e)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	cache := m8Redis(t, "REDIS_URL")
	worker := m8Redis(t, "EVENT_RECEIVER_REDIS_URL")
	if cache.XAdd(ctx, &redis.XAddArgs{Stream: events.Stream, Values: map[string]any{"pointer": "bad"}}).Err() == nil {
		t.Fatal("API cache published")
	}
	if worker.XAdd(ctx, &redis.XAddArgs{Stream: events.Stream, Values: map[string]any{"pointer": "bad"}}).Err() == nil {
		t.Fatal("receiver published")
	}
	if _, err := sender.Read(ctx); err == nil {
		t.Fatal("sender read messages")
	}
	senderClient := m8Redis(t, "EVENT_SENDER_REDIS_URL")
	if senderClient.XAck(ctx, events.Stream, events.Group, "1-0").Err() == nil {
		t.Fatal("sender acknowledged")
	}
	cacheURL, _ := url.Parse(os.Getenv("REDIS_URL"))
	password, _ := cacheURL.User.Password()
	if cache.Do(ctx, "AUTH", "event_operator", password).Err() == nil {
		t.Fatal("cache credential escalated to broker operator")
	}
	var saved events.Pointer
	if json.Unmarshal(m.Body, &saved) != nil || saved.EventID != again.EventID {
		t.Fatal("transport payload was not a reference")
	}
}

type m8AfterAuditFailure struct{}

func (m8AfterAuditFailure) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	if err := (audit.Postgres{}).Append(ctx, q, e); err != nil {
		return err
	}
	return errors.New("injected after audit append")
}
func TestM8ConsumerMutationAuditDerivedOutboxAndInboxAtomic(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	record := m5Create(t, h, base, token, "before consumer")
	d, w := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	p := m8Claim(t, dispatch)
	if e := dispatch.Finish(context.Background(), p, true); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	source, e := m3Registry(t).Parse([]byte(strings.Replace(string(raw), "app.contacts", app, 1)))
	if e != nil {
		t.Fatal(e)
	}
	program, diagnostics := compiler.Compile(source, compiler.PlatformCatalog())
	if !program.Valid() {
		t.Fatal(diagnostics)
	}
	scope, e := data.NewScope(program, 1)
	if e != nil {
		t.Fatal(e)
	}
	consumer := m8Consumer(w)
	effect := consumer.Handler
	consumer.Handler = func(ctx context.Context, q postgres.Executor, a tenant.Access, envelope events.Envelope) error {
		if envelope.IRHash != program.Hash() {
			return events.ErrDelivery
		}
		u, e := (identity.Store{}).ResolveWorker(ctx, q, envelope.ID)
		if e != nil {
			return e
		}
		subject := policy.Subject{User: u, Access: a}
		native := policy.Native{}
		guard := data.Guard(func(ctx context.Context, action string, r data.Resource) error {
			decision := native.Decide(ctx, subject, action, policy.Resource{TenantID: a.TenantID(), Type: strings.Split(action, ".")[1], AppID: app, EntityID: r.EntityID, FieldID: r.FieldID, RecordID: r.RecordID, OwnerID: r.OwnerID}, policy.Context{})
			if !decision.Allow {
				return events.ErrDelivery
			}
			return nil
		})
		resolve := data.ScopeResolver(func(ctx context.Context, action string, r data.Resource) (policy.DataScope, error) {
			typ := "entity"
			if r.FieldID != "" {
				typ = "field"
			}
			return native.DataScope(ctx, subject, action, policy.Resource{TenantID: a.TenantID(), Type: typ, AppID: app, EntityID: r.EntityID, FieldID: r.FieldID}, policy.ScopeContext{})
		})
		changed, e := (data.Store{}).Command(ctx, q, scope, a, envelope.AggregateType, data.Command{Operation: "update", ID: record.ID, ExpectedVersion: 1, Values: m5Values("local consumer mutation")}, guard, resolve)
		if e != nil {
			return e
		}
		id, e := events.NewID()
		if e != nil {
			return e
		}
		intent := events.Intent{ID: id, TenantID: a.TenantID(), AppID: app, IRHash: program.Hash(), EntityID: envelope.AggregateType, RecordID: changed.ID, Type: "record.updated", MetadataRevision: 1, RecordVersion: changed.Version, RequestID: envelope.RequestID}
		evidence := audit.Entry{ActorID: a.ActorID(), TenantID: a.TenantID(), Action: intent.Type, Resource: "record", Allow: true, DecisionID: "test.trusted.consumer", PolicyVersion: policy.Version, RequestID: intent.RequestID, State: audit.State{RecordID: changed.ID, RecordVersion: changed.Version, MetadataApp: app, MetadataRevision: 1, IRHash: program.Hash(), EntityID: intent.EntityID}}
		if e = events.Append(ctx, q, audit.Postgres{}, events.Postgres{}, evidence, intent); e != nil {
			return e
		}
		return effect(ctx, q, a, envelope)
	}
	consumer.Audit = m8AfterAuditFailure{}
	if consumer.Consume(context.Background(), p) == nil {
		t.Fatal("post-audit failure accepted")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
	check := func(version int64, outbox int) {
		var v int64
		var n int
		if e := f.operator.QueryRow(`SELECT row_version,(SELECT count(*) FROM runtime.event_envelopes WHERE tenant_id=$1) FROM runtime.records WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID).Scan(&v, &n); e != nil || v != version || n != outbox {
			t.Fatal("local change/derived event failed atomicity", e)
		}
	}
	check(1, 1)
	consumer.Audit = audit.Postgres{}
	if e := consumer.Consume(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	if e := consumer.Consume(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	check(2, 2)
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	derived := m8Claim(t, dispatch)
	if derived.EventID == p.EventID {
		t.Fatal("derived event lost")
	}
}

func TestM8DeadPendingMessagesDoNotStarveAnotherTenant(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	d, w := m8Roles(t)
	sender, e := events.OpenRedis(os.Getenv("EVENT_SENDER_REDIS_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer sender.Close()
	receiver, e := events.OpenRedis(os.Getenv("EVENT_RECEIVER_REDIS_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer receiver.Close()
	operator := m8Redis(t, "EVENT_OPERATOR_REDIS_URL")
	ctx := context.Background()
	if operator.Del(ctx, events.Stream).Err() != nil {
		t.Fatal("owned stream reset")
	}
	t.Cleanup(func() { _ = operator.Del(context.Background(), events.Stream).Err() })
	dispatch := events.Dispatcher{DB: d}
	for i := 0; i < 4; i++ {
		m5Create(t, h, base, token, "will dead-letter")
		p := m8Claim(t, dispatch)
		if sender.Publish(ctx, p) != nil {
			t.Fatal("dead reference publication")
		}
		for attempt := 1; attempt <= 5; attempt++ {
			if dispatch.Finish(ctx, p, false) != nil {
				t.Fatal("dead budget")
			}
			if attempt < 5 {
				m8Expire(t, f.operator, p)
				p = m8Claim(t, dispatch)
			}
		}
		m, e := receiver.Read(ctx)
		if e != nil || m.ID == "" {
			t.Fatal("pending dead reference")
		}
	}
	time.Sleep(1100 * time.Millisecond)
	raw, e := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	bob := f.token(t, f.bobSubject, nil)
	if got := m3Call(h, "POST", "/v1/tenants/"+f.tenantB+"/metadata/"+app+"/versions", bob, 0, raw); got.Code != 201 {
		t.Fatal("tenant B fixture", got.Code)
	}
	baseB := strings.Replace(base, f.tenantA, f.tenantB, 1)
	m5Create(t, h, baseB, bob, "fresh tenant B")
	fresh := m8Claim(t, dispatch)
	if fresh.TenantID != f.tenantB {
		t.Fatal("fresh route tenant")
	}
	if sender.Publish(ctx, fresh) != nil || dispatch.Finish(ctx, fresh, true) != nil {
		t.Fatal("fresh publication")
	}
	peer, e := events.OpenRedis(os.Getenv("EVENT_RECEIVER_REDIS_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer peer.Close()
	consumer := m8Consumer(w)
	found := false
	for i := 0; i < 3; i++ {
		m, e := peer.Read(ctx)
		if e != nil || m.ID == "" {
			t.Fatal("fair delivery unavailable")
		}
		pointer, e := events.DecodePointer(m.Body)
		if e != nil {
			t.Fatal(e)
		}
		if e = consumer.Handle(ctx, peer, m); e != nil {
			t.Fatal(e)
		}
		if pointer.EventID == fresh.EventID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("dead pending references starved fresh tenant")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 0)
	m8AssertCounts(t, f.operator, f.tenantB, 1)
}
