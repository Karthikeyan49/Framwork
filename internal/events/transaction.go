// Package events owns atomic event intent, delivery and inbox contracts.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"go.opentelemetry.io/otel/propagation"
)

type Intent struct {
	ID, TenantID, AppID, IRHash, EntityID, RecordID, Type string
	MetadataRevision, RecordVersion                       int64
	RequestID, TraceParent                                string
}
type Writer interface {
	Append(context.Context, postgres.Executor, Intent) error
}
type Postgres struct{}

func (Postgres) Append(ctx context.Context, q postgres.Executor, e Intent) error {
	if q == nil || !validUUID(e.ID) || !validUUID(e.TenantID) || !validUUID(e.RecordID) || e.AppID == "" || len(e.AppID) > 160 || !validHash(e.IRHash) || e.EntityID == "" || len(e.EntityID) > 160 || e.RecordVersion < 1 || e.MetadataRevision < 1 || len(e.RequestID) > 64 || !validEventType(e.Type) {
		return errors.New("invalid event intent")
	}
	if e.TraceParent == "" {
		carrier := propagation.MapCarrier{}
		(propagation.TraceContext{}).Inject(ctx, carrier)
		e.TraceParent = carrier.Get("traceparent")
	}
	if !validTrace(e.TraceParent) {
		return errors.New("invalid event correlation")
	}
	_, err := q.ExecContext(ctx, `INSERT INTO runtime.outbox_intents(tenant_id,id,app_id,metadata_revision,ir_hash,entity_id,record_id,record_version,event_type,request_id,traceparent) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, e.TenantID, e.ID, e.AppID, e.MetadataRevision, e.IRHash, e.EntityID, e.RecordID, e.RecordVersion, e.Type, e.RequestID, e.TraceParent)
	return err
}

// Append coordinates evidence and event intent on the caller's business
// transaction. Errors must propagate to WithRuntime; no network publish occurs.
func Append(ctx context.Context, q postgres.Executor, a audit.Writer, w Writer, evidence audit.Entry, event Intent) error {
	if a == nil || w == nil || q == nil || evidence.TenantID != event.TenantID || evidence.State.RecordID != event.RecordID || evidence.State.IRHash != event.IRHash || evidence.State.EntityID != event.EntityID || evidence.State.MetadataApp != event.AppID || evidence.State.MetadataRevision != event.MetadataRevision || evidence.State.RecordVersion != event.RecordVersion || evidence.Action != event.Type || evidence.RequestID != event.RequestID || !evidence.Allow {
		return errors.New("invalid atomic change evidence")
	}
	evidence.State.EventID = event.ID
	if e := a.Append(ctx, q, evidence); e != nil {
		return e
	}
	return w.Append(ctx, q, event)
}

func NewID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	s := hex.EncodeToString(b)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}
