//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/redis/go-redis/v9"
)

// Own the sender identity and the tenant-bound receiver's unique stream. Never
// change a shared sender ACL, stop Redis, or alter event/attempt timestamps.
func r7bSender(t *testing.T, audience string) (*events.RedisTransport, *redis.Client, string) {
	t.Helper()
	operator := m8Redis(t, "EVENT_OPERATOR_REDIS_URL")
	user := "r7b_send_" + strings.ReplaceAll(uuid(t), "-", "")
	password := strings.ReplaceAll(uuid(t)+uuid(t), "-", "")
	hash := sha256.Sum256([]byte(password))
	stream := events.StreamPrefix + audience
	if err := operator.ACLSetUser(context.Background(), user, "reset", "on", "#"+hex.EncodeToString(hash[:]), "~"+stream, "-@all", "+ping", "+xadd").Err(); err != nil {
		t.Fatal("owned sender provisioning")
	}
	t.Cleanup(func() { _ = operator.ACLDelUser(context.Background(), user).Err() })
	u, err := url.Parse(os.Getenv("EVENT_SENDER_REDIS_URL"))
	if err != nil {
		t.Fatal("sender binding")
	}
	u.User = url.UserPassword(user, password)
	options, err := redis.ParseURL(u.String())
	if err != nil || options.Addr != operator.Options().Addr || options.DB != operator.Options().DB || options.Username != user || options.Password != password {
		t.Fatal("effective owned sender target mismatch")
	}
	sender, err := events.OpenRedis(u.String())
	if err != nil || sender.Ping(context.Background()) != nil {
		t.Fatal("owned sender unavailable")
	}
	t.Cleanup(func() { sender.Close() })
	return sender, operator, user
}

func TestR7bConsumerOutageFirstThirtySeconds(t *testing.T) {
	r7bRealOutage(t, 30*time.Second)
}

