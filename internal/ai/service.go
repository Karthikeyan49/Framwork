package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// The orchestration of ADR104 amendments A5-A9. A Service turns one authenticated
// human's request into a stored, hashed proposal and a human decision into a draft
// revision or a requirement-set version. It holds no authority of its own: every
// write is a definer port (store.go) or the existing metadata / requirement paths
// called as that human, and the model call (Execute) never runs inside a
// transaction. The HTTP layer (internal/platform/access/ai.go) owns policy, audit
// and the transaction envelope and hands this package the platform facts it needs
// (compile function, active release program, tool summaries, role names).

const (
	// APIWallClockCeiling bounds the provider phase of one request: the contract's
	// DefaultWallClock. The phase holds no transaction; the route extends the
	// connection's write deadline for it (the HTTP server's own is 10 s), and a proxy
	// in front of the API may still impose a smaller limit of its own (ADR104 A5).
	APIWallClockCeiling = DefaultWallClock
	// leaseMargin is added to the wall clock to form a PENDING proposal's lease: it
	// covers the admission and result transactions.
	leaseMargin = 20 * time.Second
	// maxAcknowledge bounds the acknowledge list of an apply request.
	maxAcknowledge = 512
	// maxDiagnosticRunes bounds the message of one stored or returned diagnostic.
	maxDiagnosticRunes = 300
)

// ServiceLimits are the server-side numbers of ADR104 A5. Zero values take the
// defaults; configuration may lower a ceiling and never raise it.
type ServiceLimits struct {
	MaxAttempts       int
	MaxOutputTokens   int
	InFlightPerTenant int
	DailyTokens       int64
	WallClock         time.Duration
}

// Effective applies the defaults and the ceilings of the contract and of the
// API (the wall clock never exceeds APIWallClockCeiling).
func (l ServiceLimits) Effective() ServiceLimits {
	if l.MaxAttempts <= 0 || l.MaxAttempts > MaxAttempts {
		l.MaxAttempts = MaxAttempts
	}
	if l.MaxOutputTokens <= 0 {
		l.MaxOutputTokens = DefaultMaxOutputTokens
	}
	if l.MaxOutputTokens > MaxOutputTokensCeiling {
		l.MaxOutputTokens = MaxOutputTokensCeiling
	}
	if l.InFlightPerTenant <= 0 {
		l.InFlightPerTenant = DefaultInFlightPerTenant
	}
	if l.InFlightPerTenant > MaxInFlightPerTenant {
		l.InFlightPerTenant = MaxInFlightPerTenant
	}
	if l.DailyTokens <= 0 {
		l.DailyTokens = DefaultDailyToken
	}
	if l.DailyTokens > maxDBTokens {
		l.DailyTokens = maxDBTokens
	}
	if l.WallClock <= 0 || l.WallClock > APIWallClockCeiling {
		l.WallClock = APIWallClockCeiling
	}
	return l
}

// LimitsOf derives the service limits from a validated provider configuration.
func LimitsOf(c ProviderConfig) ServiceLimits {
	return ServiceLimits{MaxAttempts: c.MaxAttempts, MaxOutputTokens: c.MaxOutputTokens, InFlightPerTenant: c.InFlightPerTenant, DailyTokens: c.DailyTokenBudget, WallClock: c.WallClock}
}

// Service orchestrates proposals over an injected Gateway. ApplyFn and DeriveFn are
// the pure functions of the pipeline (ApplyProposal and DeriveSensitiveAspects in
// production); tests may inject others.
type Service struct {
	Gateway  Gateway
	Registry *metadata.Registry
	Limits   ServiceLimits
	ApplyFn  ApplyPatch
	DeriveFn DeriveSensitive
	Now      func() time.Time
}

// NewService composes a Service with the production pure functions.
func NewService(g Gateway, r *metadata.Registry, l ServiceLimits) *Service {
	return &Service{Gateway: g, Registry: r, Limits: l, ApplyFn: ApplyProposal, DeriveFn: DeriveSensitiveAspects, Now: time.Now}
}

