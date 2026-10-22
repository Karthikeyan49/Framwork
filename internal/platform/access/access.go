// Package access composes identity, tenant, policy and audit public contracts.
package access

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/platform/traffic"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type API struct {
	DB               *sql.DB
	Admission        *postgres.Admission
	Traffic          *traffic.Gate
	Auth             identity.Authenticator
	Policy           policy.Engine
	PolicyLoader     policy.Loader
	Audit            audit.Writer
	Events           events.Writer
	WorkflowEvents   events.LifecycleWriter
	Registry         *metadata.Registry
	PreviewEnabled   bool
	WorkflowsEnabled bool
	// Trusted platform configuration, never selected by a request or metadata.
	CompilerCatalog *compiler.Catalog
	// ToolCatalog is the operator-owned tool registry (ADR100); it feeds
	// sandbox evidence and, with the compiler, tool admission.
	ToolCatalog tools.Catalog
	// AI is the optional AI builder (ADR104). The zero value leaves every AI route
	// answering 503 ai_unavailable.
	AI AIConfig

	// Bodies bounds the request bytes that serve buffers before the database admission slot, in all and per verified identity.
	// Nil is the process default (DefaultBodyBudgetBytes and DefaultBodyBudgetPerUser).
	Bodies *BodyBudget

	// Recompute remembers the pure recomputation of stored release candidates and
	// bounds the cold ones per tenant and per process (ADR102 amendment 10). Nil is
	// the process default.
	Recompute *promotion.Recompute
}

// store is the release store of this API: its recomputations share one memo.
func (a API) store() promotion.Store { return promotion.Store{Recompute: a.Recompute} }

func (a API) catalog() compiler.Catalog {
	if a.CompilerCatalog != nil {
		return *a.CompilerCatalog
	}
	return compiler.PlatformCatalog()
}

