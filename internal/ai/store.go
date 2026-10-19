package ai

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

// Persistence of AI proposals, their per-attempt trace and the daily budget
// (ADR040 amendments A1-A4). Every write goes through a SECURITY DEFINER port of
// migration 60 that derives tenant and actor from the leased session settings;
// this code never sets a requester, a decider, a tenant or a status itself, and
// the runtime role has no DML on the tables. These wrappers reject malformed
// arguments before SQL and translate SQLSTATEs into typed errors. They are not
// an authorization decision: the HTTP layer runs policy.Decide first.

// ProposalKind is which pipeline produced a proposal.
type ProposalKind string

// ProposalKind values (migration 60 CHECK).
const (
	ProposalPatch        ProposalKind = "patch"
	ProposalRequirements ProposalKind = "requirements"
)

// ProposalStatus is the ADR040 A4 status machine.
type ProposalStatus string

// ProposalStatus values. NEEDS_HUMAN, FAILED, EXPIRED, APPLIED, CONFIRMED and
// REJECTED are terminal.
const (
	ProposalPending    ProposalStatus = "PENDING"
	ProposalReady      ProposalStatus = "READY"
	ProposalNeedsHuman ProposalStatus = "NEEDS_HUMAN"
	ProposalFailed     ProposalStatus = "FAILED"
	ProposalExpired    ProposalStatus = "EXPIRED"
	ProposalApplied    ProposalStatus = "APPLIED"
	ProposalConfirmed  ProposalStatus = "CONFIRMED"
	ProposalRejected   ProposalStatus = "REJECTED"
)

// Failure codes stored with a FAILED (or EXPIRED) proposal: the attempt outcomes
// of contract.go plus three the platform itself assigns.
const (
	FailureExpired       = "EXPIRED"
	FailureNotConfigured = "NOT_CONFIGURED"
	FailureInternal      = "INTERNAL"
)

// Decisions understood by ai_decide.
const (
	DecisionApply   = "APPLY"
	DecisionConfirm = "CONFIRM"
	DecisionReject  = "REJECT"
)

// RejectReasons is the closed vocabulary of a human rejection.
var RejectReasons = []string{"NOT_WANTED", "INCORRECT", "UNSAFE", "SUPERSEDED", "OTHER"}

// Bounds of the arguments and reads (migration 60 ceilings).
const (
	// MaxListRows is the most proposals one list returns (newest first).
	MaxListRows = MaxListItems
	// maxDBTokens is the database ceiling of any token count or budget.
	maxDBTokens = 100_000_000
	// maxReserve is the database ceiling of one reservation.
	maxReserve = 10_000_000
	// maxAttemptRows is the database ceiling of recorded attempts.
	maxAttemptRows = 16
	// maxDBInFlight is the database ceiling of the in-flight limit.
	maxDBInFlight = 16
)

var (
	// ErrProposalNotFound: no such proposal in this tenant and application (P0002),
	// or no application draft to propose against.
	ErrProposalNotFound = errors.New("ai: proposal not found")
	// ErrProposalConflict: the proposal or the artifact it was decided against moved
	// (SQLSTATE 55000, 40001, 40P01, 55P03, 23505, 23503). The PortError detail names
	// which one: draft_changed, not_pending, not_ready, decided, expired.
	ErrProposalConflict = errors.New("ai: proposal state conflict")
	// ErrNotPermitted: the database refused the actor (SQLSTATE 42501).
	ErrNotPermitted = errors.New("ai: not permitted")
	// ErrInvalidRequest: a malformed argument, refused in Go before SQL or by a port
	// (SQLSTATE 22023).
	ErrInvalidRequest = errors.New("ai: invalid request")
	// ErrBusy: the tenant's in-flight limit is reached (SQLSTATE 53400, detail
	// in_flight). The daily token budget is ErrBudget of contract.go.
	ErrBusy = errors.New("ai: too many requests in flight")
	// ErrCorrupt: a stored proposal does not match its own hash; it is never shown.
	ErrCorrupt = errors.New("ai: stored proposal does not match its hash")
)

