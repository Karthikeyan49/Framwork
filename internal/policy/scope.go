package policy

import (
	"context"
	"encoding/json"
)

// DataScope is trusted policy output, never an HTTP/query payload.
// Teams are resolved server-side from live memberships (including hierarchy).
type DataScope struct {
	Kind       string          `json:"kind"` // ALL, NONE, OWNER, TEAM, SHARE, CONDITION, AND, OR
	Teams      []string        `json:"teams,omitempty"`
	WriteShare bool            `json:"write_share,omitempty"`
	Condition  *ScopeCondition `json:"condition,omitempty"`
	Children   []DataScope     `json:"children,omitempty"`
}
type ScopeCondition struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}
type ScopeContext struct {
	Teams              []string
	RequestID, TraceID string
}
type ScopeEngine interface {
	DataScope(context.Context, Subject, string, Resource, ScopeContext) (DataScope, error)
}

func (Native) DataScope(ctx context.Context, s Subject, action string, r Resource, _ ScopeContext) (DataScope, error) {
	if ctx.Err() != nil || s.User.ID() == "" || s.User.ID() != s.Access.ActorID() || r.TenantID != s.Access.TenantID() {
		return DataScope{Kind: "NONE"}, ErrScope
	}
	if action != "data.read" && action != "data.write" {
		return DataScope{Kind: "NONE"}, ErrScope
	}
	for _, role := range s.Access.Roles() {
		if role == "owner" || role == "admin" || role == "member" || (role == "viewer" && action == "data.read") {
			return DataScope{Kind: "ALL"}, nil
		}
	}
	return DataScope{Kind: "NONE"}, ErrScope
}