func (a API) Register(mux *http.ServeMux) {
	if a.Traffic == nil {
		var err error
		a.Traffic, err = traffic.New(traffic.DefaultLimits(), nil)
		if err != nil {
			panic("API admission initialization failed")
		}
	}
	if a.Admission == nil && a.DB != nil {
		limit := a.DB.Stats().MaxOpenConnections
		if limit < 1 || limit > 4 {
			limit = 4
		}
		a.Admission, _ = postgres.NewAdmission(limit, 128)
	}
	if a.PreviewEnabled && a.Registry != nil {
		mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/commands/{command}", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets", a.serve)
		mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/verify", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/bindings", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/evolutions", a.serve)
		mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/evolutions/{evolution}", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/sandboxes", a.serve)
		mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/sandboxes/{sandbox}", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/sandboxes/{sandbox}/freeze", a.serve)
		mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/sandboxes/{sandbox}/evidence", a.serve)
		for pattern := range releasePatterns {
			mux.HandleFunc(pattern, a.serve)
		}
		a.registerAI(mux)
		for _, op := range []string{"advance", "activate", "abort", "reverify"} {
			mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/evolutions/{evolution}/"+op, a.serve)
		}
		mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/bindings/{binding}", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/bindings/{binding}/entities/{entity}/query", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/bindings/{binding}/entities/{entity}/commands", a.serve)
		mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/versions/{revision}", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/entities/{entity}/query", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/entities/{entity}/commands", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/rules/{rule}/evaluate", a.serve)
		for _, prefix := range []string{
			"/v1/tenants/{tenant}/preview/{app}/versions/{revision}",
			"/v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/bindings/{binding}",
		} {
			mux.HandleFunc("GET "+prefix+"/entities/{entity}/records/{record}/access", a.serve)
			mux.HandleFunc("POST "+prefix+"/entities/{entity}/records/{record}/access", a.serve)
			for _, kind := range []string{"indexes", "search-indexes", "unique-constraints"} {
				path := prefix + "/entities/{entity}/" + kind + "/{index}"
				mux.HandleFunc("GET "+path, a.serve)
				mux.HandleFunc("POST "+path+"/register", a.serve)
				mux.HandleFunc("POST "+path+"/build", a.serve)
				if kind == "unique-constraints" {
					mux.HandleFunc("POST "+path+"/abort", a.serve)
				}
			}
		}
		if a.WorkflowsEnabled {
			mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/workflows/{workflow}/start", a.serve)
			mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/bindings/{binding}/workflows/{workflow}/start", a.serve)
			mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/bindings/{binding}/workflows/{workflow}/instances/{instance}", a.serve)
			mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/datasets/{dataset}/bindings/{binding}/workflows/{workflow}/instances/{instance}/signals", a.serve)
			mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/versions/{revision}/workflows/{workflow}/instances/{instance}", a.serve)
			mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/workflows/{workflow}/instances/{instance}/signals", a.serve)
		}
	}
	mux.HandleFunc("GET /v1/tenants/{tenant}/roles", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/role-contract", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/roles/{role}", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/roles/operations/{policy_operation}", a.serve)
	mux.HandleFunc("POST /v1/tenants/{tenant}/roles", a.serve)
	mux.HandleFunc("POST /v1/tenants/{tenant}/members/{member}/role", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/teams", a.serve)
	mux.HandleFunc("POST /v1/tenants/{tenant}/teams", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/teams/{team}", a.serve)
	mux.HandleFunc("POST /v1/tenants/{tenant}/teams/{team}/members/{team_member}", a.serve)
	mux.HandleFunc("GET /v1/session", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/access", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/members", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/members/{workspace_member}", a.serve)
	if a.Registry != nil {
		mux.HandleFunc("GET /v1/tenants/{tenant}/metadata/{app}/versions", a.serve)
		mux.HandleFunc("GET /v1/tenants/{tenant}/metadata/{app}/versions/{revision}", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/metadata/{app}/versions", a.serve)
		mux.HandleFunc("GET /v1/tenants/{tenant}/metadata/{app}/versions/{revision}/compile", a.serve)
	}
}

type reply struct {
	status        int
	code, message string
	body          any
	issues        []metadata.Issue
	// next, when set, continues the request after the first transaction committed
	// (the AI provider phase and its result transaction, ADR104 A8). Its reply
	// replaces this one.
	next func(context.Context) reply
	// nextWithin, when positive, moves the connection's write deadline to that long
	// from now before next runs: the server-wide write timeout is 10 s and the
	// provider phase may take longer.
	nextWithin time.Duration

	// retryAfter, when positive, is sent as a Retry-After header in whole seconds.
	retryAfter time.Duration
}

// extendWriteDeadline moves the write deadline of the request's connection to d from
// now. Writers that do not support deadlines (a test recorder) are left alone: the
// call is best effort and its failure changes nothing but the available time.
func extendWriteDeadline(w http.ResponseWriter, d time.Duration) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d))
}

// replayedBody is the request body that a handler reads after serve consumed it: the bytes received, then the error that
// ended the read (nil: the end of the body).
func replayedBody(body []byte, cause error) io.ReadCloser {
	reader := io.Reader(bytes.NewReader(body))
	if cause != nil {
		reader = io.MultiReader(reader, failingReader{cause})
	}
	return io.NopCloser(reader)
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

// A service rejection must roll back its entire business transaction before
// becoming an HTTP response. Audit the rejection in a fresh live-identity scope.
type requestRejection struct {
	response reply
	evidence *audit.Entry
	// cause is the error the response was mapped from, when a retry may depend on it.
	cause error
}

func (e *requestRejection) Error() string { return "request rejected" }

// Unwrap exposes the database error a handler mapped to its typed response, so that the
// retry of an idempotent request can recognise a known abort.
func (e *requestRejection) Unwrap() error { return e.cause }

func denied(status int, code, message string) reply {
	return reply{status: status, code: code, message: message}
}

func (a API) transaction(ctx context.Context, run func(context.Context, postgres.Executor) error) error {
	return a.transactionAt(ctx, sql.LevelRepeatableRead, run)
}

// freezePattern is the one route whose port needs READ COMMITTED: runtime.freeze_sandbox locks the
// sandbox, the dataset and the ledger first and must then read the committed state, not the snapshot
// of a transaction that started before the locks were granted (M11 review R02).
const freezePattern = "POST /v1/tenants/{tenant}/preview/{app}/sandboxes/{sandbox}/freeze"

func (a API) transactionAt(ctx context.Context, level sql.IsolationLevel, run func(context.Context, postgres.Executor) error) error {
	if a.Admission != nil {
		queueCtx, span := otel.Tracer("framework/postgres").Start(ctx, "postgres.request.admission")
		release, err := a.Admission.Acquire(queueCtx)
		span.End()
		if err != nil {
			return err
		}
		defer release()
	}
	return postgres.WithRuntimeIsolation(ctx, a.DB, level, run)
}
func (a API) serve(w http.ResponseWriter, r *http.Request) {
	if a.rateRejected(w, r, a.Traffic.Peer(r.Context(), r.RemoteAddr)) {
		return
	}
	if a.Auth == nil || a.Policy == nil || a.Audit == nil {
		httpserver.Error(w, r, 503, "identity_unavailable", "Identity service is not configured")
		return
	}
	headers := r.Header.Values("Authorization")
	var prePrincipal identity.Principal
	preError := identity.ErrUnauthenticated
	if len(headers) == 1 && strings.HasPrefix(headers[0], "Bearer ") {
		prePrincipal, preError = a.Auth.Verify(strings.TrimPrefix(headers[0], "Bearer "))
	}
	var trafficDecision traffic.Decision
	if preError != nil {
		trafficDecision = a.Traffic.Invalid(r.Context(), r.RemoteAddr)
	} else {
		trafficDecision = a.Traffic.Subject(r.Context(), prePrincipal.Issuer(), prePrincipal.Subject())
	}
	if a.rateRejected(w, r, trafficDecision) {
		return
	}
	var p identity.Principal
	authErr := identity.ErrUnauthenticated
	var response reply
	var rejectedEvidence audit.Entry
	localCommand := r.Method == http.MethodPost && r.PathValue("entity") != "" && strings.HasSuffix(r.URL.Path, "/commands")
	// The POSTs of the release builder that are idempotent by content (a candidate with the
	// same bytes, an acceptance spec with the same body) repeat after a known abort, so that
	// concurrent identical requests converge on the replay instead of one 409 each (review F7).
	retryRelease := releaseIdempotentPOST[r.Pattern] && r.Body != nil
	// The creation of an AI proposal repeats after a known abort too: its admission writes the tenant's usage row, which
	// concurrent creations contest, and an aborted admission has stored and charged nothing (review M14R-03).
	retryAI := aiCreatePOST[r.Pattern]
	// Every other body is read here too. Bodies are read before the admission slot and the transaction: the handlers read them
	// later, and a client that sends its bytes slowly would otherwise hold a database slot for as long as the server's read timeout
	// (independent review IR-02: four such connections stalled the whole API and lost AI results). Only a caller whose bearer
	// already verified is read, on every route, so an anonymous caller cannot make the server buffer or wait for anything. What is
	// buffered is booked first, per verified identity and in all (BodyBudget), and a booking that does not fit is refused before a
	// byte is read: without it one token could pin 1.14 MiB per connection (review F-01). The handlers keep their own limits and
	// error codes: they read the replay, which ends with the error of the front door's 1 MiB cap when the body was larger, exactly
	// as the live body would have.
	preRead := preError == nil && !localCommand && !retryRelease && r.Method == http.MethodPost && r.Body != nil && (r.ContentLength != 0 || len(r.TransferEncoding) > 0)
	var body []byte
	var bodyErr error
	if preError == nil && (localCommand || retryRelease || preRead) {
		limit := int64(1<<20 + 1)
		if localCommand {
			limit = data.MaxRecordBytes + 1
		}
		release, refused := a.bodies().Reserve(prePrincipal.Issuer()+"\x00"+prePrincipal.Subject(), reservation(r.ContentLength, limit))
		if refused != "" {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Cache-Control", "no-store")
			if refused == bodyRefusedSubject {
				httpserver.Error(w, r, http.StatusTooManyRequests, "too_many_uploads", "Too many request bodies are in flight for this identity; retry shortly")
			} else {
				httpserver.Error(w, r, http.StatusServiceUnavailable, "service_busy", "The service is buffering too many request bodies; retry shortly")
			}
			return
		}
		defer release()
		switch {
		case localCommand:
			var err error
			body, err = io.ReadAll(io.LimitReader(r.Body, data.MaxRecordBytes+1))
			if err != nil || len(body) > data.MaxRecordBytes {
				httpserver.Error(w, r, 400, "invalid_query_or_command", "Data request exceeds the typed request budget")
				return
			}
		case retryRelease:
			var err error
			body, err = io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
			if err != nil || len(body) > 1<<20 {
				httpserver.Error(w, r, 400, "invalid_release_request", "Request body is invalid")
				return
			}
		default:
			body, bodyErr = io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
			var tooLarge *http.MaxBytesError
			if bodyErr != nil && !errors.As(bodyErr, &tooLarge) {
				httpserver.Error(w, r, 400, "invalid_request", "Request body could not be read")
				return
			}
		}
	}
	parentContext := r.Context()
	if localCommand {
		var cancel context.CancelFunc
		parentContext, cancel = context.WithTimeout(parentContext, 4*time.Second)
		defer cancel()
	}
	run := func(parent context.Context) error {
		response, rejectedEvidence = reply{}, audit.Entry{}
		p, authErr = identity.Principal{}, preError
		if localCommand || retryRelease {
			r.Body = io.NopCloser(bytes.NewReader(body))
		} else if preRead {
			r.Body = replayedBody(body, bodyErr)
		}
		level := sql.LevelRepeatableRead
		if r.Pattern == freezePattern {
			level = sql.LevelReadCommitted
		}
		return a.transactionAt(parent, level, func(ctx context.Context, tx postgres.Executor) error {
			// Recheck valid signatures/time bounds after admission/queue waits and
			// on each known-aborted retry. No token validity or policy cache.
			if preError == nil {
				p, authErr = a.Auth.Verify(strings.TrimPrefix(headers[0], "Bearer "))
			}
			result, evidence, err := a.perform(ctx, tx, r, p, authErr)
			rejectedEvidence = evidence
			if err != nil {
				var rejected *requestRejection
				if errors.As(err, &rejected) {
					if rejected.evidence != nil {
						rejectedEvidence = *rejected.evidence
					}
				}
				return err
			}
			// A typed rejection is still a failed command. The repository may
			// already have written before a final read/validation failed; never
			// commit those effects merely because HTTP can represent the error.
			if localCommand && result.status >= 400 && evidence.ActorID != "" && evidence.TenantID != "" {
				return &requestRejection{response: result, evidence: &evidence}
			}
			if err = a.Audit.Append(ctx, tx, evidence); err != nil {
				return err
			}
			response = result
			return nil
		})
	}
	var err error
	if localCommand {
		err = postgres.RetryLocal(parentContext, run)
		if postgres.KnownAbort(err) {
			err = &requestRejection{response: denied(409, "concurrent_conflict", "Concurrent changes prevented this command; reconcile its key and reload before retrying")}
		}
	} else if retryRelease {
		err = postgres.RetryLocalWhen(parentContext, run, releaseKnownAbort)
	} else if retryAI {
		err = postgres.RetryWithin(parentContext, aiAdmissionBudget, aiAdmissionAttempts, run, postgres.KnownAbort)
		if postgres.KnownAbort(err) {
			err = &requestRejection{response: denied(409, "ai_conflict", "A concurrent change prevented this request; reload and retry")}
		}
	} else {
		err = run(parentContext)
	}
	var rejected *requestRejection
	if errors.As(err, &rejected) {
		err = a.transaction(parentContext, func(ctx context.Context, q postgres.Executor) error {
			u, e := (identity.Store{}).Resolve(ctx, q, p)
			if e != nil {
				return e
			}
			if e = (tenant.Store{}).BindActor(ctx, q, u); e != nil {
				return e
			}
			access, e := (tenant.Store{}).Resolve(ctx, q, u, r.PathValue("tenant"))
			if e != nil {
				return e
			}
			if u.ID() != rejectedEvidence.ActorID || access.TenantID() != rejectedEvidence.TenantID {
				return tenant.ErrDenied
			}
			if rejectedEvidence.Resource == "workflow" {
				rejectedEvidence.Action = "workflows.rejected"
			} else {
				rejectedEvidence.Action = strings.TrimSuffix(rejectedEvidence.Action, ".rejected") + ".rejected"
			}
			rejectedEvidence.Allow = false
			return a.Audit.Append(ctx, q, rejectedEvidence)
		})
		if err == nil {
			response = rejected.response
		}
	}
	if err != nil {
		httpserver.Error(w, r, 503, "service_unavailable", "A required service or security transaction is unavailable")
		return
	}
	if err == nil && response.next != nil {
		if response.nextWithin > 0 {
			extendWriteDeadline(w, response.nextWithin)
		}
		response = response.next(parentContext)
	}
	// No protected response or security success is sent before the outer commit.
	if response.status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="framework"`)
	}
	if response.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int((response.retryAfter+time.Second-1)/time.Second)))
	}
	if len(response.issues) > 0 {
		httpserver.JSON(w, response.status, struct {
			httpserver.ErrorResponse
			Issues []metadata.Issue `json:"issues"`
		}{httpserver.ErrorResponse{Code: response.code, Message: response.message, RequestID: httpserver.RequestID(r.Context())}, response.issues})
		return
	}
	if response.code != "" {
		httpserver.Error(w, r, response.status, response.code, response.message)
		return
	}
	httpserver.JSON(w, response.status, response.body)
}

func (a API) rateRejected(w http.ResponseWriter, r *http.Request, d traffic.Decision) bool {
	if d.Allowed() {
		return false
	}
	w.Header().Set("Retry-After", strconv.FormatInt(d.RetrySeconds(), 10))
	w.Header().Set("Cache-Control", "no-store")
	httpserver.Error(w, r, http.StatusTooManyRequests, "rate_limited", "Request rate exceeded; wait before a new request")
	return true
}

func (a API) perform(ctx context.Context, tx postgres.Executor, r *http.Request, p identity.Principal, authErr error) (reply, audit.Entry, error) {
	evidence := audit.Entry{Action: "authentication.denied", Resource: "identity", RequestID: httpserver.RequestID(r.Context()), TraceID: trace.SpanContextFromContext(r.Context()).TraceID().String(), PolicyVersion: policy.Version}
	unauthenticated := denied(401, "unauthenticated", "Authentication is required")
	forbidden := denied(403, "forbidden", "Access is denied")
	if authErr != nil {
		return unauthenticated, evidence, nil
	}
	u, err := (identity.Store{}).Resolve(ctx, tx, p)
	if errors.Is(err, identity.ErrUnauthenticated) {
		return unauthenticated, evidence, nil
	}
	if err != nil {
		return reply{}, evidence, err
	}
	ts := tenant.Store{}
	if err = ts.BindActor(ctx, tx, u); err != nil {
		return reply{}, evidence, err
	}
	evidence.ActorID = u.ID()
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attribute.String("actor_id", u.ID()))
	policyContext := policy.Context{RequestID: evidence.RequestID, TraceID: evidence.TraceID}
	recordDecision := func(d policy.Decision) {
		evidence.DecisionID = d.ID
		evidence.PolicyVersion = d.Version
		evidence.Allow = d.Allow
		span.SetAttributes(attribute.String("decision_id", d.ID), attribute.String("policy_version", d.Version))
	}
	if r.URL.Path == "/v1/session" {
		d := enforce(a.Policy.Decide(ctx, policy.Subject{User: u}, "identity.session.read", policy.Resource{Type: "user", OwnerID: u.ID()}, policyContext))
		recordDecision(d)
		if !d.Allow {
			evidence.Action = "identity.session.denied"
			return forbidden, evidence, nil
		}
		tenants, err := ts.List(ctx, tx, u)
		if err != nil {
			return reply{}, evidence, err
		}
		evidence.Action = "authentication.accepted"
		return reply{status: 200, body: struct {
			User    identity.User    `json:"user"`
			Tenants []tenant.Summary `json:"tenants"`
		}{u, tenants}}, evidence, nil
	}
	access, err := ts.Resolve(ctx, tx, u, r.PathValue("tenant"))
	if errors.Is(err, tenant.ErrDenied) {
		evidence.Action = "tenant.denied"
		evidence.Resource = "tenant"
		return forbidden, evidence, nil
	}
	if err != nil {
		return reply{}, evidence, err
	}
	span.SetAttributes(attribute.String("tenant_id", access.TenantID()))
	if a.PolicyLoader != nil {
		engine, err := a.PolicyLoader(ctx, tx, access)
		if errors.Is(err, policy.ErrScope) {
			evidence.Action = "policy.unavailable"
			evidence.TenantID = access.TenantID()
			var deniedRole policy.RoleDenied
			if errors.As(err, &deniedRole) {
				decision := (policy.Native{}).Decide(ctx, policy.Subject{}, "tenant.access", policy.Resource{}, policyContext)
				decision.Version = deniedRole.PolicyVersion()
				recordDecision(decision)
				evidence.State.Role = deniedRole.Name
				evidence.State.PolicyRevision = deniedRole.Version
				evidence.State.ArtifactHash = deniedRole.Hash
			}
			return forbidden, evidence, nil
		}
		if err != nil {
			return reply{}, evidence, err
		}
		a.Policy = engine
	}
	action, resource := "tenant.access", "tenant"
	if strings.HasSuffix(r.URL.Path, "/members") || r.Pattern == "GET /v1/tenants/{tenant}/members/{workspace_member}" {
		action, resource = "memberships.read", "membership"
	}
	if strings.Contains(r.URL.Path, "/metadata/") {
		action, resource = "metadata.read", "metadata"
		if strings.HasSuffix(r.URL.Path, "/compile") {
			action = "metadata.compile"
		}
		if r.Method == http.MethodPost {
			action = "metadata.write"
		}
	}
	if strings.Contains(r.URL.Path, "/preview/") {
		action = "preview.read"
		resource = "preview"
		if strings.HasSuffix(r.URL.Path, "/commands") || r.Method == http.MethodPost && r.PathValue("record") != "" {
			action = "preview.write"
		}
		if r.PathValue("command") != "" {
			action = "commands.reconcile"
		}
		if r.Method == http.MethodPost && datasetManagementPOST.MatchString(r.URL.Path) {
			action = "datasets.manage"
		}
		if r.Method == http.MethodGet && r.PathValue("dataset") != "" && r.PathValue("binding") == "" {
			action = "datasets.manage"
		}
		if r.PathValue("index") != "" {
			action = "indexes.manage"
		}
		if r.PathValue("evolution") != "" || strings.HasSuffix(r.URL.Path, "/evolutions") {
			action = "datasets.evolve"
		}
		if sandboxPatterns[r.Pattern] {
			action = "sandbox.read"
			if r.Method == http.MethodPost {
				action = "sandbox.manage"
			}
		}
		if releaseAction, ok := releasePatterns[r.Pattern]; ok {
			action = releaseAction
		}
		if aiAction, ok := aiPatterns[r.Pattern]; ok {
			action = aiAction
		}
	}
	// Exact registered patterns only (as for teams below): a path suffix would capture
	// any route whose last variable segment is literally "roles", for example a release
	// artifact id (M12 API review F2).
	if r.PathValue("role") != "" || r.PathValue("policy_operation") != "" || rolePatterns[r.Pattern] {
		resource = "role"
		action = "roles.read"
		if r.Method == http.MethodPost {
			action = "roles.write"
		}
	}
	if r.PathValue("member") != "" {
		resource = "role"
		action = "roles.assign"
	}
	// Exact registered patterns only: a suffix would capture other routes whose
	// last variable segment is literally "teams" (for example a role name).
	if teamPatterns[r.Pattern] {
		resource, action = "team", "teams.read"
		if r.Method == http.MethodPost {
			action = "teams.manage"
		}
	}
	appID := ""
	if resource == "preview" || resource == "metadata" {
		appID = r.PathValue("app")
		if len(appID) > 160 || !metadataAppID.MatchString(appID) {
			// An authenticated, tenant-resolved request: record it as such (tenant, the
			// action it asked for) and not as an authentication failure (review F12).
			evidence.TenantID = access.TenantID()
			evidence.Action = action + ".rejected"
			evidence.Resource = resource
			return denied(400, "invalid_application", "Invalid application selection"), evidence, nil
		}
		evidence.State.MetadataApp = appID
	}
	d := enforce(a.Policy.Decide(ctx, policy.Subject{User: u, Access: access}, action, policy.Resource{TenantID: access.TenantID(), Type: resource, AppID: appID}, policyContext))
	evidence.TenantID = access.TenantID()
	evidence.Action = action
	evidence.Resource = resource
	recordDecision(d)
	if !d.Allow {
		return forbidden, evidence, nil
	}
	if resource == "role" {
		result, err := a.role(ctx, tx, r, access, &evidence)
		return result, evidence, err
	}
	if resource == "team" {
		result, err := a.team(ctx, tx, r, access, &evidence)
		return result, evidence, err
	}
	if resource == "preview" {
		var result reply
		var err error
		if r.PathValue("command") != "" {
			if !tenant.ValidID(r.PathValue("command")) {
				return denied(400, "invalid_command_key", "Invalid command key"), evidence, nil
			}
			var receipt data.Receipt
			evidence.State.CommandKey = r.PathValue("command")
			receipt, err = data.CommandReceipt(ctx, tx, access, appID, r.PathValue("command"))
			result = reply{status: 200, body: receipt}
		} else if _, ok := releasePatterns[r.Pattern]; ok {
			result, err = a.release(ctx, tx, r, u, access, &evidence)
		} else if _, ok := aiPatterns[r.Pattern]; ok {
			result, err = a.ai(ctx, tx, r, u, access, &evidence)
		} else if sandboxPatterns[r.Pattern] {
			result, err = a.sandbox(ctx, tx, r, u, access, &evidence)
		} else if r.PathValue("evolution") != "" || strings.HasSuffix(r.URL.Path, "/evolutions") {
			result, err = a.evolution(ctx, tx, r, access, &evidence)
		} else if strings.Contains(r.URL.Path, "/datasets") {
			result, err = a.dataset(ctx, tx, r, u, access, &evidence)
		} else {
			result, err = a.preview(ctx, tx, r, u, access, &evidence)
		}
		if result.status >= 400 {
			evidence.Action += ".rejected"
			evidence.Allow = false
		}
		return result, evidence, err
	}
	if resource == "metadata" {
		result, err := a.metadata(ctx, tx, r, access, &evidence)
		if result.status >= 400 {
			evidence.Action += ".rejected"
			evidence.Allow = false
		}
		return result, evidence, err
	}
	var result any
	if action == "memberships.read" {
		if member := r.PathValue("workspace_member"); member != "" {
			if !tenant.ValidID(member) {
				return denied(400, "invalid_member", "Invalid member identifier"), evidence, nil
			}
			value, err := ts.Member(ctx, tx, access, member)
			if errors.Is(err, sql.ErrNoRows) {
				return denied(404, "member_not_found", "Member is unavailable"), evidence, nil
			}
			if err != nil {
				return reply{}, evidence, err
			}
			return reply{status: 200, body: value}, evidence, nil
		}
		members, err := ts.Members(ctx, tx, access)
		if err != nil {
			return reply{}, evidence, err
		}
		result = struct {
			Members []tenant.Member `json:"members"`
		}{members}
	} else {
		result = struct {
			TenantID string          `json:"tenant_id"`
			Roles    []string        `json:"roles"`
			Decision policy.Decision `json:"decision"`
		}{access.TenantID(), access.Roles(), d}
	}
	return reply{status: 200, body: result}, evidence, nil
}

var rolePatterns = map[string]bool{
	"GET /v1/tenants/{tenant}/roles":         true,
	"GET /v1/tenants/{tenant}/role-contract": true,
	"POST /v1/tenants/{tenant}/roles":        true,
}

var teamPatterns = map[string]bool{
	"GET /v1/tenants/{tenant}/teams":                               true,
	"POST /v1/tenants/{tenant}/teams":                              true,
	"GET /v1/tenants/{tenant}/teams/{team}":                        true,
	"POST /v1/tenants/{tenant}/teams/{team}/members/{team_member}": true,
}

func enforce(d policy.Decision) policy.Decision {
	// Audit is mandatory even if omitted by an adapter. Unknown obligations deny.
	for _, obligation := range d.Obligations {
		if obligation != "audit" {
			d.Allow = false
		}
	}
	if d.ID == "" || d.Version == "" {
		d.Allow = false
	}
	return d
}
