// Package access composes identity, tenant, policy and audit public contracts.
package access

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type API struct {
	DB               *sql.DB
	Auth             identity.Authenticator
	Policy           policy.Engine
	Audit            audit.Writer
	Events           events.Writer
	WorkflowEvents   events.LifecycleWriter
	Registry         *metadata.Registry
	PreviewEnabled   bool
	WorkflowsEnabled bool
}

func (a API) Register(mux *http.ServeMux) {
	if a.PreviewEnabled && a.Registry != nil {
		mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/versions/{revision}", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/entities/{entity}/query", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/entities/{entity}/commands", a.serve)
		mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/rules/{rule}/evaluate", a.serve)
		if a.WorkflowsEnabled {
			mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/workflows/{workflow}/start", a.serve)
			mux.HandleFunc("GET /v1/tenants/{tenant}/preview/{app}/versions/{revision}/workflows/{workflow}/instances/{instance}", a.serve)
			mux.HandleFunc("POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/workflows/{workflow}/instances/{instance}/signals", a.serve)
		}
	}
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
func (a API) serve(w http.ResponseWriter, r *http.Request) {
	if a.Auth == nil || a.Policy == nil || a.Audit == nil {
		httpserver.Error(w, r, 503, "identity_unavailable", "Identity service is not configured")
		return
	}
	headers := r.Header.Values("Authorization")
	var p identity.Principal
	authErr := identity.ErrUnauthenticated
	if len(headers) == 1 && strings.HasPrefix(headers[0], "Bearer ") {
		p, authErr = a.Auth.Verify(strings.TrimPrefix(headers[0], "Bearer "))
	}
	var response reply
	var rejectedEvidence audit.Entry
	err := postgres.WithRuntime(r.Context(), a.DB, func(ctx context.Context, tx postgres.Executor) error {
		result, evidence, err := a.perform(ctx, tx, r, p, authErr)
		if err != nil {
			var rejected *requestRejection
			if errors.As(err, &rejected) {
				rejectedEvidence = evidence
				if rejected.evidence != nil {
					rejectedEvidence = *rejected.evidence
				}
			}
			return err
		}
		if err = a.Audit.Append(ctx, tx, evidence); err != nil {
			return err
		}
		response = result
		return nil
	})
	var rejected *requestRejection
	if errors.As(err, &rejected) {
		err = postgres.WithRuntime(r.Context(), a.DB, func(ctx context.Context, q postgres.Executor) error {
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
			rejectedEvidence.Action = "workflows.rejected"
			rejectedEvidence.Resource = "workflow"
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
		if strings.HasSuffix(r.URL.Path, "/commands") {
			action = "preview.write"
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
	if resource == "preview" {
		result, err := a.preview(ctx, tx, r, u, access, &evidence)
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