// Ready reports whether every dependency is present. An unconfigured service
// makes every AI route answer 503 ai_unavailable.
func (s *Service) Ready() bool {
	return s != nil && s.Gateway != nil && s.Registry != nil && s.ApplyFn != nil && s.DeriveFn != nil
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Typed refusals of the human-facing steps. The HTTP layer maps them (ADR104 A7).
var (
	ErrNoDraft               = errors.New("ai: the application has no draft")
	ErrDraftChanged          = errors.New("ai: the draft moved")
	ErrNotReady              = errors.New("ai: the proposal is not ready")
	ErrAlreadyDecided        = errors.New("ai: the proposal was already decided")
	ErrWrongKind             = errors.New("ai: the proposal is of another kind")
	ErrApplyConflict         = errors.New("ai: the proposal conflicts with the current draft")
	ErrAcknowledgeMismatch   = errors.New("ai: the acknowledged list is not the derived sensitive list")
	ErrPatchInvalid          = errors.New("ai: the patched draft is not valid")
	ErrRequirementsChanged   = errors.New("ai: the requirement set changed")
	ErrRequirementsInvalid   = errors.New("ai: the confirmed requirement set is not valid")
	ErrBadDecisions          = errors.New("ai: the decisions do not match the drafts")
	ErrCategoryNotResolvable = errors.New("ai: the category cannot be resolved by the verifier")
	ErrProposalExpired       = errors.New("ai: the proposal expired before its result was stored")
	ErrGatewayFailed         = errors.New("ai: the pipeline failed internally")
)

// Refusal is a typed refusal with the facts a client may see. Err is one of the
// sentinels above; the other fields are set only for the refusals that return them.
type Refusal struct {
	Err         error
	Conflicts   []Conflict
	Diagnostics []compiler.Diagnostic
	Sensitive   []string
}

func (r *Refusal) Error() string { return r.Err.Error() }
func (r *Refusal) Unwrap() error { return r.Err }

func refuse(err error) *Refusal { return &Refusal{Err: err} }

// validRequestText: 1..MaxRequestRunes runes of well-formed UTF-8 text with no
// control character (other than tab, line feed and carriage return) and no
// invisible or bidirectional formatting character that could reorder or hide what
// a reviewer reads. The text is untrusted model input and is never stored (only
// its hash is).
func validRequestText(s string) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" {
		return false
	}
	n := 0
	for _, r := range s {
		n++
		if n > MaxRequestRunes {
			return false
		}
		switch {
		case r == '\t' || r == '\n' || r == '\r':
		case unicode.IsControl(r):
			return false
		case r == 0x061C || r == 0x200E || r == 0x200F || r >= 0x202A && r <= 0x202E || r >= 0x2066 && r <= 0x2069 || r == 0x2028 || r == 0x2029 || r == 0xFEFF || r >= 0xFFF9 && r <= 0xFFFB || r >= 0x2060 && r <= 0x2064 || r == 0x200B:
			return false
		}
	}
	return true
}

// ProposeInput is a parsed creation request of the authenticated human.
type ProposeInput struct {
	Kind             ProposalKind
	App              string
	Request          string
	ExpectedRevision *int64
}

// Admission is the result of the admission transaction: a PENDING proposal and
// the context built for it. It lives only in memory between the three exchanges.
type Admission struct {
	ProposalID    string
	Kind          ProposalKind
	App           string
	RequesterID   string
	DraftRevision int64
	DraftHash     string
	Draft         metadata.Document
	RequestHash   string
	Reserve       int64
	Limits        ServiceLimits
	Context       Context
	request       string
	compile       CompileFunc
	compilerHash  string
}

// PlatformInputs are the facts only the composition root can supply.
type PlatformInputs struct {
	// Compile validates and compiles a document with the operator catalogs.
	Compile CompileFunc
	// BaseProgram is the active release's program, nil when none is active.
	BaseProgram *compiler.Program
	Tools       []ToolSummary
	RoleNames   []string
	// Requirements is the current requirement set, for requirement capture.
	Requirements *intent.RequirementSet
	// CompilerHash identifies the compiler and operator catalogs in the trace.
	CompilerHash string
}

// repairOverheadBytes covers what an attempt after the first adds to the request: the
// repair lead-in and at most MaxDiagnostics capped diagnostics.
const repairOverheadBytes = 26 << 10

