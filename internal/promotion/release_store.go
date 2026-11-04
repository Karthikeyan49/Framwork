package promotion

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Persistence of requirement sets, acceptance specs and candidates. Every write
// goes through a SECURITY DEFINER port (migration 83) that stamps the authenticated
// actor; this code never sets an author, decider or requester itself.

var ErrNotFound = errors.New("release artifact not found")

// AppendRequirements stores the next immutable requirement-set version. expected
// is the version the caller read (0 for none); a different head is a conflict
// (SQLSTATE 55000) so concurrent editors cannot overwrite each other.
func (Store) AppendRequirements(ctx context.Context, q postgres.Executor, a tenant.Access, app string, expected int64, canonical []byte) (StoredRequirements, error) {
	if a.TenantID() == "" || app == "" || expected < 0 || expected >= 1000000 || len(canonical) < 2 || len(canonical) > 262144 {
		return StoredRequirements{}, ErrGate
	}
	var version int64
	if err := q.QueryRowContext(ctx, `SELECT control.append_requirements($1,$2,$3)`, app, expected, canonical).Scan(&version); err != nil {
		return StoredRequirements{}, err
	}
	return (Store{}).Requirements(ctx, q, a, app, version)
}

// Requirements reads one version; version 0 reads the head.
func (Store) Requirements(ctx context.Context, q postgres.Executor, a tenant.Access, app string, version int64) (StoredRequirements, error) {
	var r StoredRequirements
	var at time.Time
	query := `SELECT app_id,version,body_hash,decided_by::text,decided_at,body FROM control.requirement_sets WHERE tenant_id=$1 AND app_id=$2 AND version=$3`
	args := []any{a.TenantID(), app, version}
	if version == 0 {
		query = `SELECT app_id,version,body_hash,decided_by::text,decided_at,body FROM control.requirement_sets WHERE tenant_id=$1 AND app_id=$2 ORDER BY version DESC LIMIT 1`
		args = args[:2]
	}
	err := q.QueryRowContext(ctx, query, args...).Scan(&r.AppID, &r.Version, &r.Hash, &r.DecidedBy, &at, &r.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredRequirements{}, ErrNotFound
	}
	if err != nil {
		return StoredRequirements{}, err
	}
	r.DecidedAt = at.UTC().Format(time.RFC3339Nano)
	if Hash(r.Body) != r.Hash {
		return StoredRequirements{}, ErrGate
	}
	return r, nil
}

// AuthorAcceptance stores an immutable acceptance spec authored by the caller.
// Resubmitting identical content returns the caller's original row.
func (Store) AuthorAcceptance(ctx context.Context, q postgres.Executor, a tenant.Access, app, requirementsHash string, canonical []byte) (StoredAcceptance, error) {
	if a.TenantID() == "" || app == "" || !ValidHash(requirementsHash) || len(canonical) < 2 || len(canonical) > 1<<20 {
		return StoredAcceptance{}, ErrGate
	}
	var id string
	if err := q.QueryRowContext(ctx, `SELECT control.author_acceptance($1,$2,$3)::text`, app, requirementsHash, canonical).Scan(&id); err != nil {
		return StoredAcceptance{}, err
	}
	return (Store{}).Acceptance(ctx, q, a, app, id)
}

