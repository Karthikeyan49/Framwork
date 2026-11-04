package risk

import (
	"fmt"
	"strings"
)

// Separation is the approval requirement derived from the risk level: how many
// distinct approvers are needed and which parties may not be among them.
// Enforcement belongs to the approval service; CheckSeparation is its pure check.
type Separation struct {
	Approvals int      `json:"approvals"`
	Excluded  []string `json:"excluded"`
}

// Parties identifies the actors bound to a candidate. The ids come from the
// candidate record, never from the approver.
type Parties struct {
	Requester        string
	AcceptanceAuthor string
	SandboxFreezer   string
}

func (p Parties) id(role string) string {
	switch role {
	case RoleRequester:
		return p.Requester
	case RoleAcceptanceAuthor:
		return p.AcceptanceAuthor
	case RoleSandboxFreezer:
		return p.SandboxFreezer
	}
	return ""
}

// actorKey validates an actor id (1..128 visible ASCII bytes) and folds its
// case, so ids that differ only by case count as the same actor, which can only
// make the check stricter.
func actorKey(id string) (string, bool) {
	if !token(id, 128, func(c byte) bool { return c > 0x20 && c < 0x7f }) {
		return "", false
	}
	return strings.ToLower(id), true
}

func separationError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrSeparation, fmt.Sprintf(format, args...))
}

// CheckSeparation reports nil only if approvers satisfy sep for the candidate's
// parties: at least sep.Approvals distinct approvers (duplicates count once),
// none of them an excluded party. It fails closed: a requirement of fewer than
// one approval or an unknown excluded role is invalid, an excluded role whose
// party id is missing cannot be enforced, and an empty or malformed approver id
// is rejected rather than ignored.
func CheckSeparation(sep Separation, parties Parties, approvers []string) error {
	if sep.Approvals < 1 || sep.Approvals > maxApprovals || len(sep.Excluded) > len(roles) {
		return separationError("invalid requirement")
	}
	if len(approvers) > MaxEntries {
		return separationError("too many approvers")
	}
	excluded := map[string]string{}
	for _, role := range sep.Excluded {
		if !in(roles, role) {
			return separationError("unknown excluded role")
		}
		key, ok := actorKey(parties.id(role))
		if !ok {
			return separationError("%s is not identified", role)
		}
		excluded[key] = role
	}
	distinct := map[string]bool{}
	for _, a := range approvers {
		key, ok := actorKey(a)
		if !ok {
			return separationError("invalid approver id")
		}
		if role, bad := excluded[key]; bad {
			return separationError("approver is the %s", role)
		}
		distinct[key] = true
	}
	if len(distinct) < sep.Approvals {
		return separationError("%d distinct approvals required, have %d", sep.Approvals, len(distinct))
	}
	return nil
}
