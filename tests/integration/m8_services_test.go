//go:build integration

package integration

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/app"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestM8ReadinessAndProcessBindingFailures(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	d, w := m8Roles(t)
	ctx := context.Background()
	if events.Ready(ctx, f.db, "framework_app") != nil || events.Ready(ctx, w, "framework_worker") != nil || events.Ready(ctx, d, "framework_dispatcher") != nil {
		t.Fatal("valid event contracts not ready")
	}
	if _, e := f.operator.Exec(`ALTER TABLE runtime.outbox_intents DISABLE TRIGGER freeze_event`); e != nil {
		t.Fatal(e)
	}
	if events.Ready(ctx, f.db, "framework_app") == nil || events.Ready(ctx, w, "framework_worker") == nil {
		t.Fatal("disabled freeze trigger ready")
	}
	if _, e := f.operator.Exec(`ALTER TABLE runtime.outbox_intents ENABLE TRIGGER freeze_event`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.operator.Exec(`REVOKE INSERT(request_id) ON runtime.outbox_intents FROM framework_app`); e != nil {
		t.Fatal(e)
	}
	if events.Ready(ctx, f.db, "framework_app") == nil {
		t.Fatal("missing emission grant ready")
	}
	if _, e := f.operator.Exec(`GRANT INSERT(request_id) ON runtime.outbox_intents TO framework_app`); e != nil {
		t.Fatal(e)
	}
	c, e := config.Load(os.Getenv)
	if e != nil {
		t.Fatal(e)
	}
	c.EventsEnabled = true
	c.OTelEndpoint = ""
	c.EventRedisURL = os.Getenv("EVENT_SENDER_REDIS_URL")
	if app.Run(ctx, "framework-worker", c, slog.New(slog.NewJSONHandler(io.Discard, nil))) == nil {
		t.Fatal("worker accepted sender credential")
	}
	c.EventRedisURL = os.Getenv("EVENT_RECEIVER_REDIS_URL")
	if app.Run(ctx, "framework-dispatcher", c, slog.New(slog.NewJSONHandler(io.Discard, nil))) == nil {
		t.Fatal("dispatcher accepted receiver credential")
	}
	if _, e = f.operator.Exec(`DROP FUNCTION control.resolve_event_v2(uuid,uuid,text,text,bigint,uuid)`); e != nil {
		t.Fatal(e)
	}
	if events.Ready(ctx, w, "framework_worker") == nil {
		t.Fatal("missing worker routing contract ready")
	}
	if app.Run(ctx, "framework-worker", c, slog.New(slog.NewJSONHandler(io.Discard, nil))) == nil {
		t.Fatal("worker started without routing migration")
	}
}

func TestM8ExactTenantJobRoutesAndPoolRebinding(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	d, w := m8Roles(t)
	w.SetMaxOpenConns(1)
	id := uuid(t)
	ctx := context.Background()
	for _, route := range []struct{ tenant, actor, hash string }{{f.tenantA, f.alice, strings.Repeat("a", 64)}, {f.tenantB, f.bob, strings.Repeat("b", 64)}} {
		if _, e := f.operator.Exec(`INSERT INTO control.job_routes(tenant_id,id,actor_id,release_hash) VALUES($1,$2,$3,$4)`, route.tenant, id, route.actor, route.hash); e != nil {
			t.Fatal(e)
		}
		ticket, e := jobs.FindRoute(ctx, d, route.tenant, id)
		if e != nil || ticket.ReleaseHash() != route.hash {
			t.Fatal("tenant-inclusive route lookup failed")
		}
		ran := false
		if e = jobs.WithJob(ctx, w, ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
			ran = true
			if a.TenantID() != route.tenant || a.ActorID() != route.actor {
				t.Fatal("job identity switched across tenant-inclusive routing key")
			}
			var n int
			if e := q.QueryRowContext(ctx, `SELECT count(*) FROM iam.memberships WHERE tenant_id<>$1::uuid`, route.tenant).Scan(&n); e != nil {
				return e
			}
			if n != 0 {
				t.Fatal("foreign memberships visible")
			}
			return nil
		}); e == nil || ran {
			t.Fatal("routing reference without execution capability was admitted")
		}
	}
	var tenantContext string
	if e := w.QueryRow(`SELECT coalesce(current_setting('app.tenant_id',true),'')`).Scan(&tenantContext); e != nil || tenantContext != "" {
		t.Fatal("pooled worker tenant context retained")
	}
}

