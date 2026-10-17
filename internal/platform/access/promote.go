package access

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Approval and promotion routes (ADR039, M13). Like the M12 builder routes they
// exist only where preview exists. They never compile source or IR ("no
// recompile after approval"): each request restores the stored bytes
// (promotion.Store.LoadCandidate / Release), re-evaluates staleness against the
// live pointer, policy roles and operator tool catalog inside the API's
// REPEATABLE READ transaction, pre-checks separation of duties in Go and only
// then calls the SECURITY DEFINER port of migration 49, which re-derives actor,
// tenant, floors, quorum and the pointer compare-and-set from database-stamped
// rows. promote_test.go guards the no-compile rule statically.
const (
	approvePattern  = "POST /v1/tenants/{tenant}/preview/{app}/candidates/{candidate}/approve"
	rejectPattern   = "POST /v1/tenants/{tenant}/preview/{app}/candidates/{candidate}/reject"
	activatePattern = "POST /v1/tenants/{tenant}/preview/{app}/candidates/{candidate}/activate"
	rollbackPattern = "POST /v1/tenants/{tenant}/preview/{app}/releases/rollback"
	historyPattern  = "GET /v1/tenants/{tenant}/preview/{app}/releases"
	activePattern   = "GET /v1/tenants/{tenant}/preview/{app}/releases/active"
)

// ReleaseView is the active release pointer (or the release a rollback moved it
// to). Candidate is present for a 2.0.0 candidate; a 1.0.0 (M6R) release has
// only its identity. Rollback is the class stored in this release's own body
// ("" for 1.0.0); RollbackPlanRequired says whether rolling back FROM it needs
// an acknowledged plan.
type ReleaseView struct {
	ArtifactHash         string         `json:"artifact_hash"`
	Generation           int64          `json:"generation"`
	BundleVersion        string         `json:"bundle_version"`
	IRHash               string         `json:"ir_hash"`
	Rollback             string         `json:"rollback"`
	RollbackPlanRequired bool           `json:"rollback_plan_required"`
	Candidate            *CandidateView `json:"candidate,omitempty"`
}

// HistoryView lists the newest pointer moves of an application, newest first.
type HistoryView struct {
	History []promotion.HistoryEntry `json:"history"`
}

// Request bodies. Nothing authority-bearing (actor, risk, parties, quorum, the
// resulting generation, the rollback class) has a field: readDatasetRequest
// refuses unknown and duplicate keys, so such a field is a 400, never ignored.
type approveRequest struct {
	ArtifactHash string `json:"artifact_hash"`
}
type rejectRequest struct {
	ArtifactHash string `json:"artifact_hash"`
	Reason       string `json:"reason"`
	Note         string `json:"note"`
}
type activateRequest struct {
	ArtifactHash       string  `json:"artifact_hash"`
	ExpectedBase       *string `json:"expected_base"`
	ExpectedGeneration *int64  `json:"expected_generation"`
}
type rollbackPlanRequest struct {
	StorageCompatible       bool   `json:"storage_compatible"`
	ExternalEffectsReviewed bool   `json:"external_effects_reviewed"`
	RecoveryRequired        bool   `json:"recovery_required"`
	PlanHash                string `json:"plan_hash"`
}
type rollbackRequest struct {
	Target             string               `json:"target"`
	ExpectedCurrent    string               `json:"expected_current"`
	ExpectedGeneration *int64               `json:"expected_generation"`
	Plan               *rollbackPlanRequest `json:"plan"`
}

func invalidPromotion() reply {
	return denied(400, "invalid_release_request", "Request body is invalid; authority-bearing fields are derived by the server")
}

func hashMismatch() reply {
	return denied(400, "hash_mismatch", "The body artifact_hash must equal the candidate of the route")
}

// readPromotionRequest is readDatasetRequest (one JSON object, application/json,
// no unknown or duplicate keys, bounded size) plus exact key spelling:
// encoding/json matches keys case-insensitively, so "Artifact_Hash" would
// otherwise alias a field and slip past the duplicate-key check. Every key of
// these bodies is lowercase.
func readPromotionRequest(r *http.Request, dest any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return data.ErrInvalid
	}
	value, err := metadata.DecodeJSON(raw)
	if err != nil || !lowercaseKeys(value) {
		return data.ErrInvalid
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return readDatasetRequest(r, dest)
}

func lowercaseKeys(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key != strings.ToLower(key) || !lowercaseKeys(child) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !lowercaseKeys(child) {
				return false
			}
		}
	}
	return true
}

