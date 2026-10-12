package tenant

import (
	"context"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// Teams resolves direct memberships plus descendants. No UI-supplied team IDs.
func (Store) Teams(ctx context.Context, q postgres.Executor, a Access) ([]string, error) {
	if a.ActorID() == "" || a.TenantID() == "" {
		return nil, ErrDenied
	}
	rows, e := q.QueryContext(ctx, `WITH RECURSIVE allowed(id,path,depth,cycle) AS (
 SELECT team_id,ARRAY[team_id],0,false FROM iam.team_memberships WHERE tenant_id=$1 AND user_id=$2 AND active
 UNION ALL SELECT t.id,a.path||t.id,a.depth+1,t.id=ANY(a.path) FROM iam.teams t JOIN allowed a ON t.parent_id=a.id WHERE t.tenant_id=$1 AND a.depth<9 AND NOT a.cycle) SELECT DISTINCT id::text,bool_or(depth>8 OR cycle) OVER() AS invalid FROM allowed ORDER BY id LIMIT 65`, a.TenantID(), a.ActorID())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		var invalid bool
		if e = rows.Scan(&id, &invalid); e != nil {
			return nil, e
		}
		if invalid {
			return nil, errors.New("team hierarchy depth/cycle budget exceeded")
		}
		out = append(out, id)
	}
	if len(out) > 64 {
		return nil, errors.New("team hierarchy budget exceeded")
	}
	return out, rows.Err()
}