// reserveFor is the pessimistic token reservation of one run: every attempt may read
// the whole context, the closed output schema and the repair feedback, and write a
// full answer. The schema is part of every request, so it is part of the reservation.
func reserveFor(l ServiceLimits, draftBytes, requestBytes, schemaBytes int) int64 {
	input := draftBytes + requestBytes + 4096
	if input > MaxContextBytes {
		input = MaxContextBytes
	}
	input += schemaBytes + repairOverheadBytes
	per := int64((input+3)/4) + int64(l.MaxOutputTokens)
	reserve := int64(l.MaxAttempts) * per
	if reserve > maxReserve {
		reserve = maxReserve
	}
	return reserve
}

// Admit is the admission step: it reads the draft head, applies the caller's
// compare-and-set assertion and runs the begin port (authority, in-flight limit,
// daily budget). It writes one PENDING row; it does not call a model.
func (s *Service) Admit(ctx context.Context, q postgres.Executor, a tenant.Access, in ProposeInput) (*Admission, error) {
	if !s.Ready() {
		return nil, ErrNotConfigured
	}
	if (in.Kind != ProposalPatch && in.Kind != ProposalRequirements) || in.App == "" || !validRequestText(in.Request) {
		return nil, &PortError{Kind: ErrInvalidRequest}
	}
	st := Store{}
	head, err := st.Head(ctx, q, a, in.App)
	if err != nil {
		return nil, err
	}
	if head == 0 {
		return nil, refuse(ErrNoDraft)
	}
	if in.ExpectedRevision != nil && *in.ExpectedRevision != head {
		return nil, refuse(ErrDraftChanged)
	}
	version, err := s.Registry.Get(ctx, q, a, in.App, head)
	if err != nil {
		return nil, err
	}
	var doc metadata.Document
	if err = json.Unmarshal(version.Document, &doc); err != nil {
		return nil, fmt.Errorf("stored draft is unreadable")
	}
	limits := s.Limits.Effective()
	schema := RequirementOutputSchema()
	if in.Kind == ProposalPatch {
		if schema, err = PatchOutputSchema(doc.SchemaVersion); err != nil {
			return nil, &PortError{Kind: ErrInvalidRequest}
		}
	}
	reserve := reserveFor(limits, len(version.Document), len(in.Request), len(schema))
	requestHash := apiDigest([]byte(in.Request))
	lease := int((limits.WallClock + leaseMargin + time.Second - 1) / time.Second)
	id, err := st.Begin(ctx, q, a, BeginArgs{App: in.App, Kind: in.Kind, RequestHash: requestHash, DraftRevision: head, DraftHash: version.SourceHash,
		DailyTokens: limits.DailyTokens, Reserve: reserve, InFlight: limits.InFlightPerTenant, LeaseSeconds: lease})
	if err != nil {
		return nil, err
	}
	return &Admission{ProposalID: id, Kind: in.Kind, App: in.App, RequesterID: a.ActorID(), DraftRevision: head, DraftHash: version.SourceHash, Draft: doc,
		RequestHash: requestHash, Reserve: reserve, Limits: limits, request: in.Request}, nil
}

// Prepare builds the semantic context inside the caller's tenant scope. The
// context is metadata only (ADR104 section 4); a bundle over the size cap is
// ErrContextTooLarge, never truncated.
func (s *Service) Prepare(adm *Admission, in PlatformInputs) error {
	if !s.Ready() || adm == nil || in.Compile == nil || !apiHex(in.CompilerHash) {
		return &PortError{Kind: ErrInvalidRequest}
	}
	var draftProgram *compiler.Program
	if program, diagnostics, err := in.Compile(adm.Draft); err == nil && len(diagnostics) == 0 && program.Valid() {
		draftProgram = &program
	}
	task := TaskApplicationPatch
	if adm.Kind == ProposalRequirements {
		task = TaskRequirementCapture
	}
	bundle, err := s.Gateway.BuildContext(ContextInput{Task: task, Request: adm.request, Draft: adm.Draft, DraftRevision: adm.DraftRevision, DraftHash: adm.DraftHash,
		DraftProgram: draftProgram, BaseProgram: in.BaseProgram, Tools: in.Tools, RoleNames: in.RoleNames, Requirements: in.Requirements})
	if err != nil {
		return err
	}
	adm.Context, adm.compile, adm.compilerHash = bundle, in.Compile, in.CompilerHash
	return nil
}

// Outcome is the result of the provider phase: exactly one of Patch and
// Requirements is set unless Err is.
type Outcome struct {
	Kind         ProposalKind
	Patch        *PatchRun
	Requirements *RequirementRun
	Err          error
}