// authority is what separation of duties is decided on: the database-stamped
// risk level and parties of the candidate index row and the separation of the
// restored bundle. The stricter of the bundle's separation and the database
// floor applies.
type authority struct {
	level      string
	separation risk.Separation
	parties    risk.Parties
}

func (au authority) required() int {
	n := promotion.RequiredApprovals(au.level)
	if au.separation.Approvals > n {
		n = au.separation.Approvals
	}
	return n
}

func (au authority) excluded() []string {
	roles := append([]string{}, promotion.FloorRoles(au.level)...)
	for _, role := range au.separation.Excluded {
		if !containsRole(roles, role) {
			roles = append(roles, role)
		}
	}
	return roles
}

func containsRole(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (au authority) party(role string) string {
	switch role {
	case risk.RoleRequester:
		return au.parties.Requester
	case risk.RoleAcceptanceAuthor:
		return au.parties.AcceptanceAuthor
	case risk.RoleSandboxFreezer:
		return au.parties.SandboxFreezer
	}
	return ""
}

// bars names the first separation role that excludes actor. It fails closed: an
// invalid actor, or an excluded role whose party is unknown, bars everyone.
func (au authority) bars(actor string) (role string, barred bool) {
	if !tenant.ValidID(actor) {
		return "", true
	}
	for _, r := range au.excluded() {
		id := au.party(r)
		if id == "" || strings.EqualFold(id, actor) {
			return r, true
		}
	}
	return "", false
}

// eligible returns the distinct recorded approvers that no separation role bars,
// in recorded order. The activation port additionally drops approvers who are no
// longer active non-viewer members, so this is an upper bound of its count.
func (au authority) eligible(approvals []promotion.Decision) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, d := range approvals {
		key := strings.ToLower(d.ActorID)
		if seen[key] {
			continue
		}
		if _, barred := au.bars(d.ActorID); !barred {
			seen[key] = true
			out = append(out, d.ActorID)
		}
	}
	return out
}

// promotable is a candidate restored and recomputed from its stored bytes in
// this transaction, with its database index row.
type promotable struct {
	candidate promotion.Candidate
	bundle    promotion.BundleV2
	row       promotion.CandidateRow
	authority authority
}

// failedVerification refuses a candidate whose stored bytes do not restore and
// recompute. The reason stays in audit (a closed code); the response says only
// that the artifact cannot be promoted.
func failedVerification(evidence *audit.Entry) *reply {
	evidence.State.VerifierOutcome = "RESTORE_FAILED"
	refusal := denied(409, "candidate_failed_verification", "The stored candidate does not restore and recompute; it cannot be approved or promoted")
	return &refusal
}

// auditHash records a path or body hash only when it is a valid digest, so a
// malformed route can never make the audit entry itself invalid.
func auditHash(v string) string {
	if promotion.ValidHash(v) {
		return v
	}
	return ""
}

// loadPromotable restores the candidate of the route through LoadCandidate
// (never Restore directly) and requires its database index row to agree with the
// restored body; either failure is candidate_failed_verification.
func (a API) loadPromotable(ctx context.Context, q postgres.Executor, access tenant.Access, app, hash string, evidence *audit.Entry) (promotable, *reply, error) {
	c, row, err := (promotion.Store{}).LoadCandidate(ctx, q, access, app, hash, compiler.SupportedIRVersions())
	if err != nil && !errors.Is(err, promotion.ErrNotFound) && errors.Is(err, promotion.ErrGate) {
		// LoadCandidate also refuses a 1.0.0 (M6R) bundle, which has no
		// candidate row and can never be promoted: that is not found, not a
		// failed verification.
		if legacy, lerr := (promotion.Store{}).Release(ctx, q, access, app, hash, compiler.SupportedIRVersions()); lerr == nil && legacy.Header().Version != promotion.BundleVersion2 {
			return promotable{}, nil, promotion.ErrNotFound
		}
		return promotable{}, failedVerification(evidence), nil
	}
	if err != nil {
		return promotable{}, nil, err
	}
	b, ok := c.V2()
	if !ok || c.Hash() != hash || row.ArtifactHash != hash || row.AppID != app || b.AppID != app || b.TenantID != access.TenantID() ||
		row.RequesterID != b.RequesterID || row.AcceptanceAuthor != b.Acceptance.AuthorID || row.SandboxFrozenBy != b.Sandbox.FrozenBy ||
		row.RiskLevel != b.Risk.Level || row.HighRisk != b.HighRisk || row.BaseRelease != b.BaseRelease || row.BaseGeneration != b.BaseGeneration ||
		row.IRHash != b.IR.Hash || row.AcceptanceID != b.Acceptance.ID || row.SandboxDatasetID != b.Sandbox.DatasetID || row.RequirementsHash != b.Requirements.Hash {
		return promotable{}, failedVerification(evidence), nil
	}
	evidence.State.RiskLevel, evidence.State.IRHash = row.RiskLevel, row.IRHash
	return promotable{candidate: c, bundle: b, row: row, authority: authority{level: row.RiskLevel, separation: b.Risk.Separation,
		parties: risk.Parties{Requester: row.RequesterID, AcceptanceAuthor: row.AcceptanceAuthor, SandboxFreezer: row.SandboxFrozenBy}}}, nil, nil
}

