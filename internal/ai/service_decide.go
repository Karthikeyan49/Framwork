package ai

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

// Human decisions (ADR040 A9). A decision is always a separate, authenticated
// request: nothing here runs as part of a proposal. Apply re-validates against the
// CURRENT draft and never trusts the stored result; confirm calls the M12
// requirement port as the confirming human. Both finish with the ai_decide port in
// the same transaction as the write they record.

// loadDecidable reads the proposal of the route and applies the status gate shared
// by all three decisions.
func loadDecidable(ctx context.Context, q postgres.Executor, a tenant.Access, app, id string, kind ProposalKind) (Proposal, error) {
	p, err := (Store{}).Get(ctx, q, a, app, id)
	if err != nil {
		return Proposal{}, err
	}
	switch p.Status {
	case ProposalReady:
	case ProposalApplied, ProposalConfirmed, ProposalRejected:
		return p, refuse(ErrAlreadyDecided)
	default:
		return p, refuse(ErrNotReady)
	}
	if kind != "" && p.Kind != kind {
		return p, refuse(ErrWrongKind)
	}
	return p, nil
}

// ApplyInput is a parsed apply request plus the platform facts it needs.
type ApplyInput struct {
	App              string
	ProposalID       string
	ExpectedRevision int64
	Acknowledge      []string
	// Compile validates and compiles a document with the operator catalogs.
	Compile CompileFunc
	// Base is the active release's program, nil when none is active; it is the
	// base of the sensitive-aspect derivation when the current draft itself does
	// not compile.
	Base *compiler.Program
}

// ApplyResult is the outcome of a successful apply.
type ApplyResult struct {
	Proposal  Proposal
	Revision  metadata.Version
	Sensitive []string
	// AckHash is the digest of the acknowledged list, for audit.
	AckHash string
}

func apiSortedUnique(in []string) []string {
	out := append([]string{}, in...)
	slices.Sort(out)
	return slices.Compact(out)
}

// AckHash digests an acknowledge list order-insensitively.
func AckHash(list []string) string {
	return apiDigest([]byte(strings.Join(apiSortedUnique(list), "\n")))
}

func issueDiagnostics(err error) []compiler.Diagnostic {
	var invalid *metadata.Invalid
	if errors.As(err, &invalid) {
		out := make([]compiler.Diagnostic, 0, len(invalid.Issues))
		for _, i := range invalid.Issues {
			out = append(out, compiler.Diagnostic{Path: i.Path, Code: i.Code, Message: i.Message})
		}
		return out
	}
	return []compiler.Diagnostic{{Path: "", Code: "invalid_metadata", Message: "the patched draft failed validation"}}
}

