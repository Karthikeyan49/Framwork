package promotion

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Store reads and writes release artifacts inside the caller's transaction.
// Since M13 the runtime role has no direct write on release approvals or the
// active pointer: approval, rejection, activation and rollback go only through
// the ADR039 ports wrapped in decisions.go.
type Store struct{ Audit audit.Writer }

// RollbackPlan is the caller's assessment of a rollback (ADR039 section 5). The
// port requires it acknowledged (see Acknowledged) unless the current release
// is POINTER_SAFE; a plan that requires recovery is never executed.
type RollbackPlan struct {
	StorageCompatible, ExternalEffectsReviewed bool
	RecoveryRequired                           bool
	PlanHash                                   string
}

func (st Store) evidence(ctx context.Context, q postgres.Executor, a tenant.Access, c Candidate, action string) error {
	w := st.Audit
	if w == nil {
		w = audit.Postgres{}
	}
	return w.Append(ctx, q, audit.Entry{ActorID: a.ActorID(), TenantID: a.TenantID(), Action: action, Resource: "release", Allow: true, PolicyVersion: "m6r-release-contract-v1", State: audit.State{IRHash: c.Header().IRHash, ArtifactHash: c.Hash(), MetadataApp: c.Header().AppID}})
}

// Stage persists a server-built 2.0.0 candidate through control.stage_candidate,
// which binds the body to the authenticated requester and to the stored
// requirement set, acceptance spec, frozen sandbox evidence, source revision and
// active base. Bundle 1.0.0 is no longer stageable: its claims were caller
// declared, and the runtime role has no INSERT on control.release_bundles.
func (st Store) Stage(ctx context.Context, q postgres.Executor, a tenant.Access, c Candidate, gate Gate) (bool, error) {
	h := c.Header()
	if h.Version != BundleVersion2 || a.ActorID() == "" || h.RequesterID != a.ActorID() || h.TenantID != a.TenantID() || gate == nil || !gate.AuthorizeRelease(a.TenantID(), a.ActorID(), "stage", c.Hash()) {
		return false, ErrGate
	}
	var staged bool
	if e := q.QueryRowContext(ctx, `SELECT control.stage_candidate($1,$2,$3)`, h.AppID, c.Hash(), c.JSON()).Scan(&staged); e != nil {
		return false, e
	}
	if !staged {
		return false, nil
	}
	return true, st.evidence(ctx, q, a, c, "release.staged")
}
func bApplication(b Bundle) string {
	p, e := compiler.Restore(b.IR, b.IRHash, compiler.SupportedIRVersions())
	if e != nil {
		return ""
	}
	return p.IR().Application.AppID
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
	p, e := c.Program(supported)
	return p, hash, generation, e
}