// staleReply is 409 candidate_stale with the sorted reasons.
func staleReply(r *http.Request, reasons []promotion.StaleReason) reply {
	return reply{status: 409, body: struct {
		httpserver.ErrorResponse
		StaleReasons []promotion.StaleReason `json:"stale_reasons"`
	}{httpserver.ErrorResponse{Code: "candidate_stale", Message: "The candidate no longer matches the live release, policy, tools or evidence; rebuild it", RequestID: httpserver.RequestID(r.Context())}, reasons}}
}

// refuseUndecidable refuses a rejected or stale candidate before any port call.
// The database re-checks both (rejection, bound base) under its own locks.
func (a API) refuseUndecidable(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, p promotable, evidence *audit.Entry) (*reply, error) {
	rejections, err := (promotion.Store{}).Rejections(ctx, q, access, app, p.candidate.Hash())
	if err != nil {
		return nil, err
	}
	if len(rejections) > 0 {
		refusal := denied(409, "candidate_rejected", "The candidate was rejected; a rejection is permanent for this exact artifact, so change an input and rebuild")
		return &refusal, nil
	}
	reasons, err := a.staleness(ctx, q, access, app, p.bundle)
	if err != nil {
		return nil, err
	}
	if len(reasons) > 0 {
		for _, reason := range reasons {
			evidence.State.StaleReasons = append(evidence.State.StaleReasons, string(reason))
		}
		refusal := staleReply(r, reasons)
		return &refusal, nil
	}
	return nil, nil
}

// lifecycle fills the derived ADR039 fields of a candidate view. Nothing here is
// stored: status is recomputed from the recorded decisions and the pointer.
func (a API) lifecycle(ctx context.Context, q postgres.Executor, access tenant.Access, app string, view *CandidateView, au authority) error {
	store := promotion.Store{}
	hash := view.ArtifactHash
	approvals, err := store.Approvals(ctx, q, access, app, hash)
	if err != nil {
		return err
	}
	rejections, err := store.Rejections(ctx, q, access, app, hash)
	if err != nil {
		return err
	}
	current, _, err := store.ActiveBase(ctx, q, access, app)
	if err != nil {
		return err
	}
	ever, err := store.WasActivated(ctx, q, access, app, hash)
	if err != nil {
		return err
	}
	active := current == hash
	view.Status = promotion.DeriveStatus(len(rejections), len(au.eligible(approvals)), au.required(), active, ever)
	view.Approvals, view.ApprovalsRequired, view.Rejections, view.Active = approvals, au.required(), rejections, active
	return nil
}

// authorityOf derives the authority of a candidate from its own restored body,
// for views of candidates that were not loaded through their index row.
func authorityOf(b promotion.BundleV2) authority {
	return authority{level: b.Risk.Level, separation: b.Risk.Separation,
		parties: risk.Parties{Requester: b.RequesterID, AcceptanceAuthor: b.Acceptance.AuthorID, SandboxFreezer: b.Sandbox.FrozenBy}}
}

func (a API) promotableView(ctx context.Context, q postgres.Executor, access tenant.Access, app string, p promotable) (CandidateView, error) {
	view := viewOf(p.candidate, true)
	return view, a.lifecycle(ctx, q, access, app, &view, p.authority)
}

