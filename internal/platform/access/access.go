// Package access composes identity, tenant, policy and audit public contracts.
package access

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
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
	// ToolCatalog is the operator-owned tool registry (ADR036); it feeds
	// sandbox evidence and, with the compiler, tool admission.
	ToolCatalog tools.Catalog
}

func (a API) catalog() compiler.Catalog {
	if a.CompilerCatalog != nil {
		return *a.CompilerCatalog
	}
	return compiler.PlatformCatalog()
}

func (a API) Register(mux *http.ServeMux) {
	if a.Admission == nil && a.DB != nil {
		limit := a.DB.Stats().MaxOpenConnections
		if limit < 1 {
			limit = 10
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
			path := prefix + "/entities/{entity}/indexes/{index}"
			mux.HandleFunc("GET "+path, a.serve)
			mux.HandleFunc("POST "+path+"/register", a.serve)
			mux.HandleFunc("POST "+path+"/build", a.serve)
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
}

// A service rejection must roll back its entire business transaction before
// becoming an HTTP response. Audit the rejection in a fresh live-identity scope.
type requestRejection struct {
	response reply
	evidence *audit.Entry
}

func (e *requestRejection) Error() string { return "request rejected" }

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
	if a.Auth == nil || a.Policy == nil || a.Audit == nil {
		httpserver.Error(w, r, 503, "identity_unavailable", "Identity service is not configured")
		return
	}
	headers := r.Header.Values("Authorization")
	var p identity.Principal
	authErr := identity.ErrUnauthenticated
	var response reply
	var rejectedEvidence audit.Entry
	localCommand := r.Method == http.MethodPost && r.PathValue("entity") != "" && strings.HasSuffix(r.URL.Path, "/commands")
	var body []byte
	if localCommand {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, data.MaxRecordBytes+1))
		if err != nil || len(body) > data.MaxRecordBytes {
			httpserver.Error(w, r, 400, "invalid_query_or_command", "Data request exceeds the typed request budget")
			return
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
		p, authErr = identity.Principal{}, identity.ErrUnauthenticated
		if localCommand {
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		level := sql.LevelRepeatableRead
		if r.Pattern == freezePattern {
			level = sql.LevelReadCommitted
		}
		return a.transactionAt(parent, level, func(ctx context.Context, tx postgres.Executor) error {
			if len(headers) == 1 && strings.HasPrefix(headers[0], "Bearer ") {
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
	// No protected response or security success is sent before the outer commit.
	if response.status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="framework"`)
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
	if strings.HasSuffix(r.URL.Path, "/members") {
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
	}
	if r.PathValue("role") != "" || r.PathValue("policy_operation") != "" || strings.HasSuffix(r.URL.Path, "/roles") {
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
