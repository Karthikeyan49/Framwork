package access

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/verification"
	"github.com/jackc/pgx/v5/pgconn"
)

// Release builder routes (ADR038, M12) and approval/promotion routes (ADR039,
// M13, promote.go). They exist only where preview exists. The POST allowlist
// must accept exactly the POST patterns below (promote_test.go compares them).
var releasePOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(requirements|acceptance-specs|candidates|candidates/[0-9a-f]{64}/(approve|reject|activate)|releases/rollback)$`)

// Exact registered patterns and their actions; a suffix match could capture
// unrelated routes.
var releasePatterns = map[string]string{
	"POST /v1/tenants/{tenant}/preview/{app}/requirements":           "requirements.decide",
	"GET /v1/tenants/{tenant}/preview/{app}/requirements":            "requirements.read",
	"GET /v1/tenants/{tenant}/preview/{app}/requirements/{version}":  "requirements.read",
	"POST /v1/tenants/{tenant}/preview/{app}/acceptance-specs":       "acceptance.author",
	"GET /v1/tenants/{tenant}/preview/{app}/acceptance-specs/{spec}": "acceptance.read",
	"POST /v1/tenants/{tenant}/preview/{app}/candidates":             "candidates.build",
	"GET /v1/tenants/{tenant}/preview/{app}/candidates":              "candidates.read",
	"GET /v1/tenants/{tenant}/preview/{app}/candidates/{candidate}":  "candidates.read",
	approvePattern:  "releases.approve",
	rejectPattern:   "releases.approve",
	activatePattern: "releases.activate",
	rollbackPattern: "releases.rollback",
	historyPattern:  "releases.read",
	activePattern:   "releases.read",
}

// releaseGate records that the API already holds an ALLOW decision for the
// action (policy.Decide ran before this handler) and the stage port re-checks
// membership; M6R's Gate interface is satisfied, not bypassed.
type releaseGate struct{}

func (releaseGate) AuthorizeRelease(string, string, string, string) bool { return true }

// releaseRetryAfter is what a request that timed out is told to wait.
const releaseRetryAfter = 2 * time.Second

// busyReply is the typed refusal of a request that would start a cold recomputation
// beyond the bound (ADR038 amendment 10): 429 when the tenant's bound is reached, 503
// when the process-wide one is, both with Retry-After. Nothing was computed or written.
func busyReply(b *promotion.BusyError) reply {
	status := 503
	if b.Scope == "tenant" {
		status = 429
	}
	r := denied(status, "release_busy", "Release artifacts are being verified; retry after the indicated delay")
	r.retryAfter = b.RetryAfter
	return r
}

// refusalCode is the closed audit code of a refusal that has no other evidence.
func refusalCode(ctx context.Context, err error) string {
	var busy *promotion.BusyError
	switch {
	case errors.As(err, &busy):
		return "RECOMPUTE_BUSY_" + strings.ToUpper(busy.Scope)
	case ctx.Err() != nil:
		return "TIMEOUT"
	case errors.Is(err, policy.ErrDefinition):
		return "ROLE_HEADS_EXCEEDED"
	}
	return ""
}

func releaseReply(ctx context.Context, err error) (reply, error) {
	if err == nil {
		return reply{}, nil
	}
	status, code := 0, ""
	var pg *pgconn.PgError
	var violation *intent.Violation
	var busy *promotion.BusyError
	switch {
	case errors.As(err, &busy):
		return reply{}, &requestRejection{response: busyReply(busy)}
	case ctx.Err() != nil:
		// The request transaction ran out of its budget (or the client left) while the
		// handler was working. Nothing was committed; say so, typed and audited, rather
		// than as an unexplained outage.
		timeout := denied(503, "release_timeout", "The request did not finish in time; nothing was changed, retry after the indicated delay")
		timeout.retryAfter = releaseRetryAfter
		return reply{}, &requestRejection{response: timeout}
	case errors.As(err, &violation):
		return reply{}, &requestRejection{response: denied(400, "invalid_release_artifact", "Release artifact rejected: "+violation.Code)}
	case errors.Is(err, policy.ErrDefinition):
		// The tenant's role snapshot is above policy.MaxRoleHeads, so no policy bundle can
		// be derived (review F10): typed and audited, not an unexplained outage.
		return reply{}, &requestRejection{response: denied(409, "release_policy_snapshot_too_large", "The tenant has more role definitions than a release policy snapshot can carry; release candidates cannot be built or decided until an operator reduces them")}
	case errors.Is(err, promotion.ErrNotFound), errors.Is(err, metadata.ErrNotFound), errors.Is(err, data.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, promotion.ErrRecoveryRequired):
		status, code = 409, "recovery_required"
	case errors.Is(err, promotion.ErrGate), errors.Is(err, data.ErrInvalid):
		status, code = 400, "invalid_release_request"
	case errors.Is(err, promotion.ErrForbidden):
		// The approve port names the excluded separation role (a closed code)
		// in DETAIL; a response never names another actor's id.
		if errors.As(err, &pg) && (pg.Detail == risk.RoleRequester || pg.Detail == risk.RoleAcceptanceAuthor || pg.Detail == risk.RoleSandboxFreezer) {
			return reply{}, &requestRejection{response: separationRefused(pg.Detail)}
		}
		status, code = 403, "forbidden"
	case errors.Is(err, promotion.ErrConflict):
		status, code = 409, "release_conflict"
	case errors.As(err, &pg):
		switch pg.Code {
		case "22023":
			status, code = 400, "invalid_release_request"
		case "42501":
			status, code = 403, "forbidden"
		case "55000", "23505", "23503", "40001", "40P01", "55P03":
			status, code = 409, "release_conflict"
		case "P0002":
			status, code = 404, "not_found"
		}
	}
	if status != 0 {
		return reply{}, &requestRejection{response: denied(status, code, "Release request rejected; reload the artifacts before retrying"), cause: err}
	}
	return reply{}, err
}

// releaseIdempotentPOST are the release POSTs whose repetition with the same body is a
// replay by construction: a candidate is addressed by the hash of its bytes, an acceptance
// spec by the hash of its body and its author. Requirement appends are compare-and-set on
// the head and are deliberately not repeated: a second identical append is a real conflict.
var releaseIdempotentPOST = map[string]bool{
	"POST /v1/tenants/{tenant}/preview/{app}/candidates":       true,
	"POST /v1/tenants/{tenant}/preview/{app}/acceptance-specs": true,
}

// releaseKnownAbort is postgres.KnownAbort plus the unique violation of a concurrent
// identical insert into the release tables: the repeated attempt sees the committed row and
// answers as a replay. Anything else, and a third failure, stays a 409.
func releaseKnownAbort(err error) bool {
	if postgres.KnownAbort(err) {
		return true
	}
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.SchemaName == "control" &&
		(pg.TableName == "acceptance_specs" || pg.TableName == "release_bundles" || pg.TableName == "release_candidates")
}

func (a API) release(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, evidence *audit.Entry) (reply, error) {
	if !a.PreviewEnabled || a.Registry == nil {
		return denied(404, "not_found", "Release builder is unavailable"), nil
	}
	app := r.PathValue("app")
	evidence.State.MetadataApp = app
	// Cold recomputations of this request count against its tenant's bound.
	ctx = promotion.WithTenant(ctx, access.TenantID())
	var result reply
	var err error
	switch r.Pattern {
	case "POST /v1/tenants/{tenant}/preview/{app}/requirements":
		result, err = a.postRequirements(ctx, q, r, access, app, evidence)
	case "GET /v1/tenants/{tenant}/preview/{app}/requirements", "GET /v1/tenants/{tenant}/preview/{app}/requirements/{version}":
		result, err = a.getRequirements(ctx, q, r, access, app, evidence)
	case "POST /v1/tenants/{tenant}/preview/{app}/acceptance-specs":
		result, err = a.postAcceptance(ctx, q, r, access, app, evidence)
	case "GET /v1/tenants/{tenant}/preview/{app}/acceptance-specs/{spec}":
		var s promotion.StoredAcceptance
		if s, err = a.store().Acceptance(ctx, q, access, app, r.PathValue("spec")); err == nil {
			evidence.State.AcceptanceID = s.ID
			result = reply{status: 200, body: s}
		}
	case "POST /v1/tenants/{tenant}/preview/{app}/candidates":
		result, err = a.postCandidate(ctx, q, r, access, app, evidence)
	case "GET /v1/tenants/{tenant}/preview/{app}/candidates":
		var rows []promotion.CandidateRow
		if rows, err = a.store().Candidates(ctx, q, access, app); err == nil {
			result = reply{status: 200, body: struct {
				Candidates []promotion.CandidateRow `json:"candidates"`
			}{rows}}
		}
	case "GET /v1/tenants/{tenant}/preview/{app}/candidates/{candidate}":
		result, err = a.getCandidate(ctx, q, r, access, app, evidence)
	case approvePattern:
		result, err = a.approveCandidate(ctx, q, r, access, app, evidence)
	case rejectPattern:
		result, err = a.rejectCandidate(ctx, q, r, access, app, evidence)
	case activatePattern:
		result, err = a.activateCandidate(ctx, q, r, access, app, evidence)
	case rollbackPattern:
		result, err = a.rollbackRelease(ctx, q, r, access, app, evidence)
	case historyPattern:
		result, err = a.releaseHistory(ctx, q, access, app)
	case activePattern:
		result, err = a.activeRelease(ctx, q, access, app, evidence)
	default:
		return denied(404, "not_found", "Unknown release route"), nil
	}
	if err != nil {
		refused, rejection := releaseReply(ctx, err)
		var rejected *requestRejection
		if errors.As(rejection, &rejected) && evidence.State.RefusalCode == "" {
			evidence.State.RefusalCode = refusalCode(ctx, err)
		}
		return refused, rejection
	}
	return result, nil
}

func (a API) postRequirements(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	var body struct {
		ExpectedVersion int64           `json:"expected_version"`
		Requirements    json.RawMessage `json:"requirements"`
	}
	if readDatasetRequest(r, &body) != nil || body.ExpectedVersion < 0 || body.ExpectedVersion >= 1000000 {
		return denied(400, "invalid_release_request", "Request body is invalid"), nil
	}
	set, canonical, hash, err := intent.ParseRequirementSet(body.Requirements)
	if err != nil {
		return reply{}, err
	}
	if set.AppID != app {
		return denied(400, "app_mismatch", "The requirement set must name the application of the route"), nil
	}
	stored, err := a.store().AppendRequirements(ctx, q, access, app, body.ExpectedVersion, canonical)
	if err != nil {
		return reply{}, err
	}
	evidence.State.RequirementsVersion, evidence.State.ManifestHash = stored.Version, hash
	return reply{status: 201, body: stored}, nil
}

func (a API) getRequirements(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	version := int64(0)
	if v := r.PathValue("version"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 || n >= 1000000 {
			return denied(400, "invalid_release_request", "Invalid requirement version"), nil
		}
		version = n
	}
	stored, err := a.store().Requirements(ctx, q, access, app, version)
	if err != nil {
		return reply{}, err
	}
	evidence.State.RequirementsVersion, evidence.State.ManifestHash = stored.Version, stored.Hash
	return reply{status: 200, body: stored}, nil
}

func (a API) postAcceptance(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	var body struct {
		RequirementsVersion int64           `json:"requirements_version"`
		Acceptance          json.RawMessage `json:"acceptance"`
	}
	if readDatasetRequest(r, &body) != nil || body.RequirementsVersion < 1 || body.RequirementsVersion >= 1000000 {
		return denied(400, "invalid_release_request", "Request body is invalid"), nil
	}
	row, err := a.store().Requirements(ctx, q, access, app, body.RequirementsVersion)
	if err != nil {
		return reply{}, err
	}
	set, _, setHash, err := intent.ParseRequirementSet(row.Body)
	if err != nil || setHash != row.Hash {
		return denied(409, "release_conflict", "The stored requirement set cannot be read"), nil
	}
	spec, canonical, _, err := intent.ParseAcceptanceSpec(body.Acceptance)
	if err != nil {
		return reply{}, err
	}
	// The authoring gate: binding, mandatory coverage and agreement with the oracle.
	if spec.RequirementsHash != row.Hash {
		return denied(400, "invalid_release_artifact", "Release artifact rejected: HASH_MISMATCH"), nil
	}
	if err = intent.ValidateAcceptance(spec, set); err != nil {
		return reply{}, err
	}
	stored, err := a.store().AuthorAcceptance(ctx, q, access, app, row.Hash, canonical)
	if err != nil {
		return reply{}, err
	}
	evidence.State.RequirementsVersion, evidence.State.AcceptanceID, evidence.State.ManifestHash = row.Version, stored.ID, stored.Hash
	return reply{status: 201, body: stored}, nil
}

type candidateRequest struct {
	Revision           int64                     `json:"revision"`
	RequirementVersion int64                     `json:"requirement_set_version"`
	AcceptanceSpecID   string                    `json:"acceptance_spec_id"`
	SandboxID          string                    `json:"sandbox_id"`
	EvolutionIntent    *compiler.EvolutionIntent `json:"evolution_intent"`
	ExpectedBase       *string                   `json:"expected_base"`
}

// CandidateView is the API shape of a staged candidate. Every field is derived
// from the verified stored bytes; none is echoed from a request.
type CandidateView struct {
	ArtifactHash        string                  `json:"artifact_hash"`
	Staged              bool                    `json:"staged"`
	AppID               string                  `json:"app_id"`
	RequesterID         string                  `json:"requester_id"`
	HighRisk            bool                    `json:"high_risk"`
	RiskLevel           string                  `json:"risk_level"`
	RiskReasons         []string                `json:"risk_reasons"`
	Separation          risk.Separation         `json:"separation"`
	Rollback            string                  `json:"rollback"`
	StorageClass        string                  `json:"storage_class"`
	BaseRelease         string                  `json:"base_release"`
	BaseGeneration      int64                   `json:"base_generation"`
	SourceRevision      int64                   `json:"source_revision"`
	RequirementsVersion int64                   `json:"requirements_version"`
	AcceptanceID        string                  `json:"acceptance_id"`
	AcceptanceAuthor    string                  `json:"acceptance_author"`
	SandboxID           string                  `json:"sandbox_id"`
	SandboxFrozenBy     string                  `json:"sandbox_frozen_by"`
	VerificationReport  json.RawMessage         `json:"verification_report,omitempty"`
	Stale               *bool                   `json:"stale,omitempty"`
	StaleReasons        []promotion.StaleReason `json:"stale_reasons,omitempty"`
	// Lifecycle (ADR039), derived from the recorded decisions and the pointer on
	// every read and never stored. ApprovalsRequired is the stricter of the
	// bundle's separation and the database quorum floor.
	Status            promotion.Status             `json:"status"`
	Approvals         []promotion.Decision         `json:"approvals"`
	ApprovalsRequired int                          `json:"approvals_required"`
	Rejections        []promotion.ReleaseRejection `json:"rejections"`
	Active            bool                         `json:"active"`
}

func viewOf(c promotion.Candidate, staged bool) CandidateView {
	b, _ := c.V2()
	return CandidateView{ArtifactHash: c.Hash(), Staged: staged, AppID: b.AppID, RequesterID: b.RequesterID, HighRisk: b.HighRisk, RiskLevel: b.Risk.Level, RiskReasons: b.Risk.Reasons,
		Separation: b.Risk.Separation, Rollback: b.Risk.Rollback, StorageClass: b.Storage.Class, BaseRelease: b.BaseRelease, BaseGeneration: b.BaseGeneration, SourceRevision: b.Source.Revision,
		RequirementsVersion: b.Requirements.Version, AcceptanceID: b.Acceptance.ID, AcceptanceAuthor: b.Acceptance.AuthorID, SandboxID: b.Sandbox.DatasetID, SandboxFrozenBy: b.Sandbox.FrozenBy}
}

// activeBase reads the active release and restores its program; both are empty
// when nothing is active.
func (a API) activeBase(ctx context.Context, q postgres.Executor, access tenant.Access, app string) (string, int64, compiler.Program, error) {
	store := a.store()
	hash, generation, err := store.ActiveBase(ctx, q, access, app)
	if err != nil || hash == "" {
		return "", 0, compiler.Program{}, err
	}
	_, program, err := store.ReleaseProgram(ctx, q, access, app, hash, compiler.SupportedIRVersions())
	if err != nil {
		return "", 0, compiler.Program{}, err
	}
	return hash, generation, program, nil
}

func (a API) policyBundle(ctx context.Context, q postgres.Executor, access tenant.Access) (promotion.PolicyBundleInput, error) {
	heads, err := (policy.RoleStore{}).Snapshot(ctx, q, access)
	if err != nil {
		return promotion.PolicyBundleInput{}, err
	}
	in := promotion.PolicyBundleInput{EngineVersion: policy.Version, DefinitionVersion: policy.DefinitionVersion, ActionCatalogHash: policy.ActionCatalogHash()}
	for _, h := range heads {
		in.Roles = append(in.Roles, promotion.PolicyRole{Name: h.Name, Version: h.Version, BodyHash: h.Hash, Active: h.Active})
	}
	return in, nil
}

func (a API) frozenSandbox(ctx context.Context, q postgres.Executor, access tenant.Access, app, id string) (promotion.FrozenSandbox, error) {
	store := data.DatasetStore{}
	d, err := store.Get(ctx, q, access, app, id)
	if err != nil {
		return promotion.FrozenSandbox{}, err
	}
	if d.Environment != "sandbox" || d.Sandbox == nil || d.Sandbox.State != "FROZEN" {
		return promotion.FrozenSandbox{}, promotion.ErrGate
	}
	e, err := store.Evidence(ctx, q, access, app, id)
	if err != nil {
		return promotion.FrozenSandbox{}, err
	}
	return promotion.FrozenSandbox{DatasetID: id, EvidenceHash: e.Hash, FrozenBy: e.FrozenBy, Evidence: string(e.Body)}, nil
}

func (a API) postCandidate(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	var body candidateRequest
	if readDatasetRequest(r, &body) != nil || body.Revision < 1 || body.Revision > 1e9 || body.RequirementVersion < 1 || !tenant.ValidID(body.AcceptanceSpecID) || !tenant.ValidID(body.SandboxID) {
		return denied(400, "invalid_release_request", "Request body is invalid; authority-bearing fields are derived by the server"), nil
	}
	store := a.store()
	version, err := a.Registry.Get(ctx, q, access, app, body.Revision)
	if err != nil {
		return reply{}, err
	}
	source, err := a.Registry.Parse(version.Document)
	if err != nil {
		return reply{}, err
	}
	metadataEvidence(ctx, evidence, version)
	catalog := a.catalog()
	program, diagnostics := compiler.Compile(source, catalog)
	if !program.Valid() {
		return reply{status: 422, body: struct {
			Diagnostics []compiler.Diagnostic `json:"diagnostics"`
		}{diagnostics}}, nil
	}
	evidence.State.IRHash = program.Hash()
	requirements, err := store.Requirements(ctx, q, access, app, body.RequirementVersion)
	if err != nil {
		return reply{}, err
	}
	acceptance, err := store.Acceptance(ctx, q, access, app, body.AcceptanceSpecID)
	if err != nil {
		return reply{}, err
	}
	sandbox, err := a.frozenSandbox(ctx, q, access, app, body.SandboxID)
	if err != nil {
		return reply{}, err
	}
	baseHash, baseGeneration, baseProgram, err := a.activeBase(ctx, q, access, app)
	if err != nil {
		return reply{}, err
	}
	// The caller may only assert the base it saw; the active release decides.
	if body.ExpectedBase != nil && *body.ExpectedBase != baseHash {
		return denied(409, "base_changed", "The active release changed; reload it and rebuild the candidate"), nil
	}
	policyInput, err := a.policyBundle(ctx, q, access)
	if err != nil {
		return reply{}, err
	}
	in := promotion.BuildInput{TenantID: access.TenantID(), AppID: app, RequesterID: access.ActorID(), Source: source, SourceRevision: body.Revision, Target: program,
		Requirements: requirements, Acceptance: acceptance, Sandbox: sandbox, Tools: catalog.ToolCatalog, Policy: policyInput}
	if body.EvolutionIntent != nil {
		in.Intent = *body.EvolutionIntent
	}
	if baseHash != "" {
		in.Base = &promotion.BaseRelease{ArtifactHash: baseHash, Generation: baseGeneration, Program: baseProgram}
	}
	candidate, err := store.Build(ctx, in)
	var rejected *promotion.Rejection
	if errors.As(err, &rejected) {
		// The audit row says why the build was refused: the closed reason, the verifier
		// report hash and the hash of the risk assessment, never the free-text findings.
		evidence.State.ArtifactHash = ""
		evidence.State.RefusalCode = rejected.Reason
		if rejected.Report != nil {
			evidence.State.VerifierOutcome = rejected.Report.Outcome
			evidence.State.ArtifactHash = rejected.Report.Hash()
		}
		if rejected.Assessment != nil {
			evidence.State.RiskLevel = rejected.Assessment.Level.String()
			evidence.State.AssessmentHash = assessmentHash(*rejected.Assessment)
		}
		return reply{status: 422, body: struct {
			Reason     string               `json:"reason"`
			Report     *verification.Report `json:"verification_report,omitempty"`
			Assessment *risk.Assessment     `json:"risk_assessment,omitempty"`
		}{rejected.Reason, rejected.Report, rejected.Assessment}}, nil
	}
	if err != nil {
		return reply{}, err
	}
	staged, err := store.Stage(ctx, q, access, candidate, releaseGate{})
	if err != nil {
		return reply{}, err
	}
	b, _ := candidate.V2()
	evidence.Action = "release.candidate_replayed"
	if staged {
		evidence.Action = "release.candidate_staged"
	}
	evidence.State.ArtifactHash, evidence.State.RiskLevel, evidence.State.VerifierOutcome = candidate.Hash(), b.Risk.Level, b.Verification.Outcome
	evidence.State.RequirementsVersion, evidence.State.AcceptanceID, evidence.State.DatasetID = b.Requirements.Version, b.Acceptance.ID, b.Sandbox.DatasetID
	status := 201
	if !staged {
		status = 200
	}
	// A replayed build shows the recorded lifecycle of the same hash, so an
	// identical rebuild of a rejected candidate is visibly still REJECTED.
	view := viewOf(candidate, staged)
	if err = a.lifecycle(ctx, q, access, app, &view, authorityOf(b)); err != nil {
		return reply{}, err
	}
	return reply{status: status, body: view}, nil
}

// assessmentHash is the digest of the canonical encoding of a risk assessment, which
// binds the level, the reasons, the facts hash and the policy that produced them.
func assessmentHash(a risk.Assessment) string {
	raw, err := json.Marshal(a)
	if err != nil {
		return ""
	}
	return promotion.Hash(raw)
}

func (a API) getCandidate(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	// loadPromotable maps a LoadCandidate ErrGate (the stored bytes do not
	// recompute, including against the candidate's own base release) to 409
	// candidate_failed_verification; details are audited, never returned.
	p, refusal, err := a.loadPromotable(ctx, q, access, app, r.PathValue("candidate"), evidence)
	if refusal != nil || err != nil {
		return derefReply(refusal), err
	}
	view, err := a.promotableView(ctx, q, access, app, p)
	if err != nil {
		return reply{}, err
	}
	view.VerificationReport = p.bundle.Verification.Report
	reasons, err := a.staleness(ctx, q, access, app, p.bundle)
	if err != nil {
		return reply{}, err
	}
	stale := len(reasons) > 0
	view.Stale, view.StaleReasons = &stale, reasons
	evidence.State.ArtifactHash = p.candidate.Hash()
	return reply{status: 200, body: view}, nil
}

// staleness evaluates the candidate against the live world. It is derived on
// every read and never stored.
func (a API) staleness(ctx context.Context, q postgres.Executor, access tenant.Access, app string, b promotion.BundleV2) ([]promotion.StaleReason, error) {
	baseHash, baseGeneration, baseProgram, err := a.activeBase(ctx, q, access, app)
	if err != nil {
		return nil, err
	}
	baseSchema := ""
	if baseProgram.Valid() {
		if _, baseSchema, err = compiler.Storage(baseProgram); err != nil {
			return nil, err
		}
	}
	policyInput, err := a.policyBundle(ctx, q, access)
	if err != nil {
		return nil, err
	}
	policyHash, err := promotion.PolicyBundleHash(policyInput)
	if err != nil {
		return nil, err
	}
	catalog := a.catalog().ToolCatalog
	status := func(p promotion.ToolPin) promotion.ToolPinStatus {
		if def, ok := catalog.Lookup(p.Ref, p.Version, p.Hash); ok {
			if def.AllowsTenant(access.TenantID()) {
				return promotion.ToolOK
			}
			return promotion.ToolTenantDenied
		}
		if _, ok := catalog.Current(p.Ref, p.Version); ok {
			return promotion.ToolChanged
		}
		return promotion.ToolRemoved
	}
	evidenceState := promotion.EvidenceState{}
	if d, derr := (data.DatasetStore{}).Get(ctx, q, access, app, b.Sandbox.DatasetID); derr == nil && d.Sandbox != nil && d.Sandbox.State == "FROZEN" {
		if e, eerr := (data.DatasetStore{}).Evidence(ctx, q, access, app, b.Sandbox.DatasetID); eerr == nil {
			evidenceState = promotion.EvidenceState{Exists: true, Frozen: true, Hash: e.Hash}
		}
	} else if derr != nil && !errors.Is(derr, data.ErrNotFound) {
		return nil, derr
	}
	level, err := risk.ParseLevel(b.Risk.Level)
	if err != nil {
		return nil, err
	}
	bound := promotion.BoundState{BaseRelease: b.BaseRelease, BaseGeneration: b.BaseGeneration, BaseSchemaHash: b.Dependencies.BaseSchemaHash, PolicyBundleHash: b.Dependencies.PolicyBundleHash, ToolPins: b.Dependencies.ToolPins,
		IRVersion: b.IR.Version, RiskPolicyVersion: b.Risk.PolicyVersion, RiskLevel: level, RiskFacts: b.Risk.Facts, EvidenceHash: b.Sandbox.EvidenceHash, ComponentsHash: b.RuntimeContract.ComponentsHash}
	_, componentsHash := promotion.RuntimeComponents()
	current := promotion.CurrentState{ActiveRelease: baseHash, ActiveGeneration: baseGeneration, BaseSchemaHash: baseSchema, PolicyBundleHash: policyHash, ToolStatus: status,
		SupportedIR: compiler.SupportedIRVersions(), Volume: promotion.DefaultVolume(baseHash == ""), Evidence: evidenceState, ComponentsHash: componentsHash}
	return promotion.Staleness(bound, current), nil
}