// Acceptance reads one acceptance spec.
func (Store) Acceptance(ctx context.Context, q postgres.Executor, a tenant.Access, app, id string) (StoredAcceptance, error) {
	var s StoredAcceptance
	var at time.Time
	if !tenant.ValidID(id) {
		return s, ErrNotFound
	}
	err := q.QueryRowContext(ctx, `SELECT id::text,app_id,requirements_hash,body_hash,author_id::text,created_at,body FROM control.acceptance_specs WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, a.TenantID(), app, id).
		Scan(&s.ID, &s.AppID, &s.RequirementsHash, &s.Hash, &s.AuthorID, &at, &s.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredAcceptance{}, ErrNotFound
	}
	if err != nil {
		return StoredAcceptance{}, err
	}
	s.CreatedAt = at.UTC().Format(time.RFC3339Nano)
	if Hash(s.Body) != s.Hash {
		return StoredAcceptance{}, ErrGate
	}
	return s, nil
}

// ActiveBase reads the active release pointer of an application. Hash is empty
// and Generation 0 when none is active.
func (Store) ActiveBase(ctx context.Context, q postgres.Executor, a tenant.Access, app string) (hash string, generation int64, err error) {
	err = q.QueryRowContext(ctx, `SELECT artifact_hash,generation FROM control.active_releases WHERE tenant_id=$1 AND app_id=$2`, a.TenantID(), app).Scan(&hash, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return hash, generation, err
}

// CandidateRow is the derived index of a staged candidate. Every column was
// copied by the stage port from the verified body and the rows it bound to.
type CandidateRow struct {
	AppID               string    `json:"app_id"`
	ArtifactHash        string    `json:"artifact_hash"`
	RequesterID         string    `json:"requester_id"`
	BaseRelease         string    `json:"base_release"`
	BaseGeneration      int64     `json:"base_generation"`
	RiskLevel           string    `json:"risk_level"`
	HighRisk            bool      `json:"high_risk"`
	RiskPolicyVersion   string    `json:"risk_policy_version"`
	RequirementsVersion int64     `json:"requirements_version"`
	RequirementsHash    string    `json:"requirements_hash"`
	AcceptanceID        string    `json:"acceptance_id"`
	AcceptanceHash      string    `json:"acceptance_hash"`
	AcceptanceAuthor    string    `json:"acceptance_author"`
	SandboxDatasetID    string    `json:"sandbox_dataset_id"`
	SandboxEvidenceHash string    `json:"sandbox_evidence_hash"`
	SandboxFrozenBy     string    `json:"sandbox_frozen_by"`
	SourceRevision      int64     `json:"source_revision"`
	SourceAuthor        string    `json:"source_author"`
	IRHash              string    `json:"ir_hash"`
	CreatedAt           time.Time `json:"created_at"`
}

const candidateColumns = `app_id,artifact_hash,requester_id::text,base_release,base_generation,risk_level,high_risk,risk_policy_version,requirements_version,requirements_hash,acceptance_id::text,acceptance_hash,acceptance_author::text,sandbox_dataset_id::text,sandbox_evidence_hash,sandbox_frozen_by::text,source_revision,source_author::text,ir_hash,created_at`

func scanCandidate(row interface{ Scan(...any) error }) (CandidateRow, error) {
	var c CandidateRow
	err := row.Scan(&c.AppID, &c.ArtifactHash, &c.RequesterID, &c.BaseRelease, &c.BaseGeneration, &c.RiskLevel, &c.HighRisk, &c.RiskPolicyVersion, &c.RequirementsVersion, &c.RequirementsHash,
		&c.AcceptanceID, &c.AcceptanceHash, &c.AcceptanceAuthor, &c.SandboxDatasetID, &c.SandboxEvidenceHash, &c.SandboxFrozenBy, &c.SourceRevision, &c.SourceAuthor, &c.IRHash, &c.CreatedAt)
	return c, err
}

// Candidates lists the newest staged candidates of an application.
func (Store) Candidates(ctx context.Context, q postgres.Executor, a tenant.Access, app string) ([]CandidateRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+candidateColumns+` FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2 ORDER BY created_at DESC,artifact_hash LIMIT 50`, a.TenantID(), app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CandidateRow{}
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LoadCandidate reads and fully verifies one staged candidate: the stored bytes
// are restored (risk, verification and every base-independent derivation are
// recomputed) and then verified against the candidate's own base release (diff,
// storage class and evolution plan are recomputed) before anything is returned.
// Both recomputations are pure functions of immutable bytes, so a success is
// remembered under their digests (Recompute); the bytes are read from the database
// under the caller's row-level security on every call, and nothing that depends on
// live state is remembered.
func (st Store) LoadCandidate(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string, supported []string) (Candidate, CandidateRow, error) {
	if !ValidHash(hash) {
		return Candidate{}, CandidateRow{}, ErrNotFound
	}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT body FROM control.release_bundles WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`, a.TenantID(), app, hash).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, CandidateRow{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, CandidateRow{}, err
	}
	rc := st.Recompute.orDefault()
	got, err := rc.restore(ctx, raw, hash, supported)
	if err != nil {
		return Candidate{}, CandidateRow{}, err
	}
	// Restore cannot recompute what depends on the base release; resolve the
	// candidate's own base and finish the recomputation before anything is shown
	// or decided on.
	resolve := func(baseHash string) (compiler.Program, error) {
		_, program, e := st.ReleaseProgram(ctx, q, a, app, baseHash, supported)
		return program, e
	}
	if err = rc.verifyWithBase(ctx, got, resolve, supported); err != nil {
		return Candidate{}, CandidateRow{}, err
	}
	row, err := scanCandidate(q.QueryRowContext(ctx, `SELECT `+candidateColumns+` FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`, a.TenantID(), app, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, CandidateRow{}, ErrNotFound
	}
	return got.candidate, row, err
}

// Release reads and restores any stored bundle (1.0.0 or 2.0.0), for example the
// active base of a new candidate.
func (st Store) Release(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string, supported []string) (Candidate, error) {
	c, _, err := st.ReleaseProgram(ctx, q, a, app, hash, supported)
	return c, err
}

// ReleaseProgram is Release that also returns the executable program of the bundle's
// IR, which the restore had to build anyway.
func (st Store) ReleaseProgram(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string, supported []string) (Candidate, compiler.Program, error) {
	if !ValidHash(hash) {
		return Candidate{}, compiler.Program{}, ErrNotFound
	}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT body FROM control.release_bundles WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3`, a.TenantID(), app, hash).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, compiler.Program{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, compiler.Program{}, err
	}
	got, err := st.Recompute.orDefault().restore(ctx, raw, hash, supported)
	if err != nil {
		return Candidate{}, compiler.Program{}, err
	}
	return got.candidate, got.program, nil
}