// Called with fixed30s or600s durations by separate explicit test targets. The
// receipt/retry policy is never shortened and PostgreSQL time is not altered.
func r7bRealOutage(t *testing.T, absence time.Duration) {
	t.Helper()
	m8Database(t)
	f, h, baseA, _, app := m5Fixture(t)
	bob := f.token(t, f.bobSubject, nil)
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal("fixture source")
	}
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	if got := m3Call(h, "POST", "/v1/tenants/"+f.tenantB+"/metadata/"+app+"/versions", bob, 0, raw); got.Code != 201 {
		t.Fatal("tenant B metadata", got.Code)
	}
	base := strings.Replace(baseA, f.tenantA, f.tenantB, 1)
	db, wrongWorker := m8Roles(t)
	worker := m9RSecondPrincipal(t, &m9Fixture{f: f})
	var audience string
	if err := worker.QueryRow(`SELECT session_user`).Scan(&audience); err != nil {
		t.Fatal("worker audience")
	}
	receiver := m9RBroker(t, worker)
	sender, operator, senderUser := r7bSender(t, audience)
	dispatch := events.Dispatcher{DB: db, Transport: sender}
	firstRecord := m5Create(t, h, base, bob, "consumer absent")
	first := m8Claim(t, dispatch)
	if first.TenantID != f.tenantB || first.Audience != audience || sender.Publish(context.Background(), first) != nil || dispatch.Finish(context.Background(), first, true) != nil {
		t.Fatal("confirmed initial publication")
	}
	if got := m5Call(h, base+"/commands", bob, data.Command{Operation: "update", ID: firstRecord.ID, ExpectedVersion: 1, Values: m5Values("queued successor")}); got.Code != 200 {
		t.Fatal("successor fixture", got.Code)
	}
	secondRecord := m5Create(t, h, base, bob, "broker denied")
	if operator.ACLSetUser(context.Background(), senderUser, "-xadd").Err() != nil {
		t.Fatal("owned sender outage injection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		events.RunDispatcher(ctx, dispatch, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("owned dispatcher shutdown timeout")
		}
	})
	start := time.Now()
	for time.Since(start) < absence {
		timer := time.NewTimer(min(30*time.Second, absence-time.Since(start)))
		<-timer.C
		var firstState, secondState, secondError, bodyHash string
		var attempts, generation, successorAttempts, brokerAttempts, receipts int64
		var dbAge float64
		err := f.operator.QueryRow(`SELECT d.state,d.attempts,d.generation,d.body_hash,
 (SELECT attempts FROM control.event_deliveries WHERE tenant_id=$1 AND aggregate_key=d.aggregate_key AND sequence=2),
 (SELECT state FROM control.event_deliveries x JOIN runtime.outbox_intents i USING(tenant_id,id) WHERE x.tenant_id=$1 AND i.record_id=$3),
 (SELECT attempts FROM control.event_deliveries x JOIN runtime.outbox_intents i USING(tenant_id,id) WHERE x.tenant_id=$1 AND i.record_id=$3),
 (SELECT last_error FROM control.event_deliveries x JOIN runtime.outbox_intents i USING(tenant_id,id) WHERE x.tenant_id=$1 AND i.record_id=$3),
 (SELECT count(*) FROM runtime.event_inbox WHERE tenant_id=$1),extract(epoch FROM clock_timestamp()-a.published_at)
 FROM control.event_deliveries d JOIN control.event_attempts a ON a.tenant_id=d.tenant_id AND a.id=d.id AND a.generation=$4
 WHERE d.tenant_id=$1 AND d.id=$2`, f.tenantB, first.EventID, secondRecord.ID, first.Generation).Scan(&firstState, &attempts, &generation, &bodyHash, &successorAttempts, &secondState, &brokerAttempts, &secondError, &receipts, &dbAge)
		if err != nil {
			t.Fatal("outage state observation failed")
		}
		t.Logf("outage elapsed_s=%.3f postgres_age_s=%.3f receipt_state=%s receipt_attempts=%d broker_state=%s broker_attempts=%d successor_attempts=%d receipts=%d", time.Since(start).Seconds(), dbAge, firstState, attempts, secondState, brokerAttempts, successorAttempts, receipts)
		if firstState != "SENT" || attempts != 1 || generation != first.Generation || bodyHash != first.BodyHash || successorAttempts != 0 || secondState != "PENDING" || brokerAttempts < 2 || brokerAttempts >= 20 || secondError != "publish_failed" || receipts != 0 {
			t.Fatal("outage acceptance failed: premature exhaustion/republication, ordering or receipt violation")
		}
		if time.Since(start) >= absence && dbAge < absence.Seconds() {
			t.Fatal("PostgreSQL did not observe the required real outage duration")
		}
	}
	m8AssertCounts(t, f.operator, f.tenantB, 0)
	if err := m8Consumer(wrongWorker).Consume(context.Background(), first); !errors.Is(err, events.ErrUnroutable) {
		t.Fatal("foreign worker acquired delayed event authority")
	}
	if operator.ACLSetUser(context.Background(), senderUser, "+xadd").Err() != nil {
		t.Fatal("owned sender restoration")
	}
	recovery, stop := context.WithTimeout(context.Background(), 90*time.Second)
	defer stop()
	seen := map[string]bool{}
	for len(seen) < 3 && recovery.Err() == nil {
		message, err := receiver.Read(recovery)
		if err != nil {
			t.Fatal("receiver recovery read")
		}
		if message.ID == "" {
			continue
		}
		pointer, err := events.DecodePointer(message.Body)
		if err != nil || pointer.TenantID != f.tenantB || pointer.Audience != audience {
			t.Fatal("recovered routing identity")
		}
		if len(seen) == 0 && pointer.EventID != first.EventID {
			t.Fatal("original broker publication disappeared")
		}
		if err := m8Consumer(worker).Handle(recovery, receiver, message); err != nil {
			t.Fatal("delayed event did not converge")
		}
		seen[pointer.EventID] = true
	}
	if len(seen) != 3 {
		t.Fatal("recovery did not drain all three immutable events")
	}
	if err := m8Consumer(worker).Consume(context.Background(), first); err != nil {
		t.Fatal("committed delayed duplicate did not reconcile")
	}
	m8AssertCounts(t, f.operator, f.tenantB, 3)
	var complete, intents, attempts int
	if err := f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.event_deliveries WHERE tenant_id=$1 AND state='COMPLETED'),
 (SELECT count(*) FROM runtime.outbox_intents WHERE tenant_id=$1),
 (SELECT count(*) FROM control.event_attempts WHERE tenant_id=$1)`, f.tenantB).Scan(&complete, &intents, &attempts); err != nil || complete != 3 || intents != 3 || attempts < 4 {
		t.Fatal("recovery lost durable delivery/evidence")
	}
	t.Logf("recovery PASS real_absence_s=%.3f total_elapsed_s=%.3f completed=3 inbox=3 effects=3 audits=3 immutable_intents=3 attempts_retained=%d", absence.Seconds(), time.Since(start).Seconds(), attempts)
}
