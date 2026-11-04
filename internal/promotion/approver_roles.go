package promotion

import (
	"context"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// ApproverRoles returns the role each recorded approver of the exact hash holds now, keyed by
// the lower-case actor id. An approver who was removed from the tenant or deactivated is
// absent. It is how a decision asks what the approvers hold at activation time (M13R-03: the
// database floor counts every active non-viewer member, the policy decides whether the
// member's role still grants releases.approve). It reads and grants nothing.
func (Store) ApproverRoles(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string) (map[string]string, error) {
	if !validScope(a, app) || !ValidHash(hash) {
		return nil, ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT a.actor_id::text,m.role FROM control.release_approvals a
 JOIN iam.memberships m ON m.tenant_id=a.tenant_id AND m.user_id=a.actor_id AND m.active
 WHERE a.tenant_id=$1 AND a.app_id=$2 AND a.artifact_hash=$3 ORDER BY a.approved_at,a.actor_id LIMIT $4`, a.TenantID(), app, hash, MaxDecisions)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var actor, role string
		if err = rows.Scan(&actor, &role); err != nil {
			return nil, classify(err)
		}
		out[strings.ToLower(actor)] = role
	}
	return out, classify(rows.Err())
}
