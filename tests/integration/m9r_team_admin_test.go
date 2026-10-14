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
