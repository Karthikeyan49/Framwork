// Package events defines atomic intent persistence. Delivery/inbox scheduling is M8.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

type Intent struct {
	ID, TenantID, AppID, IRHash, EntityID, RecordID, Type string
	MetadataRevision, RecordVersion                       int64
}
type Writer interface {
	Append(context.Context, postgres.Executor, Intent) error
}
type Postgres struct{}

func (Postgres) Append(ctx context.Context, q postgres.Executor, e Intent) error {
	if e.ID == "" || e.TenantID == "" || e.AppID == "" || len(e.IRHash) != 64 || e.EntityID == "" || e.RecordID == "" || e.RecordVersion < 1 {
		return errors.New("invalid event intent")
	}
	_, err := q.ExecContext(ctx, `INSERT INTO runtime.outbox_intents(tenant_id,id,app_id,metadata_revision,ir_hash,entity_id,record_id,record_version,event_type) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, e.TenantID, e.ID, e.AppID, e.MetadataRevision, e.IRHash, e.EntityID, e.RecordID, e.RecordVersion, e.Type)
	return err
}

// Append coordinates evidence and event intent on the caller's business
// transaction. Errors must propagate to WithRuntime; no network publish occurs.
func Append(ctx context.Context, q postgres.Executor, a audit.Writer, w Writer, evidence audit.Entry, event Intent) error {
	if a == nil || w == nil || q == nil || evidence.TenantID != event.TenantID || evidence.State.RecordID != event.RecordID || !evidence.Allow {
		return errors.New("invalid atomic change evidence")
	}
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
