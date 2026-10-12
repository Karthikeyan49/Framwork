package tenant

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// ResolveWorker is restricted by the worker database role and its opaque routed
// job. Unlike API membership discovery, IAM reads themselves are job-scoped.
func (Store) ResolveWorker(ctx context.Context, q postgres.Executor, u identity.User, selected string) (Access, error) {
	if _, e := q.ExecContext(ctx, `SELECT set_config('app.tenant_id','',true)`); e != nil {
		return Access{}, e
	}
	if !ValidID(u.ID()) || !ValidID(selected) {
		return Access{}, ErrDenied
	}
	if _, e := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, selected); e != nil {
		return Access{}, e
	}
	deny := func() (Access, error) {
		_, e := q.ExecContext(ctx, `SELECT set_config('app.tenant_id','',true)`)
		if e != nil {
			return Access{}, e
		}
		return Access{}, ErrDenied
	}
	var valid bool
	if e := q.QueryRowContext(ctx, `SELECT session_user='framework_worker' AND iam.worker_tenant_scope($1::uuid) AND iam.authenticated_actor()=$2::uuid`, selected, u.ID()).Scan(&valid); e != nil {
		return Access{}, e
	}
	if !valid {
		return deny()
	}
	var role string
	if e := q.QueryRowContext(ctx, `SELECT role FROM iam.memberships WHERE tenant_id=$1::uuid AND user_id=$2::uuid AND active`, selected, u.ID()).Scan(&role); e != nil {
		return deny()
	}
	if !ValidRole(role) {
		return deny()
	}
	return Access{u.ID(), selected, []string{role}}, nil
}
