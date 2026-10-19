package identity

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// ResolveWorker uses the worker-only, live delegated-identity database function.
// The composition root has already bound the opaque job and tenant routing IDs.
func (Store) ResolveWorker(ctx context.Context, q postgres.Executor, job string) (User, error) {
	var actor, tenant, issuer, subject string
	if e := q.QueryRowContext(ctx, `SELECT actor::text,tenant::text,issuer,subject FROM iam.worker_identity($1::uuid)`, job).Scan(&actor, &tenant, &issuer, &subject); e != nil {
		return User{}, ErrUnauthenticated
	}
	if _, e := q.ExecContext(ctx, `SELECT set_config('app.auth_issuer',$1,true),set_config('app.auth_subject',$2,true),set_config('app.actor_id',$3,true)`, issuer, subject, actor); e != nil {
		return User{}, e
	}
	var u User
	if e := q.QueryRowContext(ctx, `SELECT id::text,name FROM iam.users WHERE id=$1::uuid AND issuer=$2 AND subject=$3 AND active`, actor, issuer, subject).Scan(&u.id, &u.name); e != nil {
		return User{}, ErrUnauthenticated
	}
	return u, nil
}