// Apply applies a READY patch proposal to the current draft as the authenticated
// human: per-target expected-hash compare-and-set against the current draft, the
// metadata validator, a real compile, the server-derived sensitive aspects (the
// caller must echo them), the existing registry append with an expected-revision
// compare-and-set, and the decide port. The result is a new draft revision;
// sandbox, candidate and approval are untouched.
func (s *Service) Apply(ctx context.Context, q postgres.Executor, a tenant.Access, in ApplyInput) (ApplyResult, error) {
	if !s.Ready() || in.Compile == nil {
		return ApplyResult{}, ErrNotConfigured
	}
	st := Store{}
	p, err := loadDecidable(ctx, q, a, in.App, in.ProposalID, ProposalPatch)
	if err != nil {
		return ApplyResult{}, err
	}
	proposal, err := ParsePatchProposal(p.Proposal)
	if err != nil {
		return ApplyResult{}, &Refusal{Err: ErrPatchInvalid, Diagnostics: []compiler.Diagnostic{{Code: "stored_proposal_unreadable", Message: "the stored proposal no longer parses"}}}
	}
	head, err := st.Head(ctx, q, a, in.App)
	if err != nil {
		return ApplyResult{}, err
	}
	if head == 0 {
		return ApplyResult{}, refuse(ErrNoDraft)
	}
	if head != in.ExpectedRevision {
		return ApplyResult{}, refuse(ErrDraftChanged)
	}
	version, err := s.Registry.Get(ctx, q, a, in.App, head)
	if err != nil {
		return ApplyResult{}, err
	}
	var current metadata.Document
	if err = json.Unmarshal(version.Document, &current); err != nil {
		return ApplyResult{}, errors.New("stored draft is unreadable")
	}
	patched, conflicts, err := s.ApplyFn(current, proposal)
	if err != nil {
		return ApplyResult{}, err
	}
	if len(conflicts) > 0 {
		return ApplyResult{}, &Refusal{Err: ErrApplyConflict, Conflicts: conflicts}
	}
	patchedRaw, err := json.Marshal(patched)
	if err != nil {
		return ApplyResult{}, err
	}
	source, err := s.Registry.Parse(patchedRaw)
	if err != nil {
		var invalid *metadata.Invalid
		if errors.As(err, &invalid) {
			return ApplyResult{}, &Refusal{Err: ErrPatchInvalid, Diagnostics: DiagnosticsOf(issueDiagnostics(err))}
		}
		return ApplyResult{}, err
	}
	program, diagnostics, err := in.Compile(patched)
	if err != nil {
		return ApplyResult{}, err
	}
	if len(diagnostics) > 0 || !program.Valid() {
		return ApplyResult{}, &Refusal{Err: ErrPatchInvalid, Diagnostics: DiagnosticsOf(diagnostics)}
	}
	// The base of the derivation: the current draft when it compiles, else the
	// active release, else nothing (every aspect is then new).
	var base compiler.Program
	if cur, curDiagnostics, cerr := in.Compile(current); cerr == nil && len(curDiagnostics) == 0 && cur.Valid() {
		base = cur
	} else if in.Base != nil && in.Base.Valid() {
		base = *in.Base
	}
	derived, err := s.DeriveFn(base, program)
	if err != nil {
		return ApplyResult{}, err
	}
	sensitive := apiSortedUnique(derived.Sensitive)
	if len(in.Acknowledge) > maxAcknowledge || !slices.Equal(sensitive, sortedCopy(in.Acknowledge)) {
		return ApplyResult{}, &Refusal{Err: ErrAcknowledgeMismatch, Sensitive: sensitive}
	}
	appended, err := s.Registry.Append(ctx, q, a, head, source)
	if errors.Is(err, metadata.ErrConflict) {
		return ApplyResult{}, refuse(ErrDraftChanged)
	}
	if err != nil {
		return ApplyResult{}, err
	}
	ackHash := AckHash(in.Acknowledge)
	record, _ := json.Marshal(struct {
		Decision       string `json:"decision"`
		Proposal       string `json:"proposal"`
		ProposalHash   string `json:"proposal_hash"`
		Expected       int64  `json:"expected_revision"`
		ResultRevision int64  `json:"result_revision"`
		ResultHash     string `json:"result_hash"`
		AckHash        string `json:"ack_hash"`
	}{DecisionApply, p.ID, p.ProposalHash, head, appended.Revision, appended.SourceHash, ackHash})
	if _, err = st.Decide(ctx, q, DecideArgs{App: in.App, Proposal: p.ID, Decision: DecisionApply, ExpectedRevision: &head, ResultRevision: &appended.Revision,
		ResultHash: appended.SourceHash, DecisionHash: apiDigest(record)}); err != nil {
		return ApplyResult{}, err
	}
	stored, err := st.Get(ctx, q, a, in.App, p.ID)
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Proposal: stored, Revision: appended, Sensitive: sensitive, AckHash: ackHash}, nil
}

// sortedCopy sorts without removing duplicates, so a duplicated acknowledgement is
// not equal to the (unique) derived list.
func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	slices.Sort(out)
	return out
}

