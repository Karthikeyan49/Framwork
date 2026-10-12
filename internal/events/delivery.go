package events

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
	"time"
)

type Message struct {
	ID   string
	Body []byte
}
type Transport interface {
	Publish(context.Context, Pointer) error
	Read(context.Context) (Message, error)
	Ack(context.Context, string) error
}
type Dispatcher struct {
	DB        *sql.DB
	Transport Transport
}

func (d Dispatcher) Claim(ctx context.Context) (Pointer, bool, error) {
	// A confirmed PostgreSQL serialization abort has no committed lease. Retry
	// that bounded routing transaction only; unknown commit outcomes are retained.
	for attempt := 0; attempt < 4; attempt++ {
		p, found, err := d.claim(ctx)
		var pg *pgconn.PgError
		if err == nil {
			return p, found, nil
		}
		if !errors.As(err, &pg) || (pg.Code != "40001" && pg.Code != "40P01") {
			return Pointer{}, false, ErrDelivery
		}
		pause(ctx, 5*time.Millisecond)
	}
	return Pointer{}, false, ErrDelivery
}
func (d Dispatcher) claim(ctx context.Context) (Pointer, bool, error) {
	p := Pointer{SchemaVersion: "1.0.0"}
	token, err := NewID()
	if err != nil {
		return p, false, ErrDelivery
	}
	found := false
	err = postgres.WithRuntime(ctx, d.DB, func(ctx context.Context, q postgres.Executor) error {
		e := q.QueryRowContext(ctx, `SELECT tenant_id::text,id::text,body_hash,consumer_id,generation,lease_token::text FROM control.claim_event($1::uuid)`, token).Scan(&p.TenantID, &p.EventID, &p.BodyHash, &p.ConsumerID, &p.Generation, &p.LeaseToken)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		found = e == nil
		return e
	})
	if err != nil {
		return Pointer{}, false, err
	}
	if !found {
		return Pointer{}, false, nil
	}
	if !p.Valid() {
		return Pointer{}, false, ErrDelivery
	}
	return p, true, nil
}
func (d Dispatcher) Finish(ctx context.Context, p Pointer, published bool) error {
	if !p.Valid() {
		return ErrDelivery
	}
	var ok bool
	err := postgres.WithRuntime(ctx, d.DB, func(ctx context.Context, q postgres.Executor) error {
		return q.QueryRowContext(ctx, `SELECT control.finish_event($1::uuid,$2::uuid,$3,$4::uuid,$5)`, p.TenantID, p.EventID, p.Generation, p.LeaseToken, published).Scan(&ok)
	})
	if err != nil || !ok {
		return ErrDelivery
	}
	return nil
}
func (d Dispatcher) Once(ctx context.Context) (bool, error) {
	if d.Transport == nil {
		return false, ErrDelivery
	}
	p, found, err := d.Claim(ctx)
	if err != nil || !found {
		return false, err
	}
	ctx, span := otel.Tracer("framework/events").Start(ctx, "events.publish", trace.WithAttributes(attribute.String("event_id", p.EventID), attribute.String("tenant_id", p.TenantID), attribute.Int64("delivery_generation", p.Generation)))
	defer span.End()
	err = d.Transport.Publish(ctx, p)
	if e := d.Finish(ctx, p, err == nil); e != nil {
		return true, e
	}
	if err != nil {
		return true, ErrDelivery
	}
	return true, nil
}

// LocalHandler is trusted Go only. It must perform local changes using q and
// explicit policy/DataScope contracts. Network/tool effects are prohibited here.
type LocalHandler func(context.Context, postgres.Executor, tenant.Access, Envelope) error
type Consumer struct {
	DB      *sql.DB
	Policy  policy.Engine
	Audit   audit.Writer
	Handler LocalHandler
}

