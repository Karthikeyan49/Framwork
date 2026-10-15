package policy

import (
	"context"
	"slices"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// LockWorkflowApprover binds the pinned role requirement to live membership.
// The SQL port fences revocation; capabilities and record scopes are separate
// mandatory checks in workflow composition. Token role claims are never read.
func (RoleStore) LockWorkflowApprover(ctx context.Context, q postgres.Executor, a tenant.Access, permitted []string) (bool, error) {
	roles := a.Roles()
	if q == nil || !tenant.ValidID(a.ActorID()) || !tenant.ValidID(a.TenantID()) || len(roles) != 1 || !ValidRoleName(roles[0]) || len(permitted) < 1 || len(permitted) > 100 || !slices.Contains(permitted, roles[0]) {
		return false, nil
	}
	var allowed bool
	err := q.QueryRowContext(ctx, `SELECT control.lock_workflow_approver($1::uuid,$2::uuid,$3)`, a.TenantID(), a.ActorID(), roles[0]).Scan(&allowed)
	return allowed, err
}
