// Package access composes identity, tenant, policy and audit public contracts.
package access

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type API struct {
	DB     *sql.DB
	Auth   identity.Authenticator
	Policy policy.Engine
	Audit  audit.Writer
}

func (a API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/session", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/access", a.serve)
	mux.HandleFunc("GET /v1/tenants/{tenant}/members", a.serve)
}
func (a API) serve(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	fail := func(status int, code, msg string) { httpserver.Error(w, r, status, code, msg) }
	if a.Auth == nil || a.Policy == nil || a.Audit == nil {
		fail(503, "identity_unavailable", "Identity service is not configured")
		return
	}
	if postgres.Ping(ctx, a.DB) != nil {
		fail(503, "service_unavailable", "Runtime database privileges or availability are unsafe")
		return
	}
	headers := r.Header.Values("Authorization")
	var p identity.Principal
	var err error
	if len(headers) == 1 && strings.HasPrefix(headers[0], "Bearer ") {
		p, err = a.Auth.Verify(strings.TrimPrefix(headers[0], "Bearer "))
	} else {
		err = identity.ErrUnauthenticated
	}
	tx, dbErr := a.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if dbErr != nil {
		fail(503, "service_unavailable", "A required service is unavailable")
		return
	}
	defer tx.Rollback()
	evidence := audit.Entry{Action: "authentication.denied", Resource: "identity", RequestID: httpserver.RequestID(r.Context()), TraceID: trace.SpanContextFromContext(r.Context()).TraceID().String(), PolicyVersion: policy.Version}
	commit := func() bool {
		if a.Audit.Append(ctx, tx, evidence) != nil || tx.Commit() != nil {
			fail(503, "service_unavailable", "Security evidence could not be recorded")
			return false
		}
		return true
	}
	if err != nil {
		if commit() {
			w.Header().Set("WWW-Authenticate", `Bearer realm="framework"`)
			fail(401, "unauthenticated", "Authentication is required")
		}
		return
	}
	u, err := (identity.Store{}).Resolve(ctx, tx, p)
	if err != nil {
		if errors.Is(err, identity.ErrUnauthenticated) {
			if commit() {
				fail(401, "unauthenticated", "Authentication is required")
			}
		} else {
			fail(503, "service_unavailable", "A required service is unavailable")
		}
		return
	}
	ts := tenant.Store{}
	if ts.BindActor(ctx, tx, u) != nil {
		fail(503, "service_unavailable", "A required service is unavailable")
		return
	}
	evidence.ActorID = u.ID()
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attribute.String("actor_id", u.ID()))
	if r.URL.Path == "/v1/session" {
		d := enforce(a.Policy.Decide(ctx, policy.Subject{User: u}, "identity.session.read", policy.Resource{Type: "user", OwnerID: u.ID()}, policy.Context{RequestID: evidence.RequestID, TraceID: evidence.TraceID}))
		evidence.DecisionID = d.ID
		evidence.PolicyVersion = d.Version
		evidence.Allow = d.Allow
		span.SetAttributes(attribute.String("decision_id", d.ID), attribute.String("policy_version", d.Version))
		if !d.Allow {
			evidence.Action = "identity.session.denied"
			if commit() {
				fail(403, "forbidden", "Access is denied")
			}
			return
		}
		tenants, err := ts.List(ctx, tx, u)
		if err != nil {
			fail(503, "service_unavailable", "A required service is unavailable")
			return
		}
		evidence.Action = "authentication.accepted"
		evidence.Allow = true
		if commit() {
			httpserver.JSON(w, 200, struct {
				User    identity.User    `json:"user"`
				Tenants []tenant.Summary `json:"tenants"`
			}{u, tenants})
		}
		return
	}
	access, err := ts.Resolve(ctx, tx, u, r.PathValue("tenant"))
	if err != nil {
		if errors.Is(err, tenant.ErrDenied) {
			evidence.Action = "tenant.denied"
			evidence.Resource = "tenant"
			if commit() {
				fail(403, "forbidden", "Access is denied")
			}
		} else {
			fail(503, "service_unavailable", "A required service is unavailable")
		}
		return
	}
	span.SetAttributes(attribute.String("tenant_id", access.TenantID()))
	action, resource := "tenant.access", "tenant"
	if strings.HasSuffix(r.URL.Path, "/members") {
		action, resource = "memberships.read", "membership"
	}
	d := enforce(a.Policy.Decide(ctx, policy.Subject{User: u, Access: access}, action, policy.Resource{TenantID: access.TenantID(), Type: resource}, policy.Context{RequestID: evidence.RequestID, TraceID: evidence.TraceID}))
	evidence.TenantID = access.TenantID()
	evidence.Action = action
	evidence.Resource = resource
	evidence.Allow = d.Allow
	evidence.DecisionID = d.ID
	evidence.PolicyVersion = d.Version
	span.SetAttributes(attribute.String("decision_id", d.ID), attribute.String("policy_version", d.Version))
	if !d.Allow {
		if commit() {
			fail(403, "forbidden", "Access is denied")
		}
		return
	}
	var result any
	if action == "memberships.read" {
		members, err := ts.Members(ctx, tx, access)
		if err != nil {
			fail(503, "service_unavailable", "A required service is unavailable")
			return
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
	if commit() {
		httpserver.JSON(w, 200, result)
	}
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
