package promotion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

// M13 approval and promotion (ADR039). Every write goes through a SECURITY
// DEFINER port of migration 49 that derives tenant, actor, membership,
// separation floors, quorum, the candidate's bound base and the pointer
// compare-and-set from database-stamped rows. These wrappers only reject
// malformed arguments before SQL and translate SQLSTATEs into typed errors.
// They are not an authorization decision: callers first run policy.Decide,
// LoadCandidate (Restore) and Staleness in the same REPEATABLE READ transaction
// (ADR039 section 1), and they append their own audit entry in that
// transaction. Nothing here sets an actor, a risk level, a quorum or a
// generation itself.

// RejectReason is the closed vocabulary of a rejection (ADR039 section 2).
type RejectReason string

const (
	ReasonRequirementsNotMet   RejectReason = "REQUIREMENTS_NOT_MET"
	ReasonRiskUnacceptable     RejectReason = "RISK_UNACCEPTABLE"
	ReasonEvidenceInsufficient RejectReason = "EVIDENCE_INSUFFICIENT"
	ReasonSuperseded           RejectReason = "SUPERSEDED"
	ReasonWithdrawn            RejectReason = "WITHDRAWN"
	ReasonOther                RejectReason = "OTHER"
)

// Valid reports whether r is one of the six reasons the database accepts.
func (r RejectReason) Valid() bool {
	switch r {
	case ReasonRequirementsNotMet, ReasonRiskUnacceptable, ReasonEvidenceInsufficient, ReasonSuperseded, ReasonWithdrawn, ReasonOther:
		return true
	}
	return false
}

// Decision is one recorded approval. ActorID and At were stamped by the port.
type Decision struct {
	ActorID string    `json:"actor_id"`
	At      time.Time `json:"at"`
}

// ReleaseRejection is one recorded, permanent rejection of an exact hash. (The
// name Rejection is taken by the build-time rejection of bundle2.go.)
type ReleaseRejection struct {
	ActorID string       `json:"actor_id"`
	Reason  RejectReason `json:"reason"`
	Note    string       `json:"note"`
	At      time.Time    `json:"at"`
}

// History kinds.
const (
	HistoryActivate = "ACTIVATE"
	HistoryRollback = "ROLLBACK"
)

// HistoryEntry is one append-only row of control.release_history. PreviousHash
// is empty for the first activation; PlanHash is empty unless a rollback plan
// was acknowledged.
type HistoryEntry struct {
	Seq          int64     `json:"seq"`
	Kind         string    `json:"kind"`
	ArtifactHash string    `json:"artifact_hash"`
	PreviousHash string    `json:"previous_hash"`
	Generation   int64     `json:"generation"`
	ActorID      string    `json:"actor_id"`
	At           time.Time `json:"at"`
	RiskLevel    string    `json:"risk_level"`
	StorageClass string    `json:"storage_class"`
	PlanHash     string    `json:"plan_hash"`
}

// Bounds of the promotion arguments and reads.
const (
	// MaxNoteRunes bounds a rejection note (database: char_length <= 280).
	MaxNoteRunes = 280
	// MaxGeneration keeps generations exact in JSON numbers.
	MaxGeneration = 1<<53 - 1
	// MaxHistory is the most history rows one read returns (newest first).
	MaxHistory = 50
	// MaxDecisions bounds the approvals or rejections read for one hash; it
	// equals the approver bound of risk.CheckSeparation.
	MaxDecisions = risk.MaxEntries
)

var (
	// ErrConflict: the release state moved or forbids the step (SQLSTATE 55000,
	// 40001, 40P01, 55P03, 23505, 23503), e.g. a stale base, a rejected hash, a
	// quorum not met or a lost compare-and-set. Reload, then decide again.
	ErrConflict = errors.New("release state conflict")
	// ErrForbidden: the database refused the actor (SQLSTATE 42501), e.g. not an
	// active non-viewer member or excluded by a separation-of-duties floor.
	ErrForbidden = errors.New("release authority denied")
	// ErrInvalid: a malformed argument, refused in Go before SQL or by a port
	// (SQLSTATE 22023). It wraps ErrGate, so existing ErrGate mappings hold.
	ErrInvalid = fmt.Errorf("%w: invalid release decision", ErrGate)
	// ErrRecoveryRequired: a rollback plan that requires recovery is never
	// executed by a pointer move (ADR039 section 5). It wraps ErrInvalid.
	ErrRecoveryRequired = fmt.Errorf("%w: rollback requires an assessed recovery", ErrInvalid)
	// errPortResult reports a port result that contradicts its own contract; it
	// is deliberately not one of the typed refusals (an internal error).
	errPortResult = errors.New("release port returned an unexpected result")
)

