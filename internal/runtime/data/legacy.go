package data

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

type LegacyAdapter struct{}

func (LegacyAdapter) Query(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e string, v Query, g Guard, resolve ...ScopeResolver) (Page, error) {
	return (Store{legacy: true}).Query(ctx, q, s, a, e, v, g, resolve...)
}
func (st Store) recordsTable() string {
	if st.legacy {
		return "(SELECT p.*,p.created_by AS owner_user_id,NULL::uuid AS owner_team_id FROM runtime.preview_records p)"
	}
	return "runtime.records"
}