// PortError is a refusal by an AI database port. Kind is one of the sentinels
// above (or ErrBudget); Detail is the port's closed DETAIL code ("" when it
// named none). Both Kind and the driver error are reachable with errors.Is /
// errors.As; Error() is the kind only, so driver text never reaches a log
// through it.
type PortError struct {
	Kind   error
	Detail string
	Cause  error
}

func (e *PortError) Error() string   { return e.Kind.Error() }
func (e *PortError) Unwrap() []error { return []error{e.Kind, e.Cause} }

// classifyPort maps the SQLSTATEs of ADR040 A3; anything else (transport,
// unknown SQLSTATE) is returned unchanged.
func classifyPort(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	kind := error(nil)
	switch pg.Code {
	case "22023":
		kind = ErrInvalidRequest
	case "42501":
		kind = ErrNotPermitted
	case "P0002":
		kind = ErrProposalNotFound
	case "55000", "40001", "40P01", "55P03", "23505", "23503":
		kind = ErrProposalConflict
	case "53400":
		kind = ErrBudget
		if pg.Detail == "in_flight" {
			kind = ErrBusy
		}
	default:
		return err
	}
	return &PortError{Kind: kind, Detail: pg.Detail, Cause: err}
}

// Store reads and writes AI proposals through the migration-60 ports.
type Store struct{}

// apiDigest is the lowercase hex SHA-256 used for every hash this package stores.
func apiDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func apiHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// apiHexOrEmpty accepts the empty value or a digest.
func apiHexOrEmpty(s string) bool { return s == "" || apiHex(s) }

// BeginArgs are the admission facts. Nothing here is an authority: the port
// derives tenant and actor itself and re-checks the draft head.
type BeginArgs struct {
	App           string
	Kind          ProposalKind
	RequestHash   string
	DraftRevision int64
	DraftHash     string
	DailyTokens   int64
	Reserve       int64
	InFlight      int
	LeaseSeconds  int
}

// Begin runs ai_begin and returns the new PENDING proposal's id.
func (Store) Begin(ctx context.Context, q postgres.Executor, a tenant.Access, b BeginArgs) (string, error) {
	if a.TenantID() == "" || b.App == "" || (b.Kind != ProposalPatch && b.Kind != ProposalRequirements) || !apiHex(b.RequestHash) || !apiHex(b.DraftHash) ||
		b.DraftRevision < 1 || b.DraftRevision > 1_000_000_000 || b.DailyTokens < 1 || b.DailyTokens > maxDBTokens || b.Reserve < 1 || b.Reserve > maxReserve ||
		b.InFlight < 1 || b.InFlight > maxDBInFlight || b.LeaseSeconds < 1 || b.LeaseSeconds > 600 {
		return "", &PortError{Kind: ErrInvalidRequest}
	}
	var id string
	err := q.QueryRowContext(ctx, `SELECT control.ai_begin($1,$2,$3,$4,$5,$6,$7,$8,$9)::text`,
		b.App, string(b.Kind), b.RequestHash, b.DraftRevision, b.DraftHash, b.DailyTokens, b.Reserve, b.InFlight, b.LeaseSeconds).Scan(&id)
	if err != nil {
		return "", classifyPort(err)
	}
	return id, nil
}

// AttemptRecord is the trace of one provider attempt: hashes, labels, counts and
// a closed outcome. There is deliberately no field for prompt or model text.
type AttemptRecord struct {
	Attempt         int
	Provider, Model string
	Template        string
	ContextHash     string
	RequestHash     string
	OutputHash      string
	InputTokens     int64
	OutputTokens    int64
	LatencyMS       int
	Outcome         string
	DiagnosticsHash string
	CompilerHash    string
}