// Decisions on one requirement draft at confirm time.
const (
	ConfirmResolve  = "RESOLVE"
	ConfirmKeepOpen = "KEEP_OPEN"
	ConfirmReject   = "REJECT"
)

// ConfirmDecision is the human's decision on one draft.
type ConfirmDecision struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
}

// ConfirmInput is a parsed confirm request.
type ConfirmInput struct {
	App             string
	ProposalID      string
	ExpectedVersion int64
	Decisions       []ConfirmDecision
}

// RecordedRequirement is what confirm wrote for one draft.
type RecordedRequirement struct {
	ID                string `json:"id"`
	Category          string `json:"category"`
	Requiredness      string `json:"requiredness"`
	RequirednessBasis string `json:"requiredness_basis"`
	Status            string `json:"status"`
	Provenance        string `json:"provenance"`
}

// ConfirmResult is the outcome of a successful confirm.
type ConfirmResult struct {
	Proposal     Proposal
	Requirements promotion.StoredRequirements
	Recorded     []RecordedRequirement
}

func statusOfDecision(d string) (string, bool) {
	switch d {
	case ConfirmResolve:
		return intent.StatusResolved, true
	case ConfirmKeepOpen:
		return intent.StatusOpenQuestion, true
	case ConfirmReject:
		return intent.StatusRejected, true
	}
	return "", false
}

