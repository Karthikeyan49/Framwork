//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

func r5cSaveTeam(t *testing.T, h http.Handler, root, token string, team tenant.Team, expected int64, want int) tenant.Team {
	t.Helper()
	w := m5Call(h, root+"/teams", token, map[string]any{"id": team.ID, "parent_id": team.ParentID, "name": team.Name, "expected_version": expected})
	if w.Code != want {
		t.Fatal("save team", team.Name, w.Code, w.Body.String())
	}
	var out tenant.Team
	if want == 200 && (json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Version != expected+1) {
		t.Fatal("team witness", w.Body.String())
	}
	return out
}
func r5cMember(t *testing.T, h http.Handler, root, token, team, user string, active bool, expected *bool, want int) {
	t.Helper()
	w := m5Call(h, root+"/teams/"+team+"/members/"+user, token, map[string]any{"active": active, "expected_active": expected})
	if w.Code != want {
		t.Fatal("team member", w.Code, w.Body.String())
	}
}

func TestM9RTeamAdministrationHierarchyMembershipAndScope(t *testing.T) {
	m8Database(t)
	f, h, base, alice, _ := m5Fixture(t)
	root := "/v1/tenants/" + f.tenantA
	viewer := f.token(t, f.viewerSubject, nil)
	bob := f.token(t, f.bobSubject, nil)
	parent, child, other := uuid(t), uuid(t), uuid(t)

	r5cSaveTeam(t, h, root, viewer, tenant.Team{ID: parent, Name: "Sales"}, 0, 403)
	r5cSaveTeam(t, h, "/v1/tenants/"+f.tenantB, alice, tenant.Team{ID: parent, Name: "Sales"}, 0, 403)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: parent, Name: " padded"}, 0, 400)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: parent, Name: "Sales", ParentID: other}, 0, 400)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: parent, Name: "Sales"}, 0, 200)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: parent, Name: "Sales"}, 0, 409)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: child, Name: "Sales East", ParentID: parent}, 0, 200)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: other, Name: "Support"}, 0, 200)
	// Reparenting a team under its own descendant would create a cycle.
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: parent, Name: "Sales", ParentID: child}, 1, 400)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: parent, Name: "Sales"}, 7, 409)
	renamed := r5cSaveTeam(t, h, root, alice, tenant.Team{ID: parent, Name: "Global Sales"}, 1, 200)
	if renamed.Version != 2 {
		t.Fatal("rename version")
	}
	// Depth: a chain of nine levels (depth 8) is valid; a tenth is rejected.
	previous := other
	for i := 0; i < 8; i++ {
		next := uuid(t)
		r5cSaveTeam(t, h, root, alice, tenant.Team{ID: next, Name: "Level", ParentID: previous}, 0, 200)
		previous = next
	}
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: uuid(t), Name: "Too deep", ParentID: previous}, 0, 400)

	// Membership: only current tenant members; CAS on the observed state.
	r5cMember(t, h, root, alice, child, f.bob, true, nil, 400)
	r5cMember(t, h, root, viewer, child, f.viewer, true, nil, 403)
	no := false
	r5cMember(t, h, root, alice, child, f.viewer, true, &no, 409)
	r5cMember(t, h, root, alice, child, f.viewer, true, nil, 200)
	r5cMember(t, h, root, alice, child, f.viewer, true, nil, 409)
	yes := true
	r5cMember(t, h, root, alice, child, f.viewer, true, &yes, 400)

	// TEAM scope follows administered ownership and membership immediately.
	record := m5Create(t, h, base, alice, "Team owned")
	access := base + "/records/" + record.ID + "/access"
	r5cChange(t, h, access, alice, recordOwner(f.alice, parent), 200)
	teams := m5Handler(t, f, scopedPolicy{mode: "team"}, audit.Postgres{}, true)
	if r5cVisible(t, teams, base, viewer, record.ID) {
		t.Fatal("child member saw parent team record")
	}
	moved := uuid(t)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: moved, Name: "Owner team", ParentID: child}, 0, 200)
	r5cChange(t, h, access, alice, recordOwnerAt(f.alice, moved, 2), 200)
	if !r5cVisible(t, teams, base, viewer, record.ID) {
		t.Fatal("hierarchy member missing descendant team record")
	}
	r5cMember(t, h, root, alice, child, f.viewer, false, &yes, 200)
	if r5cVisible(t, teams, base, viewer, record.ID) {
		t.Fatal("deactivated member retained team access")
	}

	w := m3Call(h, "GET", root+"/teams/"+child, alice, 0, nil)
	var detail struct {
		tenant.Team
		Members []tenant.TeamMember `json:"members"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Name != "Sales East" || len(detail.Members) != 1 || detail.Members[0] != (tenant.TeamMember{UserID: f.viewer, Active: false}) {
		t.Fatal("team detail", w.Code, w.Body.String())
	}
	if w = m3Call(h, "GET", root+"/teams", viewer, 0, nil); w.Code != 403 {
		t.Fatal("viewer listed teams", w.Code)
	}
	if w = m3Call(h, "GET", "/v1/tenants/"+f.tenantB+"/teams/"+child, bob, 0, nil); w.Code != 404 {
		t.Fatal("foreign tenant team disclosed", w.Code)
	}
	var changed int
	if err := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND allowed AND action IN('teams.changed','teams.membership.changed')`, f.tenantA).Scan(&changed); err != nil || changed != 15 {
		t.Fatal("team audit evidence", err, changed)
	}
	_ = policy.Version
}

