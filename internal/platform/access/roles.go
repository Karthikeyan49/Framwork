package access

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

var rolePOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/(roles|members/[0-9a-f-]{36}/role)$`)

func roleReply(value any, err error) (reply, error) {
	if err == nil {
		return reply{status: 200, body: value}, nil
	}
	status, code := 0, ""
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, policy.ErrTooManyRoles):
		status, code = 409, "role_limit"
	case errors.Is(err, policy.ErrDefinition):
		status, code = 400, "invalid_role"
	case errors.Is(err, policy.ErrScope):
		status, code = 404, "role_not_found"
	case errors.As(err, &pg):
		switch pg.Code {
		case "22023", "23514":
			status, code = 400, "invalid_role"
		case "42501":
			status, code = 403, "forbidden"
		case "55000", "23503", "23505", "40001", "40P01", "55P03":
			status, code = 409, "role_conflict"
		}
	}
	if status != 0 {
		return reply{}, &requestRejection{response: denied(status, code, "Role operation rejected; inspect the current version before retrying")}
	}
	return reply{}, err
}
func (a API) role(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, evidence *audit.Entry) (reply, error) {
	store := policy.RoleStore{}
	if r.Method == http.MethodGet {
		if r.Pattern == "GET /v1/tenants/{tenant}/role-contract" || r.Pattern == "GET /v1/tenants/{tenant}/roles" {
			key := "after"
			if r.Pattern == "GET /v1/tenants/{tenant}/role-contract" {
				key = "name"
			}
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil || len(query) > 1 || len(query[key]) > 1 || len(query) == 1 && query[key] == nil {
				return roleReply(nil, policy.ErrDefinition)
			}
			if key == "name" {
				v, err := policy.DefinitionContract(query.Get(key))
				return roleReply(v, err)
			}
			v, err := store.List(ctx, q, access, query.Get(key))
			return roleReply(v, err)
		}
		if id := r.PathValue("policy_operation"); id != "" {
			v, err := store.Operation(ctx, q, access, id)
			return roleReply(v, err)
		}
		v, err := store.Current(ctx, q, access, r.PathValue("role"))
		return roleReply(v, err)
	}
	if r.PathValue("member") != "" {
		var body struct {
			Role         string `json:"role"`
			ExpectedRole string `json:"expected_role"`
		}
		if readDataCommand(r, &body) != nil {
			return roleReply(nil, policy.ErrDefinition)
		}
		evidence.State.Role = body.Role
		evidence.State.PolicySubject = r.PathValue("member")
		err := store.Assign(ctx, q, access, r.PathValue("member"), body.Role, body.ExpectedRole)
		return roleReply(struct {
			Assigned     bool   `json:"assigned"`
			UserID       string `json:"user_id"`
			Role         string `json:"role"`
			ExpectedRole string `json:"expected_role"`
		}{err == nil, r.PathValue("member"), body.Role, body.ExpectedRole}, err)
	}
	var body struct {
		OperationID     string          `json:"operation_id"`
		ExpectedVersion int64           `json:"expected_version"`
		Active          *bool           `json:"active"`
		Definition      json.RawMessage `json:"definition"`
	}
	if readDataCommand(r, &body) != nil || body.Active == nil {
		return roleReply(nil, policy.ErrDefinition)
	}
	v, err := store.Publish(ctx, q, access, body.OperationID, body.ExpectedVersion, body.Definition, *body.Active)
	if err == nil {
		evidence.State.Role = v.Name
		evidence.State.ArtifactHash = v.Hash
		evidence.State.PolicyRevision = v.Version
		evidence.State.PolicyOperation = body.OperationID
	}
	return roleReply(v, err)
}