// Execute is the provider phase. It holds no database connection and no
// transaction: it only calls the gateway within the wall clock and a token
// budget equal to what admission reserved. A panic inside the pipeline becomes an
// internal failure that is still recorded.
func (s *Service) Execute(ctx context.Context, adm *Admission) (out Outcome) {
	out.Kind = adm.Kind
	defer func() {
		if recover() != nil {
			out.Patch, out.Requirements, out.Err = nil, nil, ErrGatewayFailed
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, adm.Limits.WallClock)
	defer cancel()
	budget := Budget{MaxAttempts: adm.Limits.MaxAttempts, MaxOutputTokens: adm.Limits.MaxOutputTokens, WallClock: adm.Limits.WallClock, TokensRemaining: adm.Reserve}
	switch adm.Kind {
	case ProposalPatch:
		run, err := s.Gateway.RunPatch(ctx, PatchInput{Context: adm.Context, Draft: adm.Draft, Compile: adm.compile, Budget: budget, Now: s.now})
		out.Patch, out.Err = &run, err
	case ProposalRequirements:
		run, err := s.Gateway.RunRequirements(ctx, RequirementInput{Context: adm.Context, Budget: budget, Now: s.now})
		out.Requirements, out.Err = &run, err
	default:
		out.Err = ErrGatewayFailed
	}
	return out
}

// apiLabel reduces a provider-reported label (untrusted text) to the characters a trace label may carry, [A-Za-z0-9._:/@+-]: each
// run of other characters becomes one underscore between two kept characters and none at either end, so "Beta-3.1 (70B)" stays
// readable as "Beta-3.1_70B" instead of merging into "Beta-3.170B" (independent review F-07). It is idempotent, and the result is at
// most max bytes long; fallback is returned when nothing is left.
func apiLabel(s, fallback string, max int) string {
	var b strings.Builder
	gap := false
	for _, r := range s {
		if !(r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/@+-", r))) {
			gap = true
			continue
		}
		need := 1
		if gap && b.Len() > 0 {
			need = 2
		}
		if b.Len()+need > max {
			break
		}
		if need == 2 {
			b.WriteByte('_')
		}
		gap = false
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return fallback
	}
	return b.String()
}

func apiClamp(v, hi int64) int64 {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

var validOutcomes = map[string]bool{OutcomeOK: true, OutcomeParseError: true, OutcomeSchemaError: true, OutcomeForbidden: true, OutcomeConflict: true,
	OutcomeCompileError: true, OutcomeProviderError: true, OutcomeTimeout: true, OutcomeBudget: true}

// attemptRecord turns the gateway's trace of one attempt into the stored row. The
// attempt number is the position, whatever numbering the gateway used. An outcome
// outside the contract is recorded as a provider error rather than lost.
func attemptRecord(position int, at Attempt, compilerHash string) AttemptRecord {
	outcome := at.Outcome
	if !validOutcomes[outcome] {
		outcome = OutcomeProviderError
	}
	template := at.TemplateVersion
	if template == "" || len(template) > 40 || apiLabel(template, "", 40) != template {
		template = TemplateVersion
	}
	hash := func(h string) string {
		if apiHexOrEmpty(h) {
			return h
		}
		return ""
	}
	return AttemptRecord{Attempt: position, Provider: apiLabel(at.Provider, "unknown", 64), Model: apiLabel(at.Model, "", 128), Template: template,
		ContextHash: hash(at.ContextHash), RequestHash: hash(at.RequestHash), OutputHash: hash(at.OutputHash),
		InputTokens: apiClamp(at.Usage.InputTokens, maxDBTokens), OutputTokens: apiClamp(at.Usage.OutputTokens, maxDBTokens),
		LatencyMS: int(apiClamp(at.Latency.Milliseconds(), 3_600_000)), Outcome: outcome, DiagnosticsHash: hash(at.DiagnosticsHash), CompilerHash: compilerHash}
}

// apiDiagnostics bounds, sanitizes and hashes compiler diagnostics for storage and
// display: compiler-authored, but they may echo author-controlled identifiers.
func apiDiagnostics(in []compiler.Diagnostic) (json.RawMessage, string) {
	if len(in) == 0 {
		return nil, ""
	}
	clean := func(s string, max int) string {
		var b strings.Builder
		n := 0
		for _, r := range s {
			if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				continue
			}
			b.WriteRune(r)
			if n++; n >= max {
				break
			}
		}
		return b.String()
	}
	out := make([]compiler.Diagnostic, 0, min(len(in), MaxDiagnostics))
	for _, d := range in {
		if len(out) >= MaxDiagnostics {
			break
		}
		out = append(out, compiler.Diagnostic{Path: clean(d.Path, 200), Code: clean(d.Code, 64), Message: clean(d.Message, maxDiagnosticRunes)})
	}
	raw, _ := json.Marshal(out)
	return raw, apiDigest(raw)
}

// DiagnosticsOf returns the sanitized diagnostics as the API shows them.
func DiagnosticsOf(in []compiler.Diagnostic) []compiler.Diagnostic {
	raw, _ := apiDiagnostics(in)
	var out []compiler.Diagnostic
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = []compiler.Diagnostic{}
	}
	return out
}