// loadRelease restores any stored release: a 2.0.0 candidate through
// loadPromotable (with its index row), a 1.0.0 (M6R) bundle through
// Store.Release. A missing bundle is ErrNotFound; one that does not restore is
// candidate_failed_verification.
func (a API) loadRelease(ctx context.Context, q postgres.Executor, access tenant.Access, app, hash string, evidence *audit.Entry) (promotion.Candidate, *promotable, *reply, error) {
	p, refusal, err := a.loadPromotable(ctx, q, access, app, hash, evidence)
	if refusal != nil {
		return promotion.Candidate{}, nil, refusal, nil
	}
	if err == nil {
		return p.candidate, &p, nil, nil
	}
	if !errors.Is(err, promotion.ErrNotFound) {
		return promotion.Candidate{}, nil, nil, err
	}
	c, err := (promotion.Store{}).Release(ctx, q, access, app, hash, compiler.SupportedIRVersions())
	if err != nil && !errors.Is(err, promotion.ErrNotFound) && errors.Is(err, promotion.ErrGate) {
		return promotion.Candidate{}, nil, failedVerification(evidence), nil
	}
	if err != nil {
		return promotion.Candidate{}, nil, nil, err
	}
	if c.Header().Version == promotion.BundleVersion2 {
		// A 2.0.0 body without its index row was never staged by the port.
		return promotion.Candidate{}, nil, failedVerification(evidence), nil
	}
	return c, nil, nil, nil
}

func (a API) releaseView(ctx context.Context, q postgres.Executor, access tenant.Access, app string, c promotion.Candidate, p *promotable, generation int64) (ReleaseView, error) {
	h := c.Header()
	view := ReleaseView{ArtifactHash: c.Hash(), Generation: generation, BundleVersion: h.Version, IRHash: h.IRHash, RollbackPlanRequired: true}
	if p != nil {
		view.Rollback = p.bundle.Risk.Rollback
		view.RollbackPlanRequired = promotion.RollbackClassNeedsPlan(view.Rollback)
		candidate, err := a.promotableView(ctx, q, access, app, *p)
		if err != nil {
			return ReleaseView{}, err
		}
		view.Candidate = &candidate
	}
	return view, nil
}

// rollbackClass reads the rollback class from the CURRENT release's stored body.
// A 1.0.0 body, a body that no longer restores or a missing class all yield ""
// (plan required); only a database failure is an error.
func (a API) rollbackClass(ctx context.Context, q postgres.Executor, access tenant.Access, app, hash string) (string, error) {
	c, err := (promotion.Store{}).Release(ctx, q, access, app, hash, compiler.SupportedIRVersions())
	if errors.Is(err, promotion.ErrNotFound) || err != nil && errors.Is(err, promotion.ErrGate) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if b, ok := c.V2(); ok {
		switch b.Risk.Rollback {
		case risk.RollbackPointerSafe, risk.RollbackDataNotReversible, risk.RollbackExternalEffects:
			return b.Risk.Rollback, nil
		}
	}
	return "", nil
}

func separationRefused(role string) reply {
	if role == "" {
		return denied(403, "separation_of_duties", "Separation of duties: you may not approve this candidate")
	}
	return denied(403, "separation_of_duties", "Separation of duties: the "+role+" of this candidate may not approve it")
}

// ------------------------------------------------------------------ handlers

func (a API) approveCandidate(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	hash := r.PathValue("candidate")
	var body approveRequest
	if readPromotionRequest(r, &body) != nil {
		return invalidPromotion(), nil
	}
	if body.ArtifactHash != hash {
		return hashMismatch(), nil
	}
	evidence.State.ArtifactHash = auditHash(hash)
	p, refusal, err := a.loadPromotable(ctx, q, access, app, hash, evidence)
	if refusal != nil || err != nil {
		return derefReply(refusal), err
	}
	if refusal, err = a.refuseUndecidable(ctx, q, r, access, app, p, evidence); refusal != nil || err != nil {
		return derefReply(refusal), err
	}
	actor := access.ActorID()
	role, barred := p.authority.bars(actor)
	if barred || risk.CheckSeparation(risk.Separation{Approvals: 1, Excluded: p.authority.excluded()}, p.authority.parties, []string{actor}) != nil {
		return separationRefused(role), nil
	}
	store := promotion.Store{}
	before, err := store.Approvals(ctx, q, access, app, hash)
	if err != nil {
		return reply{}, err
	}
	replay := false
	for _, d := range before {
		replay = replay || strings.EqualFold(d.ActorID, actor)
	}
	count, err := store.ApproveRelease(ctx, q, access, app, hash)
	if err != nil {
		return reply{}, err
	}
	view, err := a.promotableView(ctx, q, access, app, p)
	if err != nil {
		return reply{}, err
	}
	status := 201
	evidence.Action = "release.approved"
	if replay {
		status, evidence.Action = 200, "release.approval_replayed"
	}
	evidence.State.ApprovalCount, evidence.State.ApprovalsRequired, evidence.State.ReleaseGeneration = int64(count), int64(p.authority.required()), p.bundle.BaseGeneration
	return reply{status: status, body: view}, nil
}

