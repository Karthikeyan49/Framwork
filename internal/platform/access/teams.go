package access

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

var teamPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/teams(/[0-9a-f-]{36}/members/[0-9a-f-]{36})?$`)

// Every rejection rolls back the transaction before a fresh denial audit.
// Team changes are never retried automatically; after an unknown commit the
// caller reloads the team and compares its version or membership state.
func teamReply(value any, err error) (reply, error) {
	if err == nil {
		return reply{status: 200, body: value}, nil
	}
	status, code := 0, ""
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, tenant.ErrTeamInvalid):
		status, code = 400, "invalid_team"
	case errors.As(err, &pg):
		switch pg.Code {
		case "22023", "22P02", "23514":
			status, code = 400, "invalid_team"
		case "42501":
			status, code = 403, "forbidden"
		case "55000", "23503", "23505", "40001", "40P01", "55P03":
			status, code = 409, "team_conflict"
		}
	}
	if status != 0 {
		return reply{}, &requestRejection{response: denied(status, code, "Team request rejected; reload the team before retrying")}
	}
	return reply{}, err
}

func (a API) team(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, evidence *audit.Entry) (reply, error) {
	store := tenant.Store{}
	id := r.PathValue("team")
	if id != "" && !tenant.ValidID(id) {
		return teamReply(nil, tenant.ErrTeamInvalid)
	}
	evidence.State.AccessTeamID = id
	if r.Method == http.MethodGet {
		if id == "" {
			teams, err := store.ListTeams(ctx, q, access)
			return teamReply(struct {
				Teams []tenant.Team `json:"teams"`
			}{teams}, err)
		}
		teams, err := store.ListTeams(ctx, q, access)
		if err != nil {
			return teamReply(nil, err)
		}
		for _, t := range teams {
			if t.ID == id {
				members, err := store.TeamMembers(ctx, q, access, id)
				return teamReply(struct {
					tenant.Team
					Members []tenant.TeamMember `json:"members"`
				}{t, members}, err)
			}
		}
		return reply{}, &requestRejection{response: denied(404, "team_not_found", "Team not found")}
	}
	if member := r.PathValue("team_member"); member != "" {
		var body struct {
			Active         *bool `json:"active"`
			ExpectedActive *bool `json:"expected_active"`
		}
		if readDataCommand(r, &body) != nil || body.Active == nil || !tenant.ValidID(member) {
			return teamReply(nil, tenant.ErrTeamInvalid)
		}
		evidence.State.PolicySubject = member
		err := store.SetTeamMember(ctx, q, access, id, member, *body.Active, body.ExpectedActive)
		if err == nil {
			evidence.Action = "teams.membership.changed"
		}
		return teamReply(tenant.TeamMember{UserID: member, Active: *body.Active}, err)
	}
	var body struct {
		ID              string `json:"id"`
		ParentID        string `json:"parent_id"`
		Name            string `json:"name"`
		ExpectedVersion *int64 `json:"expected_version"`
	}
	if readDataCommand(r, &body) != nil || body.ExpectedVersion == nil {
		return teamReply(nil, tenant.ErrTeamInvalid)
	}
	if tenant.ValidID(body.ID) {
		evidence.State.AccessTeamID = body.ID
	}
	saved, err := store.SaveTeam(ctx, q, access, tenant.Team{ID: body.ID, ParentID: body.ParentID, Name: body.Name}, *body.ExpectedVersion)
	if err == nil {
		evidence.Action = "teams.changed"
		evidence.State.PolicyRevision = saved.Version
	}
	return teamReply(saved, err)
}
