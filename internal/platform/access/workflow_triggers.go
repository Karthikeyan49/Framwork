package access

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Start matching workflows while the original business transaction still owns
// its record snapshot. Replaying M8's receipt cannot hydrate or start new work.
func (a API) workflowTriggers(ctx context.Context, q postgres.Executor, actor identity.User, access tenant.Access, p compiler.Program, e events.Intent) error {
	lifecycle := a.WorkflowEvents
	if lifecycle == nil {
		lifecycle = events.Postgres{}
	}
	store := WorkflowStore(a.Policy, a.Audit, lifecycle, func(context.Context, postgres.Executor, tenant.Access) (identity.User, error) { return actor, nil })
	for _, plan := range p.IR().Workflows {
		if plan.EntityID != e.EntityID || plan.Trigger != e.Type {
			continue
		}
		if !a.WorkflowsEnabled {
			return workflow.ErrDenied
		}
		if _, err := store.Start(ctx, q, access, workflow.Start{IR: p.JSON(), IRHash: p.Hash(), Revision: e.MetadataRevision, WorkflowID: plan.ID, RecordID: e.RecordID, Key: "event:" + e.ID, RequestID: e.RequestID, TraceParent: e.TraceParent, Trigger: e.Type}); err != nil {
			return err
		}
	}
	return nil
}