// Confirm records a READY requirement proposal in the M12 requirement set as the
// confirming human: provenance AI_PROPOSED_HUMAN_CONFIRMED, requiredness from
// requirement-policy-v1 (never the model), status from the human's decision per
// draft. A mandatory requirement that stays OPEN_QUESTION (or is REJECTED) blocks a
// candidate build exactly as in M12; a draft in a category the verifier does not
// understand can never be RESOLVED.
func (s *Service) Confirm(ctx context.Context, q postgres.Executor, a tenant.Access, in ConfirmInput) (ConfirmResult, error) {
	if !s.Ready() {
		return ConfirmResult{}, ErrNotConfigured
	}
	st := Store{}
	p, err := loadDecidable(ctx, q, a, in.App, in.ProposalID, ProposalRequirements)
	if err != nil {
		return ConfirmResult{}, err
	}
	proposal, err := ParseRequirementProposal(p.Proposal)
	if err != nil {
		return ConfirmResult{}, refuse(ErrRequirementsInvalid)
	}
	decisions := make(map[string]string, len(in.Decisions))
	for _, d := range in.Decisions {
		if _, ok := statusOfDecision(d.Decision); !ok {
			return ConfirmResult{}, refuse(ErrBadDecisions)
		}
		if _, dup := decisions[d.ID]; dup {
			return ConfirmResult{}, refuse(ErrBadDecisions)
		}
		decisions[d.ID] = d.Decision
	}
	if len(decisions) != len(proposal.Requirements) {
		return ConfirmResult{}, refuse(ErrBadDecisions)
	}
	for _, d := range proposal.Requirements {
		decision, ok := decisions[d.ID]
		if !ok {
			return ConfirmResult{}, refuse(ErrBadDecisions)
		}
		if decision == ConfirmResolve && !(intent.RequirementV2{Category: d.Category}).Known() {
			return ConfirmResult{}, refuse(ErrCategoryNotResolvable)
		}
	}
	pstore := promotion.Store{}
	set := intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: in.App, Requirements: []intent.RequirementV2{}}
	head := int64(0)
	row, err := pstore.Requirements(ctx, q, a, in.App, 0)
	switch {
	case errors.Is(err, promotion.ErrNotFound):
	case err != nil:
		return ConfirmResult{}, err
	default:
		parsed, _, _, perr := intent.ParseRequirementSet(row.Body)
		if perr != nil {
			return ConfirmResult{}, refuse(ErrRequirementsInvalid)
		}
		set, head = parsed, row.Version
	}
	if head != in.ExpectedVersion {
		return ConfirmResult{}, refuse(ErrRequirementsChanged)
	}
	recorded := make([]RecordedRequirement, 0, len(proposal.Requirements))
	for _, d := range proposal.Requirements {
		if _, exists := set.Find(d.ID); exists {
			return ConfirmResult{}, refuse(ErrRequirementsChanged)
		}
		status, _ := statusOfDecision(decisions[d.ID])
		requiredness, basis := RequirementPolicy(d.Category)
		set.Requirements = append(set.Requirements, intent.RequirementV2{ID: d.ID, Category: d.Category, Requiredness: requiredness, RequirednessBasis: basis,
			Status: status, Provenance: intent.ProvenanceAIConfirmed, Semantics: d.Semantics})
		recorded = append(recorded, RecordedRequirement{ID: d.ID, Category: d.Category, Requiredness: requiredness, RequirednessBasis: basis, Status: status, Provenance: intent.ProvenanceAIConfirmed})
	}
	canonical, err := set.Canonical()
	if err != nil {
		return ConfirmResult{}, refuse(ErrRequirementsInvalid)
	}
	stored, err := pstore.AppendRequirements(ctx, q, a, in.App, head, canonical)
	if err != nil {
		// The set moved between the read and the append, or an id exists already.
		var pg *pgconn.PgError
		if errors.As(err, &pg) && (pg.Code == "55000" || pg.Code == "23505") {
			return ConfirmResult{}, refuse(ErrRequirementsChanged)
		}
		return ConfirmResult{}, err
	}
	record, _ := json.Marshal(struct {
		Decision     string            `json:"decision"`
		Proposal     string            `json:"proposal"`
		ProposalHash string            `json:"proposal_hash"`
		Expected     int64             `json:"expected_version"`
		Result       int64             `json:"result_version"`
		ResultHash   string            `json:"result_hash"`
		Decisions    map[string]string `json:"decisions"`
	}{DecisionConfirm, p.ID, p.ProposalHash, head, stored.Version, stored.Hash, decisions})
	if _, err = st.Decide(ctx, q, DecideArgs{App: in.App, Proposal: p.ID, Decision: DecisionConfirm, ExpectedRevision: &head, ResultRevision: &stored.Version,
		ResultHash: stored.Hash, DecisionHash: apiDigest(record)}); err != nil {
		return ConfirmResult{}, err
	}
	final, err := st.Get(ctx, q, a, in.App, p.ID)
	if err != nil {
		return ConfirmResult{}, err
	}
	return ConfirmResult{Proposal: final, Requirements: stored, Recorded: recorded}, nil
}

// RejectInput is a parsed reject request.
type RejectInput struct {
	App        string
	ProposalID string
	Reason     string
}

// Reject withdraws a READY proposal of either kind. A veto is always safe, so it
// needs no draft or acknowledgement check.
func (s *Service) Reject(ctx context.Context, q postgres.Executor, a tenant.Access, in RejectInput) (Proposal, error) {
	if !slices.Contains(RejectReasons, in.Reason) {
		return Proposal{}, &PortError{Kind: ErrInvalidRequest}
	}
	st := Store{}
	p, err := loadDecidable(ctx, q, a, in.App, in.ProposalID, "")
	if err != nil {
		return Proposal{}, err
	}
	record, _ := json.Marshal(struct {
		Decision     string `json:"decision"`
		Proposal     string `json:"proposal"`
		ProposalHash string `json:"proposal_hash"`
		Reason       string `json:"reason"`
	}{DecisionReject, p.ID, p.ProposalHash, in.Reason})
	if _, err = st.Decide(ctx, q, DecideArgs{App: in.App, Proposal: p.ID, Decision: DecisionReject, RejectReason: in.Reason, DecisionHash: apiDigest(record)}); err != nil {
		return Proposal{}, err
	}
	return st.Get(ctx, q, a, in.App, p.ID)
}
