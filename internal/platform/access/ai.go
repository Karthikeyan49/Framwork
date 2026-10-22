package access

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/trace"
)

// AI builder routes (ADR104 amendments A5-A11, M14). They exist only where preview
// exists. A model holds no authority: the requesting human is the actor of record in
// every exchange, a proposal is data, and apply / confirm / reject are separate
// authenticated requests. The POST allowlist (aiPOST) must accept exactly the POST
// patterns below (ai_test.go compares them).

const aiRoot = "/v1/tenants/{tenant}/preview/{app}/ai"

// aiPatterns are the exact registered patterns and the policy action of each.
var aiPatterns = map[string]string{
	"POST " + aiRoot + "/patch-proposals":        "ai.propose",
	"POST " + aiRoot + "/requirement-proposals":  "ai.propose",
	"GET " + aiRoot + "/proposals":               "ai.read",
	"GET " + aiRoot + "/proposals/{id}":          "ai.read",
	"POST " + aiRoot + "/proposals/{id}/apply":   "ai.apply",
	"POST " + aiRoot + "/proposals/{id}/confirm": "ai.apply",
	"POST " + aiRoot + "/proposals/{id}/reject":  "ai.apply",
}

// aiCreatePOST are the routes that create a proposal. Their first exchange writes the tenant's usage row (admission reserves
// tokens and an in-flight slot), which concurrent creations of one tenant contest under repeatable read: the losers abort with a
// serialization failure that leaves nothing behind, so serve repeats the exchange (independent review M14R-03).
var aiCreatePOST = map[string]bool{
	"POST " + aiRoot + "/patch-proposals":       true,
	"POST " + aiRoot + "/requirement-proposals": true,
}

// The first exchange of a creation is tried up to aiAdmissionAttempts times within aiAdmissionBudget: with n creations of one
// tenant racing for its usage row each round lets one of them through, and the pause between tries spreads the rest.
const (
	aiAdmissionAttempts = 5
	aiAdmissionBudget   = 4 * time.Second
)

var aiPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/ai/(patch-proposals|requirement-proposals|proposals/[0-9a-f-]{36}/(apply|confirm|reject))$`)

// AIConfig is the optional AI builder wiring (ADR104 A11). The zero value means
// "not configured": every AI route then answers 503 ai_unavailable and nothing else
// changes. It lives here so that only this file imports the ai package.
type AIConfig struct {
	Service *ai.Service
}

func (a API) registerAI(mux *http.ServeMux) {
	for pattern := range aiPatterns {
		mux.HandleFunc(pattern, a.serve)
	}
}

// Body caps of ADR104 A5.
const (
	aiCreateBody  = 32 << 10
	aiApplyBody   = 128 << 10
	aiConfirmBody = 64 << 10
	aiRejectBody  = 4 << 10
)

func aiDeny(status int, code, message string) reply { return denied(status, code, message) }

// aiBody reads one strict JSON object: application/json, within limit bytes, no
// duplicate or Unicode case-folded key, no trailing data, no depth bomb, only the
// listed lowercase keys (the standard decoder would also match a differently cased
// spelling, so the key set is checked on the decoded tree first) and every required
// key present.
func aiBody(r *http.Request, limit int, dest any, required []string, optional ...string) (reply, bool) {
	invalid := aiDeny(400, "invalid_ai_request", "Request body is invalid")
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		return aiDeny(415, "unsupported_media_type", "Use application/json"), false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
	if err != nil {
		return invalid, false
	}
	if len(raw) > limit {
		return aiDeny(413, "body_too_large", "Request body is too large"), false
	}
	tree, err := metadata.DecodeJSONBounded(raw, limit)
	if err != nil {
		return invalid, false
	}
	object, ok := tree.(map[string]any)
	if !ok {
		return invalid, false
	}
	for key := range object {
		if !slices.Contains(required, key) && !slices.Contains(optional, key) {
			return invalid, false
		}
	}
	for _, key := range required {
		if _, present := object[key]; !present {
			return invalid, false
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dest) != nil || d.More() {
		return invalid, false
	}
	return reply{}, true
}

func aiDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// aiEvidence copies the hashes, codes and counts of a stored proposal into the audit
// state. Never a prompt, a request text or model output.
func aiEvidence(e *audit.Entry, p ai.Proposal) {
	s := &e.State
	s.AIProposalID, s.AIKind, s.AIStatus, s.AIFailure = p.ID, string(p.Kind), string(p.Status), p.FailureCode
	s.AIRequestHash, s.AIContextHash, s.AIProposalHash, s.AIDiagnosticsHash = p.RequestHash, p.ContextHash, p.ProposalHash, p.DiagnosticsHash
	s.AIAttempts, s.AIInputTokens, s.AIOutputTokens, s.AISourceRevision = int64(p.Attempts), p.Usage.InputTokens, p.Usage.OutputTokens, p.SourceRevision
	s.MetadataApp = p.AppID
}

// aiRejection wraps a refusal so that the whole business transaction rolls back and
// the refusal is audited in a fresh one.
func aiRejection(status int, code, message string) (reply, error) {
	return reply{}, &requestRejection{response: denied(status, code, message)}
}

type aiErrorBody struct {
	httpserver.ErrorResponse
	ProposalID  string                `json:"proposal_id,omitempty"`
	Conflicts   []ai.Conflict         `json:"conflicts,omitempty"`
	Diagnostics []compiler.Diagnostic `json:"diagnostics,omitempty"`
	Sensitive   []string              `json:"sensitive,omitempty"`
}

// aiReply maps the typed refusals of the ai package (ADR104 A7) to HTTP. A refusal
// is returned as a requestRejection so its transaction rolls back; an error that is
// none of them is returned unchanged and becomes 503 service_unavailable.
func aiReply(r *http.Request, err error) (reply, error) {
	failure := func(status int, code, message string, mutate func(*aiErrorBody)) (reply, error) {
		body := aiErrorBody{ErrorResponse: httpserver.ErrorResponse{Code: code, Message: message, RequestID: httpserver.RequestID(r.Context())}}
		if mutate != nil {
			mutate(&body)
		}
		return reply{}, &requestRejection{response: reply{status: status, body: body}}
	}
	var refusal *ai.Refusal
	var port *ai.PortError
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &refusal):
		switch {
		case errors.Is(refusal.Err, ai.ErrNoDraft), errors.Is(refusal.Err, ai.ErrProposalNotFound):
			return aiRejection(404, "not_found", "Not found")
		case errors.Is(refusal.Err, ai.ErrDraftChanged):
			return aiRejection(409, "draft_changed", "The draft changed; reload it and ask again")
		case errors.Is(refusal.Err, ai.ErrNotReady):
			return aiRejection(409, "proposal_not_ready", "The proposal is not ready")
		case errors.Is(refusal.Err, ai.ErrWrongKind):
			return aiRejection(409, "proposal_wrong_kind", "The proposal is of another kind")
		case errors.Is(refusal.Err, ai.ErrAlreadyDecided):
			return aiRejection(409, "proposal_decided", "The proposal was already decided")
		case errors.Is(refusal.Err, ai.ErrApplyConflict):
			return failure(409, "proposal_conflict", "The proposal conflicts with the current draft", func(b *aiErrorBody) { b.Conflicts = refusal.Conflicts })
		case errors.Is(refusal.Err, ai.ErrAcknowledgeMismatch):
			return failure(409, "acknowledge_mismatch", "Acknowledge exactly the sensitive aspects of this change", func(b *aiErrorBody) { b.Sensitive = refusal.Sensitive })
		case errors.Is(refusal.Err, ai.ErrPatchInvalid), errors.Is(refusal.Err, ai.ErrRequirementsInvalid):
			return failure(422, "proposal_invalid", "The change is not valid against the current state", func(b *aiErrorBody) { b.Diagnostics = refusal.Diagnostics })
		case errors.Is(refusal.Err, ai.ErrRequirementsChanged):
			return aiRejection(409, "requirements_changed", "The requirement set changed; reload it and confirm again")
		case errors.Is(refusal.Err, ai.ErrBadDecisions):
			return aiRejection(400, "invalid_ai_request", "Decide every requirement exactly once")
		case errors.Is(refusal.Err, ai.ErrCategoryNotResolvable):
			return aiRejection(422, "category_not_resolvable", "This requirement category cannot be resolved by the verifier")
		case errors.Is(refusal.Err, ai.ErrProposalExpired):
			return aiRejection(409, "proposal_expired", "The proposal expired")
		}
		return reply{}, err
	case errors.As(err, &port):
		switch {
		case errors.Is(port.Kind, ai.ErrInvalidRequest):
			return aiRejection(400, "invalid_ai_request", "Request is invalid")
		case errors.Is(port.Kind, ai.ErrNotPermitted):
			return aiRejection(403, "forbidden", "Access is denied")
		case errors.Is(port.Kind, ai.ErrProposalNotFound):
			return aiRejection(404, "not_found", "Not found")
		case errors.Is(port.Kind, ai.ErrBusy):
			return aiRejection(429, "ai_busy", "Too many AI requests are in flight; retry shortly")
		case errors.Is(port.Kind, ai.ErrBudget):
			return aiRejection(429, "ai_budget_exhausted", "The daily AI budget is exhausted")
		case errors.Is(port.Kind, ai.ErrProposalConflict):
			switch port.Detail {
			case "draft_changed":
				return aiRejection(409, "draft_changed", "The draft changed; reload it and ask again")
			case "not_ready":
				return aiRejection(409, "proposal_not_ready", "The proposal is not ready")
			case "decided":
				return aiRejection(409, "proposal_decided", "The proposal was already decided")
			case "not_pending", "expired":
				return aiRejection(409, "proposal_expired", "The proposal expired")
			}
			return aiRejection(409, "ai_conflict", "A concurrent change prevented this request; reload and retry")
		}
		return reply{}, err
	case errors.Is(err, ai.ErrNotConfigured):
		return aiRejection(503, "ai_unavailable", "The AI builder is not configured")
	case errors.Is(err, ai.ErrContextTooLarge):
		return aiRejection(422, "ai_context_too_large", "The application is too large to describe to the model; nothing was stored")
	case errors.Is(err, ai.ErrInvalidInput):
		return aiRejection(400, "invalid_ai_request", "Request is invalid")
	case errors.Is(err, ai.ErrProposalNotFound), errors.Is(err, metadata.ErrNotFound), errors.Is(err, promotion.ErrNotFound):
		return aiRejection(404, "not_found", "Not found")
	case errors.Is(err, metadata.ErrConflict):
		return aiRejection(409, "draft_changed", "The draft changed; reload it and ask again")
	case errors.As(err, &pg):
		switch pg.Code {
		case "42501":
			return aiRejection(403, "forbidden", "Access is denied")
		case "55000", "23505", "23503", "40001", "40P01", "55P03":
			return aiRejection(409, "ai_conflict", "A concurrent change prevented this request; reload and retry")
		}
	}
	return reply{}, err
}

// aiReplyRetrying is aiReply for the exchanges that are repeated: a failure that a second try resolves (a serialization failure of
// the tenant's usage row, a deadlock) is returned unchanged, so that the retry loop sees it, instead of becoming a 409 at once.
func aiReplyRetrying(r *http.Request, err error) (reply, error) {
	if postgres.KnownAbort(err) {
		return reply{}, err
	}
	return aiReply(r, err)
}

// ai is the standard-transaction handler of the seven routes. Creation continues in
// reply.next after this transaction commits (ADR104 A8).
func (a API) ai(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, evidence *audit.Entry) (reply, error) {
	app := r.PathValue("app")
	evidence.State.MetadataApp = app
	svc := a.AI.Service
	if !a.PreviewEnabled || a.Registry == nil || !svc.Ready() {
		// Before any body is read or any row is written.
		return aiDeny(503, "ai_unavailable", "The AI builder is not configured"), nil
	}
	root := "/v1/tenants/{tenant}/preview/{app}/ai"
	switch r.Pattern {
	case "POST " + root + "/patch-proposals":
		return a.aiPropose(ctx, q, r, u, access, app, ai.ProposalPatch, evidence)
	case "POST " + root + "/requirement-proposals":
		return a.aiPropose(ctx, q, r, u, access, app, ai.ProposalRequirements, evidence)
	case "GET " + root + "/proposals":
		list, err := (ai.Store{}).List(ctx, q, access, app, ai.MaxListItems)
		if err != nil {
			return reply{}, err
		}
		return reply{status: 200, body: struct {
			Proposals []ai.Proposal `json:"proposals"`
		}{list}}, nil
	}
	id := r.PathValue("id")
	if !tenant.ValidID(id) {
		return aiDeny(404, "not_found", "Not found"), nil
	}
	evidence.State.AIProposalID = id
	switch r.Pattern {
	case "GET " + root + "/proposals/{id}":
		p, err := (ai.Store{}).Get(ctx, q, access, app, id)
		if err != nil {
			return aiReply(r, err)
		}
		aiEvidence(evidence, p)
		return reply{status: 200, body: p}, nil
	case "POST " + root + "/proposals/{id}/apply":
		return a.aiApply(ctx, q, r, u, access, app, id, evidence)
	case "POST " + root + "/proposals/{id}/confirm":
		return a.aiConfirm(ctx, q, r, u, access, app, id, evidence)
	case "POST " + root + "/proposals/{id}/reject":
		return a.aiReject(ctx, q, r, access, app, id, evidence)
	}
	return aiDeny(404, "not_found", "Unknown AI route"), nil
}

// aiCompose evaluates the capability the underlying write needs in addition to the
// AI action, so delegating ai.apply alone never bypasses it (ADR104 A6). A denial
// records its own decision id in the audit evidence.
func (a API) aiCompose(ctx context.Context, u identity.User, access tenant.Access, action, resourceType, app string, evidence *audit.Entry) error {
	d := enforce(a.Policy.Decide(ctx, policy.Subject{User: u, Access: access}, action, policy.Resource{TenantID: access.TenantID(), Type: resourceType, AppID: app},
		policy.Context{RequestID: evidence.RequestID, TraceID: evidence.TraceID}))
	if d.Allow {
		return nil
	}
	evidence.DecisionID, evidence.PolicyVersion, evidence.Allow = d.ID, d.Version, false
	return &requestRejection{response: denied(403, "forbidden", "Access is denied")}
}

// compilerHash identifies the compiler and operator catalogs in the attempt trace.
func (a API) compilerHash() string {
	c := a.catalog()
	return aiDigest(strings.Join([]string{compiler.Version, c.Version, c.ToolCatalog.Hash(), policy.ActionCatalogHash()}, "\n"))
}

// aiCompile is the CompileFunc of the running server: the metadata validator and the
// compiler with the operator catalogs. A document the validator refuses yields its
// issues as diagnostics, not an error.
func (a API) aiCompile() ai.CompileFunc {
	catalog := a.catalog()
	return func(doc metadata.Document) (compiler.Program, []compiler.Diagnostic, error) {
		raw, err := json.Marshal(doc)
		if err != nil {
			return compiler.Program{}, nil, err
		}
		source, err := a.Registry.Parse(raw)
		var invalid *metadata.Invalid
		if errors.As(err, &invalid) {
			diagnostics := make([]compiler.Diagnostic, 0, len(invalid.Issues))
			for _, i := range invalid.Issues {
				diagnostics = append(diagnostics, compiler.Diagnostic{Path: i.Path, Code: i.Code, Message: i.Message})
			}
			return compiler.Program{}, diagnostics, nil
		}
		if err != nil {
			return compiler.Program{}, nil, err
		}
		program, diagnostics := compiler.Compile(source, catalog)
		return program, diagnostics, nil
	}
}

// aiPlatform gathers the facts only the composition root knows, inside the caller's
// tenant scope: the active release program, the tool summaries, role names and the
// requirement set.
func (a API) aiPlatform(ctx context.Context, q postgres.Executor, access tenant.Access, app string) (ai.PlatformInputs, error) {
	in := ai.PlatformInputs{Compile: a.aiCompile(), CompilerHash: a.compilerHash()}
	_, _, base, err := a.activeBase(ctx, q, access, app)
	if err != nil {
		return in, err
	}
	if base.Valid() {
		in.BaseProgram = &base
	}
	for _, t := range a.catalog().ToolCatalog.Summaries(access.TenantID()) {
		in.Tools = append(in.Tools, ai.ToolSummary{Ref: t.Ref, Version: t.Version, SideEffect: t.SideEffect, Approval: t.Approval, Input: t.Input, Output: t.Output})
	}
	roles := []string{"owner", "admin", "member", "viewer"}
	heads, err := (policy.RoleStore{}).Snapshot(ctx, q, access)
	if err != nil {
		return in, err
	}
	for _, h := range heads {
		if h.Active {
			roles = append(roles, h.Name)
		}
	}
	in.RoleNames = roles
	stored, err := (promotion.Store{}).Requirements(ctx, q, access, app, 0)
	switch {
	case errors.Is(err, promotion.ErrNotFound):
	case err != nil:
		return in, err
	default:
		if set, _, _, perr := intent.ParseRequirementSet(stored.Body); perr == nil {
			in.Requirements = &set
		}
	}
	return in, nil
}

// aiPropose is exchange one of a creation: strict body, admission, context. The
// provider phase and the result transaction follow in reply.next.
func (a API) aiPropose(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, app string, kind ai.ProposalKind, evidence *audit.Entry) (reply, error) {
	var body struct {
		Request          string `json:"request"`
		ExpectedRevision *int64 `json:"expected_revision"`
	}
	if rep, ok := aiBody(r, aiCreateBody, &body, []string{"request"}, "expected_revision"); !ok {
		return rep, nil
	}
	if body.ExpectedRevision != nil && (*body.ExpectedRevision < 1 || *body.ExpectedRevision > 1_000_000_000) {
		return aiDeny(400, "invalid_ai_request", "Request body is invalid"), nil
	}
	svc := a.AI.Service
	adm, err := svc.Admit(ctx, q, access, ai.ProposeInput{Kind: kind, App: app, Request: body.Request, ExpectedRevision: body.ExpectedRevision})
	if err != nil {
		return aiReplyRetrying(r, err)
	}
	inputs, err := a.aiPlatform(ctx, q, access, app)
	if err != nil {
		return reply{}, err
	}
	if err = svc.Prepare(adm, inputs); err != nil {
		return aiReply(r, err)
	}
	evidence.Action = "ai.proposal.admitted"
	evidence.State.AIProposalID, evidence.State.AIKind, evidence.State.AIStatus = adm.ProposalID, string(kind), string(ai.ProposalPending)
	evidence.State.AIRequestHash, evidence.State.AIContextHash, evidence.State.AISourceRevision = adm.RequestHash, adm.Context.Hash, adm.DraftRevision
	return reply{status: 202, nextWithin: adm.Limits.WallClock + aiResultMargin, next: func(ctx context.Context) reply { return a.aiRun(ctx, r, app, adm) }}, nil
}

// aiResultMargin is added to the provider wall clock to form the connection's write deadline for
// the whole exchange: the result transaction (capped at 3 s) and the response.
const aiResultMargin = 10 * time.Second

// aiAuthorize re-authenticates the bearer token of the request in a new transaction
// and re-evaluates policy for the action: identity, tenant membership and policy are
// decided again in every exchange (ADR104 A8). A refusal is returned as a reply.
func (a API) aiAuthorize(ctx context.Context, tx postgres.Executor, r *http.Request, action, app string) (identity.User, tenant.Access, audit.Entry, *reply, error) {
	evidence := audit.Entry{Action: "authentication.denied", Resource: "identity", RequestID: httpserver.RequestID(r.Context()), TraceID: trace.SpanContextFromContext(r.Context()).TraceID().String(), PolicyVersion: policy.Version}
	unauthenticated, forbidden := denied(401, "unauthenticated", "Authentication is required"), denied(403, "forbidden", "Access is denied")
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") {
		return identity.User{}, tenant.Access{}, evidence, &unauthenticated, nil
	}
	p, err := a.Auth.Verify(strings.TrimPrefix(headers[0], "Bearer "))
	if err != nil {
		return identity.User{}, tenant.Access{}, evidence, &unauthenticated, nil
	}
	u, err := (identity.Store{}).Resolve(ctx, tx, p)
	if errors.Is(err, identity.ErrUnauthenticated) {
		return identity.User{}, tenant.Access{}, evidence, &unauthenticated, nil
	}
	if err != nil {
		return identity.User{}, tenant.Access{}, evidence, nil, err
	}
	if err = (tenant.Store{}).BindActor(ctx, tx, u); err != nil {
		return u, tenant.Access{}, evidence, nil, err
	}
	evidence.ActorID = u.ID()
	access, err := (tenant.Store{}).Resolve(ctx, tx, u, r.PathValue("tenant"))
	if errors.Is(err, tenant.ErrDenied) {
		evidence.Action, evidence.Resource = "tenant.denied", "tenant"
		return u, tenant.Access{}, evidence, &forbidden, nil
	}
	if err != nil {
		return u, tenant.Access{}, evidence, nil, err
	}
	engine := a.Policy
	if a.PolicyLoader != nil {
		if engine, err = a.PolicyLoader(ctx, tx, access); err != nil {
			if errors.Is(err, policy.ErrScope) {
				evidence.Action, evidence.TenantID = "policy.unavailable", access.TenantID()
				return u, access, evidence, &forbidden, nil
			}
			return u, access, evidence, nil, err
		}
	}
	policyContext := policy.Context{RequestID: evidence.RequestID, TraceID: evidence.TraceID}
	d := enforce(engine.Decide(ctx, policy.Subject{User: u, Access: access}, action, policy.Resource{TenantID: access.TenantID(), Type: "preview", AppID: app}, policyContext))
	evidence.TenantID, evidence.Action, evidence.Resource = access.TenantID(), action, "preview"
	evidence.DecisionID, evidence.PolicyVersion, evidence.Allow = d.ID, d.Version, d.Allow
	evidence.State.MetadataApp = app
	if !d.Allow {
		return u, access, evidence, &forbidden, nil
	}
	return u, access, evidence, nil, nil
}

// aiRun is exchanges two and three of a creation: the provider phase (no transaction,
// no connection held) and the result transaction, detached from client cancellation.
func (a API) aiRun(ctx context.Context, r *http.Request, app string, adm *ai.Admission) reply {
	svc := a.AI.Service
	out := svc.Execute(ctx, adm)
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer cancel()
	return a.aiResultExchange(detached, r, "ai.propose", app, func(ctx context.Context, tx postgres.Executor, u identity.User, access tenant.Access, evidence *audit.Entry) (reply, error) {
		if access.ActorID() != adm.RequesterID {
			return aiRejection(403, "forbidden", "Access is denied")
		}
		p, err := svc.Finish(ctx, tx, access, adm, out)
		if err != nil {
			return aiReplyRetrying(r, err)
		}
		aiEvidence(evidence, p)
		if trace := out.Attempts(); len(trace) > 0 {
			evidence.State.AIProvider, evidence.State.AIModel, evidence.State.AITemplate = trace[0].Provider, trace[0].Model, trace[0].TemplateVersion
		}
		if p.Status == ai.ProposalExpired {
			evidence.Allow = false // audited as ai.propose.rejected; the usage charge still commits
			return aiDeny(409, "proposal_expired", "The proposal expired before its result was stored"), nil
		}
		evidence.Action = "ai.proposal.created"
		failed := func(status int, code, message string) reply {
			return reply{status: status, body: aiErrorBody{ErrorResponse: httpserver.ErrorResponse{Code: code, Message: message, RequestID: httpserver.RequestID(r.Context())}, ProposalID: p.ID}}
		}
		if p.Status == ai.ProposalFailed {
			switch p.FailureCode {
			case ai.OutcomeProviderError, ai.OutcomeTimeout, ai.FailureNotConfigured:
				return failed(503, "ai_unavailable", "The AI provider did not answer; nothing was applied"), nil
			case ai.OutcomeBudget:
				return failed(429, "ai_budget_exhausted", "The AI budget for this request was exhausted"), nil
			}
		}
		return reply{status: 201, body: p}, nil
	})
}

// aiHandler is the body of one exchange: it runs inside the exchange's transaction after re-authentication and policy.
type aiHandler = func(context.Context, postgres.Executor, identity.User, tenant.Access, *audit.Entry) (reply, error)

// The result exchange is repeated while its transaction ends in a known abort (independent review M14R-03). The provider has been
// called and a paid answer is in memory when it runs, and the answer is lost if the exchange is not: results of one tenant that
// finish together all update the tenant's usage row under repeatable read, and each round lets one of them through. A try is a whole
// new exchange (bearer, tenant, policy and audit again), an aborted one stored nothing, and a finished proposal answers not_pending,
// so a repetition can neither charge nor store twice. The budget stays inside the 8 s of the detached context.
const (
	aiResultAttempts = 6
	aiResultBudget   = 6 * time.Second
)

// aiResultExchange is aiExchangeOnce with that repetition. When the last try still aborts, the answer is lost either way: the
// request is refused as the conflict it is, audited, instead of a bare 503.
func (a API) aiResultExchange(ctx context.Context, r *http.Request, action, app string, handle aiHandler) reply {
	var response reply
	err := postgres.RetryWithin(ctx, aiResultBudget, aiResultAttempts, func(ctx context.Context) (err error) {
		response, err = a.aiExchangeOnce(ctx, r, action, app, handle)
		return err
	}, postgres.KnownAbort)
	if postgres.KnownAbort(err) {
		response, err = a.aiExchangeOnce(ctx, r, action, app, func(context.Context, postgres.Executor, identity.User, tenant.Access, *audit.Entry) (reply, error) {
			return aiRejection(409, "ai_conflict", "A concurrent change prevented this request; reload and retry")
		})
	}
	if err != nil {
		return reply{status: 503, code: "service_unavailable", message: "A required service or security transaction is unavailable"}
	}
	return response
}

// aiExchangeOnce runs one transaction of a multi-exchange request with the guarantees of
// the standard envelope: re-authentication, policy, audit in the same transaction and
// rollback plus a fresh audit transaction for a refusal. It returns the error of a transaction
// that did not complete, so that a caller can repeat the exchange when the abort is a known one.
func (a API) aiExchangeOnce(ctx context.Context, r *http.Request, action, app string, handle aiHandler) (reply, error) {
	var response reply
	var refused audit.Entry
	err := a.transaction(ctx, func(ctx context.Context, tx postgres.Executor) error {
		response, refused = reply{}, audit.Entry{}
		u, access, evidence, denial, err := a.aiAuthorize(ctx, tx, r, action, app)
		refused = evidence
		if err != nil {
			return err
		}
		if denial != nil {
			response = *denial
			return a.Audit.Append(ctx, tx, evidence)
		}
		result, err := handle(ctx, tx, u, access, &evidence)
		refused = evidence
		if err != nil {
			return err
		}
		if result.status >= 400 && evidence.Action == action {
			evidence.Action += ".rejected"
			evidence.Allow = false
		}
		if err = a.Audit.Append(ctx, tx, evidence); err != nil {
			return err
		}
		response = result
		return nil
	})
	var rejected *requestRejection
	if errors.As(err, &rejected) {
		err = a.transaction(ctx, func(ctx context.Context, q postgres.Executor) error {
			_, access, fresh, denial, e := a.aiAuthorize(ctx, q, r, action, app)
			if e != nil {
				return e
			}
			if denial != nil || fresh.ActorID != refused.ActorID || access.TenantID() != refused.TenantID {
				return tenant.ErrDenied
			}
			refused.Action = strings.TrimSuffix(refused.Action, ".rejected") + ".rejected"
			refused.Allow = false
			return a.Audit.Append(ctx, q, refused)
		})
		if err == nil {
			response = rejected.response
		}
	}
	return response, err
}