// PortError is a refusal by an M13 database port or read. Kind is ErrNotFound
// (P0002), ErrConflict, ErrForbidden or ErrInvalid; both Kind and the driver
// error are reachable with errors.Is / errors.As (for example *pgconn.PgError
// for the SQLSTATE and the port's message). Error() returns the kind only, so
// raw driver text never reaches a log through it.
type PortError struct {
	Kind  error
	Cause error
}

func (e *PortError) Error() string   { return e.Kind.Error() }
func (e *PortError) Unwrap() []error { return []error{e.Kind, e.Cause} }

// classify maps the SQLSTATEs of ADR039 section 7 to typed errors; anything
// else (transport, unknown SQLSTATE) is returned unchanged.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	var kind error
	switch pg.Code {
	case "P0002":
		kind = ErrNotFound
	case "55000", "40001", "40P01", "55P03", "23505", "23503":
		kind = ErrConflict
	case "42501":
		kind = ErrForbidden
	case "22023":
		kind = ErrInvalid
	default:
		return err
	}
	return &PortError{Kind: kind, Cause: err}
}

var appIDPattern = regexp.MustCompile(`^app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// validScope requires a resolved tenant and actor and a well-formed application
// id. The ports derive tenant and actor themselves; this only keeps malformed
// requests away from SQL.
func validScope(a tenant.Access, app string) bool {
	return validScopeOf(a.TenantID(), a.ActorID(), app)
}

func validScopeOf(tenantID, actorID, app string) bool {
	return tenant.ValidID(tenantID) && tenant.ValidID(actorID) && len(app) <= 160 && appIDPattern.MatchString(app)
}

// ValidNote reports whether a rejection note is acceptable: valid UTF-8, at most
// MaxNoteRunes characters, no control characters and no bidirectional or line
// separator formatting that could disguise the text where it is displayed.
func ValidNote(note string) bool {
	if !utf8.ValidString(note) || utf8.RuneCountInString(note) > MaxNoteRunes {
		return false
	}
	for _, r := range note {
		if unicode.IsControl(r) || r == ' ' || r == ' ' || r >= '‪' && r <= '‮' || r >= '⁦' && r <= '⁩' {
			return false
		}
	}
	return true
}

// activationArgs accepts an exact hash over the empty pointer ("" at generation
// 0, a first release) or over an exact base hash at a generation that can still
// advance.
func activationArgs(hash, expectedBase string, expectedGeneration int64) bool {
	if !ValidHash(hash) {
		return false
	}
	if expectedBase == "" {
		return expectedGeneration == 0
	}
	return ValidHash(expectedBase) && expectedGeneration >= 1 && expectedGeneration < MaxGeneration
}

// rollbackArgs checks the shape of a rollback request. A plan that requires
// recovery is refused whatever the class of the current release.
func rollbackArgs(target, expectedCurrent string, expectedGeneration int64, plan RollbackPlan) error {
	if !ValidHash(target) || !ValidHash(expectedCurrent) || target == expectedCurrent || expectedGeneration < 1 || expectedGeneration >= MaxGeneration || plan.PlanHash != "" && !ValidHash(plan.PlanHash) {
		return ErrInvalid
	}
	if plan.RecoveryRequired {
		return ErrRecoveryRequired
	}
	return nil
}

// ApproveRelease records the caller's approval of the exact hash through
// control.approve_release and returns the number of distinct eligible approvals
// recorded for it afterwards. Repeating an approval is idempotent.
func (Store) ApproveRelease(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string) (approvals int, err error) {
	if !validScope(a, app) || !ValidHash(hash) {
		return 0, ErrInvalid
	}
	var n int64
	if err := q.QueryRowContext(ctx, `SELECT control.approve_release($1,$2)`, app, hash).Scan(&n); err != nil {
		return 0, classify(err)
	}
	// The caller's own approval passed the floor, so at least one counts.
	if n < 1 || n > MaxDecisions {
		return 0, errPortResult
	}
	return int(n), nil
}

// RejectRelease records the caller's permanent rejection of the exact hash
// through control.reject_release. recorded is false when the caller had already
// rejected it (the first reason and note stay). The note is free text: it is
// stored for reviewers and must never be copied into audit state.
func (Store) RejectRelease(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string, reason RejectReason, note string) (recorded bool, err error) {
	if !validScope(a, app) || !ValidHash(hash) || !reason.Valid() || !ValidNote(note) {
		return false, ErrInvalid
	}
	if err := q.QueryRowContext(ctx, `SELECT control.reject_release($1,$2,$3,$4)`, app, hash, string(reason), note).Scan(&recorded); err != nil {
		return false, classify(err)
	}
	return recorded, nil
}

// ActivateRelease moves the application's release pointer to hash through
// control.activate_release and returns the new generation. expectedBase and
// expectedGeneration are the pointer the caller read ("" and 0 for a first
// release); the port also requires them to equal the candidate's bound base.
func (Store) ActivateRelease(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash, expectedBase string, expectedGeneration int64) (generation int64, err error) {
	if !validScope(a, app) || !activationArgs(hash, expectedBase, expectedGeneration) {
		return 0, ErrInvalid
	}
	if err := q.QueryRowContext(ctx, `SELECT control.activate_release($1,$2,$3,$4)`, app, hash, expectedBase, expectedGeneration).Scan(&generation); err != nil {
		return 0, classify(err)
	}
	if generation != expectedGeneration+1 {
		return 0, errPortResult
	}
	return generation, nil
}

// Acknowledged is the p_plan_ack of ADR039 section 5: storage compatible,
// external effects reviewed and no recovery required.
func (p RollbackPlan) Acknowledged() bool {
	return p.StorageCompatible && p.ExternalEffectsReviewed && !p.RecoveryRequired
}

// RollbackRelease moves the pointer from expectedCurrent at expectedGeneration
// back to target through control.rollback_release and returns the new
// generation. The port requires target to be a previously activated release and,
// unless the current release is POINTER_SAFE, an acknowledged plan with a hash.
// A plan that requires recovery is refused here, whatever the class.
func (Store) RollbackRelease(ctx context.Context, q postgres.Executor, a tenant.Access, app, target, expectedCurrent string, expectedGeneration int64, plan RollbackPlan) (generation int64, err error) {
	if !validScope(a, app) {
		return 0, ErrInvalid
	}
	if err := rollbackArgs(target, expectedCurrent, expectedGeneration, plan); err != nil {
		return 0, err
	}
	if err := q.QueryRowContext(ctx, `SELECT control.rollback_release($1,$2,$3,$4,$5,$6)`, app, target, expectedCurrent, expectedGeneration, plan.PlanHash, plan.Acknowledged()).Scan(&generation); err != nil {
		return 0, classify(err)
	}
	if generation != expectedGeneration+1 {
		return 0, errPortResult
	}
	return generation, nil
}

// Approvals lists the recorded approvals of the exact hash, oldest first. It
// includes approvers the activation port would no longer count (demoted or
// removed members, floor-excluded actors of M6R rows); use CountEligible for
// the floor and let the port decide quorum.
func (Store) Approvals(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string) ([]Decision, error) {
	if !validScope(a, app) || !ValidHash(hash) {
		return nil, ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT actor_id::text,approved_at FROM control.release_approvals WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3 ORDER BY approved_at,actor_id LIMIT $4`, a.TenantID(), app, hash, MaxDecisions)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []Decision{}
	for rows.Next() {
		var d Decision
		if err = rows.Scan(&d.ActorID, &d.At); err != nil {
			return nil, classify(err)
		}
		d.At = d.At.UTC()
		out = append(out, d)
	}
	return out, classify(rows.Err())
}