func (c Consumer) Consume(ctx context.Context, p Pointer) error {
	if !p.Valid() {
		return ErrUnroutable
	}
	if c.Policy == nil || c.Audit == nil || c.DB == nil {
		return ErrDelivery
	}
	ticket, completed, ready, dead, err := jobs.EventTicket(ctx, c.DB, p.TenantID, p.EventID, p.BodyHash, p.ConsumerID, p.Generation, p.LeaseToken)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnroutable
	}
	if err != nil {
		return ErrDelivery
	}
	if completed {
		return nil
	}
	if dead {
		return ErrDead
	}
	if !ready {
		return ErrNotReady
	}
	return jobs.WithJob(ctx, c.DB, ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var body []byte
		var hash string
		if err := q.QueryRowContext(ctx, `SELECT body,body_hash FROM runtime.event_envelopes WHERE tenant_id=$1::uuid AND id=$2::uuid`, p.TenantID, p.EventID).Scan(&body, &hash); err != nil || hash != p.BodyHash {
			return ErrDelivery
		}
		e, err := DecodeEnvelope(body, p)
		if err != nil || e.ActorID != a.ActorID() || e.IRHash != ticket.ReleaseHash() {
			return ErrDelivery
		}
		ctx = (propagation.TraceContext{}).Extract(ctx, propagation.MapCarrier{"traceparent": e.TraceParent})
		ctx, span := otel.Tracer("framework/events").Start(ctx, "events.consume", trace.WithAttributes(attribute.String("event_id", e.ID), attribute.String("tenant_id", e.TenantID), attribute.String("actor_id", e.ActorID), attribute.String("metadata_version", e.IRHash), attribute.String("request_id", e.RequestID), attribute.Int64("aggregate_sequence", e.AggregateSequence)))
		defer span.End()
		u, err := (identity.Store{}).ResolveWorker(ctx, q, e.ID)
		if err != nil {
			return ErrDelivery
		}
		decision := c.Policy.Decide(ctx, policy.Subject{User: u, Access: a}, "events.consume", policy.Resource{TenantID: e.TenantID, Type: "event", AppID: e.AppID, EntityID: e.AggregateType, RecordID: e.AggregateID}, policy.Context{RequestID: e.RequestID, TraceID: trace.SpanContextFromContext(ctx).TraceID().String()})
		if !decision.Allow || decision.ID == "" || len(decision.ID) > 64 || decision.Version == "" || len(decision.Version) > 80 || len(decision.Obligations) != 1 || decision.Obligations[0] != "audit" {
			return ErrDelivery
		}
		result, err := q.ExecContext(ctx, `INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1::uuid,$2,$3::uuid,$4) ON CONFLICT DO NOTHING`, e.TenantID, p.ConsumerID, e.ID, p.BodyHash)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var old string
			if err = q.QueryRowContext(ctx, `SELECT body_hash FROM runtime.event_inbox WHERE tenant_id=$1::uuid AND consumer_id=$2 AND event_id=$3::uuid`, e.TenantID, p.ConsumerID, e.ID).Scan(&old); err != nil || old != p.BodyHash {
				return ErrDelivery
			}
		} else {
			if c.Handler != nil {
				if err = c.Handler(ctx, q, a, e); err != nil {
					return err
				}
			}
			if err = c.Audit.Append(ctx, q, audit.Entry{TenantID: e.TenantID, ActorID: e.ActorID, Action: "events.consume", Resource: "event", Allow: true, DecisionID: decision.ID, PolicyVersion: decision.Version, RequestID: e.RequestID, TraceID: trace.SpanContextFromContext(ctx).TraceID().String(), State: audit.State{EventID: e.ID, ConsumerID: p.ConsumerID, AggregateSequence: e.AggregateSequence, IRHash: e.IRHash, MetadataApp: e.AppID, MetadataRevision: e.MetadataRevision, EntityID: e.AggregateType, RecordID: e.AggregateID, RecordVersion: e.Payload.RecordVersion}}); err != nil {
				return err
			}
		}
		var ok bool
		if err = q.QueryRowContext(ctx, `SELECT control.complete_event($1::uuid,$2::uuid,$3)`, e.TenantID, e.ID, p.BodyHash).Scan(&ok); err != nil || !ok {
			return ErrDelivery
		}
		return nil
	})
}
func (c Consumer) Quarantine(ctx context.Context, raw []byte, reason string) error {
	sum := sha256.Sum256(raw)
	return postgres.WithRuntime(ctx, c.DB, func(ctx context.Context, q postgres.Executor) error {
		_, err := q.ExecContext(ctx, `SELECT control.quarantine_event($1,$2)`, hex.EncodeToString(sum[:]), reason)
		return err
	})
}

// Handle acknowledges only confirmed commits (including an existing receipt), or
// durable quarantine of a malformed reference. Unknown outcomes retain redelivery.
func (c Consumer) Handle(ctx context.Context, t Transport, m Message) error {
	if t == nil {
		return ErrDelivery
	}
	p, err := DecodePointer(m.Body)
	if err != nil {
		err = c.Quarantine(ctx, m.Body, "malformed")
	} else {
		err = c.Consume(ctx, p)
		if errors.Is(err, ErrDead) {
			err = nil
		} // Durable PG DLQ retains identity and manual recovery.
		if errors.Is(err, ErrUnroutable) {
			err = c.Quarantine(ctx, m.Body, "unroutable")
		}
	}
	if err != nil {
		return err
	}
	if t.Ack(ctx, m.ID) != nil {
		return ErrDelivery
	}
	return nil
}
func RunDispatcher(ctx context.Context, d Dispatcher, logger *slog.Logger) {
	for ctx.Err() == nil {
		worked, err := d.Once(ctx)
		if err != nil {
			logger.Warn("event dispatch deferred")
		}
		if !worked || err != nil {
			pause(ctx, 250*time.Millisecond)
		}
	}
}
func RunConsumer(ctx context.Context, c Consumer, t Transport, logger *slog.Logger) {
	for ctx.Err() == nil {
		m, err := t.Read(ctx)
		if err == nil && m.ID != "" {
			err = c.Handle(ctx, t, m)
		}
		if err != nil && ctx.Err() == nil {
			logger.Warn("event consumption deferred")
			pause(ctx, 250*time.Millisecond)
		}
	}
}
func pause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
