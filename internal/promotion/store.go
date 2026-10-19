package promotion

import (
	"context"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Store reads and writes release artifacts inside the caller's transaction.
// Since M13 the runtime role has no direct write on release approvals or the
// active pointer: approval, rejection, activation and rollback go only through
// the ADR039 ports wrapped in decisions.go.
//
// Store writes no audit entry: the API handler appends its own, correlated with the
// request and the policy decision, in the same transaction (ADR038 amendment 8).
type Store struct {
	// Recompute remembers the pure recomputation of stored candidates and bounds the
	// cold ones; nil is the process default.
	Recompute *Recompute
}

// RollbackPlan is the caller's assessment of a rollback (ADR039 section 5). The
// port requires it acknowledged (see Acknowledged) unless the current release
// is POINTER_SAFE; a plan that requires recovery is never executed.
type RollbackPlan struct {
	StorageCompatible, ExternalEffectsReviewed bool
	RecoveryRequired                           bool
	PlanHash                                   string
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
	return staged, nil
}

// Build is BuildCandidate for a request: it takes the admission tokens of the request's
// tenant while it runs (a build is the most expensive computation of the API) and
// remembers the outcome of the self-check, so the first read of the new candidate is a
// memo hit.
func (st Store) Build(ctx context.Context, in BuildInput) (Candidate, error) {
	rc := st.Recompute.orDefault()
	giveBack, err := rc.Admit(ctx)
	if err != nil {
		return Candidate{}, err
	}
	defer giveBack()
	c, program, err := buildCandidate(in)
	if err != nil {
		return Candidate{}, err
	}
	rc.remember(c, program, compiler.SupportedIRVersions())
	return c, nil
}

func bApplication(b Bundle) string {
	p, e := compiler.Restore(b.IR, b.IRHash, compiler.SupportedIRVersions())
	if e != nil {
		return ""
	}
	return p.IR().Application.AppID
}

// Active restores exact persisted bytes, never calls the current compiler.
func (st Store) Active(ctx context.Context, q postgres.Executor, a tenant.Access, app string, supported []string) (compiler.Program, string, int64, error) {
	var raw []byte
	var hash string
	var generation int64
	e := q.QueryRowContext(ctx, `SELECT b.body,b.artifact_hash,a.generation FROM control.active_releases a JOIN control.release_bundles b USING(tenant_id,app_id,artifact_hash) WHERE a.tenant_id=$1 AND a.app_id=$2`, a.TenantID(), app).Scan(&raw, &hash, &generation)
	if e != nil {
		return compiler.Program{}, "", 0, e
	}
	got, e := st.Recompute.orDefault().restore(ctx, raw, hash, supported)
	if e != nil {
		return compiler.Program{}, "", 0, e
	}
	return got.program, hash, generation, nil
}
