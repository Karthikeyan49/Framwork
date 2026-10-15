//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
)

func r5cAccess(t *testing.T, h http.Handler, path, token string, want int) data.Access {
	t.Helper()
	w := m3Call(h, "GET", path, token, 0, nil)
	if w.Code != want {
		t.Fatal("record access read", w.Code, w.Body.String())
	}
	var v data.Access
	if want == 200 && json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal("access contract")
	}
	return v
}
func r5cChange(t *testing.T, h http.Handler, path, token string, c data.AccessCommand, want int) data.Access {
	t.Helper()
	w := m5Call(h, path, token, c)
	if w.Code != want {
		t.Fatal("record access change", c.Operation, w.Code, w.Body.String())
	}
	var v data.Access
	if want == 200 && (json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Version != c.ExpectedVersion+1) {
		t.Fatal("access witness", w.Body.String())
	}
	return v
}
func r5cVisible(t *testing.T, h http.Handler, base, token, id string) bool {
	t.Helper()
	page := m5Page(t, m5Call(h, base+"/query", token, data.Query{RecordID: id, Projection: []string{"field.contact.name"}, Limit: 1}))
	return len(page.Records) == 1
}

func TestM9RRecordAccessAdministrationLegacyPath(t *testing.T) {
	m8Database(t)
	f, h, base, alice, _ := m5Fixture(t)
	record := m5Create(t, h, base, alice, "Shared")
	hidden := m5Create(t, h, base, alice, "Unrelated")
	path := base + "/records/" + record.ID + "/access"
	viewer := f.token(t, f.viewerSubject, nil)
	shares := m5Handler(t, f, scopedPolicy{mode: "share"}, audit.Postgres{}, true)
	team := uuid(t)
	foreignTeam := uuid(t)
	if _, err := f.operator.Exec(`INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'Sales'),($3,$4,'Foreign')`, f.tenantA, team, f.tenantB, foreignTeam); err != nil {
		t.Fatal(err)
	}

	v := r5cAccess(t, h, path, alice, 200)
	if v.OwnerID != f.alice || v.Version != 1 || len(v.Shares) != 0 {
		t.Fatal("initial access", v)
	}
	// The bootstrap viewer can neither read share recipients nor administer.
	r5cAccess(t, h, path, viewer, 403)
	yes, no := true, false
	r5cChange(t, h, path, viewer, data.AccessCommand{Operation: "share", ExpectedVersion: 1, UserID: f.viewer, CanWrite: &no}, 403)
	if r5cVisible(t, shares, base, viewer, record.ID) {
		t.Fatal("share-scoped viewer saw unshared record")
	}
	// Malformed and non-member recipients reject before any effect.
	r5cChange(t, h, path, alice, data.AccessCommand{Operation: "share", ExpectedVersion: 1, UserID: f.viewer}, 400)
	r5cChange(t, h, path, alice, data.AccessCommand{Operation: "share", ExpectedVersion: 1, UserID: f.bob, CanWrite: &no}, 400)
	r5cChange(t, h, path, alice, data.AccessCommand{Operation: "owner", ExpectedVersion: 1, UserID: f.viewer, TeamID: foreignTeam}, 400)
	r5cChange(t, h, path, alice, data.AccessCommand{Operation: "owner", ExpectedVersion: 1, UserID: f.alice}, 400)
	r5cChange(t, h, path, alice, data.AccessCommand{Operation: "share", ExpectedVersion: 7, UserID: f.viewer, CanWrite: &no}, 409)
	if v = r5cAccess(t, h, path, alice, 200); v.Version != 1 || len(v.Shares) != 0 {
		t.Fatal("rejected change left effects", v)
	}

	v = r5cChange(t, h, path, alice, data.AccessCommand{Operation: "share", ExpectedVersion: 1, UserID: f.viewer, CanWrite: &no}, 200)
	if !r5cVisible(t, shares, base, viewer, record.ID) || r5cVisible(t, shares, base, viewer, hidden.ID) {
		t.Fatal("explicit share not applied exactly")
	}
	// A business edit from the stale witness conflicts after an access change.
	if w := m5Call(h, base+"/commands", alice, data.Command{Operation: "update", ID: record.ID, ExpectedVersion: 1, Values: m5Values("Stale")}); w.Code != 409 {
		t.Fatal("stale business write accepted", w.Code)
	}
	v = r5cChange(t, h, path, alice, data.AccessCommand{Operation: "share", ExpectedVersion: 2, UserID: f.viewer, CanWrite: &yes}, 200)
	if got := r5cAccess(t, h, path, alice, 200); got.Version != 3 || len(got.Shares) != 1 || got.Shares[0] != (data.Share{UserID: f.viewer, CanWrite: true}) {
		t.Fatal("share upgrade", got)
	}
	r5cChange(t, h, path, alice, data.AccessCommand{Operation: "unshare", ExpectedVersion: 3, UserID: f.viewer}, 200)
	if r5cVisible(t, shares, base, viewer, record.ID) {
		t.Fatal("revoked share still visible")
	}
	r5cChange(t, h, path, alice, data.AccessCommand{Operation: "unshare", ExpectedVersion: 4, UserID: f.viewer}, 409)

	// Owner/team transfer is visible to OWNER/TEAM scopes and removes the old owner's OWNER access.
	v = r5cChange(t, h, path, alice, data.AccessCommand{Operation: "owner", ExpectedVersion: 4, UserID: f.viewer, TeamID: team}, 200)
	if v.OwnerID != f.viewer || v.TeamID != team {
		t.Fatal("owner witness", v)
	}
	owners := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	if r5cVisible(t, owners, base, alice, record.ID) || !r5cVisible(t, owners, base, viewer, record.ID) {
		t.Fatal("owner scope did not follow transfer")
	}
	var owner, recordTeam string
	if err := f.operator.QueryRow(`SELECT owner_user_id::text,owner_team_id::text FROM runtime.records WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID).Scan(&owner, &recordTeam); err != nil || owner != f.viewer || recordTeam != team {
		t.Fatal("stored owner", err, owner, recordTeam)
	}
	// Tenant B owner cannot address tenant A records.
	bob := f.token(t, f.bobSubject, nil)
	r5cChange(t, h, path, bob, data.AccessCommand{Operation: "share", ExpectedVersion: 5, UserID: f.bob, CanWrite: &no}, 403)

	// Share budget is enforced by the SQL port, not only by the client.
	if _, err := f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,gen_random_uuid() FROM runtime.records,generate_series(1,64) WHERE tenant_id=$1 AND id=$2`, f.tenantA, hidden.ID); err != nil {
		t.Fatal(err)
	}
	r5cChange(t, h, base+"/records/"+hidden.ID+"/access", alice, data.AccessCommand{Operation: "share", ExpectedVersion: 1, UserID: f.viewer, CanWrite: &no}, 400)
	// Excess legacy shares remain listable (bounded) so they can be revoked.
	if _, err := f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,gen_random_uuid() FROM runtime.records,generate_series(1,200) WHERE tenant_id=$1 AND id=$2`, f.tenantA, hidden.ID); err != nil {
		t.Fatal(err)
	}
	if listed := r5cAccess(t, h, base+"/records/"+hidden.ID+"/access", alice, 200); len(listed.Shares) != 256 || !listed.Truncated {
		t.Fatal("bounded share listing", len(listed.Shares), listed.Truncated)
	}
	excess := r5cAccess(t, h, base+"/records/"+hidden.ID+"/access", alice, 200).Shares[0].UserID
	r5cChange(t, h, base+"/records/"+hidden.ID+"/access", alice, data.AccessCommand{Operation: "unshare", ExpectedVersion: 1, UserID: excess}, 200)

	var changed, rejected int
	if err := f.operator.QueryRow(`SELECT count(*) FILTER(WHERE action='records.access.changed' AND allowed AND state->>'record_id'=$2),count(*) FILTER(WHERE NOT allowed AND state->>'access_operation' IS NOT NULL) FROM audit.entries WHERE tenant_id=$1`, f.tenantA, record.ID).Scan(&changed, &rejected); err != nil || changed != 4 || rejected < 5 {
		t.Fatal("access audit evidence", err, changed, rejected)
	}
}

func TestM9RRecordAccessSQLPortBoundary(t *testing.T) {
	m8Database(t)
	f, h, base, alice, app := m5Fixture(t)
	record := m5Create(t, h, base, alice, "Port")
	var hash string
	if err := f.operator.QueryRow(`SELECT ir_hash FROM runtime.records WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	call := `SELECT runtime.administer_record_access($1,$2,1,$3,'entity.contact',$4,1,'share',$5,NULL,false)`
	// Without trusted tenant context the app role is rejected inside the port.
	var pe *pgconn.PgError
	if _, err := f.db.Exec(call, f.tenantA, app, hash, record.ID, f.viewer); !errors.As(err, &pe) || pe.Code != "42501" {
		t.Fatal("contextless app call", err)
	}
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if _, err = worker.Exec(call, f.tenantA, app, hash, record.ID, f.viewer); !errors.As(err, &pe) || pe.Code != "42501" {
		t.Fatal("worker reached record access port", err)
	}
	// Even with a trusted live tenant context, a viewer actor cannot use the port.
	viewer, err := f.verifier.Verify(f.token(t, f.viewerSubject, nil))
	if err != nil {
		t.Fatal(err)
	}
	err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := m3Scope(f, ctx, q, viewer, f.tenantA); e != nil {
			return e
		}
		_, e := q.ExecContext(ctx, call, f.tenantA, app, hash, record.ID, f.viewer)
		return e
	})
	if !errors.As(err, &pe) || pe.Code != "42501" {
		t.Fatal("viewer actor reached record access port", err)
	}
	var shares int
	if err = f.operator.QueryRow(`SELECT count(*) FROM runtime.record_shares WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID).Scan(&shares); err != nil || shares != 0 {
		t.Fatal("port boundary leaked effect", err, shares)
	}
}

func TestM9RRecordAccessCanonicalDatasetGeneration(t *testing.T) {
	m8Database(t)
	f, h, base, alice, app := m5Fixture(t)
	record := m5Create(t, h, base, alice, "Canonical")
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	d, path := shadowAdopt(t, h, alice, root)
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	target := shadowSource(t, h, alice, f.tenantA, app, 2, raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	ready := shadowReady(t, h, alice, path, shadowPrepare(t, h, alice, path, 1, target, compiler.EvolutionIntent{}))
	shadowStep(t, h, alice, path, ready, "activate")
	current := path + "/bindings/2/entities/entity.contact"
	retired := path + "/bindings/1/entities/entity.contact"
	access := current + "/records/" + record.ID + "/access"
	v := r5cAccess(t, h, access, alice, 200)
	no := false
	// Retired bindings remain readable but cannot accept access changes.
	r5cChange(t, h, retired+"/records/"+record.ID+"/access", alice, data.AccessCommand{Operation: "share", ExpectedVersion: v.Version, UserID: f.viewer, CanWrite: &no}, 409)
	v = r5cChange(t, h, access, alice, data.AccessCommand{Operation: "share", ExpectedVersion: v.Version, UserID: f.viewer, CanWrite: &no}, 200)
	viewer := f.token(t, f.viewerSubject, nil)
	shares := m5Handler(t, f, scopedPolicy{mode: "share"}, audit.Postgres{}, true)
	if !r5cVisible(t, shares, current, viewer, record.ID) || !r5cVisible(t, shares, retired, viewer, record.ID) {
		t.Fatal("canonical share not live across generations")
	}
	var identityShares int
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.identity_shares WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3 AND user_id=$4`, f.tenantA, d.ID, record.ID, f.viewer).Scan(&identityShares); err != nil || identityShares != 1 {
		t.Fatal("canonical identity share", err, identityShares)
	}
	v = r5cChange(t, h, access, alice, data.AccessCommand{Operation: "owner", ExpectedVersion: v.Version, UserID: f.viewer}, 200)
	var owner string
	if err := f.operator.QueryRow(`SELECT owner_user_id::text FROM runtime.record_identity WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3`, f.tenantA, d.ID, record.ID).Scan(&owner); err != nil || owner != f.viewer {
		t.Fatal("canonical identity owner", err, owner)
	}
	if got := r5cAccess(t, h, access, alice, 200); got.OwnerID != f.viewer || got.Version != v.Version || len(got.Shares) != 1 {
		t.Fatal("canonical access view", got)
	}
	r5cChange(t, h, access, alice, data.AccessCommand{Operation: "unshare", ExpectedVersion: v.Version, UserID: f.viewer}, 200)
	if r5cVisible(t, shares, retired, viewer, record.ID) {
		t.Fatal("revoked canonical share visible through retired generation")
	}
	// The decision engine version advances with each milestone that adds actions
	// (m9r, m10, m11 ...); the invariant is a stable, non-empty native label.
	if !regexp.MustCompile(`^m[0-9]+r?-native-v[0-9]+$`).MatchString(policy.Version) {
		t.Fatal("policy version", policy.Version)
	}
}

func recordOwner(user, team string) data.AccessCommand {
	return recordOwnerAt(user, team, 1)
}
func recordOwnerAt(user, team string, version int64) data.AccessCommand {
	return data.AccessCommand{Operation: "owner", ExpectedVersion: version, UserID: user, TeamID: team}
}