func TestM8RunningDeliveryServices(t *testing.T) {
	if kind := os.Getenv("M8_SERVICE_CHILD"); kind != "" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		defer stop()
		if app.Main(ctx, kind) != 0 {
			t.Fatal("delivery process failed")
		}
		return
	}
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	record := m5Create(t, h, base, token, "receipt only")
	operator := m8Redis(t, "EVENT_OPERATOR_REDIS_URL")
	if operator.Del(context.Background(), events.Stream).Err() != nil {
		t.Fatal("owned stream initialization")
	}
	t.Cleanup(func() { _ = operator.Del(context.Background(), events.Stream).Err() })
	type child struct {
		command *exec.Cmd
		done    chan error
		log     string
	}
	children := []child{}
	t.Cleanup(func() {
		for _, c := range children {
			_ = c.command.Process.Kill()
		}
	})
	for _, kind := range []string{"framework-dispatcher", "framework-worker"} {
		dbKey, brokerKey := "DISPATCHER_DATABASE_URL", "EVENT_SENDER_REDIS_URL"
		if kind == "framework-worker" {
			dbKey = "WORKER_DATABASE_URL"
			brokerKey = "EVENT_RECEIVER_REDIS_URL"
		}
		log := filepath.Join(t.TempDir(), "process.log")
		output, e := os.OpenFile(log, os.O_CREATE|os.O_WRONLY, 0o600)
		if e != nil {
			t.Fatal(e)
		}
		command := exec.Command(os.Args[0], "-test.run=^TestM8RunningDeliveryServices$", "-test.count=1")
		command.Env = []string{"PATH=" + os.Getenv("PATH"), "APP_ENV=test", "EVENTS_ENABLED=true", "M8_SERVICE_CHILD=" + kind, "DATABASE_URL=" + os.Getenv(dbKey), dbKey + "=" + os.Getenv(dbKey), "REDIS_URL=" + os.Getenv(brokerKey), "EVENT_REDIS_URL=" + os.Getenv(brokerKey)}
		command.Stdout = output
		command.Stderr = output
		if command.Start() != nil {
			output.Close()
			t.Fatal("service spawn")
		}
		output.Close()
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		children = append(children, child{command, done, log})
	}
	deadline := time.Now().Add(8 * time.Second)
	var receipts int
	for time.Now().Before(deadline) {
		if e := f.operator.QueryRow(`SELECT count(*) FROM runtime.event_inbox WHERE tenant_id=$1`, f.tenantA).Scan(&receipts); e != nil {
			t.Fatal(e)
		}
		if receipts == 1 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if receipts != 1 {
		t.Fatal("live dispatcher/worker did not complete receipt")
	}
	var audits int
	var version int64
	var name string
	if e := f.operator.QueryRow(`SELECT (SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='events.consume'),row_version,custom_data->>'field.contact.name' FROM runtime.records WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID).Scan(&audits, &version, &name); e != nil || audits != 1 || version != 1 || name != "receipt only" {
		t.Fatal("receipt consumer changed business behavior or missed audit")
	}
	for _, c := range children {
		log, e := os.ReadFile(c.log)
		if e != nil || !strings.Contains(string(log), "event delivery started") {
			t.Fatal("service did not confirm initialized delivery")
		}
		if c.command.Process.Signal(syscall.SIGTERM) != nil {
			t.Fatal("service stopped unexpectedly")
		}
		select {
		case e := <-c.done:
			if e != nil {
				t.Fatal("graceful process shutdown failed")
			}
		case <-time.After(12 * time.Second):
			t.Fatal("delivery process shutdown timeout")
		}
	}
}