// RecordAttempt runs ai_record_attempt.
func (Store) RecordAttempt(ctx context.Context, q postgres.Executor, app, proposal string, r AttemptRecord) error {
	if !tenant.ValidID(proposal) || app == "" || r.Attempt < 1 || r.Attempt > maxAttemptRows || !apiHexOrEmpty(r.ContextHash) || !apiHexOrEmpty(r.RequestHash) ||
		!apiHexOrEmpty(r.OutputHash) || !apiHexOrEmpty(r.DiagnosticsHash) || !apiHex(r.CompilerHash) {
		return &PortError{Kind: ErrInvalidRequest}
	}
	_, err := q.ExecContext(ctx, `SELECT control.ai_record_attempt($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		app, proposal, r.Attempt, r.Provider, r.Model, r.Template, r.ContextHash, r.RequestHash, r.OutputHash, r.InputTokens, r.OutputTokens, r.LatencyMS, r.Outcome, r.DiagnosticsHash, r.CompilerHash)
	return classifyPort(err)
}

// FinishArgs is the validated result of a run. Body is the canonical proposal
// (nil for FAILED); ProposalHash must equal the SHA-256 of Body.
type FinishArgs struct {
	App, Proposal   string
	Status          ProposalStatus
	Body            []byte
	ProposalHash    string
	Derived         json.RawMessage
	Diagnostics     json.RawMessage
	DiagnosticsHash string
	FailureCode     string
	ContextHash     string
	Attempts        int
	InputTokens     int64
	OutputTokens    int64
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfNone(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// Finish runs ai_finish and returns the stored status. A proposal that outlived
// its lease comes back EXPIRED (its usage is charged either way).
func (Store) Finish(ctx context.Context, q postgres.Executor, f FinishArgs) (ProposalStatus, error) {
	if !tenant.ValidID(f.Proposal) || f.App == "" || f.Attempts < 0 || f.Attempts > maxAttemptRows || f.InputTokens < 0 || f.InputTokens > maxDBTokens || f.OutputTokens < 0 || f.OutputTokens > maxDBTokens ||
		(len(f.Body) > 0 && f.ProposalHash != apiDigest(f.Body)) || (len(f.Body) == 0 && f.ProposalHash != "") {
		return "", &PortError{Kind: ErrInvalidRequest}
	}
	var derived, diagnostics any
	if len(f.Derived) > 0 {
		derived = string(f.Derived)
	}
	if len(f.Diagnostics) > 0 {
		diagnostics = string(f.Diagnostics)
	}
	var stored string
	err := q.QueryRowContext(ctx, `SELECT control.ai_finish($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,$11,$12,$13)`,
		f.App, f.Proposal, string(f.Status), nullIfNone(f.Body), nullIfEmpty(f.ProposalHash), derived, diagnostics, nullIfEmpty(f.DiagnosticsHash), nullIfEmpty(f.FailureCode),
		nullIfEmpty(f.ContextHash), f.Attempts, f.InputTokens, f.OutputTokens).Scan(&stored)
	if err != nil {
		return "", classifyPort(err)
	}
	return ProposalStatus(stored), nil
}

// DecideArgs is one human decision. ExpectedRevision, ResultRevision and
// ResultHash are set for APPLY and CONFIRM (the artifact the caller just wrote in
// this transaction) and nil/empty for REJECT, which instead names a reason.
type DecideArgs struct {
	App, Proposal    string
	Decision         string
	ExpectedRevision *int64
	ResultRevision   *int64
	ResultHash       string
	RejectReason     string
	DecisionHash     string
}

// Decide runs ai_decide and returns the new status (APPLIED, CONFIRMED or REJECTED).
func (Store) Decide(ctx context.Context, q postgres.Executor, d DecideArgs) (ProposalStatus, error) {
	if !tenant.ValidID(d.Proposal) || d.App == "" || !apiHex(d.DecisionHash) {
		return "", &PortError{Kind: ErrInvalidRequest}
	}
	var res any
	if d.ResultHash != "" {
		res = d.ResultHash
	}
	var reason any
	if d.RejectReason != "" {
		reason = d.RejectReason
	}
	var expected, result any
	if d.ExpectedRevision != nil {
		expected = *d.ExpectedRevision
	}
	if d.ResultRevision != nil {
		result = *d.ResultRevision
	}
	var stored string
	err := q.QueryRowContext(ctx, `SELECT control.ai_decide($1,$2,$3,$4,$5,$6,$7,$8)`, d.App, d.Proposal, d.Decision, expected, result, res, reason, d.DecisionHash).Scan(&stored)
	if err != nil {
		return "", classifyPort(err)
	}
	return ProposalStatus(stored), nil
}

// Proposal is the API shape of a stored proposal. Every field is read from the
// forced-RLS table; none is echoed from a request. A PENDING proposal past its
// lease is reported EXPIRED. Proposal is the stored canonical object (absent for
// FAILED and EXPIRED), never a raw model answer.
type Proposal struct {
	ID              string          `json:"id"`
	AppID           string          `json:"app_id"`
	Kind            ProposalKind    `json:"kind"`
	Status          ProposalStatus  `json:"status"`
	RequesterID     string          `json:"requester_id"`
	CreatedAt       time.Time       `json:"created_at"`
	ExpiresAt       time.Time       `json:"expires_at"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	SourceRevision  int64           `json:"source_revision"`
	DraftHash       string          `json:"draft_hash"`
	RequestHash     string          `json:"request_hash"`
	ContextHash     string          `json:"context_hash,omitempty"`
	ProposalHash    string          `json:"proposal_hash,omitempty"`
	DiagnosticsHash string          `json:"diagnostics_hash,omitempty"`
	Attempts        int             `json:"attempts"`
	Usage           Usage           `json:"usage"`
	Proposal        json.RawMessage `json:"proposal,omitempty"`
	Derived         json.RawMessage `json:"derived,omitempty"`
	Diagnostics     json.RawMessage `json:"diagnostics,omitempty"`
	FailureCode     string          `json:"failure_code,omitempty"`
	DecidedBy       string          `json:"decided_by,omitempty"`
	DecidedAt       *time.Time      `json:"decided_at,omitempty"`
	RejectReason    string          `json:"reject_reason,omitempty"`
	ResultRevision  int64           `json:"result_revision,omitempty"`
	ResultHash      string          `json:"result_hash,omitempty"`
}

// The effective status reports a lapsed PENDING row as EXPIRED before a sweep
// stored it.
const effectiveStatus = `CASE WHEN p.status='PENDING' AND p.expires_at<=clock_timestamp() THEN 'EXPIRED' ELSE p.status END`

const summaryColumns = `p.id::text,p.app_id,p.kind,` + effectiveStatus + `,p.requester_id::text,p.created_at,p.expires_at,p.finished_at,p.source_revision,p.draft_hash,p.request_hash,
 coalesce(p.context_hash,''),coalesce(p.proposal_hash,''),coalesce(p.diagnostics_hash,''),coalesce(p.attempts,0),coalesce(p.input_tokens,0),coalesce(p.output_tokens,0),
 coalesce(p.failure_code,CASE WHEN p.status='PENDING' AND p.expires_at<=clock_timestamp() THEN 'EXPIRED' ELSE '' END),
 coalesce(p.decided_by::text,''),p.decided_at,coalesce(p.reject_reason,''),coalesce(p.result_revision,0),coalesce(p.result_hash,'')`

func scanProposal(row interface{ Scan(...any) error }, withBody bool) (Proposal, error) {
	var p Proposal
	var kind, status string
	var finished, decided sql.NullTime
	var body, derived, diagnostics []byte
	dest := []any{&p.ID, &p.AppID, &kind, &status, &p.RequesterID, &p.CreatedAt, &p.ExpiresAt, &finished, &p.SourceRevision, &p.DraftHash, &p.RequestHash,
		&p.ContextHash, &p.ProposalHash, &p.DiagnosticsHash, &p.Attempts, &p.Usage.InputTokens, &p.Usage.OutputTokens, &p.FailureCode,
		&p.DecidedBy, &decided, &p.RejectReason, &p.ResultRevision, &p.ResultHash}
	if withBody {
		dest = append(dest, &body, &derived, &diagnostics)
	}
	if err := row.Scan(dest...); err != nil {
		return Proposal{}, err
	}
	p.Kind, p.Status = ProposalKind(kind), ProposalStatus(status)
	p.CreatedAt, p.ExpiresAt = p.CreatedAt.UTC(), p.ExpiresAt.UTC()
	if finished.Valid {
		t := finished.Time.UTC()
		p.FinishedAt = &t
	}
	if decided.Valid {
		t := decided.Time.UTC()
		p.DecidedAt = &t
	}
	if withBody {
		if len(body) > 0 {
			// The table guarantees it; verifying again means a restored or hand-edited
			// row is never shown as a validated proposal.
			if p.ProposalHash != apiDigest(body) {
				return Proposal{}, ErrCorrupt
			}
			p.Proposal = json.RawMessage(body)
		}
		if len(derived) > 0 {
			p.Derived = json.RawMessage(derived)
		}
		if len(diagnostics) > 0 {
			p.Diagnostics = json.RawMessage(diagnostics)
		}
	}
	return p, nil
}

// Get reads one proposal of the tenant and application. A malformed id is
// ErrProposalNotFound, like an unknown one: ids carry no oracle.
func (Store) Get(ctx context.Context, q postgres.Executor, a tenant.Access, app, id string) (Proposal, error) {
	if !tenant.ValidID(id) || app == "" || a.TenantID() == "" {
		return Proposal{}, ErrProposalNotFound
	}
	p, err := scanProposal(q.QueryRowContext(ctx, `SELECT `+summaryColumns+`,p.body,p.derived,p.diagnostics FROM control.ai_proposals p WHERE p.tenant_id=$1 AND p.app_id=$2 AND p.id=$3`, a.TenantID(), app, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, ErrProposalNotFound
	}
	return p, err
}

// List returns the newest proposals of an application without their bodies.
func (Store) List(ctx context.Context, q postgres.Executor, a tenant.Access, app string, limit int) ([]Proposal, error) {
	if app == "" || a.TenantID() == "" {
		return []Proposal{}, nil
	}
	if limit < 1 || limit > MaxListRows {
		limit = MaxListRows
	}
	rows, err := q.QueryContext(ctx, `SELECT `+summaryColumns+` FROM control.ai_proposals p WHERE p.tenant_id=$1 AND p.app_id=$2 ORDER BY p.created_at DESC,p.id LIMIT $3`, a.TenantID(), app, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Head reads the authoring head (the draft revision) of an application; 0 when the
// application has no draft.
func (Store) Head(ctx context.Context, q postgres.Executor, a tenant.Access, app string) (int64, error) {
	var revision int64
	err := q.QueryRowContext(ctx, `SELECT revision FROM control.metadata_heads WHERE tenant_id=$1 AND id=$2`, a.TenantID(), app).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}

// Ready checks the migration-60 contract without reading or writing a proposal:
// the four ports are definer functions this role may execute, and the three
// tables are forced-RLS, readable and not writable. Missing migrations or
// grants fail closed.
func (Store) Ready(ctx context.Context, db *sql.DB) error {
	var ready bool
	err := db.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=4 AND bool_and(p.prosecdef AND pg_catalog.has_function_privilege(p.oid,'EXECUTE')) FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
   WHERE n.nspname='control' AND p.proname IN('ai_begin','ai_record_attempt','ai_finish','ai_decide'))
 AND (SELECT count(*)=3 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity AND pg_catalog.has_table_privilege(c.oid,'SELECT') AND NOT pg_catalog.has_table_privilege(c.oid,'INSERT,UPDATE,DELETE,TRUNCATE'))
   FROM pg_catalog.pg_class c WHERE c.oid IN('control.ai_proposals'::regclass,'control.ai_invocations'::regclass,'control.ai_usage'::regclass))`).Scan(&ready)
	if err != nil || !ready {
		return errors.New("ai schema unavailable")
	}
	return nil
}