// Rejections lists the recorded rejections of the exact hash, oldest first.
func (Store) Rejections(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string) ([]ReleaseRejection, error) {
	if !validScope(a, app) || !ValidHash(hash) {
		return nil, ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT actor_id::text,reason,note,rejected_at FROM control.release_rejections WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3 ORDER BY rejected_at,actor_id LIMIT $4`, a.TenantID(), app, hash, MaxDecisions)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []ReleaseRejection{}
	for rows.Next() {
		var r ReleaseRejection
		var reason string
		if err = rows.Scan(&r.ActorID, &reason, &r.Note, &r.At); err != nil {
			return nil, classify(err)
		}
		r.Reason, r.At = RejectReason(reason), r.At.UTC()
		out = append(out, r)
	}
	return out, classify(rows.Err())
}

// History lists the newest activation and rollback rows of the application,
// newest first; limit is 1..MaxHistory.
func (Store) History(ctx context.Context, q postgres.Executor, a tenant.Access, app string, limit int) ([]HistoryEntry, error) {
	if !validScope(a, app) || limit < 1 || limit > MaxHistory {
		return nil, ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT seq,kind,artifact_hash,coalesce(previous_hash,''),generation,actor_id::text,"at",coalesce(risk_level,''),coalesce(storage_class,''),coalesce(plan_hash,'') FROM control.release_history WHERE tenant_id=$1 AND app_id=$2 ORDER BY seq DESC LIMIT $3`, a.TenantID(), app, limit)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if err = rows.Scan(&h.Seq, &h.Kind, &h.ArtifactHash, &h.PreviousHash, &h.Generation, &h.ActorID, &h.At, &h.RiskLevel, &h.StorageClass, &h.PlanHash); err != nil {
			return nil, classify(err)
		}
		h.At = h.At.UTC()
		out = append(out, h)
	}
	return out, classify(rows.Err())
}