func (a API) rejectCandidate(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	hash := r.PathValue("candidate")
	var body rejectRequest
	if readPromotionRequest(r, &body) != nil || !promotion.RejectReason(body.Reason).Valid() || !promotion.ValidNote(body.Note) {
		return invalidPromotion(), nil
	}
	if body.ArtifactHash != hash {
		return hashMismatch(), nil
	}
	evidence.State.ArtifactHash = auditHash(hash)
	p, refusal, err := a.loadPromotable(ctx, q, access, app, hash, evidence)
	if refusal != nil || err != nil {
		return derefReply(refusal), err
	}
	// A veto is always safe (ADR039 section 3): no staleness or separation check.
	recorded, err := (promotion.Store{}).RejectRelease(ctx, q, access, app, hash, promotion.RejectReason(body.Reason), body.Note)
	if err != nil {
		return reply{}, err
	}
	view, err := a.promotableView(ctx, q, access, app, p)
	if err != nil {
		return reply{}, err
	}
	status := 201
	evidence.Action = "release.rejected"
	if !recorded {
		status, evidence.Action = 200, "release.rejection_replayed"
	}
	// The free-text note is stored for reviewers and never copied into audit.
	evidence.State.RejectReason, evidence.State.ReleaseGeneration = body.Reason, p.bundle.BaseGeneration
	return reply{status: status, body: view}, nil
}

func (a API) activateCandidate(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	hash := r.PathValue("candidate")
	var body activateRequest
	if readPromotionRequest(r, &body) != nil || body.ExpectedBase == nil || body.ExpectedGeneration == nil {
		return invalidPromotion(), nil
	}
	base, generation := *body.ExpectedBase, *body.ExpectedGeneration
	if generation < 0 || generation >= promotion.MaxGeneration || base == "" && generation != 0 || base != "" && (!promotion.ValidHash(base) || generation < 1) {
		return invalidPromotion(), nil
	}
	if body.ArtifactHash != hash {
		return hashMismatch(), nil
	}
	evidence.State.ArtifactHash, evidence.State.PreviousHash = auditHash(hash), auditHash(base)
	p, refusal, err := a.loadPromotable(ctx, q, access, app, hash, evidence)
	if refusal != nil || err != nil {
		return derefReply(refusal), err
	}
	if refusal, err = a.refuseUndecidable(ctx, q, r, access, app, p, evidence); refusal != nil || err != nil {
		return derefReply(refusal), err
	}
	// The candidate is fresh, so its bound base is the live pointer; the caller
	// must have seen exactly that pointer.
	if base != p.bundle.BaseRelease || generation != p.bundle.BaseGeneration {
		return denied(409, "base_changed", "The expected base is not the active release this candidate was built on; reload it"), nil
	}
	store := promotion.Store{}
	approvals, err := store.Approvals(ctx, q, access, app, hash)
	if err != nil {
		return reply{}, err
	}
	eligible := p.authority.eligible(approvals)
	required := p.authority.required()
	evidence.State.ApprovalCount, evidence.State.ApprovalsRequired = int64(len(eligible)), int64(required)
	if risk.CheckSeparation(risk.Separation{Approvals: required, Excluded: p.authority.excluded()}, p.authority.parties, eligible) != nil {
		return denied(409, "approval_quorum_not_met", "The candidate does not have the independent approvals its risk level requires"), nil
	}
	next, err := store.ActivateRelease(ctx, q, access, app, hash, base, generation)
	if err != nil {
		return reply{}, err
	}
	view, err := a.releaseView(ctx, q, access, app, p.candidate, &p, next)
	if err != nil {
		return reply{}, err
	}
	evidence.Action = "release.activated"
	evidence.State.ReleaseGeneration, evidence.State.StorageClass = next, p.bundle.Storage.Class
	return reply{status: 200, body: view}, nil
}

