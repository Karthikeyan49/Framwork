package promotion

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// CheckActiveCompatibility is an operator deployment gate. A normal tenant
// connection cannot report a falsely empty global inventory through RLS.
func CheckActiveCompatibility(ctx context.Context, q postgres.Executor, supported []string) error {
	var role string
	if e := q.QueryRowContext(ctx, `SELECT session_user`).Scan(&role); e != nil || role != "framework_migrator" {
		return ErrGate
	}
	rows, e := q.QueryContext(ctx, `SELECT b.body,b.artifact_hash FROM control.active_releases a JOIN control.release_bundles b USING(tenant_id,app_id,artifact_hash)`)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var hash string
		if e = rows.Scan(&raw, &hash); e != nil {
			return e
		}
		if _, e = Restore(raw, hash, supported); e != nil {
			return e
		}
	}
	return rows.Err()
}