func derivedJSON(d Derived) json.RawMessage {
	if d.Targets == nil {
		d.Targets = []DerivedTarget{}
	}
	if d.Sensitive == nil {
		d.Sensitive = []string{}
	}
	if d.Aspects == nil {
		d.Aspects = []string{}
	}
	raw, _ := json.Marshal(d)
	return raw
}

// RequirementPolicy is how confirm sets requiredness: from requirement-policy-v1
// evaluated at its strictest storage class (the class of a future release is not
// known at confirm time), never from the model. A category with a floor is
// MANDATORY on the policy-floor basis; any other is OPTIONAL by the confirming
// human's decision.
func RequirementPolicy(category string) (requiredness, basis string) {
	if intent.FloorFor(category, "TRANSFORM") == intent.Mandatory {
		return intent.Mandatory, intent.BasisPolicyFloor
	}
	return intent.Optional, intent.BasisOwnerDecision
}

type requirementPreview struct {
	ID                string `json:"id"`
	Category          string `json:"category"`
	Requiredness      string `json:"requiredness"`
	RequirednessBasis string `json:"requiredness_basis"`
	Verifiable        bool   `json:"verifiable"`
}

// requirementsDerived is the server-derived preview of what confirm will record.
func requirementsDerived(p RequirementProposal) json.RawMessage {
	rows := make([]requirementPreview, 0, len(p.Requirements))
	for _, d := range p.Requirements {
		requiredness, basis := RequirementPolicy(d.Category)
		rows = append(rows, requirementPreview{ID: d.ID, Category: d.Category, Requiredness: requiredness, RequirednessBasis: basis, Verifiable: intent.RequirementV2{Category: d.Category}.Known()})
	}
	raw, _ := json.Marshal(struct {
		Requirements  []requirementPreview `json:"requirements"`
		OpenQuestions int                  `json:"open_questions"`
	}{rows, len(p.OpenQuestions)})
	return raw
}

// failureOf is the failure code of a run that produced no proposal: the outcome of
// its last attempt, or the platform's own code.
func failureOf(attempts []Attempt, status RunStatus) string {
	if status == RunBudgetExceeded {
		return OutcomeBudget
	}
	if n := len(attempts); n > 0 && validOutcomes[attempts[n-1].Outcome] && attempts[n-1].Outcome != OutcomeOK {
		return attempts[n-1].Outcome
	}
	return FailureInternal
}

// finishPlan is what a run becomes: the attempts to trace and the finish call.
type finishPlan struct {
	attempts []AttemptRecord
	args     FinishArgs
}

