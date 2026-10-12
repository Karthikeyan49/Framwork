package promotion

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

type Store struct{ Audit audit.Writer }
type RollbackPlan struct {
	StorageCompatible, ExternalEffectsReviewed bool
	RecoveryRequired                           bool
	PlanHash                                   string
}

// Rollback switches an approved immutable pointer only. Data migrations and
// external effects require separate assessed recovery; they are never undone here.
func (st Store) Rollback(ctx context.Context, q postgres.Executor, a tenant.Access, target Candidate, currentHash string, generation int64, plan RollbackPlan, gate Gate) error {
	if !plan.StorageCompatible || !plan.ExternalEffectsReviewed || plan.RecoveryRequired || !ValidHash(plan.PlanHash) || gate == nil || !gate.AuthorizeRelease(a.TenantID(), a.ActorID(), "rollback", target.Hash()) {
		return ErrGate
	}
	r, e := q.ExecContext(ctx, `UPDATE control.active_releases SET artifact_hash=$3,generation=generation+1 WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$4 AND generation=$5`, a.TenantID(), bApplication(target.Bundle()), target.Hash(), currentHash, generation)
	if e != nil {
		return e
	}
	count, _ := r.RowsAffected()
	if count != 1 {
		return ErrGate
	}
	return st.evidence(ctx, q, a, target, "release.rolled_back")
}

func (st Store) evidence(ctx context.Context, q postgres.Executor, a tenant.Access, c Candidate, action string) error {
	w := st.Audit
	if w == nil {
		w = audit.Postgres{}
	}
	return w.Append(ctx, q, audit.Entry{ActorID: a.ActorID(), TenantID: a.TenantID(), Action: action, Resource: "release", Allow: true, PolicyVersion: "m6r-release-contract-v1", State: audit.State{IRHash: c.Bundle().IRHash, ArtifactHash: c.Hash(), MetadataApp: bApplication(c.Bundle())}})
}

func (st Store) Stage(ctx context.Context, q postgres.Executor, a tenant.Access, c Candidate, gate Gate) error {
	b := c.Bundle()
	if a.ActorID() == "" || b.RequesterID != a.ActorID() || gate == nil || !gate.AuthorizeRelease(a.TenantID(), a.ActorID(), "stage", c.Hash()) {
		return ErrGate
	}
	_, e := q.ExecContext(ctx, `INSERT INTO control.release_bundles(tenant_id,app_id,artifact_hash,body) VALUES($1,$2,$3,$4)`, a.TenantID(), bApplication(b), c.Hash(), c.JSON())
	if e != nil {
		return e
	}
	return st.evidence(ctx, q, a, c, "release.staged")
}
func bApplication(b Bundle) string {
	p, e := compiler.Restore(b.IR, b.IRHash, []string{compiler.IRVersion})
	if e != nil {
		return ""
	}
	return p.IR().Application.AppID
}
func (st Store) Approve(ctx context.Context, q postgres.Executor, a tenant.Access, c Candidate, exactHash string, gate Gate) (Approval, error) {
	approval, e := Approve(c, exactHash, a.TenantID(), a.ActorID(), gate)
	if e != nil {
		return Approval{}, e
	}
	_, e = q.ExecContext(ctx, `INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash) VALUES($1,$2,$3)`, a.TenantID(), bApplication(c.Bundle()), c.Hash())
	if e != nil {
		return Approval{}, e
	}
	if e = st.evidence(ctx, q, a, c, "release.approved"); e != nil {
		return Approval{}, e
	}
	return approval, nil
}
func (st Store) Activate(ctx context.Context, q postgres.Executor, a tenant.Access, c Candidate, approval Approval, expectedGeneration int64, currentBase string, deps Dependencies, gate Gate) error {
	if e := ValidateActivation(c, approval, currentBase, deps, a.TenantID(), a.ActorID(), gate); e != nil {
		return e
	}
	app := bApplication(c.Bundle())
	var changed int64
	if expectedGeneration == 0 && currentBase == "" {
		r, e := q.ExecContext(ctx, `INSERT INTO control.active_releases(tenant_id,app_id,artifact_hash,generation) VALUES($1,$2,$3,1) ON CONFLICT DO NOTHING`, a.TenantID(), app, c.Hash())
		if e != nil {
			return e
		}
		changed, _ = r.RowsAffected()
	} else {
		r, e := q.ExecContext(ctx, `UPDATE control.active_releases SET artifact_hash=$3,generation=generation+1 WHERE tenant_id=$1 AND app_id=$2 AND generation=$4 AND artifact_hash=$5`, a.TenantID(), app, c.Hash(), expectedGeneration, currentBase)
		if e != nil {
			return e
		}
		changed, _ = r.RowsAffected()
	}
	if changed != 1 {
		return ErrGate
	}
	return st.evidence(ctx, q, a, c, "release.activated")
}

// Active restores exact persisted bytes, never calls the current compiler.
func (Store) Active(ctx context.Context, q postgres.Executor, a tenant.Access, app string, supported []string) (compiler.Program, string, int64, error) {
	var raw []byte
	var hash string
	var generation int64
	e := q.QueryRowContext(ctx, `SELECT b.body,b.artifact_hash,a.generation FROM control.active_releases a JOIN control.release_bundles b USING(tenant_id,app_id,artifact_hash) WHERE a.tenant_id=$1 AND a.app_id=$2`, a.TenantID(), app).Scan(&raw, &hash, &generation)
	if e != nil {
		return compiler.Program{}, "", 0, e
	}
	c, e := Restore(raw, hash, supported)
	if e != nil {
		return compiler.Program{}, "", 0, e
	}
	p, e := compiler.Restore(c.Bundle().IR, c.Bundle().IRHash, supported)
	return p, hash, generation, e
}