func (a API) rollbackRelease(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	var body rollbackRequest
	if readPromotionRequest(r, &body) != nil || body.ExpectedGeneration == nil || !promotion.ValidHash(body.Target) || !promotion.ValidHash(body.ExpectedCurrent) ||
		*body.ExpectedGeneration < 1 || *body.ExpectedGeneration >= promotion.MaxGeneration || body.Plan != nil && !promotion.ValidHash(body.Plan.PlanHash) {
		return invalidPromotion(), nil
	}
	generation := *body.ExpectedGeneration
	evidence.State.ArtifactHash, evidence.State.PreviousHash = body.Target, body.ExpectedCurrent
	plan := promotion.RollbackPlan{}
	if body.Plan != nil {
		plan = promotion.RollbackPlan{StorageCompatible: body.Plan.StorageCompatible, ExternalEffectsReviewed: body.Plan.ExternalEffectsReviewed, RecoveryRequired: body.Plan.RecoveryRequired, PlanHash: body.Plan.PlanHash}
		evidence.State.PlanHash = plan.PlanHash
		if plan.RecoveryRequired {
			return denied(409, "recovery_required", "A rollback that requires data recovery is not a pointer move; use the assessed recovery procedure"), nil
		}
		// A supplied plan must be acknowledged, even where none is required.
		if !plan.Acknowledged() {
			return denied(409, "rollback_plan_required", "The rollback plan must confirm storage compatibility and reviewed external effects"), nil
		}
	}
	store := promotion.Store{}
	current, currentGeneration, err := store.ActiveBase(ctx, q, access, app)
	if err != nil {
		return reply{}, err
	}
	if current == "" {
		return denied(409, "no_active_release", "No release is active, so there is nothing to roll back"), nil
	}
	if current != body.ExpectedCurrent || currentGeneration != generation {
		return denied(409, "release_changed", "The active release changed; reload it before retrying"), nil
	}
	if body.Target == current {
		return denied(409, "rollback_target_is_current", "The rollback target is the active release"), nil
	}
	class, err := a.rollbackClass(ctx, q, access, app, current)
	if err != nil {
		return reply{}, err
	}
	evidence.State.RollbackClass = class
	target, tp, refusal, err := a.loadRelease(ctx, q, access, app, body.Target, evidence)
	if refusal != nil || err != nil {
		return derefReply(refusal), err
	}
	activated, err := store.WasActivated(ctx, q, access, app, body.Target)
	if err != nil {
		return reply{}, err
	}
	if !activated {
		return denied(409, "rollback_target_not_released", "Only a release that was activated before is a rollback target"), nil
	}
	rejections, err := store.Rejections(ctx, q, access, app, body.Target)
	if err != nil {
		return reply{}, err
	}
	if len(rejections) > 0 {
		return denied(409, "rollback_target_rejected", "A rejected release is not a rollback target"), nil
	}
	if promotion.RollbackClassNeedsPlan(class) && body.Plan == nil {
		return denied(409, "rollback_plan_required", "Rolling back this release requires an acknowledged rollback plan"), nil
	}
	next, err := store.RollbackRelease(ctx, q, access, app, body.Target, current, generation, plan)
	if err != nil {
		return reply{}, err
	}
	view, err := a.releaseView(ctx, q, access, app, target, tp, next)
	if err != nil {
		return reply{}, err
	}
	evidence.Action = "release.rolled_back"
	evidence.State.ReleaseGeneration, evidence.State.RiskLevel, evidence.State.StorageClass = next, "", ""
	if tp != nil {
		evidence.State.RiskLevel, evidence.State.StorageClass = tp.row.RiskLevel, tp.bundle.Storage.Class
	}
	return reply{status: 200, body: view}, nil
}

func (a API) releaseHistory(ctx context.Context, q postgres.Executor, access tenant.Access, app string) (reply, error) {
	entries, err := (promotion.Store{}).History(ctx, q, access, app, promotion.MaxHistory)
	if err != nil {
		return reply{}, err
	}
	return reply{status: 200, body: HistoryView{History: entries}}, nil
}

func (a API) activeRelease(ctx context.Context, q postgres.Executor, access tenant.Access, app string, evidence *audit.Entry) (reply, error) {
	hash, generation, err := (promotion.Store{}).ActiveBase(ctx, q, access, app)
	if err != nil {
		return reply{}, err
	}
	if hash == "" {
		return denied(404, "not_found", "No release is active"), nil
	}
	evidence.State.ArtifactHash, evidence.State.ReleaseGeneration = auditHash(hash), generation
	c, p, refusal, err := a.loadRelease(ctx, q, access, app, hash, evidence)
	if refusal != nil || err != nil {
		return derefReply(refusal), err
	}
	view, err := a.releaseView(ctx, q, access, app, c, p, generation)
	if err != nil {
		return reply{}, err
	}
	return reply{status: 200, body: view}, nil
}

func derefReply(r *reply) reply {
	if r == nil {
		return reply{}
	}
	return *r
}
