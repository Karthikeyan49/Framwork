package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"time"
)

// WorkflowEvent is an immutable lifecycle snapshot. This outbox is separate from
// M8's record receipt topic. A future subscriber must explicitly register delivery.
type WorkflowEvent struct {
	SchemaVersion   string    `json:"schema_version"`
	TenantID        string    `json:"tenant_id"`
	InstanceID      string    `json:"instance_id"`
	Sequence        int64     `json:"sequence"`
	ActorID         string    `json:"actor_id"`
	IRHash          string    `json:"ir_hash"`
	WorkflowID      string    `json:"workflow_id"`
	WorkflowVersion string    `json:"workflow_version"`
	PlanHash        string    `json:"plan_hash"`
	StateID         string    `json:"state_id"`
	Status          string    `json:"status"`
	Event           string    `json:"event"`
	OccurredAt      time.Time `json:"occurred_at"`
	RequestID       string    `json:"request_id"`
	TraceID         string    `json:"trace_id"`
}
type LifecycleWriter interface {
	AppendWorkflow(context.Context, postgres.Executor, WorkflowEvent) error
}

func (Postgres) AppendWorkflow(ctx context.Context, q postgres.Executor, e WorkflowEvent) error {
	if q == nil || e.SchemaVersion != "1.0.0" || !validUUID(e.TenantID) || !validUUID(e.InstanceID) || !validUUID(e.ActorID) || e.Sequence < 1 || !validHash(e.IRHash) || !validHash(e.PlanHash) || e.WorkflowID == "" || len(e.WorkflowID) > 160 || e.WorkflowVersion == "" || len(e.WorkflowVersion) > 20 || e.StateID == "" || len(e.StateID) > 160 || e.Status == "" || len(e.Status) > 40 || e.Event == "" || len(e.Event) > 40 || len(e.RequestID) > 64 || len(e.TraceID) > 32 || e.OccurredAt.IsZero() {
		return ErrDelivery
	}
	raw, err := json.Marshal(e)
	if err != nil || len(raw) > 8192 {
		return ErrDelivery
	}
	h := sha(raw)
	_, err = q.ExecContext(ctx, `INSERT INTO runtime.workflow_outbox(tenant_id,instance_id,sequence,body,body_hash) VALUES($1,$2,$3,$4,$5)`, e.TenantID, e.InstanceID, e.Sequence, raw, h)
	return err
}
func sha(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