func TestM9RTeamAdministrationConcurrentReparentAndPortBoundary(t *testing.T) {
	m8Database(t)
	f, h, _, alice, _ := m5Fixture(t)
	root := "/v1/tenants/" + f.tenantA
	a, b := uuid(t), uuid(t)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: a, Name: "A"}, 0, 200)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: b, Name: "B"}, 0, 200)
	// Each reparent is valid alone; together they form a cycle. The tenant
	// fence must let at most one commit.
	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i, change := range []tenant.Team{{ID: a, Name: "A", ParentID: b}, {ID: b, Name: "B", ParentID: a}} {
			wg.Add(1)
			go func(i int, change tenant.Team) {
				defer wg.Done()
				var version int64
				_ = f.operator.QueryRow(`SELECT version FROM iam.teams WHERE tenant_id=$1 AND id=$2`, f.tenantA, change.ID).Scan(&version)
				codes[i] = m5Call(h, root+"/teams", alice, map[string]any{"id": change.ID, "parent_id": change.ParentID, "name": change.Name, "expected_version": version}).Code
			}(i, change)
		}
		wg.Wait()
		var valid bool
		if err := f.operator.QueryRow(`SELECT control.team_hierarchy_valid($1)`, f.tenantA).Scan(&valid); err != nil || !valid {
			t.Fatal("concurrent reparent committed a cycle", err, codes)
		}
		for _, c := range codes {
			if c != 200 && c != 400 && c != 409 {
				t.Fatal("undefined concurrent response", codes)
			}
		}
		// Reset both to roots for the next round.
		for _, id := range []string{a, b} {
			var version int64
			_ = f.operator.QueryRow(`SELECT version FROM iam.teams WHERE tenant_id=$1 AND id=$2`, f.tenantA, id).Scan(&version)
			if w := m5Call(h, root+"/teams", alice, map[string]any{"id": id, "name": "Root", "expected_version": version}); w.Code != 200 {
				t.Fatal("reset", w.Code, w.Body.String())
			}
		}
	}
	var pe *pgconn.PgError
	if _, err := f.db.Exec(`SELECT control.save_team($1,$2,0,'Raw',NULL)`, f.tenantA, uuid(t)); !errors.As(err, &pe) || pe.Code != "42501" {
		t.Fatal("contextless app team write", err)
	}
	viewer, err := f.verifier.Verify(f.token(t, f.viewerSubject, nil))
	if err != nil {
		t.Fatal(err)
	}
	err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := m3Scope(f, ctx, q, viewer, f.tenantA); e != nil {
			return e
		}
		_, e := q.ExecContext(ctx, `SELECT control.save_team($1,$2,0,'Raw',NULL)`, f.tenantA, uuid(t))
		return e
	})
	if !errors.As(err, &pe) || pe.Code != "42501" {
		t.Fatal("viewer actor reached team port", err)
	}
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if _, err = worker.Exec(`SELECT control.save_team($1,$2,0,'Raw',NULL)`, f.tenantA, uuid(t)); !errors.As(err, &pe) || pe.Code != "42501" {
		t.Fatal("worker reached team port", err)
	}
	var teams int
	if err = f.operator.QueryRow(`SELECT count(*) FROM iam.teams WHERE tenant_id=$1`, f.tenantA).Scan(&teams); err != nil || teams != 2 {
		t.Fatal("raw team write leaked", err, teams)
	}
}

