//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type m8CrashInput struct {
	Tenant, Actor, Subject, Record, App, IRHash string
	Pointer                                     events.Pointer
}

func m8CrashChild(t *testing.T, stage string) {
	raw, e := os.ReadFile(os.Getenv("M8_INPUT"))
	var input m8CrashInput
	if e != nil || json.Unmarshal(raw, &input) != nil {
		t.Fatal("boundary input")
	}
	ctx := context.Background()
	write := func(p events.Pointer) {
		b, _ := json.Marshal(p)
		if os.WriteFile(os.Getenv("M8_MARKER"), b, 0o600) != nil {
			t.Fatal("boundary marker")
		}
	}
	hold := func() { select {} }
	if stage == "producer-before-commit" {
		db, e := postgres.Open(os.Getenv("DATABASE_URL"))
		if e != nil {
			t.Fatal("producer binding")
		}
		defer db.Close()
		e = postgres.WithRuntime(ctx, db, func(ctx context.Context, q postgres.Executor) error {
			if _, e := q.ExecContext(ctx, `SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$2,true),set_config('app.tenant_id',$3,true)`, input.Actor, input.Subject, input.Tenant); e != nil {
				return e
			}
			if _, e := q.ExecContext(ctx, `UPDATE runtime.records SET row_version=row_version+1,custom_data=jsonb_set(custom_data,'{field.contact.name}','"crash must roll back"') WHERE tenant_id=$1::uuid AND id=$2::uuid`, input.Tenant, input.Record); e != nil {
				return e
			}
			id, e := events.NewID()
			if e != nil {
				return e
			}
			intent := events.Intent{ID: id, TenantID: input.Tenant, AppID: input.App, IRHash: input.IRHash, EntityID: "entity.contact", RecordID: input.Record, Type: "record.updated", MetadataRevision: 1, RecordVersion: 2, RequestID: "test.crash"}
			evidence := audit.Entry{ActorID: input.Actor, TenantID: input.Tenant, Action: intent.Type, Resource: "record", Allow: true, DecisionID: "test.crash", PolicyVersion: policyCrashVersion, RequestID: intent.RequestID, State: audit.State{RecordID: intent.RecordID, IRHash: intent.IRHash, EntityID: intent.EntityID, MetadataApp: intent.AppID, MetadataRevision: 1, RecordVersion: 2}}
			if e = events.Append(ctx, q, audit.Postgres{}, events.Postgres{}, evidence, intent); e != nil {
				return e
			}
			write(events.Pointer{EventID: id})
			hold()
			return nil
		})
		if e != nil {
			t.Fatal("producer boundary failed")
		}
		return
	}
	if stage == "consumer-after-commit" {
		db, e := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
		if e != nil {
			t.Fatal("worker binding")
		}
		defer db.Close()
		if m8Consumer(db).Consume(ctx, input.Pointer) != nil {
			t.Fatal("consumer commit failed")
		}
		write(input.Pointer)
		hold()
		return
	}
	db, e := postgres.Open(os.Getenv("DISPATCHER_DATABASE_URL"))
	if e != nil {
		t.Fatal("dispatcher binding")
	}
	defer db.Close()
	d := events.Dispatcher{DB: db}
	p, found, e := d.Claim(ctx)
	if e != nil || !found {
		t.Fatal("claim boundary failed")
	}
	if stage == "publisher-after-send" {
		sender, e := events.OpenRedis(os.Getenv("EVENT_SENDER_REDIS_URL"))
		if e != nil {
			t.Fatal("sender binding")
		}
		defer sender.Close()
		if sender.Publish(ctx, p) != nil {
			t.Fatal("publish boundary failed")
		}
	}
	write(p)
	hold()
}

const policyCrashVersion = "test-trusted-crash-boundary"

