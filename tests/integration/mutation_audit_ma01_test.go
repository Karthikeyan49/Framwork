//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

func TestActivationHoldsTheApproversMemberships(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()
	m13Code(t, "approve", e.httpApprove(e.dave.token, tmpl.hash), 201)
	err := postgres.WithRuntime(context.Background(), e.f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := e.bindDirect(ctx, q, e.erin.subject, e.f.tenantA); err != nil {
			return err
		}
		var generation int64
		if err := q.QueryRowContext(ctx, `SELECT control.activate_release($1,$2,$3,$4)`, e.app, tmpl.hash, "", 0).Scan(&generation); err != nil || generation != 1 {
			t.Fatal("activation", generation, err)
		}
		// The activation transaction is still open: dave's membership row must be locked against the demotion.
		demotion, err := e.f.operator.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer demotion.Rollback()
		if _, err = demotion.ExecContext(ctx, `SET LOCAL lock_timeout='300ms'`); err != nil {
			return err
		}
		_, err = demotion.ExecContext(ctx, `UPDATE iam.memberships SET role='viewer' WHERE tenant_id=$1 AND user_id=$2`, e.f.tenantA, e.dave.id)
		m13Refused(t, "demotion of an approver while the activation is open", err, "55P03")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