func TestM9RTeamAdministrationBudgetsCustomRolesAndRouteIsolation(t *testing.T) {
	m8Database(t)
	f, _, _, alice, app := m5Fixture(t)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	if err := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Teams", f.bob, "member", true); err != nil {
		t.Fatal(err)
	}
	bob := f.token(t, f.bobSubject, nil)
	team := uuid(t)
	r5cSaveTeam(t, h, root, bob, tenant.Team{ID: team, Name: "Ops"}, 0, 403)
	r5cSaveTeam(t, h, root, alice, tenant.Team{ID: team, Name: "Ops"}, 0, 200)

	// A role literally named "teams" stays addressable through the role route.
	named := policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "teams", Grants: []policy.Grant{{Effect: "ALLOW", Actions: []string{"tenant.access"}, Resource: policy.Selector{AppID: app, EntityID: "*", FieldID: "*"}}}}
	r5Publish(t, h, root+"/roles", alice, 0, named, true, uuid(t))
	w := m3Call(h, "GET", root+"/roles/teams", alice, 0, nil)
	var role policy.RoleVersion
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &role) != nil || role.Name != "teams" {
		t.Fatal("role route captured by team mapping", w.Code, w.Body.String())
	}

	// Delegated custom roles: read-only reader and a manager.
	grant := func(name string, actions ...string) policy.Definition {
		return policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: name, Grants: []policy.Grant{{Effect: "ALLOW", Actions: actions, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}}}}
	}
	r5Publish(t, h, root+"/roles", alice, 0, grant("team_reader", "teams.read"), true, uuid(t))
	r5Assign(t, h, root, alice, f.bob, "team_reader", 200)
	if w = m3Call(h, "GET", root+"/teams/"+team, bob, 0, nil); w.Code != 200 {
		t.Fatal("custom team reader", w.Code)
	}
	r5cSaveTeam(t, h, root, bob, tenant.Team{ID: uuid(t), Name: "Denied"}, 0, 403)
	r5Publish(t, h, root+"/roles", alice, 0, grant("people_ops", "teams.read", "teams.manage"), true, uuid(t))
	r5Assign(t, h, root, alice, f.bob, "people_ops", 200)
	r5cMember(t, h, root, bob, team, f.viewer, true, nil, 200)

	// 64-team closure: a member may resolve exactly 64 teams, not 65.
	for i := 1; i < 64; i++ {
		r5cSaveTeam(t, h, root, bob, tenant.Team{ID: uuid(t), Name: "Child", ParentID: team}, 0, 200)
	}
	r5cSaveTeam(t, h, root, bob, tenant.Team{ID: uuid(t), Name: "Overflow", ParentID: team}, 0, 400)
	other := uuid(t)
	r5cSaveTeam(t, h, root, bob, tenant.Team{ID: other, Name: "Other"}, 0, 200)
	r5cMember(t, h, root, bob, other, f.viewer, true, nil, 400)

	// Retained membership rows are bounded so the team stays listable.
	if _, err := f.operator.Exec(`INSERT INTO iam.team_memberships(tenant_id,team_id,user_id,active) SELECT $1,$2,gen_random_uuid(),false FROM generate_series(1,1000)`, f.tenantA, other); err != nil {
		t.Fatal(err)
	}
	r5cMember(t, h, root, bob, other, f.alice, true, nil, 400)
	if w = m3Call(h, "GET", root+"/teams/"+other, bob, 0, nil); w.Code != 200 {
		t.Fatal("bounded team listing", w.Code)
	}
	if _, err := f.operator.Exec(`INSERT INTO iam.team_memberships(tenant_id,team_id,user_id,active) VALUES($1,$2,gen_random_uuid(),false)`, f.tenantA, other); err != nil {
		t.Fatal(err)
	}
	if w = m3Call(h, "GET", root+"/teams/"+other, bob, 0, nil); w.Code != 409 {
		t.Fatal("operator over-budget team not typed", w.Code)
	}
	var version int64
	if err := f.operator.QueryRow(`SELECT (state->>'team_version')::bigint FROM audit.entries WHERE tenant_id=$1 AND action='teams.changed' AND state->>'access_team_id'=$2`, f.tenantA, other).Scan(&version); err != nil || version != 1 {
		t.Fatal("team version evidence", err, version)
	}
}
