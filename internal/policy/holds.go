package policy

import (
	"context"
	"errors"
	"slices"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// NativeHolds answers, for the built-in roles, whether a member who holds role is granted
// action on resource: the question Native.Decide answers for the member's own request, asked
// about a member who is not the caller (a recorded approver at activation, M13R-03).
func NativeHolds(tenantID, member, role, action string, resource Resource) bool {
	return tenantID != "" && member != "" && (permitsSelf(member, action, resource) || permits(member, tenantID, []string{role}, action, resource))
}

// Holds answers the same question for the current stored head of role in the tenant of a:
// the bootstrap roles by the native rules and a custom role by its grants, an explicit DENY
// winning over any ALLOW, exactly as Decide applies them to a request by a member who holds
// it. A role that is missing, inactive or unreadable grants nothing. The answer is about the
// role, not about a request: the caller names member from a source it trusts and passes the
// resource it would pass to Decide.
func (s RoleStore) Holds(ctx context.Context, q postgres.Executor, a tenant.Access, member, role, action string, resource Resource) (bool, error) {
	if !tenant.ValidID(member) || a.TenantID() == "" || resource.TenantID != a.TenantID() {
		return false, nil
	}
	v, err := s.Current(ctx, q, a, role)
	if errors.Is(err, ErrScope) || errors.Is(err, ErrDefinition) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	d, err := compileVersion(v)
	if err != nil {
		return false, nil
	}
	return d.holds(a.TenantID(), member, action, resource), nil
}

// holds is the decision of a compiled definition for a member of the tenant, without a
// session. It must stay equal to Decide for a subject that passes validSubject
// (TestM13R03HoldsMatchesDecide compares them through the real routes).
func (d Defined) holds(tenantID, member, action string, r Resource) bool {
	if d.legacy {
		return NativeHolds(tenantID, member, d.definition.Name, action, r)
	}
	if tenantID == "" || member == "" || r.TenantID != tenantID || !knownAction(action) || !validResource(action, r, member) {
		return false
	}
	allow := false
	for _, g := range d.definition.Grants {
		if !selectorMatches(g.Resource, r) || !slices.Contains(g.Actions, action) {
			continue
		}
		if g.Effect == "DENY" {
			return false
		}
		allow = true
	}
	return allow
}