func (s *Service) plan(adm *Admission, out Outcome) finishPlan {
	p := finishPlan{args: FinishArgs{App: adm.App, Proposal: adm.ProposalID, ContextHash: adm.Context.Hash}}
	var trace []Attempt
	var usage Usage
	fail := func(code string) {
		p.args.Status, p.args.FailureCode = ProposalFailed, code
		p.args.Body, p.args.ProposalHash, p.args.Derived = nil, "", nil
	}
	switch {
	case out.Err != nil || out.Patch == nil && out.Requirements == nil:
		code := FailureInternal
		if errors.Is(out.Err, context.DeadlineExceeded) {
			code = OutcomeTimeout
		}
		fail(code)
		if out.Patch != nil {
			trace, usage = out.Patch.Attempts, out.Patch.Usage
		}
		if out.Requirements != nil {
			trace, usage = out.Requirements.Attempts, out.Requirements.Usage
		}
	case out.Patch != nil:
		run := out.Patch
		trace, usage = run.Attempts, run.Usage
		p.args.Status = ProposalFailed
		switch run.Status {
		case RunReady:
			if run.Proposal != nil && len(run.Canonical) > 0 && run.Hash == apiDigest(run.Canonical) && run.Final != nil {
				if _, err := ParsePatchProposal(run.Canonical); err == nil {
					p.args.Status, p.args.Body, p.args.ProposalHash, p.args.Derived = ProposalReady, run.Canonical, run.Hash, derivedJSON(run.Derived)
					break
				}
			}
			fail(FailureInternal) // a READY run that does not verify is never stored as READY
		case RunNeedsHuman:
			p.args.Status = ProposalNeedsHuman
			if run.Proposal != nil && len(run.Canonical) > 0 && run.Hash == apiDigest(run.Canonical) {
				p.args.Body, p.args.ProposalHash, p.args.Derived = run.Canonical, run.Hash, derivedJSON(run.Derived)
			}
			p.args.Diagnostics, p.args.DiagnosticsHash = apiDiagnostics(run.Diagnostics)
		default:
			fail(failureOf(run.Attempts, run.Status))
		}
	default:
		run := out.Requirements
		trace, usage = run.Attempts, run.Usage
		switch run.Status {
		case RunReady:
			if run.Proposal != nil && len(run.Canonical) > 0 && run.Hash == apiDigest(run.Canonical) {
				if parsed, err := ParseRequirementProposal(run.Canonical); err == nil {
					p.args.Status, p.args.Body, p.args.ProposalHash, p.args.Derived = ProposalReady, run.Canonical, run.Hash, requirementsDerived(parsed)
					break
				}
			}
			fail(FailureInternal)
		case RunNeedsHuman:
			p.args.Status = ProposalNeedsHuman
			if run.Proposal != nil && len(run.Canonical) > 0 && run.Hash == apiDigest(run.Canonical) {
				if parsed, err := ParseRequirementProposal(run.Canonical); err == nil {
					p.args.Body, p.args.ProposalHash, p.args.Derived = run.Canonical, run.Hash, requirementsDerived(parsed)
				}
			}
			p.args.Diagnostics, p.args.DiagnosticsHash = apiDiagnostics(run.Diagnostics)
		default:
			fail(failureOf(run.Attempts, run.Status))
		}
	}
	if len(trace) > maxAttemptRows {
		trace = trace[:maxAttemptRows]
	}
	var in, outTokens int64
	for i, at := range trace {
		rec := attemptRecord(i+1, at, adm.compilerHash)
		p.attempts = append(p.attempts, rec)
		in, outTokens = in+rec.InputTokens, outTokens+rec.OutputTokens
	}
	// The run's own total wins when it is larger (a gateway that charges a
	// pessimistic estimate); the port never lowers what was reported.
	p.args.InputTokens, p.args.OutputTokens = max(apiClamp(usage.InputTokens, maxDBTokens), min(in, maxDBTokens)), max(apiClamp(usage.OutputTokens, maxDBTokens), min(outTokens, maxDBTokens))
	p.args.Attempts = len(p.attempts)
	return p
}

// Finish is the result step: it records the trace, stores the validated result
// with a status compare-and-set and returns the stored proposal. A proposal whose
// lease lapsed comes back EXPIRED (its tokens are charged either way).
func (s *Service) Finish(ctx context.Context, q postgres.Executor, a tenant.Access, adm *Admission, out Outcome) (Proposal, error) {
	st := Store{}
	plan := s.plan(adm, out)
	for _, rec := range plan.attempts {
		if err := st.RecordAttempt(ctx, q, adm.App, adm.ProposalID, rec); err != nil {
			return Proposal{}, err
		}
	}
	if _, err := st.Finish(ctx, q, plan.args); err != nil {
		return Proposal{}, err
	}
	return st.Get(ctx, q, a, adm.App, adm.ProposalID)
}

// Attempts is the trace of the run, whichever pipeline produced it.
func (o Outcome) Attempts() []Attempt {
	switch {
	case o.Patch != nil:
		return o.Patch.Attempts
	case o.Requirements != nil:
		return o.Requirements.Attempts
	}
	return nil
}