// WasActivated reports whether the exact hash appears in the application's
// history, i.e. was activated (or rolled back to) at some point. Pointers set
// before M13 have no history row (ADR039 section 2).
func (Store) WasActivated(ctx context.Context, q postgres.Executor, a tenant.Access, app, hash string) (bool, error) {
	if !validScope(a, app) || !ValidHash(hash) {
		return false, ErrInvalid
	}
	var found bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM control.release_history WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3)`, a.TenantID(), app, hash).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errPortResult
	}
	return found, classify(err)
}

// ------------------------------------------------------------------ pure rules

// Status is the derived (never stored) lifecycle status of a candidate.
type Status string

const (
	StatusStaged     Status = "STAGED"
	StatusApproved   Status = "APPROVED"
	StatusRejected   Status = "REJECTED"
	StatusActive     Status = "ACTIVE"
	StatusSuperseded Status = "SUPERSEDED"
)

// DeriveStatus derives a candidate's status with the precedence REJECTED >
// ACTIVE > SUPERSEDED > APPROVED > STAGED: any rejection is final; the hash the
// pointer holds is ACTIVE; a hash that was activated before is SUPERSEDED;
// otherwise APPROVED once approvals reach required (at least one), else STAGED.
// approvals should be the floor-eligible count (CountEligible); APPROVED is a
// display state, the activation port recounts under its row lock.
func DeriveStatus(rejections, approvals, required int, active, everActivated bool) Status {
	switch {
	case rejections > 0:
		return StatusRejected
	case active:
		return StatusActive
	case everActivated:
		return StatusSuperseded
	case required >= 1 && approvals >= required:
		return StatusApproved
	}
	return StatusStaged
}

// RequiredApprovals is the database quorum floor of ADR039 section 4: two
// distinct eligible approvals for CRITICAL, otherwise one. An unknown level
// fails closed to the CRITICAL quorum. A restored bundle's own Separation may
// demand more; risk.CheckSeparation applies it in Go.
func RequiredApprovals(level string) int {
	switch level {
	case "LOW", "MEDIUM", "HIGH":
		return 1
	}
	return 2
}

// FloorRoles lists the separation roles the database floor excludes from
// approving at level (ADR039 section 3.5): none for LOW and MEDIUM, requester and
// acceptance author for HIGH, also the sandbox freezer for CRITICAL. An unknown
// level fails closed to the CRITICAL exclusions.
func FloorRoles(level string) []string {
	switch level {
	case "LOW", "MEDIUM":
		return []string{}
	case "HIGH":
		return []string{risk.RoleRequester, risk.RoleAcceptanceAuthor}
	}
	return []string{risk.RoleRequester, risk.RoleAcceptanceAuthor, risk.RoleSandboxFreezer}
}

func partyID(p risk.Parties, role string) string {
	switch role {
	case risk.RoleRequester:
		return p.Requester
	case risk.RoleAcceptanceAuthor:
		return p.AcceptanceAuthor
	case risk.RoleSandboxFreezer:
		return p.SandboxFreezer
	}
	return ""
}

// FloorExcluded returns the actor ids the database floor excludes at level, in
// FloorRoles order without duplicates. It is for server-side checks only:
// responses name the excluded role (FloorExclusion), never another actor's id.
func FloorExcluded(level string, parties risk.Parties) []string {
	out := []string{}
	for _, role := range FloorRoles(level) {
		id := strings.ToLower(partyID(parties, role))
		if id != "" && !containsString(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// FloorExclusion reports whether the database floor admits actor as an approver
// at level. When it does not, role names the first excluding separation role,
// or is empty when actor is not a valid actor id. It fails closed: an excluded
// role whose party id is unknown excludes everyone (the floor cannot be shown to
// hold). Responses may name role; they never name another actor's id.
func FloorExclusion(level string, parties risk.Parties, actor string) (role string, admitted bool) {
	if !tenant.ValidID(actor) {
		return "", false
	}
	for _, r := range FloorRoles(level) {
		id := partyID(parties, r)
		if id == "" || strings.EqualFold(id, actor) {
			return r, false
		}
	}
	return "", true
}

// CountEligible counts the distinct approvers the database floor admits at
// level. It cannot see membership: the activation port also drops approvers who
// are no longer active non-viewer members, so this is an upper bound of the
// quorum the database will count.
func CountEligible(level string, parties risk.Parties, approvers []string) int {
	seen := map[string]bool{}
	for _, actor := range approvers {
		if _, ok := FloorExclusion(level, parties, actor); ok {
			seen[actor] = true
		}
	}
	return len(seen)
}

// RollbackClassNeedsPlan mirrors the rollback port's class gate: only exactly
// POINTER_SAFE moves the pointer without an acknowledged plan. DATA_NOT_REVERSIBLE,
// EXTERNAL_EFFECTS, a 1.0.0 body (no class) and any other value need one.
func RollbackClassNeedsPlan(class string) bool { return class != risk.RollbackPointerSafe }
