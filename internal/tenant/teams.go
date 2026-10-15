package tenant

import (
	"context"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// Teams resolves direct memberships plus descendants. No UI-supplied team IDs.
func (Store) Teams(ctx context.Context, q postgres.Executor, a Access) ([]string, error) {
	if a.ActorID() == "" || a.TenantID() == "" {
		return nil, ErrDenied
	}
	rows, e := q.QueryContext(ctx, `WITH RECURSIVE allowed(id,path,depth,cycle) AS (
 SELECT team_id,ARRAY[team_id],0,false FROM iam.team_memberships WHERE tenant_id=$1 AND user_id=$2 AND active
 UNION ALL SELECT t.id,a.path||t.id,a.depth+1,t.id=ANY(a.path) FROM iam.teams t JOIN allowed a ON t.parent_id=a.id WHERE t.tenant_id=$1 AND a.depth<9 AND NOT a.cycle) SELECT DISTINCT id::text,bool_or(depth>8 OR cycle) OVER() AS invalid FROM allowed ORDER BY id LIMIT 65`, a.TenantID(), a.ActorID())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		var invalid bool
		if e = rows.Scan(&id, &invalid); e != nil {
			return nil, e
		}
		if invalid {
			return nil, errors.New("team hierarchy depth/cycle budget exceeded")
		}
		out = append(out, id)
	}
	if len(out) > 64 {
		return nil, errors.New("team hierarchy budget exceeded")
	}
	return out, rows.Err()
}

var ErrTeamInvalid = errors.New("invalid team request")

// ErrTeamBudget reports operator-created state beyond the administered budgets.
var ErrTeamBudget = errors.New("team budget exceeded")

// MaxTeamMemberships bounds one team's retained (active and inactive) rows.
const MaxTeamMemberships = 1000

const MaxTeams = 256

type Team struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Name     string `json:"name"`
	Version  int64  `json:"version"`
}
type TeamMember struct {
	UserID string `json:"user_id"`
	Active bool   `json:"active"`
}

// ListTeams returns the tenant's bounded team hierarchy for administrators.
func (Store) ListTeams(ctx context.Context, q postgres.Executor, a Access) ([]Team, error) {
	if a.ActorID() == "" || a.TenantID() == "" {
		return nil, ErrDenied
	}
	rows, err := q.QueryContext(ctx, `SELECT id::text,COALESCE(parent_id::text,''),name,version FROM iam.teams WHERE tenant_id=$1 ORDER BY id LIMIT $2`, a.TenantID(), MaxTeams+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var t Team
		if err = rows.Scan(&t.ID, &t.ParentID, &t.Name, &t.Version); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if len(out) > MaxTeams {
		return nil, ErrTeamBudget
	}
	return out, rows.Err()
}

// TeamMembers lists one team's direct memberships through the tenant-scoped port.
func (Store) TeamMembers(ctx context.Context, q postgres.Executor, a Access, team string) ([]TeamMember, error) {
	if a.ActorID() == "" || a.TenantID() == "" {
		return nil, ErrDenied
	}
	if !ValidID(team) {
		return nil, ErrTeamInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT user_id::text,active FROM control.team_members($1,$2)`, a.TenantID(), team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamMember{}
	for rows.Next() {
		var m TeamMember
		if err = rows.Scan(&m.UserID, &m.Active); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if len(out) > MaxTeamMemberships {
		return nil, ErrTeamBudget
	}
	return out, rows.Err()
}

// SaveTeam creates (expected 0, caller-chosen ID for reconciliation) or
// renames/reparents a team at its exact version. SQL validates the hierarchy.
func (Store) SaveTeam(ctx context.Context, q postgres.Executor, a Access, t Team, expected int64) (Team, error) {
	if a.ActorID() == "" || a.TenantID() == "" {
		return Team{}, ErrDenied
	}
	if !ValidID(t.ID) || t.ParentID != "" && !ValidID(t.ParentID) || expected < 0 || expected >= 1e9 || len(t.Name) < 1 || len(t.Name) > 480 {
		return Team{}, ErrTeamInvalid
	}
	var parent any
	if t.ParentID != "" {
		parent = t.ParentID
	}
	err := q.QueryRowContext(ctx, `SELECT control.save_team($1,$2,$3,$4,$5::uuid)`, a.TenantID(), t.ID, expected, t.Name, parent).Scan(&t.Version)
	return t, err
}

// SetTeamMember activates or deactivates one membership. expected is the
// current active state, or nil when the caller observed no membership row.
func (Store) SetTeamMember(ctx context.Context, q postgres.Executor, a Access, team, user string, active bool, expected *bool) error {
	if a.ActorID() == "" || a.TenantID() == "" {
		return ErrDenied
	}
	if !ValidID(team) || !ValidID(user) {
		return ErrTeamInvalid
	}
	var witness any
	if expected != nil {
		witness = *expected
	}
	_, err := q.ExecContext(ctx, `SELECT control.set_team_member($1,$2,$3,$4,$5::boolean)`, a.TenantID(), team, user, active, witness)
	return err
}