func TestM8CrashRecovery(t *testing.T) {
	if stage := os.Getenv("M8_CRASH_STAGE"); stage != "" {
		m8CrashChild(t, stage)
		return
	}
	for _, stage := range []string{"producer-before-commit", "publisher-after-claim", "publisher-after-send", "consumer-after-commit"} {
		t.Run(stage, func(t *testing.T) {
			m8Database(t)
			f, h, base, token, app := m5Fixture(t)
			record := m5Create(t, h, base, token, "crash original")
			d, w := m8Roles(t)
			dispatch := events.Dispatcher{DB: d}
			var sender, receiver *events.RedisTransport
			var pending events.Message
			if stage != "producer-before-commit" {
				operator := m8Redis(t, "EVENT_OPERATOR_REDIS_URL")
				if operator.Del(context.Background(), events.Stream).Err() != nil {
					t.Fatal("owned test stream reset")
				}
				t.Cleanup(func() { _ = operator.Del(context.Background(), events.Stream).Err() })
				var e error
				sender, e = events.OpenRedis(os.Getenv("EVENT_SENDER_REDIS_URL"))
				if e != nil {
					t.Fatal(e)
				}
				defer sender.Close()
				receiver, e = events.OpenRedis(os.Getenv("EVENT_RECEIVER_REDIS_URL"))
				if e != nil {
					t.Fatal(e)
				}
				defer receiver.Close()
			}
			input := m8CrashInput{Tenant: f.tenantA, Actor: f.alice, Subject: f.aliceSubject, Record: record.ID, App: app}
			if e := f.operator.QueryRow(`SELECT ir_hash FROM runtime.records WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID).Scan(&input.IRHash); e != nil {
				t.Fatal(e)
			}
			if stage == "consumer-after-commit" {
				input.Pointer = m8Claim(t, dispatch)
				if e := sender.Publish(context.Background(), input.Pointer); e != nil {
					t.Fatal(e)
				}
				if e := dispatch.Finish(context.Background(), input.Pointer, true); e != nil {
					t.Fatal(e)
				}
				var e error
				pending, e = receiver.Read(context.Background())
				if e != nil || pending.ID == "" {
					t.Fatal("consumer pending boundary")
				}
			}
			dir := t.TempDir()
			inputFile, marker := filepath.Join(dir, "input.json"), filepath.Join(dir, "marker.json")
			b, _ := json.Marshal(input)
			if os.WriteFile(inputFile, b, 0o600) != nil {
				t.Fatal("input write")
			}
			command := exec.Command(os.Args[0], "-test.run=^TestM8CrashRecovery$", "-test.count=1")
			command.Stdout = io.Discard
			command.Stderr = io.Discard
			command.Env = []string{"M8_CRASH_STAGE=" + stage, "M8_INPUT=" + inputFile, "M8_MARKER=" + marker, "PATH=" + os.Getenv("PATH")}
			switch stage {
			case "producer-before-commit":
				command.Env = append(command.Env, "DATABASE_URL="+os.Getenv("DATABASE_URL"))
			case "consumer-after-commit":
				command.Env = append(command.Env, "WORKER_DATABASE_URL="+os.Getenv("WORKER_DATABASE_URL"))
			default:
				command.Env = append(command.Env, "DISPATCHER_DATABASE_URL="+os.Getenv("DISPATCHER_DATABASE_URL"), "EVENT_SENDER_REDIS_URL="+os.Getenv("EVENT_SENDER_REDIS_URL"))
			}
			if command.Start() != nil {
				t.Fatal("crash process start")
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			defer command.Process.Kill()
			deadline := time.Now().Add(5 * time.Second)
			var saved []byte
			for time.Now().Before(deadline) {
				if saved, _ = os.ReadFile(marker); len(saved) > 0 {
					break
				}
				select {
				case <-done:
					t.Fatal("child failed before boundary")
				default:
				}
				time.Sleep(20 * time.Millisecond)
			}
			if len(saved) == 0 {
				t.Fatal("child boundary timeout")
			}
			var old events.Pointer
			if json.Unmarshal(saved, &old) != nil {
				t.Fatal("boundary pointer")
			}
			if command.Process.Kill() != nil {
				t.Fatal("process kill failed")
			}
			<-done
			if stage == "producer-before-commit" {
				var version int64
				var n int
				if e := f.operator.QueryRow(`SELECT row_version,(SELECT count(*) FROM runtime.event_envelopes WHERE tenant_id=$1 AND id=$3::uuid) FROM runtime.records WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID, old.EventID).Scan(&version, &n); e != nil || version != 1 || n != 0 {
					t.Fatal("uncommitted producer state survived process death")
				}
				var auditN int
				if e := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND request_id='test.crash'`, f.tenantA).Scan(&auditN); e != nil || auditN != 0 {
					t.Fatal("uncommitted audit survived")
				}
				return
			}
			if stage == "consumer-after-commit" {
				m8AssertCounts(t, f.operator, f.tenantA, 1)
				if e := m8Consumer(w).Handle(context.Background(), receiver, pending); e != nil {
					t.Fatal(e)
				}
				m8AssertCounts(t, f.operator, f.tenantA, 1)
				return
			}
			if e := m8Consumer(w).Consume(context.Background(), old); e != events.ErrNotReady {
				t.Fatal("unconfirmed publisher became consumable")
			}
			// Let the actual PostgreSQL-time lease expire; no test clock alteration here.
			time.Sleep(5100 * time.Millisecond)
			again := m8Claim(t, dispatch)
			if again.EventID != old.EventID || again.BodyHash != old.BodyHash || again.Generation <= old.Generation {
				t.Fatal("crash recovery changed immutable event or fence")
			}
			if e := sender.Publish(context.Background(), again); e != nil {
				t.Fatal(e)
			}
			if e := dispatch.Finish(context.Background(), again, true); e != nil {
				t.Fatal(e)
			}
			if e := m8Consumer(w).Consume(context.Background(), old); e != events.ErrNotReady {
				t.Fatal("old attempt became a new payload capability", e)
			}
			if e := m8Consumer(w).Consume(context.Background(), again); e != nil {
				t.Fatal("current reissued capability did not converge", e)
			}
			if e := m8Consumer(w).Consume(context.Background(), old); e != nil {
				t.Fatal("terminal old pointer did not resolve", e)
			}
			m8AssertCounts(t, f.operator, f.tenantA, 1)
		})
	}
}
